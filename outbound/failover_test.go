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
	// Unit-level: hard quota/capacity errors defer until reset; soft rate-limit phrases do not.
	if !IsProviderDailyQuota(errors.New(`550 "5.4.5 Daily user sending limit exceeded"`)) {
		t.Fatal("expected quota detection")
	}
	if !isProviderCapacityError(errors.New("sending quota exceeded")) {
		t.Fatal("expected capacity detection")
	}
	if isProviderCapacityError(errors.New("rate limit exceeded")) {
		t.Fatal("generic rate limit must not park mailbox until midnight")
	}
	if isProviderCapacityError(errors.New("try again later")) {
		t.Fatal("soft try-again must not park mailbox until midnight")
	}
}

func TestIsAccountLevelError(t *testing.T) {
	// Recipient bounces stay permanent; capacity errors are deferred.
	if ShouldSuppressFromError(errors.New("550 5.1.1 user unknown")) {
		// ok expected
	} else {
		t.Fatal("expected suppress for unknown user")
	}
	if isProviderCapacityError(errors.New("550 5.1.1 user unknown")) {
		t.Fatal("recipient bounce is not a capacity deferral")
	}
}
