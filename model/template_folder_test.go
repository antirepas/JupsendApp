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
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	unfiledItems, err := ListTemplatesFiltered(userID, "unfiled")
	if err != nil || len(unfiledItems) != 1 || unfiledItems[0].ID != unfiledID {
		t.Fatalf("unfiled=%+v err=%v", unfiledItems, err)
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
	if err != nil || allCount != 3 || unfiledCount != 3 {
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
