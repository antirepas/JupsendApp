package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestValidateWorkflowGraphEmpty(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-empty@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "test", "")
	w, _ := GetWorkflow(wid)
	errs := ValidateWorkflowGraph(w.CurrentVersionID)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for empty graph")
	}
}

func TestValidateWorkflowGraphValid(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-valid@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "test2", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Send", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "end", EdgeType: "default"},
		},
	})
	errs := ValidateWorkflowGraph(vid)
	if len(errs) > 0 {
		t.Fatalf("expected valid graph, got: %v", errs)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-del@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "to-delete", "")
	if err := DeleteWorkflow(wid, userID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := GetWorkflowForUser(wid, userID)
	if err == nil {
		t.Fatal("expected workflow to be gone")
	}
	if err := DeleteWorkflow(wid, userID); err == nil {
		t.Fatal("expected error deleting missing workflow")
	}
}

func TestComputeDisplayStatus(t *testing.T) {
	if ComputeDisplayStatus("sent", nil, false) != "sent" {
		t.Fatal("expected sent")
	}
	if ComputeDisplayStatus("draft", nil, true) != "sending" {
		t.Fatal("expected sending")
	}
}
