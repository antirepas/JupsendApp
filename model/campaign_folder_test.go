package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestCreateLibraryFolderCreatesCampaign(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("camp-folder-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	folderID, err := CreateLibraryFolder(userID, "Spring Outreach")
	if err != nil {
		t.Fatal(err)
	}
	c, err := GetCampaignByLibraryFolder(folderID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Spring Outreach" {
		t.Fatalf("campaign name=%q", c.Name)
	}
	if c.LibraryFolderID != folderID {
		t.Fatalf("library_folder_id=%d want %d", c.LibraryFolderID, folderID)
	}
	if c.Status != "draft" {
		t.Fatalf("status=%q", c.Status)
	}
}

func TestSyncCampaignFromFolderOrderedDefaults(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("camp-sync-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	folderID, err := CreateLibraryFolder(userID, "Sync Camp")
	if err != nil {
		t.Fatal(err)
	}
	camp, err := GetCampaignByLibraryFolder(folderID, userID)
	if err != nil {
		t.Fatal(err)
	}

	t1 := Template{Name: "A", Subject: "A", Body: "<p>A</p>", FolderID: folderID}
	idA, err := t1.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t2 := Template{Name: "B", Subject: "B", Body: "<p>B</p>", FolderID: folderID}
	idB, err := t2.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t3 := Template{Name: "Extra", Subject: "X", Body: "<p>X</p>", FolderID: folderID}
	if _, err := t3.SaveTemplate(userID, nil); err != nil {
		t.Fatal(err)
	}

	listID, err := CreateLibraryContactSheet(userID, "Audience", folderID)
	if err != nil {
		t.Fatal(err)
	}
	contact := Contact{Email: fmt.Sprintf("lead-%d@ex.com", time.Now().UnixNano())}
	cid, err := contact.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddContactsToList(listID, userID, []int64{cid}); err != nil {
		t.Fatal(err)
	}

	wid, err := CreateLibraryWorkflow(userID, "Seq", folderID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := GetWorkflowForUser(wid, userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveWorkflowGraph(w.CurrentVersionID, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: "{}"},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "end", EdgeType: "default"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := PublishWorkflowVersion(wid, w.CurrentVersionID); err != nil {
		t.Fatal(err)
	}

	if err := SyncCampaignFromFolder(userID, camp.ID); err != nil {
		t.Fatal(err)
	}
	got, err := GetCampaignForUser(camp.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TemplateAID != idA || got.TemplateBID != idB {
		t.Fatalf("templates A=%d B=%d want A=%d B=%d", got.TemplateAID, got.TemplateBID, idA, idB)
	}
	if got.ContactListID != listID {
		t.Fatalf("contact_list_id=%d want %d", got.ContactListID, listID)
	}
	members, _ := GetCampaignContactIDs(camp.ID)
	if len(members) != 1 || members[0] != cid {
		t.Fatalf("members=%v", members)
	}
	pub, _ := LatestPublishedVersionID(wid)
	if got.WorkflowVersionID != pub || got.ExecutionMode != "workflow_ab" {
		t.Fatalf("mode=%s wf=%d want workflow_ab / %d", got.ExecutionMode, got.WorkflowVersionID, pub)
	}

	nav, err := BuildCampaignWorkspaceNav(userID, folderID, camp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nav) < 5 {
		t.Fatalf("nav len=%d want >=5 (manage+3tpl+sheet+wf)", len(nav))
	}
	if nav[0].Kind != "manage" {
		t.Fatalf("first=%s", nav[0].Kind)
	}
}

func TestListContactsInLibraryFolder(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("camp-prev-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := CreateLibraryFolder(userID, "Preview Camp")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := CreateLibraryContactSheet(userID, "Sheet", folderID)
	if err != nil {
		t.Fatal(err)
	}
	in := Contact{Email: fmt.Sprintf("in-%d@ex.com", time.Now().UnixNano())}
	inID, err := in.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := Contact{Email: fmt.Sprintf("out-%d@ex.com", time.Now().UnixNano())}
	if _, err := out.SaveContact(userID, nil); err != nil {
		t.Fatal(err)
	}
	_ = AddContactsToList(listID, userID, []int64{inID})

	contacts, err := ListContactsInLibraryFolder(userID, folderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].ID != inID {
		t.Fatalf("contacts=%+v", contacts)
	}
}

func TestRenameFolderRenamesCampaign(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("camp-rename-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := CreateLibraryFolder(userID, "Old Name")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameLibraryFolder(folderID, userID, "New Name"); err != nil {
		t.Fatal(err)
	}
	c, err := GetCampaignByLibraryFolder(folderID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "New Name" {
		t.Fatalf("name=%q", c.Name)
	}
}
