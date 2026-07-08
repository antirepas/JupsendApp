package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestResolveCampaignSendTemplateWorkflowAB(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wfmap-ab@test.com", "hash", "http://localhost")

	// templates
	var templateAID, templateBID, templateFollowID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('A','s','b', ?) RETURNING id`, userID).Scan(&templateAID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('B','s','b', ?) RETURNING id`, userID).Scan(&templateBID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('Follow','s','b', ?) RETURNING id`, userID).Scan(&templateFollowID)

	wid, _ := CreateWorkflow(userID, "wfmap-ab", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	// workflow: start -> send1 -> wait -> send2
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email 1", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait", ConfigJSON: `{"duration_seconds":86400}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Email 2", ConfigJSON: `{"template_id":2}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "send2", EdgeType: "default"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	campaignID, err := CreateCampaign(userID, "ab + workflow", templateAID, templateBID, "workflow_ab", vid, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Map only send2; send1 is chosen from TemplateA/TemplateB
	if err := SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send2": templateFollowID}); err != nil {
		t.Fatal(err)
	}

	gotA, err := ResolveCampaignSendTemplate(campaignID, "send1", "A", vid)
	if err != nil {
		t.Fatal(err)
	}
	if gotA != templateAID {
		t.Fatalf("send1 variant A: got %d want %d", gotA, templateAID)
	}

	gotB, err := ResolveCampaignSendTemplate(campaignID, "send1", "B", vid)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != templateBID {
		t.Fatalf("send1 variant B: got %d want %d", gotB, templateBID)
	}

	gotFollow, err := ResolveCampaignSendTemplate(campaignID, "send2", "A", vid)
	if err != nil {
		t.Fatal(err)
	}
	if gotFollow != templateFollowID {
		t.Fatalf("send2: got %d want %d", gotFollow, templateFollowID)
	}

	c, _ := GetCampaignForUser(campaignID, userID)
	if err := ValidateCampaignWorkflowReady(c); err != nil {
		t.Fatalf("expected workflow_ab to be ready: %v", err)
	}
}

func TestValidateCampaignWorkflowReadyWorkflow(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wfmap-workflow@test.com", "hash", "http://localhost")

	var templateAID, templateBID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('A','s','b', ?) RETURNING id`, userID).Scan(&templateAID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('B','s','b', ?) RETURNING id`, userID).Scan(&templateBID)

	wid, _ := CreateWorkflow(userID, "wfmap-workflow", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email 1", ConfigJSON: `{}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Email 2", ConfigJSON: `{}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "send2", EdgeType: "default"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	campaignID, err := CreateCampaign(userID, "workflow", templateAID, 0, "workflow", vid, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Missing mapping for send1 should fail
	if err := SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send2": templateBID}); err != nil {
		t.Fatal(err)
	}
	c, _ := GetCampaignForUser(campaignID, userID)
	if err := ValidateCampaignWorkflowReady(c); err == nil {
		t.Fatal("expected ValidateCampaignWorkflowReady to fail without send1 mapping")
	}

	// Add mapping for all send nodes should pass
	if err := SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send1": templateAID, "send2": templateBID}); err != nil {
		t.Fatal(err)
	}
	c, _ = GetCampaignForUser(campaignID, userID)
	if err := ValidateCampaignWorkflowReady(c); err != nil {
		t.Fatalf("expected workflow to be ready: %v", err)
	}
}

func TestListSendEmailStepsUsesDisplayReachability(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wfmap-display@test.com", "hash", "http://localhost")

	wid, _ := CreateWorkflow(userID, "wfmap-display", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "First", ConfigJSON: "{}"},
			{NodeKey: "cond", NodeType: "condition_engagement", Label: "Opened?", ConfigJSON: `{"predicate":"opened"}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Follow-up", ConfigJSON: "{}"},
			{NodeKey: "hidden", NodeType: "action_send_email", Label: "Hidden", ConfigJSON: "{}"},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "cond", EdgeType: "default"},
			{SourceNodeKey: "cond", TargetNodeKey: "send2", EdgeType: "true"},
			{SourceNodeKey: "cond", TargetNodeKey: "end", EdgeType: "false"},
			{SourceNodeKey: "cond", TargetNodeKey: "hidden", EdgeType: "default"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	steps, err := ListSendEmailSteps(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 display-reachable send steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].NodeKey != "send1" || steps[1].NodeKey != "send2" {
		t.Fatalf("unexpected steps: %+v", steps)
	}
}

