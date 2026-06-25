package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestSummarizeWorkflowSteps(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-sum@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "summary test", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "First email", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait 3 days", ConfigJSON: `{"duration_seconds":259200}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	steps := SummarizeWorkflowSteps(vid)
	if len(steps) != 2 {
		t.Fatalf("expected 2 summary steps, got %v", steps)
	}
	if steps[0] != "First email: Template #1" {
		t.Fatalf("unexpected first step: %q", steps[0])
	}
	if steps[1] != "Wait 3 days" {
		t.Fatalf("unexpected wait step: %q", steps[1])
	}
}

func TestNodeLabelForKey(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-label@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "label test", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Outreach", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "end", EdgeType: "default"},
		},
	})
	if got := NodeLabelForKey(vid, "send"); got != "Outreach" {
		t.Fatalf("got %q", got)
	}
}
