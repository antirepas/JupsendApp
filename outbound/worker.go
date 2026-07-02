package outbound

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"emailtracker.com/model"
	"github.com/google/uuid"
)

var (
	workerMu    sync.Mutex
	workerWake  = make(chan struct{}, 1)
	workerBusy  int32
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
	if !atomic.CompareAndSwapInt32(&workerBusy, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&workerBusy, 0)

	jobs := claimPendingJobs()
	for _, item := range jobs {
		if err := executeJob(item.job, item.account); err != nil {
			log.Printf("outbound job %d failed: %v", item.job.ID, err)
			handleJobFailure(item.job, err)
		}
	}
}

func claimPendingJobs() []claimedJob {
	workerMu.Lock()
	defer workerMu.Unlock()

	_ = model.ReleaseStaleJobs()

	ids, err := model.PendingSendJobIDs(BatchSize)
	if err != nil || len(ids) == 0 {
		return nil
	}

	var claimed []claimedJob
	for _, jobID := range ids {
		job, err := model.GetSendJob(jobID)
		if err != nil || job.Status != "pending" {
			continue
		}

		account, err := resolveSendAccount(job.UserID)
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
			_ = model.RescheduleSendJob(job.ID, time.Now().Add(delay), "rate limited: waiting for account capacity")
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

		account, err = resolveSendAccount(job.UserID)
		if err != nil {
			failJobConfiguration(job, err)
			continue
		}

		claimed = append(claimed, claimedJob{job: job, account: account})
	}
	return claimed
}
