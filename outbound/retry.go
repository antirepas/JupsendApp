package outbound

import (
	"strings"
	"time"
)

var backoffSchedule = []time.Duration{
	15 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	60 * time.Minute,
}

type ErrorClass int

const (
	ErrorTransient ErrorClass = 1
	ErrorPermanent ErrorClass = 2
)

func ClassifySMTPError(err error) ErrorClass {
	if err == nil {
		return ErrorTransient
	}
	msg := strings.ToLower(err.Error())
	permanent := []string{
		"535", "authentication", "auth failed", "invalid credentials",
		"550", "551", "552", "553", "554",
		"user unknown", "mailbox unavailable", "mailbox not found",
		"no such user", "address rejected", "recipient address rejected",
		"does not exist", "invalid mailbox",
		"connect gmail", "gmail connection", "gmail not connected", "gmail oauth",
		"no sending profile",
	}
	for _, p := range permanent {
		if strings.Contains(msg, p) {
			return ErrorPermanent
		}
	}
	return ErrorTransient
}

func ShouldSuppressFromError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	patterns := []string{
		"user unknown", "mailbox unavailable", "mailbox not found",
		"no such user", "does not exist", "invalid mailbox", "550 5.1.1",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func BackoffForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		return backoffSchedule[0]
	}
	idx := attempt - 1
	if idx >= len(backoffSchedule) {
		return backoffSchedule[len(backoffSchedule)-1]
	}
	return backoffSchedule[idx]
}
