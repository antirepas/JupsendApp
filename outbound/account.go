package outbound

import (
	"time"

	"emailtracker.com/model"
)

func resolveSendAccount(userID int64) (model.SMTPAccount, error) {
	return model.GetSendReadyAccountForUser(userID)
}

func failJobConfiguration(job model.SendJob, err error) {
	msg := err.Error()
	_ = model.FailSendJob(job.ID, msg, "failed")
	if job.EmailSendID > 0 {
		_ = model.MarkEmailSendFailed(job.EmailSendID)
	}
	if job.CampaignID > 0 {
		reconcileCampaign(job.CampaignID)
	}
}

func nextMidnight() time.Duration {
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return end.Sub(now)
}

func rateLimitDelay(account model.SMTPAccount) time.Duration {
	if !accountUnderDailyCap(account) {
		return nextMidnight()
	}
	return NextRateLimitDelay([]model.SMTPAccount{account})
}

func rateLimitDelayForJob(account model.SMTPAccount, job model.SendJob) time.Duration {
	if !accountUnderDailyCap(account) {
		return nextMidnight()
	}
	if job.Priority >= PriorityManual {
		return nextManualSendDelay(account)
	}
	return NextRateLimitDelay([]model.SMTPAccount{account})
}

func nextManualSendDelay(account model.SMTPAccount) time.Duration {
	if !accountUnderMinuteLimit(account) {
		return 15 * time.Second
	}
	return time.Second
}
