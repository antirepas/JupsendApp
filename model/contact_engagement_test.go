package model

import (
	"fmt"
	"testing"

	"emailtracker.com/db"
)

func TestEnrichContactsEngagement(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("eng-enrich@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	c := Contact{Email: "eng@test.com"}
	cid, _ := c.SaveContact(userID, nil)
	campaignID, _ := CreateCampaign(userID, "Eng Camp", templateID, 0, "bulk", 0, "", "")
	sendID, err := enqueueTestSend(userID, templateID, cid, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, created_at)
		VALUES (?, ?, 'open', CURRENT_TIMESTAMP)
	`, sendID, fmt.Sprintf("track-test-%d", cid))
	if err != nil {
		t.Fatal(err)
	}

	eng, err := EnrichContactsEngagement(userID, []int64{cid})
	if err != nil {
		t.Fatal(err)
	}
	got := eng[cid]
	if got.LastCampaignID != campaignID {
		t.Fatalf("campaign=%d want %d", got.LastCampaignID, campaignID)
	}
	if got.LastCampaignName != "Eng Camp" {
		t.Fatalf("name=%q", got.LastCampaignName)
	}
	if got.LastSignal != "open" {
		t.Fatalf("signal=%q", got.LastSignal)
	}
	if got.LastActivity == nil {
		t.Fatal("expected last activity")
	}
}

func TestListContactsFilteredEngagementAndCampaign(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("eng-filter@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	opened := Contact{Email: "opened@test.com"}
	openedID, _ := opened.SaveContact(userID, nil)
	quiet := Contact{Email: "quiet@test.com"}
	quietID, _ := quiet.SaveContact(userID, nil)
	replied := Contact{Email: "replied@test.com"}
	repliedID, _ := replied.SaveContact(userID, nil)
	_, _ = db.Exec(`UPDATE contact SET replied_at = CURRENT_TIMESTAMP WHERE id = ?`, repliedID)

	campaignID, _ := CreateCampaign(userID, "Filter Camp", templateID, 0, "bulk", 0, "", "")
	sendID, err := enqueueTestSend(userID, templateID, openedID, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, created_at)
		VALUES (?, ?, 'open', CURRENT_TIMESTAMP)
	`, sendID, fmt.Sprintf("track-test-%d", openedID))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = enqueueTestSend(userID, templateID, quietID, campaignID)

	page, err := ListContactsFiltered(userID, ContactListFilter{Engagement: "opened_no_reply", PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !contactIDsContain(page.Items, openedID) {
		t.Fatal("expected opened contact in opened_no_reply")
	}
	if contactIDsContain(page.Items, quietID) {
		t.Fatal("quiet contact should not match opened_no_reply")
	}
	if contactIDsContain(page.Items, repliedID) {
		t.Fatal("replied contact should not match opened_no_reply")
	}

	campPage, err := ListContactsFiltered(userID, ContactListFilter{CampaignID: campaignID, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !contactIDsContain(campPage.Items, openedID) || !contactIDsContain(campPage.Items, quietID) {
		t.Fatal("campaign filter should include sent contacts")
	}
	if contactIDsContain(campPage.Items, repliedID) {
		t.Fatal("replied-only contact not in campaign")
	}
}

func TestDismissInterestedContacts(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("dismiss@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	c := Contact{Email: "dismiss-me@test.com"}
	cid, _ := c.SaveContact(userID, nil)
	campaignID, _ := CreateCampaign(userID, "Dismiss Camp", templateID, 0, "bulk", 0, "", "")
	sendID, err := enqueueTestSend(userID, templateID, cid, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, created_at)
		VALUES (?, ?, 'click', CURRENT_TIMESTAMP)
	`, sendID, fmt.Sprintf("track-test-%d", cid))
	if err != nil {
		t.Fatal(err)
	}

	before, err := ListInterestedContacts(userID, 10)
	if err != nil || len(before) == 0 {
		t.Fatalf("expected interested before dismiss: %v len=%d", err, len(before))
	}

	n, err := DismissInterestedContacts(userID, []int64{cid})
	if err != nil || n != 1 {
		t.Fatalf("dismiss n=%d err=%v", n, err)
	}
	after, err := ListInterestedContacts(userID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range after {
		if item.ContactID == cid {
			t.Fatal("dismissed contact still in interested queue")
		}
	}
}

func TestListContactsInListFilteredAndSchema(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("list-ops@test.com", "hash", "http://localhost")
	listID, err := CreateContactList(userID, "Ops List")
	if err != nil {
		t.Fatal(err)
	}

	a := Contact{Email: "alice@test.com"}
	aID, _ := a.SaveContact(userID, []ContactVariables{{Key: "first_name", Value: "Alice"}})
	b := Contact{Email: "bob@test.com"}
	bID, _ := b.SaveContact(userID, []ContactVariables{{Key: "first_name", Value: "Bob"}})

	if err := AddContactsToList(listID, userID, []int64{aID, bID}); err != nil {
		t.Fatal(err)
	}
	if err := SetListVariableSchema(listID, userID, ParseSchemaKeysInput("first_name, company")); err != nil {
		t.Fatal(err)
	}

	schema, err := GetListVariableSchema(listID, userID)
	if err != nil || len(schema) != 2 {
		t.Fatalf("schema=%v err=%v", schema, err)
	}

	_, rows, cols, err := ListContactsInListFiltered(listID, userID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != aID {
		t.Fatalf("search rows=%+v", rows)
	}
	if len(cols) != 2 {
		t.Fatalf("cols=%v", cols)
	}
	if rows[0].Variables != nil && len(rows[0].Variables) > 0 {
		t.Fatalf("list detail should not load variables, got %+v", rows[0].Variables)
	}

	page, err := ListContactsInListPage(listID, userID, ListMembersFilter{Page: 1, PageSize: 1, Sort: "email"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 {
		t.Fatalf("pagination page=%+v items=%d", page, len(page.Items))
	}

	if err := RemoveContactFromList(listID, userID, aID); err != nil {
		t.Fatal(err)
	}
	_, rows2, _, err := ListContactsInListFiltered(listID, userID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 1 || rows2[0].ID != bID {
		t.Fatalf("after remove rows=%+v", rows2)
	}
}

func contactIDsContain(items []ContactListItem, id int64) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}
