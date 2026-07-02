package outbound

import (
	"sync"
	"time"

	"emailtracker.com/model"
)

type minuteWindow struct {
	timestamps []time.Time
}

var (
	rateMu     sync.Mutex
	minuteMap  = make(map[int64]*minuteWindow)
)

func resetDailyIfNeeded(account *model.SMTPAccount) {
	model.ResetAccountDailyIfNeeded(account)
}

func accountUnderDailyCap(account model.SMTPAccount) bool {
	resetDailyIfNeeded(&account)
	cap := EffectiveDailyCap(account)
	return account.SendsToday < cap
}

func recordMinuteSend(accountID int64) {
	rateMu.Lock()
	defer rateMu.Unlock()
	w := minuteMap[accountID]
	if w == nil {
		w = &minuteWindow{}
		minuteMap[accountID] = w
	}
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	var kept []time.Time
	for _, t := range w.timestamps {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	w.timestamps = kept
}

func minuteSendCount(accountID int64) int {
	rateMu.Lock()
	defer rateMu.Unlock()
	w := minuteMap[accountID]
	if w == nil {
		return 0
	}
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	n := 0
	for _, t := range w.timestamps {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

func accountUnderMinuteLimit(account model.SMTPAccount) bool {
	limit := account.PerMinuteLimit
	if limit <= 0 {
		limit = 2
	}
	return minuteSendCount(account.ID) < limit
}

func accountSpacingOK(account model.SMTPAccount) bool {
	gap := account.MinSecondsBetweenSends
	if gap <= 0 {
		return true
	}
	if account.LastSendAt == nil {
		return true
	}
	return time.Since(*account.LastSendAt) >= time.Duration(gap)*time.Second
}

func AccountCanSendNow(account model.SMTPAccount) bool {
	return accountCanSend(account, true, true)
}

// AccountCanSendNowForJob applies rate limits. Manual one-off sends skip spacing
// so a queued test email is not stuck behind campaign throttling.
func AccountCanSendNowForJob(account model.SMTPAccount, job model.SendJob) bool {
	checkSpacing := job.Priority < PriorityManual
	return accountCanSend(account, true, checkSpacing)
}

func accountCanSend(account model.SMTPAccount, checkMinute, checkSpacing bool) bool {
	if account.Status != "active" {
		return false
	}
	if !accountUnderDailyCap(account) {
		return false
	}
	if checkMinute && !accountUnderMinuteLimit(account) {
		return false
	}
	if checkSpacing && !accountSpacingOK(account) {
		return false
	}
	return true
}

func MarkAccountSent(accountID int64) {
	recordMinuteSend(accountID)
	_ = model.IncrementAccountSendCount(accountID)
}
