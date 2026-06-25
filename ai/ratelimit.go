package ai

import (
	"sync"
	"time"
)

const defaultHourlyLimit = 30

var (
	rateMu sync.Mutex
	rate   = make(map[int64][]time.Time)
)

func AllowRequest(userID int64) bool {
	return allowRequest(userID, defaultHourlyLimit, time.Hour)
}

func allowRequest(userID int64, limit int, window time.Duration) bool {
	rateMu.Lock()
	defer rateMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)
	var kept []time.Time
	for _, t := range rate[userID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		rate[userID] = kept
		return false
	}
	kept = append(kept, now)
	rate[userID] = kept
	return true
}

func ResetRateLimits() {
	rateMu.Lock()
	defer rateMu.Unlock()
	rate = make(map[int64][]time.Time)
}
