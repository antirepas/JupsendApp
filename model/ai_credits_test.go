package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestConsumeAICreditsN(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("credits-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	cap := AICreditsCapForTier(PlanTierFree)
	gotCap, rem, ok := ConsumeAICredits(userID, 5)
	if !ok || gotCap != cap || rem != cap-5 {
		t.Fatalf("consume 5: cap=%d rem=%d ok=%v", gotCap, rem, ok)
	}

	gotCap, rem, ok = ConsumeAICredits(userID, 1)
	if !ok || rem != cap-6 {
		t.Fatalf("consume 1: rem=%d ok=%v", rem, ok)
	}

	gotCap, rem, ok = ConsumeAICredits(userID, 0)
	if !ok || rem != cap-6 {
		t.Fatalf("consume 0 should be no-op: rem=%d ok=%v", rem, ok)
	}

	_, rem, ok = ConsumeAICredits(userID, cap)
	if ok {
		t.Fatalf("expected fail when n > remaining; rem=%d", rem)
	}
	if rem != cap-6 {
		t.Fatalf("remaining should be unchanged on fail: %d", rem)
	}
}
