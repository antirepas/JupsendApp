package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestSequenceStepsFromOutreachWorkflow(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf2seq@test.com", "hash", "http://localhost")
	tpl1, err := (&Template{Name: "A", Subject: "s1", Body: "b1"}).SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	tpl2, err := (&Template{Name: "B", Subject: "s2", Body: "b2"}).SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	wid, _ := CreateWorkflow(userID, "outreach", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "First", ConfigJSON: "{}"},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait", ConfigJSON: `{"duration_seconds":259200}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Follow", ConfigJSON: "{}"},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "send2", EdgeType: "default"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})
	_ = PublishWorkflowVersion(wid, vid)

	campID, err := CreateCampaign(userID, "c", tpl1, 0, "workflow", vid, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = SaveCampaignWorkflowTemplates(campID, map[string]int64{"send1": tpl1, "send2": tpl2})

	steps, err := SequenceStepsFromOutreachWorkflow(vid, campID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps: %+v", len(steps), steps)
	}
	if steps[0].TemplateID != 0 || steps[0].DelaySecondsBefore != 0 {
		t.Fatalf("first step: %+v", steps[0])
	}
	if steps[1].TemplateID != tpl2 || steps[1].DelaySecondsBefore != 259200 {
		t.Fatalf("second step: %+v (want tpl=%d wait=259200)", steps[1], tpl2)
	}
}

func TestMigrateLegacyWorkflowCampaignsToSequences(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf2seq-mig@test.com", "hash", "http://localhost")
	tpl1, err := (&Template{Name: "A", Subject: "s", Body: "b"}).SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	tpl2, err := (&Template{Name: "B", Subject: "s", Body: "b"}).SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	wid, _ := CreateWorkflow(userID, "outreach-mig", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "First", ConfigJSON: "{}"},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait", ConfigJSON: `{"duration_seconds":86400}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Follow", ConfigJSON: "{}"},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "wait", EdgeType: "default"},
			{SourceNodeKey: "wait", TargetNodeKey: "send2", EdgeType: "default"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})
	_ = PublishWorkflowVersion(wid, vid)
	campID, err := CreateCampaign(userID, "legacy", tpl1, tpl2, "workflow_ab", vid, "subject", "hypothesis")
	if err != nil {
		t.Fatal(err)
	}
	_ = SaveCampaignWorkflowTemplates(campID, map[string]int64{"send1": tpl1, "send2": tpl2})

	MigrateLegacyWorkflowCampaignsToSequences()

	c, err := GetCampaignForUser(campID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ExecutionMode != "sequence_ab" {
		t.Fatalf("mode=%s", c.ExecutionMode)
	}
	if c.WorkflowVersionID != 0 {
		t.Fatalf("workflow still bound: %d", c.WorkflowVersionID)
	}
	steps, _ := ListCampaignSequenceSteps(campID)
	if len(steps) != 2 {
		t.Fatalf("steps=%d", len(steps))
	}
}
