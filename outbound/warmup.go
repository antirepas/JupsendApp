package outbound

import (
	"fmt"
	"math"

	"emailtracker.com/model"
)

// WarmupProgress is dashboard-ready warmup state for an SMTP account (or combined seats).
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

func EffectiveDailyCap(account model.SMTPAccount) int {
	return EffectiveDailyCapWithInsights(account, "")
}

// EffectiveDailyCapWithInsights applies the Pro warmup schedule, then optional InboxKit clamps.
func EffectiveDailyCapWithInsights(account model.SMTPAccount, analyticsJSON string) int {
	_ = model.EnsureWarmupStartedAt(&account)
	schedule := scheduleDailyCap(account)
	cap, _ := ApplyInsightsToCap(schedule, account, analyticsJSON)
	if cap < 1 {
		// Never treat a ready mailbox as 0/day — that parked the queue until midnight forever.
		if account.DailyLimit > 0 {
			return account.DailyLimit
		}
		return model.DefaultWarmupDailyCap
	}
	return cap
}

func scheduleDailyCap(account model.SMTPAccount) int {
	if !account.WarmupEnabled {
		return account.DailyLimit
	}
	return model.EffectiveWarmupCurrentCap(account)
}

// ComputeWarmupProgress builds dashboard warmup metrics from an SMTP account.
func ComputeWarmupProgress(account model.SMTPAccount, hasAccount bool) WarmupProgress {
	p := WarmupProgress{HasAccount: hasAccount}
	if !hasAccount {
		return p
	}
	_ = model.EnsureWarmupStartedAt(&account)

	p.SenderEmail = account.SenderEmail()
	p.SendsToday = account.SendsToday
	p.Enabled = account.WarmupEnabled
	p.StartCap = account.WarmupDailyCap
	if p.StartCap <= 0 {
		p.StartCap = model.DefaultWarmupDailyCap
	}
	p.TargetCap = account.WarmupTargetDailyCap
	if p.TargetCap <= 0 {
		p.TargetCap = account.DailyLimit
	}
	if p.TargetCap <= 0 {
		p.TargetCap = 50
	}
	p.IncrementPerDay = account.WarmupIncrementPerDay
	if p.IncrementPerDay <= 0 {
		p.IncrementPerDay = model.DefaultWarmupIncrementPerDay
	}

	p.TodayCap = EffectiveDailyCap(account)
	p.RampCap = p.TodayCap
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

	if !p.Enabled {
		p.IsFullyWarmed = true
		p.OverallPct = 100
		return p
	}

	if p.RampCap >= p.TargetCap {
		p.IsFullyWarmed = true
		p.OverallPct = 100
	} else if p.TargetCap > p.StartCap {
		p.OverallPct = float64(p.RampCap-p.StartCap) / float64(p.TargetCap-p.StartCap) * 100
	} else {
		p.OverallPct = 100
		p.IsFullyWarmed = true
	}

	if p.TargetCap > p.StartCap {
		p.RampDaysTotal = int(math.Ceil(float64(p.TargetCap-p.StartCap) / float64(p.IncrementPerDay)))
	}
	p.DaysElapsed = account.WarmupEarnedDays
	if p.DaysElapsed < 0 {
		p.DaysElapsed = 0
	}
	// Remaining steps if every future day earns a promotion.
	if p.RampCap < p.TargetCap && p.IncrementPerDay > 0 {
		p.DaysRemaining = int(math.Ceil(float64(p.TargetCap-p.RampCap) / float64(p.IncrementPerDay)))
	} else {
		p.DaysRemaining = 0
	}
	if p.IsFullyWarmed {
		p.DaysRemaining = 0
	}

	return p
}

// ComputeCombinedWarmupProgress aggregates warmup across all send-ready mailboxes.
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
		HasAccount:    true,
		MailboxCount:  len(accounts),
		CombinedLabel: fmt.Sprintf("%d mailboxes combined", len(accounts)),
		SenderEmail:   fmt.Sprintf("%d mailboxes", len(accounts)),
	}

	anyWarmup := false
	allWarmed := true
	var weightedPct float64
	var weightSum float64
	maxDaysRemaining := 0
	maxRampDays := 0
	minInc := 0

	for _, acc := range accounts {
		one := ComputeWarmupProgress(acc, true)
		p.SendsToday += one.SendsToday
		p.TodayCap += one.TodayCap
		p.RampCap += one.RampCap
		p.TargetCap += one.TargetCap
		p.StartCap += one.StartCap
		if one.Enabled {
			anyWarmup = true
			if !one.IsFullyWarmed {
				allWarmed = false
			}
			w := float64(one.TargetCap)
			if w <= 0 {
				w = 1
			}
			weightedPct += one.OverallPct * w
			weightSum += w
			if one.DaysRemaining > maxDaysRemaining {
				maxDaysRemaining = one.DaysRemaining
			}
			if one.RampDaysTotal > maxRampDays {
				maxRampDays = one.RampDaysTotal
			}
			if one.DaysElapsed > p.DaysElapsed {
				p.DaysElapsed = one.DaysElapsed
			}
			if minInc == 0 || (one.IncrementPerDay > 0 && one.IncrementPerDay < minInc) {
				minInc = one.IncrementPerDay
			}
		}
	}
	p.Enabled = anyWarmup
	p.IncrementPerDay = minInc
	if minInc <= 0 {
		p.IncrementPerDay = model.DefaultWarmupIncrementPerDay
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
	if !anyWarmup {
		p.IsFullyWarmed = true
		p.OverallPct = 100
		return p
	}
	p.IsFullyWarmed = allWarmed
	if weightSum > 0 {
		p.OverallPct = weightedPct / weightSum
	}
	if p.IsFullyWarmed {
		p.OverallPct = 100
		p.DaysRemaining = 0
	} else {
		p.DaysRemaining = maxDaysRemaining
		p.RampDaysTotal = maxRampDays
	}
	return p
}
