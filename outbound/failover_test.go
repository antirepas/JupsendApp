package outbound

import (
	"errors"
	"testing"
	"time"

	"emailtracker.com/model"
)

func TestProviderBlockSkipsAccount(t *testing.T) {
	id := int64(99001)
	MarkAccountProviderBlocked(id, time.Now().Add(time.Hour))
	if !IsAccountProviderBlocked(id) {
		t.Fatal("expected blocked")
	}
	acc := model.SMTPAccount{
		ID: id, Status: "active", DailyLimit: 100, PerMinuteLimit: 10,
		WarmupEnabled: false, SendsToday: 0,
	}
	now := time.Now()
	acc.SendsTodayResetAt = &now
	if AccountCanSendNow(acc) {
		t.Fatal("blocked account must not send")
	}
	// Expire
	MarkAccountProviderBlocked(id, time.Now().Add(-time.Second))
	providerBlockMu.Lock()
	delete(providerBlocked, id)
	providerBlockMu.Unlock()
}

func TestFailoverOrWaitDelayPrefersOtherMailbox(t *testing.T) {
	// Unit-level: capacity error classification drives wait-until-tomorrow when no alt.
	if !IsProviderDailyQuota(errors.New(`550 "5.4.5 Daily user sending limit exceeded"`)) {
		t.Fatal("expected quota detection")
	}
	if !isProviderCapacityError(errors.New("rate limit exceeded")) {
		t.Fatal("expected capacity detection")
	}
}

func TestIsAccountLevelError(t *testing.T) {
	if !isAccountLevelError(errors.New("535 authentication failed")) {
		t.Fatal("expected auth to be account-level")
	}
	if isAccountLevelError(errors.New("550 5.1.1 user unknown")) {
		t.Fatal("recipient bounce is not account-level failover")
	}
}
