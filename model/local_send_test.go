package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func TestResolveContactTimezoneFromIANA(t *testing.T) {
	tz := ResolveContactTimezone([]ContactVariables{
		{Key: "timezone", Value: "America/Los_Angeles"},
		{Key: "country", Value: "US"},
	}, "Europe/London")
	if tz != "America/Los_Angeles" {
		t.Fatalf("got %s", tz)
	}
}

func TestResolveContactTimezoneAliasAndCountry(t *testing.T) {
	if got := ResolveContactTimezone([]ContactVariables{{Key: "tz", Value: "PST"}}, ""); got != "America/Los_Angeles" {
		t.Fatalf("alias=%s", got)
	}
	if got := ResolveContactTimezone([]ContactVariables{{Key: "country", Value: "Germany"}}, ""); got != "Europe/Berlin" {
		t.Fatalf("country=%s", got)
	}
	if got := ResolveContactTimezone(nil, "bogus"); got != DefaultLocalSendFallbackTZ {
		t.Fatalf("fallback=%s", got)
	}
	if got := ResolveContactTimezone(nil, "Asia/Tokyo"); got != "Asia/Tokyo" {
		t.Fatalf("explicit fallback=%s", got)
	}
}

func TestNextLocalSendAt(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	after := time.Date(2026, 6, 15, 8, 0, 0, 0, loc)
	got := NextLocalSendAt(after, loc, 9, 0)
	want := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("same day: got %v want %v", got, want)
	}
	after = time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got = NextLocalSendAt(after, loc, 9, 0)
	want = time.Date(2026, 6, 16, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("next day: got %v want %v", got, want)
	}
}

func TestParseAndFormatLocalSendTime(t *testing.T) {
	m, ok := ParseLocalSendTime("09:30")
	if !ok || m != 9*60+30 {
		t.Fatalf("parse=%d ok=%v", m, ok)
	}
	if LocalSendTimeString(540) != "09:00" {
		t.Fatalf("format=%s", LocalSendTimeString(540))
	}
}

func TestCampaignLocalSendAtAndSnap(t *testing.T) {
	db.OpenTestDB(t)
	config.BaseURL = "http://localhost"
	config.AdminEmails = nil

	email := fmt.Sprintf("localsend-%d@test.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	c := &Contact{Email: fmt.Sprintf("lead-%d@co.com", time.Now().UnixNano())}
	contactID, err := c.SaveContact(userID, []ContactVariables{
		{Key: "timezone", Value: "America/New_York"},
	})
	if err != nil {
		t.Fatal(err)
	}

	camp := Campaign{
		LocalSendEnabled:    true,
		LocalSendMinute:     9 * 60,
		LocalSendFallbackTZ: "UTC",
	}
	loc, _ := time.LoadLocation("America/New_York")
	after := time.Date(2026, 3, 10, 8, 0, 0, 0, loc)
	at, ok := CampaignLocalSendAt(camp, []ContactVariables{{Key: "timezone", Value: "America/New_York"}}, after)
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 3, 10, 9, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Fatalf("got %v want %v", at, want)
	}

	floor := time.Date(2026, 3, 13, 15, 0, 0, 0, loc)
	snapped := SnapLocalSendWake(camp, contactID, floor)
	wantSnap := time.Date(2026, 3, 14, 9, 0, 0, 0, loc)
	if !snapped.Equal(wantSnap) {
		t.Fatalf("snap got %v want %v", snapped, wantSnap)
	}

	camp.LocalSendEnabled = false
	if got := SnapLocalSendWake(camp, contactID, floor); !got.Equal(floor) {
		t.Fatalf("disabled should keep floor, got %v", got)
	}
}

func TestSetCampaignLocalSend(t *testing.T) {
	db.OpenTestDB(t)
	config.BaseURL = "http://localhost"
	config.AdminEmails = nil

	email := fmt.Sprintf("localsend-camp-%d@test.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	var tplID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t', 's', 'b', ?) RETURNING id`, userID).Scan(&tplID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "Local send camp", tplID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetCampaignLocalSend(campID, userID, true, 10*60+30, "Europe/Berlin"); err != nil {
		t.Fatal(err)
	}
	c, err := GetCampaign(campID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.LocalSendEnabled || c.LocalSendMinute != 630 || c.LocalSendFallbackTZ != "Europe/Berlin" {
		t.Fatalf("%+v", c)
	}
}
