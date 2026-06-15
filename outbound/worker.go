package outbound

import (
	"log"
	"sync"
	"time"

	"emailtracker.com/model"
	"github.com/google/uuid"
)

var workerMu sync.Mutex

func StartWorker() {
	LoadConfig()
	go func() {
		ticker := time.NewTicker(WorkerInterval)
		for range ticker.C {
			runWorkerBatch()
		}
	}()
}

func runWorkerBatch() {
	workerMu.Lock()
	defer workerMu.Unlock()

	_ = model.ReleaseStaleJobs()

	ids, err := model.PendingSendJobIDs(BatchSize)
	if err != nil || len(ids) == 0 {
		return
	}

	for _, jobID := range ids {
		job, err := model.GetSendJob(jobID)
		if err != nil || job.Status != "pending" {
			continue
		}

		account, err := model.GetActiveSMTPAccountForUser(job.UserID)
		if err != nil {
			_ = model.RescheduleSendJob(job.ID, time.Now().Add(5*time.Minute), "no SMTP account configured for user")
			continue
		}
		userAccounts := []model.SMTPAccount{account}

		if !AccountCanSendNow(account) {
			delay := NextRateLimitDelay(userAccounts)
			_ = model.RescheduleSendJob(job.ID, time.Now().Add(delay), "rate limited: waiting for account capacity")
			continue
		}

		lockToken := uuid.NewString()
		claimed, err := model.ClaimSendJob(job.ID, lockToken, LockDuration)
		if err != nil || !claimed {
			continue
		}

		job, err = model.GetSendJob(jobID)
		if err != nil {
			continue
		}

		if err := executeJob(job, account); err != nil {
			log.Printf("outbound job %d failed: %v", job.ID, err)
			handleJobFailure(job, err)
		}
	}
}
