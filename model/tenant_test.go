package model

import (
	"database/sql"
	"testing"

	"emailtracker.com/db"
)

func TestTenantIsolationCampaign(t *testing.T) {
	mem, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.DB = mem
	db.CreateTables()
	defer mem.Close()

	u1, err := CreateUser("tenant-a-isolated@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := CreateUser("tenant-b-isolated@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.DB.Exec(`INSERT INTO template (name, subject, body, user_id) VALUES ('t', 's', 'b', ?)`, u1)
	if err != nil {
		t.Fatal(err)
	}

	id1, err := CreateCampaign(u1, "A campaign", 1, 0, "bulk", 0)
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	_, err = GetCampaignForUser(id1, u2)
	if err == nil {
		t.Fatal("user B should not read user A campaign")
	}

	_, err = GetCampaignForUser(id1, u1)
	if err != nil {
		t.Fatalf("user A should read own campaign: %v", err)
	}
}
