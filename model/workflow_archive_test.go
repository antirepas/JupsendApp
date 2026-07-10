package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestArchiveWorkflow(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-arch@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "to-archive", "")

	if err := ArchiveWorkflow(wid, userID, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	w, err := GetWorkflowForUser(wid, userID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "archived" {
		t.Fatalf("status = %q", w.Status)
	}
	if err := ArchiveWorkflow(wid, userID, false); err == nil {
		t.Fatal("expected error archiving twice")
	}
}

func TestGetWorkflowArchivePreview(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-prev@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "preview", "")

	preview, err := GetWorkflowArchivePreview(wid, userID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.WorkflowName != "preview" {
		t.Fatalf("name = %q", preview.WorkflowName)
	}
}

func TestArchiveWorkflowCancelsQueuedJobs(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-cancel@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "cancel-test", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	contact := Contact{Email: "wf-cancel@example.com"}
	cid, _ := contact.SaveContact(userID, nil)
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	instID, _ := CreateWorkflowInstance(vid, cid, 0, "start")
	jobID, _ := CreateSendJob(SendJob{
		UserID:             userID,
		ContactID:          cid,
		TemplateID:         templateID,
		WorkflowInstanceID: instID,
		Status:             "pending",
	})

	if err := ArchiveWorkflow(wid, userID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	job, err := GetSendJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status = %q", job.Status)
	}
}
