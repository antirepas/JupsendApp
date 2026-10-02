package outbound

import (
	"testing"
	"time"

	"emailtracker.com/model"
)

func TestComputeWarmupProgressFullyWarmed(t *testing.T) {
	start := time.Now().Add(-30 * 24 * time.Hour)
	acc := model.SMTPAccount{
		ID:                    1,
		WarmupEnabled:         true,
		WarmupDailyCap:        5,
		WarmupTargetDailyCap:  50,
		WarmupIncrementPerDay: 5,
		WarmupCurrentCap:      50,
		WarmupEarnedDays:      9,
		DailyLimit:            50,
		WarmupStartedAt:       &start,
		SendsToday:            12,
		GoogleEmail:           "sender@test.com",
		AuthType:              model.AuthTypeGoogleOAuth,
		OAuthRefreshToken:     "x",
	}
	p := ComputeWarmupProgress(acc, true)
	if !p.IsFullyWarmed {
		t.Fatal("expected fully warmed")
	}
	if p.OverallPct != 100 {
		t.Fatalf("overall pct %v", p.OverallPct)
	}
	if p.TodayCap != 50 {
		t.Fatalf("today cap %d", p.TodayCap)
	}
	if p.TodayRemaining != 38 {
		t.Fatalf("remaining %d", p.TodayRemaining)
	}
}

func TestComputeWarmupProgressMidRamp(t *testing.T) {
	start := time.Now().Add(-48 * time.Hour)
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        5,
		WarmupTargetDailyCap:  50,
		WarmupIncrementPerDay: 5,
		WarmupCurrentCap:      15,
		WarmupEarnedDays:      2,
		DailyLimit:            50,
		WarmupStartedAt:       &start,
		SendsToday:            3,
	}
	p := ComputeWarmupProgress(acc, true)
	if p.TodayCap != 15 {
		t.Fatalf("today cap %d want 15", p.TodayCap)
	}
	if p.TodayRemaining != 12 {
		t.Fatalf("remaining %d", p.TodayRemaining)
	}
	if p.DaysElapsed != 2 {
		t.Fatalf("earned days %d", p.DaysElapsed)
	}
	if p.RampDaysTotal != 9 {
		t.Fatalf("ramp days %d", p.RampDaysTotal)
	}
	if p.DaysRemaining != 7 { // (50-15)/5
		t.Fatalf("days remaining %d want 7", p.DaysRemaining)
	}
	wantPct := float64(15-5) / float64(50-5) * 100
	if mathAbs(p.OverallPct-wantPct) > 0.1 {
		t.Fatalf("overall pct %v want %v", p.OverallPct, wantPct)
	}
}

func TestScheduleDailyCapUsesCurrentCapNotCalendar(t *testing.T) {
	today := time.Now().UTC()
	start := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC).Add(-30 * 24 * time.Hour)
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        20,
		WarmupTargetDailyCap:  100,
		WarmupIncrementPerDay: 20,
		WarmupCurrentCap:      40, // only one earned step despite 30 calendar days
		DailyLimit:            250,
		WarmupStartedAt:       &start,
	}
	cap := scheduleDailyCap(acc)
	if cap != 40 {
		t.Fatalf("cap=%d want 40 (usage-based, not calendar)", cap)
	}
}

func TestScheduleDailyCapNilCurrentUsesStart(t *testing.T) {
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        20,
		WarmupTargetDailyCap:  100,
		WarmupIncrementPerDay: 20,
		DailyLimit:            250,
	}
	cap := scheduleDailyCap(acc)
	if cap != 20 {
		t.Fatalf("cap=%d want 20", cap)
	}
}

func TestIdleMailboxDoesNotAutoWarm(t *testing.T) {
	// Calendar time alone must not raise the cap.
	start := time.Now().Add(-14 * 24 * time.Hour)
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        20,
		WarmupCurrentCap:      20,
		WarmupTargetDailyCap:  100,
		WarmupIncrementPerDay: 20,
		DailyLimit:            250,
		WarmupStartedAt:       &start,
		SendsToday:            0,
	}
	if got := EffectiveDailyCap(acc); got != 20 {
		t.Fatalf("idle after 14d: cap=%d want 20", got)
	}
}

func mathAbs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
