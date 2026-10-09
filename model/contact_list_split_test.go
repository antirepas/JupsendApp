package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestSplitContactListByVariable(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("split-var@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateContactList(userID, "Source split")
	if err != nil {
		t.Fatal(err)
	}
	_ = SetListVariableSchema(listID, userID, []string{"tier"})

	a, err := (&Contact{Email: "a-split@example.com"}).SaveContact(userID, []ContactVariables{{Key: "tier", Value: "hot"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := (&Contact{Email: "b-split@example.com"}).SaveContact(userID, []ContactVariables{{Key: "tier", Value: "cold"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := (&Contact{Email: "c-split@example.com"}).SaveContact(userID, []ContactVariables{{Key: "tier", Value: "hot"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := AddContactsToList(listID, userID, []int64{a, b, c}); err != nil {
		t.Fatal(err)
	}

	result, err := SplitContactList(userID, listID, "Hot tier", "Cold tier", ListMembersFilter{
		VarKey: "tier", VarOp: "equals", VarValue: "hot",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchCount != 2 || result.RestCount != 1 {
		t.Fatalf("match=%d rest=%d", result.MatchCount, result.RestCount)
	}
	matchIDs, _ := ListMemberContactIDs(result.MatchListID, userID)
	if len(matchIDs) != 2 {
		t.Fatalf("match members=%d", len(matchIDs))
	}
	src, _ := GetContactListForUser(listID, userID)
	if src.MemberCount != 3 {
		t.Fatalf("source should keep members, got %d", src.MemberCount)
	}
}

func TestThinContactListRemovesPercent(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("thin-list-%d@example.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateContactList(userID, "Thin source")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 10; i++ {
		id, err := (&Contact{Email: fmt.Sprintf("thin-%d@example.com", i)}).SaveContact(userID, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := AddContactsToList(listID, userID, ids); err != nil {
		t.Fatal(err)
	}
	result, err := ThinContactList(userID, listID, 30, ListMembersFilter{}, false, "Holdout")
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedCount != 3 {
		t.Fatalf("removed=%d want 3", result.RemovedCount)
	}
	if result.MovedListID == 0 {
		t.Fatal("expected moved list")
	}
	left, _ := ListMemberContactIDs(listID, userID)
	if len(left) != 7 {
		t.Fatalf("left=%d want 7", len(left))
	}
	moved, _ := ListMemberContactIDs(result.MovedListID, userID)
	if len(moved) != 3 {
		t.Fatalf("moved=%d want 3", len(moved))
	}
}

func TestListMembersVariableFilter(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("var-filter@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateContactList(userID, "Var filter list")
	if err != nil {
		t.Fatal(err)
	}
	_ = SetListVariableSchema(listID, userID, []string{"company"})
	id1, _ := (&Contact{Email: "vf1@example.com"}).SaveContact(userID, []ContactVariables{{Key: "company", Value: "Acme"}})
	id2, _ := (&Contact{Email: "vf2@example.com"}).SaveContact(userID, []ContactVariables{{Key: "company", Value: "Beta"}})
	_ = AddContactsToList(listID, userID, []int64{id1, id2})

	page, err := ListContactsInListPage(listID, userID, ListMembersFilter{
		VarKey: "company", VarOp: "equals", VarValue: "Acme", PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Email != "vf1@example.com" {
		t.Fatalf("got total=%d items=%v", page.Total, page.Items)
	}
	if page.Items[0].Variables["company"] != "Acme" {
		t.Fatalf("variables=%v", page.Items[0].Variables)
	}
}

func TestImportContactRowsSegment(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("import-seg@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	rows := []ImportContactRow{
		{Email: "seg1@example.com", Variables: map[string]string{"segment": "Interested"}, Segment: "Interested", EmailStatus: "valid"},
		{Email: "seg2@example.com", Variables: map[string]string{"segment": "Not interested"}, Segment: "Not interested", EmailStatus: "valid"},
		{Email: "seg3@example.com", Variables: map[string]string{"segment": "Interested"}, Segment: "Interested", EmailStatus: "valid"},
	}
	result, err := ImportContactRows(userID, rows, 0, []string{"segment"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 3 {
		t.Fatalf("created=%d", result.Created)
	}
	hotID, err := FindContactListByName(userID, "Interested")
	if err != nil {
		t.Fatal(err)
	}
	coldID, err := FindContactListByName(userID, "Not interested")
	if err != nil {
		t.Fatal(err)
	}
	hotMembers, _ := ListMemberContactIDs(hotID, userID)
	coldMembers, _ := ListMemberContactIDs(coldID, userID)
	if len(hotMembers) != 2 || len(coldMembers) != 1 {
		t.Fatalf("hot=%d cold=%d", len(hotMembers), len(coldMembers))
	}
}

func TestSaveMatchingAsList(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("save-match@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateContactList(userID, "Save match source")
	if err != nil {
		t.Fatal(err)
	}
	_ = SetListVariableSchema(listID, userID, []string{"status"})
	id1, _ := (&Contact{Email: "sm1@example.com"}).SaveContact(userID, []ContactVariables{{Key: "status", Value: "yes"}})
	id2, _ := (&Contact{Email: "sm2@example.com"}).SaveContact(userID, []ContactVariables{{Key: "status", Value: "no"}})
	_ = AddContactsToList(listID, userID, []int64{id1, id2})

	newID, count, err := SaveMatchingAsList(userID, listID, "Yes only", ListMembersFilter{
		VarKey: "status", VarOp: "equals", VarValue: "yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
	members, _ := ListMemberContactIDs(newID, userID)
	if len(members) != 1 || members[0] != id1 {
		t.Fatalf("members=%v", members)
	}
}
