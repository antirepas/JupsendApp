package model

import "testing"

func TestShouldPromoteWarmupThreshold(t *testing.T) {
	// 70% of 20 = 14
	if !ShouldPromoteWarmup(14, 20) {
		t.Fatal("14/20 should promote")
	}
	if ShouldPromoteWarmup(13, 20) {
		t.Fatal("13/20 should not promote")
	}
	if !ShouldPromoteWarmup(4, 5) {
		t.Fatal("4/5 should promote")
	}
	if ShouldPromoteWarmup(3, 5) {
		t.Fatal("3/5 should not promote")
	}
}

func TestNextWarmupCapAfterDay(t *testing.T) {
	next, ok := NextWarmupCapAfterDay(20, 14, 20, 100, 250)
	if !ok || next != 40 {
		t.Fatalf("got %d promoted=%v want 40 true", next, ok)
	}
	held, ok := NextWarmupCapAfterDay(20, 5, 20, 100, 250)
	if ok || held != 20 {
		t.Fatalf("idle hold: got %d promoted=%v want 20 false", held, ok)
	}
	capped, ok := NextWarmupCapAfterDay(90, 90, 20, 100, 250)
	if !ok || capped != 100 {
		t.Fatalf("target clamp: got %d promoted=%v", capped, ok)
	}
	atTarget, ok := NextWarmupCapAfterDay(100, 100, 20, 100, 250)
	if ok || atTarget != 100 {
		t.Fatalf("already full: got %d promoted=%v", atTarget, ok)
	}
}

func TestEffectiveWarmupCurrentCapFallsBackToStart(t *testing.T) {
	acc := SMTPAccount{WarmupDailyCap: 20, WarmupTargetDailyCap: 100, DailyLimit: 250}
	if got := EffectiveWarmupCurrentCap(acc); got != 20 {
		t.Fatalf("got %d", got)
	}
	acc.WarmupCurrentCap = 60
	if got := EffectiveWarmupCurrentCap(acc); got != 60 {
		t.Fatalf("got %d", got)
	}
}
