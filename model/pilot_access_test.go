package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func TestGrantPilotAccessCreatesProUser(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = nil
	config.BaseURL = "http://localhost"

	email := fmt.Sprintf("pilot-new-%d@test.com", time.Now().UnixNano())
	userID, created, err := GrantPilotAccess(email, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created=true")
	}
	u, err := GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	if NormalizePlanTier(u.PlanTier) != PlanTierPro {
		t.Fatalf("plan_tier=%s", u.PlanTier)
	}
	if u.SubscriptionStatus != SubStatusActive {
		t.Fatalf("subscription_status=%s", u.SubscriptionStatus)
	}
	if u.SubscriptionEndsAt == nil || !u.SubscriptionEndsAt.After(time.Now().Add(50*24*time.Hour)) {
		t.Fatalf("ends_at=%v", u.SubscriptionEndsAt)
	}
	if !UserHasAppAccess(u) || !UserIsPro(userID) {
		t.Fatal("pilot should have Pro app access")
	}
}

func TestGrantPilotAccessUpgradesExisting(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = nil
	config.BaseURL = "http://localhost"

	email := fmt.Sprintf("pilot-up-%d@test.com", time.Now().UnixNano())
	id, err := CreateUser(email, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlanLimitsToUser(id, PlanTierFree); err != nil {
		t.Fatal(err)
	}

	userID, created, err := GrantPilotAccess(email, 30)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected created=false for existing user")
	}
	if userID != id {
		t.Fatalf("userID=%d want %d", userID, id)
	}
	u, _ := GetUserByID(id)
	if NormalizePlanTier(u.PlanTier) != PlanTierPro || u.SubscriptionStatus != SubStatusActive {
		t.Fatalf("tier=%s status=%s", u.PlanTier, u.SubscriptionStatus)
	}
}

func TestGrantPilotAccessRejectsAdmin(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = map[string]struct{}{"pilot-admin@test.com": {}}
	config.BaseURL = "http://localhost"

	_, _, err := GrantPilotAccess("pilot-admin@test.com", 60)
	if err == nil {
		t.Fatal("expected error for admin email")
	}

	email := fmt.Sprintf("pilot-flag-%d@test.com", time.Now().UnixNano())
	id, err := CreateUser(email, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetUserAdmin(id, true); err != nil {
		t.Fatal(err)
	}
	_, _, err = GrantPilotAccess(email, 60)
	if err == nil {
		t.Fatal("expected error for admin account")
	}
}
