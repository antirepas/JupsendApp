package model

import (
	"testing"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func TestUserIsAdmin(t *testing.T) {
	config.AdminEmails = map[string]struct{}{"owner@example.com": {}}
	u := User{Email: "owner@example.com", IsAdmin: false}
	if !UserIsAdmin(u) {
		t.Fatal("expected env admin email")
	}
	u2 := User{Email: "other@example.com", IsAdmin: true}
	if !UserIsAdmin(u2) {
		t.Fatal("expected db admin flag")
	}
}

func TestUserIsProTreatsAdminAsPro(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = map[string]struct{}{"admin-pro@test.com": {}}
	userID, err := CreateUser("admin-pro@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_ = ApplyPlanLimitsToUser(userID, PlanTierFree)
	if !UserIsPro(userID) {
		t.Fatal("admin must be treated as Pro even on free plan_tier")
	}
	u, _ := GetUserByID(userID)
	if !UserIsAdmin(u) {
		t.Fatal("expected admin flag from ADMIN_EMAILS / CreateUser")
	}
}

func TestUserHasAppAccess(t *testing.T) {
	config.AdminEmails = nil
	admin := User{Email: "a@test.com", IsAdmin: true}
	if !UserHasAppAccess(admin) {
		t.Fatal("admin should have access")
	}
	sub := User{Email: "b@test.com", SubscriptionStatus: SubStatusActive, PlanTier: "pro"}
	if !UserHasAppAccess(sub) {
		t.Fatal("subscriber should have access")
	}
	proNone := User{Email: "c@test.com", SubscriptionStatus: SubStatusNone, PlanTier: "pro"}
	if UserHasAppAccess(proNone) {
		t.Fatal("pro without active subscription should not have access")
	}
	free := User{Email: "d@test.com", SubscriptionStatus: SubStatusNone, PlanTier: "free"}
	if !UserHasAppAccess(free) {
		t.Fatal("free plan should have access")
	}
}
