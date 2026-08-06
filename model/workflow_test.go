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

func TestComputeDisplayStatus(t *testing.T) {
	if ComputeDisplayStatus("sent", nil, false) != "sent" {
		t.Fatal("expected sent")
	}
	if ComputeDisplayStatus("draft", nil, true) != "sending" {
		t.Fatal("expected sending")
	}
}

func TestEnsureEditableWorkflowVersionForksAfterPublish(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-fork@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "forkable", "")
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
	if err := PublishWorkflowVersion(wid, vid); err != nil {
		t.Fatal(err)
	}
	published, err := GetWorkflowVersion(vid)
	if err != nil || published.Status != "published" {
		t.Fatalf("want published, got %+v err=%v", published, err)
	}

	draftID, forked, err := EnsureEditableWorkflowVersion(wid)
	if err != nil {
		t.Fatal(err)
	}
	if !forked {
		t.Fatal("expected fork after publish")
	}
	if draftID == vid {
		t.Fatal("draft should be a new version id")
	}
	draft, err := GetWorkflowVersion(draftID)
	if err != nil || draft.Status != "draft" {
		t.Fatalf("want draft, got %+v err=%v", draft, err)
	}
	// Original published row unchanged.
	stillPub, _ := GetWorkflowVersion(vid)
	if stillPub.Status != "published" {
		t.Fatalf("published version mutated: %s", stillPub.Status)
	}
	g, err := GetWorkflowGraph(draftID)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 || len(g.Edges) != 2 {
		t.Fatalf("copied graph nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
	}
	// Save on published still blocked.
	if err := SaveWorkflowGraph(vid, GraphSaveInput{Nodes: nil, Edges: nil}); err == nil {
		t.Fatal("expected error saving published version")
	}
	// Save on draft works.
	if err := SaveWorkflowGraph(draftID, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Send", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait", ConfigJSON: `{"duration_hours":24}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "end", EdgeType: "default"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := PublishWorkflowVersion(wid, draftID); err != nil {
		t.Fatal(err)
	}
	old, _ := GetWorkflowVersion(vid)
	if old.Status != "archived" {
		t.Fatalf("previous published should be archived, got %s", old.Status)
	}
	newPub, _ := GetWorkflowVersion(draftID)
	if newPub.Status != "published" {
		t.Fatalf("new version should be published, got %s", newPub.Status)
	}
	w2, _ := GetWorkflow(wid)
	if w2.CurrentVersionID != draftID {
		t.Fatalf("current_version_id=%d want %d", w2.CurrentVersionID, draftID)
	}

	// While editing a new draft, campaign picker still returns the published version.
	draft2, forked2, err := EnsureEditableWorkflowVersion(wid)
	if err != nil || !forked2 {
		t.Fatalf("second fork: id=%d forked=%v err=%v", draft2, forked2, err)
	}
	pubs, err := GetPublishedWorkflows(userID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pw := range pubs {
		if pw.ID == wid {
			found = true
			if pw.CurrentVersionID != draftID {
				t.Fatalf("picker version=%d want published %d (not draft %d)", pw.CurrentVersionID, draftID, draft2)
			}
		}
	}
	if !found {
		t.Fatal("workflow missing from GetPublishedWorkflows")
	}
}
