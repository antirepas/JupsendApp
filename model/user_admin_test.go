package model

import (
	"testing"

	"emailtracker.com/config"
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

func TestUserHasAppAccess(t *testing.T) {
	config.AdminEmails = nil
	admin := User{Email: "a@test.com", IsAdmin: true}
	if !UserHasAppAccess(admin) {
		t.Fatal("admin should have access")
	}
	sub := User{Email: "b@test.com", SubscriptionStatus: SubStatusActive}
	if !UserHasAppAccess(sub) {
		t.Fatal("subscriber should have access")
	}
	none := User{Email: "c@test.com", SubscriptionStatus: SubStatusNone}
	if UserHasAppAccess(none) {
		t.Fatal("regular user should not have access")
	}
}
