package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestValidateWorkflowGraphEmpty(t *testing.T) {
	db.Prepare()
	wid, _ := CreateWorkflow("test", "")
	w, _ := GetWorkflow(wid)
	errs := ValidateWorkflowGraph(w.CurrentVersionID)
	if len(errs) == 0 { //comment
		t.Fatal("expected validation errors for empty graph")
	}
}

func TestValidateWorkflowGraphValid(t *testing.T) {
	db.Prepare()
	wid, _ := CreateWorkflow("test2", "")
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
	if ComputeDisplayStatus("sent", nil) != "sent" {
		t.Fatal("expected sent")
	}
}
