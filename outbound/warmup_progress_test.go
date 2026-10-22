package outbound

import (
	"testing"

	"emailtracker.com/model"
)

func TestComputeWarmupProgressUsesDailyLimit(t *testing.T) {
	acc := model.SMTPAccount{
		DailyLimit: 50,
		SendsToday: 12,
	}
	p := ComputeWarmupProgress(acc, true)
	if p.Enabled {
		t.Fatal("warmup ramp should be disabled")
	}
	if !p.IsFullyWarmed {
		t.Fatal("expected fully at daily limit (no ramp)")
	}
	if p.TodayCap != 50 {
		t.Fatalf("today cap %d", p.TodayCap)
	}
	if p.TodayRemaining != 38 {
		t.Fatalf("remaining %d", p.TodayRemaining)
	}
}

func TestEffectiveDailyCapIgnoresWarmupFields(t *testing.T) {
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        20,
		WarmupCurrentCap:      20,
		WarmupTargetDailyCap:  100,
		WarmupIncrementPerDay: 20,
		DailyLimit:            250,
	}
	if got := EffectiveDailyCap(acc); got != 250 {
		t.Fatalf("cap=%d want daily_limit 250", got)
	}
}

func TestEffectiveDailyCapFallback(t *testing.T) {
	acc := model.SMTPAccount{}
	if got := EffectiveDailyCap(acc); got != model.DefaultDailyLimit {
		t.Fatalf("cap=%d want %d", got, model.DefaultDailyLimit)
	}
}
