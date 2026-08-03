package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestRemoveContactsFromCampaign(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("camp-remove@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','Hi {{name}}','b', ?) RETURNING id`, userID).Scan(&templateID)
	campaignID, _ := CreateCampaign(userID, "Remove Camp", templateID, 0, "bulk", 0, "", "")

	a := Contact{Email: "keep@test.com"}
	aID, _ := a.SaveContact(userID, nil)
	b := Contact{Email: "drop@test.com"}
	bID, _ := b.SaveContact(userID, nil)
	_ = AddContactsToCampaign(campaignID, []int64{aID, bID})

	n, err := RemoveContactsFromCampaign(campaignID, []int64{bID})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	ids, _ := GetCampaignContactIDs(campaignID)
	if len(ids) != 1 || ids[0] != aID {
		t.Fatalf("ids=%v", ids)
	}
}
