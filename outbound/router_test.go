package outbound

import (
	"testing"
	"time"

	"emailtracker.com/model"
)

func TestAccountCanSendNow(t *testing.T) {
	now := time.Now()
	acc := model.SMTPAccount{
		ID:                     1,
		Status:                 "active",
		DailyLimit:             50,
		PerMinuteLimit:         2,
		MinSecondsBetweenSends:   0,
		WarmupEnabled:          false,
		SendsToday:             0,
		SendsTodayResetAt:      &now,
	}
	if !AccountCanSendNow(acc) {
		t.Fatal("expected account can send")
	}
	acc.Status = "paused"
	if AccountCanSendNow(acc) {
		t.Fatal("paused account should not send")
	}
}

func TestPickAccount(t *testing.T) {
	now := time.Now()
	accounts := []model.SMTPAccount{
		{ID: 1, Status: "active", DailyLimit: 50, PerMinuteLimit: 10, SendsTodayResetAt: &now},
		{ID: 2, Status: "paused", DailyLimit: 50, PerMinuteLimit: 10, SendsTodayResetAt: &now},
	}
	picked, ok := PickAccount(accounts)
	if !ok || picked.ID != 1 {
		t.Fatalf("expected active account 1, got %d ok=%v", picked.ID, ok)
	}
}
