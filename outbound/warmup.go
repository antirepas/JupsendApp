package outbound

import (
	"math"
	"time"

	"emailtracker.com/model"
)

// WarmupProgress is dashboard-ready warmup state for an SMTP account.
type WarmupProgress struct {
	HasAccount      bool
	Enabled         bool
	SenderEmail     string
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
	return cap
}

func scheduleDailyCap(account model.SMTPAccount) int {
	if !account.WarmupEnabled {
		return account.DailyLimit
	}
	target := account.WarmupTargetDailyCap
	if target == 0 {
		target = account.DailyLimit
	}
	startCap := account.WarmupDailyCap
	if startCap == 0 {
		startCap = model.DefaultWarmupDailyCap
	}
	increment := account.WarmupIncrementPerDay
	if increment == 0 {
		increment = model.DefaultWarmupIncrementPerDay
	}
	started := account.WarmupStartedAt
	if started == nil {
		// Defensive: treat as day 0 so we still enforce the start cap (EnsureWarmupStartedAt should stamp).
		return minInt(target, startCap)
	}
	days := int(time.Since(*started).Hours() / 24)
	if days < 0 {
		days = 0
	}
	cap := startCap + days*increment
	if cap > target {
		cap = target
	}
	if account.DailyLimit > 0 && cap > account.DailyLimit {
		cap = account.DailyLimit
	}
	return cap
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
	if account.WarmupStartedAt != nil {
		p.DaysElapsed = int(time.Since(*account.WarmupStartedAt).Hours() / 24)
	}
	p.DaysRemaining = p.RampDaysTotal - p.DaysElapsed
	if p.DaysRemaining < 0 {
		p.DaysRemaining = 0
	}
	if p.IsFullyWarmed {
		p.DaysRemaining = 0
	}

	return p
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
