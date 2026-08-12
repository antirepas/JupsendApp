package outbound

import (
	"fmt"
	"math"
	"time"

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
	days := warmupCalendarDaysElapsed(*started, time.Now())
	cap := startCap + days*increment
	if cap > target {
		cap = target
	}
	if account.DailyLimit > 0 && cap > account.DailyLimit {
		cap = account.DailyLimit
	}
	return cap
}

// warmupCalendarDaysElapsed counts whole UTC calendar days since warmup started.
// Using Hours()/24 kept the same ramp day across midnight until the start-time
// anniversary (e.g. started Mon 15:00 still "day 0" Tue morning), so daily volume
// looked stuck for a full calendar day.
func warmupCalendarDaysElapsed(started, now time.Time) int {
	s := started.UTC()
	n := now.UTC()
	startDay := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC)
	nowDay := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	days := int(nowDay.Sub(startDay) / (24 * time.Hour))
	if days < 0 {
		return 0
	}
	return days
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
		p.DaysElapsed = warmupCalendarDaysElapsed(*account.WarmupStartedAt, time.Now())
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
