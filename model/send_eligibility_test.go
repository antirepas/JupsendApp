package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestContactListSnapshot(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("list-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	listID, err := CreateContactList(userID, "Test list")
	if err != nil {
		t.Fatal(err)
	}

	c := Contact{Email: "member@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddContactsToList(listID, userID, []int64{cid}); err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}
	campaignID, err := CreateCampaign(userID, "List campaign", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	n, err := SnapshotListToCampaign(listID, campaignID, userID)
	if err != nil || n != 1 {
		t.Fatalf("snapshot n=%d err=%v", n, err)
	}
	ids, err := GetCampaignContactIDs(campaignID)
	if err != nil || len(ids) != 1 || ids[0] != cid {
		t.Fatalf("campaign contacts: %v err=%v", ids, err)
	}
}

func TestFilterSendEligibleCooldown(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("elig-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_ = UpdateUserSendCooldownDays(userID, 30)

	c := Contact{Email: "cool@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, sent_at, delivery_status)
		VALUES (?, ?, ?, ?, ?, 'sent')
	`, userID, cid, templateID, fmt.Sprintf("track-elig-cooldown-%d", time.Now().UnixNano()), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	campaignID, err := CreateCampaign(userID, "Elig campaign", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	eligible, skipped, err := FilterSendEligible(userID, campaignID, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 0 || len(skipped) != 1 || skipped[0].Reason != SkipReasonCooldown {
		t.Fatalf("eligible=%v skipped=%+v", eligible, skipped)
	}
}

func TestFilterSendEligibleCooldownDisabled(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("elig-zero@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_ = UpdateUserSendCooldownDays(userID, 0)

	days, err := GetUserSendCooldownDays(userID)
	if err != nil || days != 0 {
		t.Fatalf("expected cooldown 0, got %d err=%v", days, err)
	}

	c := Contact{Email: "cool-zero@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, sent_at, delivery_status)
		VALUES (?, ?, ?, ?, ?, 'sent')
	`, userID, cid, templateID, fmt.Sprintf("track-elig-zero-%d", time.Now().UnixNano()), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	campaignID, err := CreateCampaign(userID, "Elig zero campaign", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	eligible, skipped, err := FilterSendEligible(userID, campaignID, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 || len(skipped) != 0 {
		t.Fatalf("expected eligible with cooldown off, eligible=%v skipped=%+v", eligible, skipped)
	}
}

func TestFilterSendEligibleCooldownIgnoresQueuedOnly(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("elig-queued@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_ = UpdateUserSendCooldownDays(userID, 30)

	c := Contact{Email: "queued-only@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, delivery_status)
		VALUES (?, ?, ?, ?, 'queued')
	`, userID, cid, templateID, fmt.Sprintf("track-elig-queued-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}

	campaignID, err := CreateCampaign(userID, "Elig queued campaign", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	eligible, skipped, err := FilterSendEligible(userID, campaignID, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 || len(skipped) != 0 {
		t.Fatalf("queued-only send should not trigger cooldown, eligible=%v skipped=%+v", eligible, skipped)
	}
}

func TestFilterSendEligibleActiveCampaign(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("active-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	c := Contact{Email: "active@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}

	campA, err := CreateCampaign(userID, "Campaign A", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddContactsToCampaign(campA, []int64{cid}); err != nil {
		t.Fatal(err)
	}

	campB, err := CreateCampaign(userID, "Campaign B", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	eligible, skipped, err := FilterSendEligible(userID, campB, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 0 || len(skipped) != 1 || skipped[0].Reason != SkipReasonActiveCampaign {
		t.Fatalf("eligible=%v skipped=%+v", eligible, skipped)
	}
}

func TestFilterSendEligibleInvalidEmail(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("invalid-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	c := Contact{Email: "bad@example.com"}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = SetContactEmailStatus(cid, "invalid", "no mx")

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}
	campaignID, err := CreateCampaign(userID, "Camp", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	eligible, skipped, err := FilterSendEligible(userID, campaignID, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 0 || len(skipped) != 1 || skipped[0].Reason != SkipReasonInvalidEmail {
		t.Fatalf("eligible=%v skipped=%+v", eligible, skipped)
	}
}
