package outbound

import (
	"time"

	"emailtracker.com/model"
)

func EffectiveDailyCap(account model.SMTPAccount) int {
	if !account.WarmupEnabled {
		return account.DailyLimit
	}
	target := account.WarmupTargetDailyCap
	if target == 0 {
		target = account.DailyLimit
	}
	startCap := account.WarmupDailyCap
	if startCap == 0 {
		startCap = 5
	}
	increment := account.WarmupIncrementPerDay
	if increment == 0 {
		increment = 5
	}
	started := account.WarmupStartedAt
	if started == nil {
		return minInt(target, startCap)
	}
	days := int(time.Since(*started).Hours() / 24)
	cap := startCap + days*increment
	if cap > target {
		cap = target
	}
	if cap > account.DailyLimit && account.DailyLimit > 0 {
		cap = account.DailyLimit
	}
	return cap
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
