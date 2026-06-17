package outbound

import (
	"time"

	"emailtracker.com/model"
)

var routerIndex int

func PickAccount(accounts []model.SMTPAccount) (model.SMTPAccount, bool) {
	if len(accounts) == 0 {
		return model.SMTPAccount{}, false
	}
	n := len(accounts)
	for i := 0; i < n; i++ {
		idx := (routerIndex + i) % n
		acc := accounts[idx]
		if AccountCanSendNow(acc) {
			routerIndex = (idx + 1) % n
			return acc, true
		}
	}
	return model.SMTPAccount{}, false
}

func NextRateLimitDelay(accounts []model.SMTPAccount) time.Duration {
	minWait := time.Duration(0)
	for _, acc := range accounts {
		if acc.Status != "active" {
			continue
		}
	if !accountUnderDailyCap(acc) {
		if minWait == 0 {
			minWait = nextMidnight()
		}
		continue
	}
		if !accountUnderMinuteLimit(acc) {
			if minWait == 0 || 30*time.Second < minWait {
				minWait = 30 * time.Second
			}
		}
		gap := acc.MinSecondsBetweenSends
		if gap > 0 && acc.LastSendAt != nil {
			elapsed := time.Since(*acc.LastSendAt)
			need := time.Duration(gap)*time.Second - elapsed
			if need > 0 && (minWait == 0 || need < minWait) {
				minWait = need
			}
		}
	}
	if minWait == 0 {
		minWait = 30 * time.Second
	}
	return minWait
}
