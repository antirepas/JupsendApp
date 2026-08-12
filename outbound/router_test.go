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

func TestAccountCanSendNowForJobSkipsSpacingForManual(t *testing.T) {
	now := time.Now()
	last := now.Add(-5 * time.Second)
	acc := model.SMTPAccount{
		ID:                     1,
		Status:                 "active",
		DailyLimit:             50,
		PerMinuteLimit:         10,
		MinSecondsBetweenSends: 30,
		WarmupEnabled:          false,
		SendsToday:             0,
		SendsTodayResetAt:      &now,
		LastSendAt:             &last,
	}
	if AccountCanSendNow(acc) {
		t.Fatal("expected spacing block for normal send")
	}
	manual := model.SendJob{Priority: PriorityManual}
	if !AccountCanSendNowForJob(acc, manual) {
		t.Fatal("manual send should skip spacing throttle")
	}
}

func TestAccountCanSendNowForJobSkipsDailyWarmupForManual(t *testing.T) {
	now := time.Now()
	acc := model.SMTPAccount{
		ID:                     1,
		Status:                 "active",
		DailyLimit:             50,
		PerMinuteLimit:         10,
		MinSecondsBetweenSends: 0,
		WarmupEnabled:          true,
		WarmupDailyCap:         20,
		WarmupTargetDailyCap:   50,
		WarmupIncrementPerDay:  20,
		WarmupStartedAt:        &now,
		SendsToday:             20,
		SendsTodayResetAt:      &now,
	}
	if AccountCanSendNow(acc) {
		t.Fatal("campaign send should be blocked at warmup cap")
	}
	manual := model.SendJob{Priority: PriorityManual}
	if !AccountCanSendNowForJob(acc, manual) {
		t.Fatal("manual one-off should ignore warmup daily cap")
	}
	if !AccountCanSendManualNow(acc) {
		t.Fatal("conversation reply path should ignore warmup daily cap")
	}
	campaign := model.SendJob{Priority: PriorityCampaign}
	if AccountCanSendNowForJob(acc, campaign) {
		t.Fatal("campaign job must still respect warmup cap")
	}
}

func TestSendPriority(t *testing.T) {
	if sendPriority(EnqueueInput{CampaignID: 5}) != PriorityCampaign {
		t.Fatal("campaign send should use campaign priority")
	}
	if sendPriority(EnqueueInput{}) != PriorityManual {
		t.Fatal("one-off send should use manual priority")
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
