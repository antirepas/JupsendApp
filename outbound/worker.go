package outbound

import (
	"log"
	"sync"
	"time"

	"emailtracker.com/model"
	"github.com/google/uuid"
)

var (
	claimMu     sync.Mutex
	workerWake  = make(chan struct{}, 1)
	userMutexes sync.Map
)

type claimedJob struct {
	job     model.SendJob
	account model.SMTPAccount
}

// NotifyWorker wakes the send worker immediately (coalesced).
func NotifyWorker() {
	select {
	case workerWake <- struct{}{}:
	default:
	}
}

func StartWorker() {
	LoadConfig()
	go func() {
		runWorkerBatch()
		ticker := time.NewTicker(WorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
			case <-workerWake:
			}
			runWorkerBatch()
		}
	}()
}

func runWorkerBatch() {
	nudgeDeferredJobsWithCapacity()

	jobs := claimPendingJobs()
	if len(jobs) == 0 {
		return
	}

	sem := make(chan struct{}, MaxConcurrent)
	var wg sync.WaitGroup
	for _, item := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(item claimedJob) {
			defer wg.Done()
			defer func() { <-sem }()
			runClaimedJob(item)
		}(item)
	}
	wg.Wait()
}

// nudgeDeferredJobsWithCapacity pulls jobs scheduled far ahead (e.g. "retry tomorrow")
// back into the ready queue when their sticky mailbox can send again.
func nudgeDeferredJobsWithCapacity() {
	ids, err := model.DeferredPendingSendJobIDs(time.Now().Add(2*time.Minute), 40)
	if err != nil || len(ids) == 0 {
		return
	}
	for _, jobID := range ids {
		job, err := model.GetSendJob(jobID)
		if err != nil || job.Status != "pending" {
			continue
		}
		account, err := ResolveAccountForJob(job)
		if err != nil {
			continue
		}
		if !AccountCanSendNowForJob(account, job) {
			continue
		}
		_ = model.RescheduleSendJob(job.ID, time.Now(), "mailbox has capacity again")
	}
}

func runClaimedJob(item claimedJob) {
	mu := userMutex(item.job.UserID)
	mu.Lock()
	defer mu.Unlock()

	account, err := ResolveAccountForJob(item.job)
	if err != nil {
		log.Printf("outbound job %d: %v", item.job.ID, err)
		failJobConfiguration(item.job, err)
		return
	}
	if account.ID > 0 && account.ID != item.job.SMTPAccountID {
		_ = model.RepinSendJobSMTPAccount(item.job.ID, account.ID)
		item.job.SMTPAccountID = account.ID
		if item.job.EmailSendID > 0 {
			_ = model.RepinEmailSendSMTPAccount(item.job.EmailSendID, account.ID)
		}
	}

	job, err := model.GetSendJob(item.job.ID)
	if err != nil || job.Status != "processing" {
		return
	}

	if !AccountCanSendNowForJob(account, job) {
		delay := rateLimitDelayForJob(account, job)
		if delay < time.Second {
			delay = time.Second
		}
		_ = model.RescheduleSendJob(job.ID, time.Now().Add(delay), rateLimitReason(account, job))
		return
	}

	if job.CampaignID > 0 && model.CampaignIsStopped(job.CampaignID) {
		_ = model.FailSendJob(job.ID, "cancelled: campaign stopped", "failed")
		if job.EmailSendID > 0 {
			_ = model.MarkEmailSendFailed(job.EmailSendID)
		}
		return
	}

	if err := executeJob(job, account); err != nil {
		log.Printf("outbound job %d failed: %v", job.ID, err)
		handleJobFailure(job, account, err)
	}
}

func userMutex(userID int64) *sync.Mutex {
	if v, ok := userMutexes.Load(userID); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := userMutexes.LoadOrStore(userID, mu)
	return actual.(*sync.Mutex)
}

func claimPendingJobs() []claimedJob {
	claimMu.Lock()
	defer claimMu.Unlock()

	_ = model.ReleaseStaleJobsReconciled()

	limit := BatchSize * 4
	if limit < MaxConcurrent {
		limit = MaxConcurrent * 2
	}
	ids, err := model.PendingSendJobIDs(limit)
	if err != nil || len(ids) == 0 {
		return nil
	}

	seenUsers := make(map[int64]bool)
	var claimed []claimedJob
	for _, jobID := range ids {
		if len(claimed) >= MaxConcurrent {
			break
		}

		job, err := model.GetSendJob(jobID)
		if err != nil || job.Status != "pending" {
			continue
		}
		if job.UserID == 0 {
			continue
		}
		if seenUsers[job.UserID] {
			continue
		}

		account, err := ResolveAccountForJob(job)
		if err != nil {
			log.Printf("outbound job %d: %v", job.ID, err)
			failJobConfiguration(job, err)
			continue
		}

		if !AccountCanSendNowForJob(account, job) {
			delay := rateLimitDelayForJob(account, job)
			if delay < time.Second {
				delay = time.Second
			}
			_ = model.RescheduleSendJob(job.ID, time.Now().Add(delay), rateLimitReason(account, job))
			continue
		}

		lockToken := uuid.NewString()
		ok, err := model.ClaimSendJob(job.ID, lockToken, LockDuration)
		if err != nil || !ok {
			continue
		}

		job, err = model.GetSendJob(jobID)
		if err != nil {
			continue
		}

		account, err = ResolveAccountForJob(job)
		if err != nil {
			failJobConfiguration(job, err)
			continue
		}
		if account.ID > 0 && account.ID != job.SMTPAccountID {
			_ = model.RepinSendJobSMTPAccount(job.ID, account.ID)
			job.SMTPAccountID = account.ID
			if job.EmailSendID > 0 {
				_ = model.RepinEmailSendSMTPAccount(job.EmailSendID, account.ID)
			}
		}

		seenUsers[job.UserID] = true
		claimed = append(claimed, claimedJob{job: job, account: account})
	}
	return claimed
}

// filterJobIDsOnePerUser keeps the first job ID for each user_id in candidate order.
func filterJobIDsOnePerUser(ids []int64, lookup func(int64) (int64, error)) []int64 {
	seen := make(map[int64]bool)
	var out []int64
	for _, id := range ids {
		userID, err := lookup(id)
		if err != nil || userID == 0 {
			continue
		}
		if seen[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, id)
	}
	return out
}
