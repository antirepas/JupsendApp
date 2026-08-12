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
	today := time.Now().UTC()
	start := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC).Add(-48 * time.Hour)
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        5,
		WarmupTargetDailyCap:  50,
		WarmupIncrementPerDay: 5,
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
		t.Fatalf("days elapsed %d", p.DaysElapsed)
	}
	if p.RampDaysTotal != 9 {
		t.Fatalf("ramp days %d", p.RampDaysTotal)
	}
	wantPct := float64(15-5) / float64(50-5) * 100
	if mathAbs(p.OverallPct-wantPct) > 0.1 {
		t.Fatalf("overall pct %v want %v", p.OverallPct, wantPct)
	}
}

func TestScheduleDailyCapRampsWithStartedAt(t *testing.T) {
	today := time.Now().UTC()
	start := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC).Add(-3 * 24 * time.Hour)
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        20,
		WarmupTargetDailyCap:  100,
		WarmupIncrementPerDay: 20,
		DailyLimit:            250,
		WarmupStartedAt:       &start,
	}
	cap := scheduleDailyCap(acc)
	if cap != 80 { // 20 + 3*20
		t.Fatalf("cap=%d want 80", cap)
	}
}

func TestScheduleDailyCapNilStartedStaysAtStart(t *testing.T) {
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

func TestWarmupCalendarDaysElapsedCrossesMidnight(t *testing.T) {
	// Started yesterday afternoon; "now" is this morning — calendar day 1, not hour-based day 0.
	started := time.Date(2026, 8, 11, 15, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	if got := warmupCalendarDaysElapsed(started, now); got != 1 {
		t.Fatalf("days=%d want 1 (calendar), Hours/24 would be 0", got)
	}
	if got := warmupCalendarDaysElapsed(started, started); got != 0 {
		t.Fatalf("same day days=%d want 0", got)
	}
	twoDaysLater := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	if got := warmupCalendarDaysElapsed(started, twoDaysLater); got != 2 {
		t.Fatalf("days=%d want 2", got)
	}
}

func mathAbs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
