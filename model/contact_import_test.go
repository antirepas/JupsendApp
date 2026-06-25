package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestFormatImportResultMessage(t *testing.T) {
	msg := FormatImportResultMessage(ImportContactsResult{Created: 3, Updated: 2, Skipped: 1})
	if msg == "" || msg == "No contacts imported" {
		t.Fatalf("unexpected message: %q", msg)
	}
}

func TestFormatSkipBreakdown(t *testing.T) {
	b := FormatSkipBreakdown(map[string]int{
		SkipReasonCooldown:       5,
		SkipReasonActiveCampaign: 2,
	})
	if b == "" {
		t.Fatal("expected breakdown text")
	}
}

func TestImportContactRows(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("import-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	rows := []ImportContactRow{
		{Email: "new@example.com", Variables: map[string]string{"name": "New"}},
	}
	r1, err := ImportContactRows(userID, rows, 0)
	if err != nil || r1.Created != 1 {
		t.Fatalf("create: %+v err=%v", r1, err)
	}

	rows2 := []ImportContactRow{
		{Email: "new@example.com", Variables: map[string]string{"name": "Updated"}},
	}
	r2, err := ImportContactRows(userID, rows2, 0)
	if err != nil || r2.Updated != 1 {
		t.Fatalf("update: %+v err=%v", r2, err)
	}
}
