package outbound

import (
	"fmt"
	"time"

	"emailtracker.com/model"
)

func resolveSendAccount(userID int64) (model.SMTPAccount, error) {
	return model.GetSendReadyAccountForUser(userID)
}

// ResolveSendAccountForContact picks a sticky mailbox for a contact (all ready seats).
func ResolveSendAccountForContact(userID, contactID int64) (model.SMTPAccount, error) {
	return ResolveSendAccountForContactInCampaign(userID, contactID, 0)
}

// ResolveSendAccountForContactInCampaign picks a sticky mailbox scoped to a campaign allowlist
// when campaignID > 0. Empty allowlist = all ready seats.
// 1) last SMTP for that contact — keep it only when it is still in the campaign seat set
//    (over-cap sticky is OK; outside-allowlist is not — campaign sends must honor the picker)
// 2) first-touch: stable hash across campaign-ready seats that can send now
// 3) fallback default ready seat
func ResolveSendAccountForContactInCampaign(userID, contactID, campaignID int64) (model.SMTPAccount, error) {
	var ready []model.SMTPAccount
	var err error
	if campaignID > 0 {
		ready, err = model.ListSendReadyAccountsForCampaign(userID, campaignID)
	} else {
		ready, err = model.ListSendReadyAccountsForUser(userID)
	}
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
			// Sticky seat may be over-cap / temporarily filtered from ready — still pin it
			// when it remains allowed for this campaign (or when there is no campaign scope).
			if campaignID <= 0 || accountInCampaignAllowlist(campaignID, lastID) {
				if acc, gErr := model.GetSMTPAccount(lastID); gErr == nil && acc.UserID == userID && acc.Status == "active" {
					_ = model.EnsureDailyCounterReset(acc.ID)
					if fresh, fErr := model.GetSMTPAccount(acc.ID); fErr == nil {
						return fresh, nil
					}
					return acc, nil
				}
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

// ResolveAccountForJob prefers a mailbox already pinned on the job, then sticky contact routing.
// If the pin cannot send yet and this contact has never received mail from us, rebalance to
// another seat with capacity (combined daily headroom is real; unsent pins must not waste it).
// Campaign jobs always stay on the campaign mailbox allowlist when one is set.
func ResolveAccountForJob(job model.SendJob) (model.SMTPAccount, error) {
	if job.SMTPAccountID > 0 {
		acc, err := model.GetSMTPAccount(job.SMTPAccountID)
		if err == nil && acc.UserID == job.UserID && acc.Status == "active" {
			_ = model.EnsureDailyCounterReset(acc.ID)
			if fresh, fErr := model.GetSMTPAccount(acc.ID); fErr == nil {
				acc = fresh
			}
			// Campaign allowlist wins over a stale pin (including prior-campaign sticky).
			if job.CampaignID > 0 && !accountInCampaignAllowlist(job.CampaignID, acc.ID) {
				if alt, aErr := pickReadyAccountForContact(job.UserID, job.ContactID, job.CampaignID, acc.ID); aErr == nil && alt.ID > 0 {
					return alt, nil
				}
				return ResolveSendAccountForContactInCampaign(job.UserID, job.ContactID, job.CampaignID)
			}
			if AccountCanSendNowForJob(acc, job) {
				return acc, nil
			}
			// Already delivered from this (or any) From — keep sticky and wait.
			if model.ContactHasDeliveredOutbound(job.UserID, job.ContactID) {
				return acc, nil
			}
			// Unsent: try another seat with capacity (campaign allowlist when set).
			if alt, aErr := pickReadyAccountForContact(job.UserID, job.ContactID, job.CampaignID, acc.ID); aErr == nil && alt.ID > 0 && alt.ID != acc.ID {
				return alt, nil
			}
			return acc, nil
		}
	}
	return ResolveSendAccountForContactInCampaign(job.UserID, job.ContactID, job.CampaignID)
}

func accountInCampaignAllowlist(campaignID, smtpID int64) bool {
	if campaignID <= 0 || smtpID <= 0 {
		return true
	}
	allowed, err := model.ListCampaignSMTPAccountIDs(campaignID)
	if err != nil || len(allowed) == 0 {
		return true // empty allowlist = all seats
	}
	for _, id := range allowed {
		if id == smtpID {
			return true
		}
	}
	return false
}

func pickReadyAccountForContact(userID, contactID, campaignID, excludeID int64) (model.SMTPAccount, error) {
	var ready []model.SMTPAccount
	var err error
	if campaignID > 0 {
		ready, err = model.ListSendReadyAccountsForCampaign(userID, campaignID)
	} else {
		ready, err = model.ListSendReadyAccountsForUser(userID)
	}
	if err != nil {
		return model.SMTPAccount{}, err
	}
	if len(ready) == 0 {
		return model.SMTPAccount{}, fmt.Errorf("no ready sending mailbox")
	}
	start := 0
	if contactID > 0 {
		start = int(contactID % int64(len(ready)))
		if start < 0 {
			start = 0
		}
	}
	for i := 0; i < len(ready); i++ {
		acc := ready[(start+i)%len(ready)]
		if excludeID > 0 && acc.ID == excludeID {
			continue
		}
		if AccountCanSendNow(acc) {
			return acc, nil
		}
	}
	return model.SMTPAccount{}, fmt.Errorf("no mailbox with capacity")
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
			if acc, gErr := model.GetSMTPAccount(lastID); gErr == nil && acc.UserID == userID && acc.Status == "active" {
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
