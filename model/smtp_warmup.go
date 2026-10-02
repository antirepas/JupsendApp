package model

import "math"

// WarmupPromoteUsageRatio is the fraction of yesterday's cap that must be used
// to earn the next warmup increment. Idle days do not raise the limit.
const WarmupPromoteUsageRatio = 0.7

// WarmupStartCap returns the ramp floor for an account.
func WarmupStartCap(a SMTPAccount) int {
	if a.WarmupDailyCap > 0 {
		return a.WarmupDailyCap
	}
	return DefaultWarmupDailyCap
}

// WarmupTargetCap returns the ramp ceiling (plan / mailbox target).
func WarmupTargetCap(a SMTPAccount) int {
	if a.WarmupTargetDailyCap > 0 {
		return a.WarmupTargetDailyCap
	}
	if a.DailyLimit > 0 {
		return a.DailyLimit
	}
	return 50
}

// WarmupIncrement returns the step size when a day earns a promotion.
func WarmupIncrement(a SMTPAccount) int {
	if a.WarmupIncrementPerDay > 0 {
		return a.WarmupIncrementPerDay
	}
	return DefaultWarmupIncrementPerDay
}

// EffectiveWarmupCurrentCap is the earned daily cap used for sending today.
// Falls back to the start cap when not yet seeded.
func EffectiveWarmupCurrentCap(a SMTPAccount) int {
	start := WarmupStartCap(a)
	target := WarmupTargetCap(a)
	current := a.WarmupCurrentCap
	if current <= 0 {
		current = start
	}
	if current < start {
		current = start
	}
	if current > target {
		current = target
	}
	if a.DailyLimit > 0 && current > a.DailyLimit {
		current = a.DailyLimit
	}
	return current
}

// WarmupSendsNeededToPromote is how many sends are required to earn +increment.
func WarmupSendsNeededToPromote(yesterdayCap int) int {
	if yesterdayCap <= 0 {
		return 1
	}
	need := int(math.Ceil(float64(yesterdayCap) * WarmupPromoteUsageRatio))
	if need < 1 {
		need = 1
	}
	return need
}

// ShouldPromoteWarmup reports whether yesterday's volume earns a ramp step.
func ShouldPromoteWarmup(sendsYesterday, yesterdayCap int) bool {
	return sendsYesterday >= WarmupSendsNeededToPromote(yesterdayCap)
}

// NextWarmupCapAfterDay returns the cap after evaluating yesterday's usage.
func NextWarmupCapAfterDay(current, sendsYesterday, increment, target, dailyLimit int) (newCap int, promoted bool) {
	if current <= 0 {
		current = 0
	}
	newCap = current
	if ShouldPromoteWarmup(sendsYesterday, current) {
		candidate := current + increment
		if target > 0 && candidate > target {
			candidate = target
		}
		if dailyLimit > 0 && candidate > dailyLimit {
			candidate = dailyLimit
		}
		if candidate > current {
			newCap = candidate
			promoted = true
		}
	}
	if target > 0 && newCap > target {
		newCap = target
	}
	if dailyLimit > 0 && newCap > dailyLimit {
		newCap = dailyLimit
	}
	return newCap, promoted
}
