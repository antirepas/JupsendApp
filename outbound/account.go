package outbound

import (
	"fmt"
	"time"

	"emailtracker.com/model"
)

func resolveSendAccount(userID int64) (model.SMTPAccount, error) {
	return model.GetSendReadyAccountForUser(userID)
}

// ResolveSendAccountForContact picks a sticky mailbox for a contact:
// 1) last SMTP used for that contact if still in the ready set (even if at cap —
//    caller reschedules; we never switch From mid-conversation)
// 2) for first-touch contacts: stable hash across seats that can send now
// 3) fallback default ready seat
func ResolveSendAccountForContact(userID, contactID int64) (model.SMTPAccount, error) {
	ready, err := model.ListSendReadyAccountsForUser(userID)
	if err != nil {
		return model.SMTPAccount{}, err
	}
	if len(ready) == 0 {
		return model.SMTPAccount{}, fmt.Errorf("no ready sending mailbox — open Mailboxes to finish setup")
	}

	byID := make(map[int64]model.SMTPAccount, len(ready))
	for _, acc := range ready {
		byID[acc.ID] = acc
	}

	if contactID > 0 {
		if lastID, lErr := model.LatestSMTPAccountForContact(userID, contactID); lErr == nil && lastID > 0 {
			if acc, ok := byID[lastID]; ok {
				return acc, nil
			}
		}
		start := int(contactID % int64(len(ready)))
		if start < 0 {
			start = 0
		}
		for i := 0; i < len(ready); i++ {
			acc := ready[(start+i)%len(ready)]
			if AccountCanSendNow(acc) {
				return acc, nil
			}
		}
	}

	for _, acc := range ready {
		if AccountCanSendNow(acc) {
			return acc, nil
		}
	}
	// All rate-limited — still return sticky/default so caller can reschedule.
	if contactID > 0 {
		return ready[int(contactID%int64(len(ready)))], nil
	}
	return ready[0], nil
}

// StickyAccountForContact returns the planned mailbox for a contact without rate-limit filtering
// (for campaign distribution UI). Prefers last-used seat when still in the ready set.
func StickyAccountForContact(ready []model.SMTPAccount, userID, contactID int64) model.SMTPAccount {
	if len(ready) == 0 {
		return model.SMTPAccount{}
	}
	byID := make(map[int64]model.SMTPAccount, len(ready))
	for _, acc := range ready {
		byID[acc.ID] = acc
	}
	if contactID > 0 {
		if lastID, err := model.LatestSMTPAccountForContact(userID, contactID); err == nil && lastID > 0 {
			if acc, ok := byID[lastID]; ok {
				return acc
			}
		}
		return ready[int(contactID%int64(len(ready)))]
	}
	return ready[0]
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
	if job.Priority >= PriorityManual {
		return nextManualSendDelay(account)
	}
	// Stay on this mailbox — wait out daily/provider caps rather than switching From.
	if !accountUnderDailyCap(account) || IsAccountProviderBlocked(account.ID) {
		d := nextMidnight()
		if d < 30*time.Minute {
			return 30 * time.Minute
		}
		return d
	}
	return NextRateLimitDelay([]model.SMTPAccount{account})
}

func nextManualSendDelay(account model.SMTPAccount) time.Duration {
	if !accountUnderMinuteLimit(account) {
		return 15 * time.Second
	}
	return time.Second
}
