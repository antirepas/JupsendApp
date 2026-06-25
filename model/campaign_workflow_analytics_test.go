package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestGetCampaignWorkflowStepDisplay(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-display@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "display test", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Outreach", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait 2 days", ConfigJSON: `{"duration_seconds":172800}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	steps, err := GetCampaignWorkflowStepDisplay(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].NodeKey != "send" || steps[0].StepIndex != 1 {
		t.Fatalf("first step: %+v", steps[0])
	}
	if steps[0].Description == "" {
		t.Fatal("expected description on send step")
	}
}

func TestGetCampaignWorkflowAnalyticsRequiresWorkflowCampaign(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-analytics@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	bulkID, _ := CreateCampaign(userID, "Bulk", templateID, 0, "bulk", 0)
	_, err := GetCampaignWorkflowAnalytics(bulkID, userID)
	if err == nil {
		t.Fatal("expected error for bulk campaign")
	}

	wid, _ := CreateWorkflow(userID, "wf", "")
	w, _ := GetWorkflow(wid)
	campaignID, _ := CreateCampaign(userID, "WF", templateID, 0, "workflow", w.CurrentVersionID)
	c := Contact{Email: "wf-analytics-contact@test.com"}
	cid, _ := c.SaveContact(userID, nil)
	_ = AddContactsToCampaign(campaignID, []int64{cid})

	analytics, err := GetCampaignWorkflowAnalytics(campaignID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.CampaignName != "WF" {
		t.Fatalf("got %q", analytics.CampaignName)
	}
	if analytics.Overview.TotalContacts != 1 {
		t.Fatalf("contacts=%d", analytics.Overview.TotalContacts)
	}
	if len(analytics.Steps) == 0 {
		t.Fatal("expected workflow steps in analytics")
	}
}

func TestDescribeWorkflowStepWaitHours(t *testing.T) {
	desc := describeWorkflowStep(WorkflowNode{
		NodeType:   "action_wait",
		ConfigJSON: `{"duration_seconds":7200}`,
	})
	if desc != "Pauses for 2 hours" {
		t.Fatalf("got %q", desc)
	}
}
