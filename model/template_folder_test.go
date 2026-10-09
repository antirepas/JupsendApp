package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestTemplateFoldersCRUDAndFilter(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("tpl-folder-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := CreateUser(fmt.Sprintf("other-%d@example.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	folderID, err := CreateTemplateFolder(userID, " Outreach ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTemplateFolder(userID, "outreach"); err == nil {
		t.Fatal("expected unique name error")
	}

	if err := RenameTemplateFolder(folderID, userID, "Cold outreach"); err != nil {
		t.Fatal(err)
	}
	f, err := GetTemplateFolderForUser(folderID, userID)
	if err != nil || f.Name != "Cold outreach" {
		t.Fatalf("rename: %+v err=%v", f, err)
	}

	otherFolder, err := CreateTemplateFolder(otherID, "Other")
	if err != nil {
		t.Fatal(err)
	}

	unfiled := Template{Name: "U", Subject: "s", Body: "b"}
	unfiledID, err := unfiled.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	inFolder := Template{Name: "F", Subject: "s", Body: "b", FolderID: folderID}
	inFolderID, err := inFolder.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Template{Name: "X", Subject: "s", Body: "b", FolderID: otherFolder}).SaveTemplate(userID, nil); err == nil {
		t.Fatal("expected reject other user's folder")
	}

	all, err := ListTemplatesFiltered(userID, "all")
	// CreateTemplateFolder may create an unfiled placeholder template for the linked campaign.
	if err != nil || len(all) < 2 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	var foundUnfiled, foundFolder bool
	for _, it := range all {
		if it.ID == unfiledID {
			foundUnfiled = true
		}
		if it.ID == inFolderID {
			foundFolder = true
		}
	}
	if !foundUnfiled || !foundFolder {
		t.Fatalf("missing expected templates in all=%+v", all)
	}
	unfiledItems, err := ListTemplatesFiltered(userID, "unfiled")
	if err != nil {
		t.Fatal(err)
	}
	foundUnfiled = false
	for _, it := range unfiledItems {
		if it.ID == unfiledID {
			foundUnfiled = true
			break
		}
	}
	if !foundUnfiled {
		t.Fatalf("unfiled=%+v missing %d", unfiledItems, unfiledID)
	}
	folderItems, err := ListTemplatesFiltered(userID, fmt.Sprintf("%d", folderID))
	if err != nil || len(folderItems) != 1 || folderItems[0].ID != inFolderID {
		t.Fatalf("folder=%+v err=%v", folderItems, err)
	}
	if folderItems[0].FolderName != "Cold outreach" {
		t.Fatalf("folder name=%q", folderItems[0].FolderName)
	}

	if err := UpdateTemplate(unfiledID, userID, "U2", "s2", "b2", nil, folderID); err != nil {
		t.Fatal(err)
	}
	st, _ := GetTemplate(unfiledID)
	if st.FolderID != folderID {
		t.Fatalf("moved folder_id=%d", st.FolderID)
	}

	dupID, err := DuplicateTemplate(userID, inFolderID, "Copy")
	if err != nil {
		t.Fatal(err)
	}
	dup, _ := GetTemplate(dupID)
	if dup.FolderID != folderID {
		t.Fatalf("dup folder_id=%d", dup.FolderID)
	}

	if err := DeleteTemplateFolder(folderID, userID); err != nil {
		t.Fatal(err)
	}
	st, _ = GetTemplate(inFolderID)
	if st.FolderID != 0 {
		t.Fatalf("expected unfiled after delete, got %d", st.FolderID)
	}
	allCount, unfiledCount, err := CountTemplatesForUser(userID)
	// stub (from folder→campaign) + U + F + dup
	if err != nil || allCount != 4 || unfiledCount != 4 {
		t.Fatalf("counts all=%d unfiled=%d err=%v", allCount, unfiledCount, err)
	}
}

func TestParseFolderIDForm(t *testing.T) {
	if ParseFolderIDForm("") != 0 || ParseFolderIDForm("0") != 0 {
		t.Fatal("empty should be 0")
	}
	if ParseFolderIDForm("42") != 42 {
		t.Fatal("parse 42")
	}
}

func TestMoveCopyRenameTemplates(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("tpl-ops-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := CreateTemplateFolder(userID, "Ops")
	if err != nil {
		t.Fatal(err)
	}
	a := Template{Name: "Alpha", Subject: "s", Body: "b"}
	aid, err := a.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := Template{Name: "Beta", Subject: "s", Body: "b"}
	bid, err := b.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	n, err := MoveTemplatesToFolder(userID, []int64{aid, bid}, folderID)
	if err != nil || n != 2 {
		t.Fatalf("move n=%d err=%v", n, err)
	}
	got, _ := GetTemplate(aid)
	if got.FolderID != folderID {
		t.Fatalf("folder_id=%d", got.FolderID)
	}

	n, err = CopyTemplatesToFolder(userID, []int64{aid}, 0)
	if err != nil || n != 1 {
		t.Fatalf("copy n=%d err=%v", n, err)
	}
	unfiled, err := ListTemplatesFiltered(userID, "unfiled")
	if err != nil {
		t.Fatal(err)
	}
	foundCopy := false
	for _, it := range unfiled {
		if it.Name == "Alpha (copy)" {
			foundCopy = true
			break
		}
	}
	if !foundCopy {
		t.Fatalf("unfiled=%+v missing Alpha (copy)", unfiled)
	}

	if err := RenameTemplate(aid, userID, "Alpha Renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ = GetTemplate(aid)
	if got.Name != "Alpha Renamed" {
		t.Fatalf("name=%q", got.Name)
	}

	n, err = DeleteTemplates(userID, []int64{bid})
	if err != nil || n != 1 {
		t.Fatalf("delete n=%d err=%v", n, err)
	}
}
