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
	analytics := model.GetMailboxAnalyticsBySMTPAccountID(account.ID)
	cap := EffectiveDailyCapWithInsights(account, analytics)
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
	return accountCanSend(account, true, true, true)
}

// AccountCanSendManualNow allows one-off sends and conversation replies even when
// the warmup / daily campaign cap is exhausted. Status and per-minute limits still apply.
func AccountCanSendManualNow(account model.SMTPAccount) bool {
	return accountCanSend(account, false, true, false)
}

// AccountCanSendNowForJob applies rate limits. Manual one-off sends skip daily warmup
// caps and spacing so replies / test emails are not blocked behind campaign throttling.
func AccountCanSendNowForJob(account model.SMTPAccount, job model.SendJob) bool {
	if job.Priority >= PriorityManual {
		return AccountCanSendManualNow(account)
	}
	return accountCanSend(account, true, true, true)
}

func accountCanSend(account model.SMTPAccount, checkDaily, checkMinute, checkSpacing bool) bool {
	if account.Status != "active" {
		return false
	}
	if checkDaily && !accountUnderDailyCap(account) {
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
