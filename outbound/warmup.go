package outbound

import (
	"fmt"

	"emailtracker.com/model"
)

// WarmupProgress is dashboard-ready daily send usage for an SMTP account (or combined seats).
// Name kept for template compatibility; there is no ramp anymore — TodayCap is the daily limit.
type WarmupProgress struct {
	HasAccount      bool
	Enabled         bool
	SenderEmail     string
	MailboxCount    int
	CombinedLabel   string
	SendsToday      int
	TodayCap        int
	TodayRemaining  int
	TodayUsedPct    float64
	StartCap        int
	TargetCap       int
	RampCap         int
	OverallPct      float64
	DaysElapsed     int
	RampDaysTotal   int
	DaysRemaining   int
	IsFullyWarmed   bool
	IncrementPerDay int
}

// EffectiveDailyCap returns the account's daily send limit.
func EffectiveDailyCap(account model.SMTPAccount) int {
	return EffectiveDailyCapWithInsights(account, "")
}

// EffectiveDailyCapWithInsights returns the configured daily limit.
// analyticsJSON is ignored — InboxKit hints no longer override the user's limit.
func EffectiveDailyCapWithInsights(account model.SMTPAccount, analyticsJSON string) int {
	_ = analyticsJSON
	if account.DailyLimit > 0 {
		return account.DailyLimit
	}
	return model.DefaultDailyLimit
}

// ComputeWarmupProgress builds daily usage metrics from an SMTP account.
func ComputeWarmupProgress(account model.SMTPAccount, hasAccount bool) WarmupProgress {
	p := WarmupProgress{HasAccount: hasAccount}
	if !hasAccount {
		return p
	}

	p.SenderEmail = account.SenderEmail()
	p.SendsToday = account.SendsToday
	p.Enabled = false
	p.IsFullyWarmed = true
	p.OverallPct = 100
	p.TodayCap = EffectiveDailyCap(account)
	p.RampCap = p.TodayCap
	p.TargetCap = p.TodayCap
	p.StartCap = p.TodayCap
	p.TodayRemaining = p.TodayCap - p.SendsToday
	if p.TodayRemaining < 0 {
		p.TodayRemaining = 0
	}
	if p.TodayCap > 0 {
		p.TodayUsedPct = float64(p.SendsToday) / float64(p.TodayCap) * 100
		if p.TodayUsedPct > 100 {
			p.TodayUsedPct = 100
		}
	}
	return p
}

// ComputeCombinedWarmupProgress aggregates daily usage across send-ready mailboxes.
func ComputeCombinedWarmupProgress(accounts []model.SMTPAccount) WarmupProgress {
	if len(accounts) == 0 {
		return WarmupProgress{HasAccount: false}
	}
	if len(accounts) == 1 {
		p := ComputeWarmupProgress(accounts[0], true)
		p.MailboxCount = 1
		return p
	}

	p := WarmupProgress{
		HasAccount:     true,
		MailboxCount:   len(accounts),
		CombinedLabel:  fmt.Sprintf("%d mailboxes combined", len(accounts)),
		SenderEmail:    fmt.Sprintf("%d mailboxes", len(accounts)),
		Enabled:        false,
		IsFullyWarmed:  true,
		OverallPct:     100,
	}
	for _, acc := range accounts {
		one := ComputeWarmupProgress(acc, true)
		p.SendsToday += one.SendsToday
		p.TodayCap += one.TodayCap
		p.RampCap += one.RampCap
		p.TargetCap += one.TargetCap
		p.StartCap += one.StartCap
	}
	p.TodayRemaining = p.TodayCap - p.SendsToday
	if p.TodayRemaining < 0 {
		p.TodayRemaining = 0
	}
	if p.TodayCap > 0 {
		p.TodayUsedPct = float64(p.SendsToday) / float64(p.TodayCap) * 100
		if p.TodayUsedPct > 100 {
			p.TodayUsedPct = 100
		}
	}
	return p
}
