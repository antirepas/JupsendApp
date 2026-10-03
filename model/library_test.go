package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestLibraryFoldersAndMixedItems(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("lib-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	folderID, err := CreateLibraryFolder(userID, "Q1")
	if err != nil {
		t.Fatal(err)
	}

	tpl := Template{Name: "T1", Subject: "Hi", Body: "Body", FolderID: folderID}
	tid, err := tpl.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wid, err := CreateLibraryWorkflow(userID, "W1", folderID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := CreateLibraryContactSheet(userID, "Leads", folderID)
	if err != nil {
		t.Fatal(err)
	}

	items, err := ListLibraryItems(userID, fmt.Sprintf("%d", folderID))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	n, err := MoveLibraryItems(userID, []LibraryRef{
		{Kind: LibraryKindTemplate, ID: tid},
		{Kind: LibraryKindWorkflow, ID: wid},
		{Kind: LibraryKindContacts, ID: sid},
	}, 0)
	if err != nil || n != 3 {
		t.Fatalf("move n=%d err=%v", n, err)
	}
	unfiled, err := ListLibraryItems(userID, "unfiled")
	if err != nil || len(unfiled) < 3 {
		t.Fatalf("unfiled=%d err=%v", len(unfiled), err)
	}

	n, err = CopyLibraryItems(userID, []LibraryRef{{Kind: LibraryKindContacts, ID: sid}}, folderID)
	if err != nil || n != 1 {
		t.Fatalf("copy n=%d err=%v", n, err)
	}

	if err := RenameLibraryItem(userID, LibraryKindTemplate, tid, "T1 renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := GetTemplate(tid)
	if got.Name != "T1 renamed" {
		t.Fatalf("name=%q", got.Name)
	}

	if err := DeleteLibraryFolder(folderID, userID); err != nil {
		t.Fatal(err)
	}
}

func TestLibrarySheetCellAndPaste(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("sheet-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateLibraryContactSheet(userID, "Sheet", 0)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := AddSheetRow(userID, listID, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateSheetCell(userID, listID, cid, "company", "Acme"); err != nil {
		t.Fatal(err)
	}
	schema, err := GetListVariableSchema(listID, userID)
	if err != nil || len(schema) == 0 {
		t.Fatalf("schema=%v err=%v", schema, err)
	}
	n, err := PasteSheetRows(userID, listID, []string{"email", "company"}, [][]string{
		{"b@example.com", "Beta"},
	})
	if err != nil || n != 1 {
		t.Fatalf("paste n=%d err=%v", n, err)
	}
	page, err := ListContactsInListPage(listID, userID, ListMembersFilter{Page: 1, PageSize: 50})
	if err != nil || page.Total < 2 {
		t.Fatalf("page total=%d err=%v", page.Total, err)
	}
}
