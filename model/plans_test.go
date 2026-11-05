package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestPaidPlanSpecs(t *testing.T) {
	cases := []struct {
		tier     PlanTier
		emails   int
		contacts int
		price    int
	}{
		{PlanTierStarter, 10000, 5000, 23},
		{PlanTierGrowth, 25000, 10000, 49},
		{PlanTierScale, 50000, 20000, 64},
		{PlanTierHyper, 100000, 25000, 87},
	}
	for _, tc := range cases {
		spec, err := PlanSpecForTier(tc.tier)
		if err != nil {
			t.Fatalf("%s: %v", tc.tier, err)
		}
		if spec.MonthlyEmailCap != tc.emails || spec.MaxContacts != tc.contacts || spec.MonthlyPriceUSD != tc.price {
			t.Fatalf("%s: emails=%d contacts=%d price=%d", tc.tier, spec.MonthlyEmailCap, spec.MaxContacts, spec.MonthlyPriceUSD)
		}
		if spec.DailyEmailCap != 0 {
			t.Fatalf("%s daily cap should be 0", tc.tier)
		}
		if !IsPaidPlanTier(tc.tier) {
			t.Fatalf("%s should be paid", tc.tier)
		}
	}
	if NormalizePlanTier("pro") != PlanTierStarter {
		t.Fatal("legacy pro should map to starter")
	}
	if NormalizePlanTier("standard") != PlanTierStarter {
		t.Fatal("legacy standard should map to starter")
	}
}

func TestAssertCanCreateContactRespectsCap(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("plan-contacts-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlanLimitsToUser(userID, PlanTierStarter); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE users SET plan_tier='starter', subscription_status='active' WHERE id=?`, userID)

	for i := 0; i < 3; i++ {
		c := Contact{Email: fmt.Sprintf("c%d-%d@ex.com", i, time.Now().UnixNano())}
		if _, err := c.SaveContact(userID, nil); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	n, err := CountUserContacts(userID)
	if err != nil || n != 3 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	if err := AssertCanCreateContact(userID); err != nil {
		t.Fatalf("under cap should allow: %v", err)
	}
}

func TestUserMonthlyEmailRemaining(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("plan-month-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE users SET plan_tier='starter', subscription_status='active' WHERE id=?`, userID)

	cap, rem, ok := UserMonthlyEmailRemaining(userID)
	if !ok || cap != 10000 || rem != 10000 {
		t.Fatalf("cap=%d rem=%d ok=%v", cap, rem, ok)
	}
	if !UserUnderMonthlyEmailCap(userID) {
		t.Fatal("expected under cap")
	}
}
