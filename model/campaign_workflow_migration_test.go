package model

import (
	"strconv"
	"testing"

	"emailtracker.com/db"
)

func TestMigrateLegacyCampaignWorkflowStepTemplates(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("migrate@test.com", "hash", "http://localhost")

	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('Legacy','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	wid, _ := CreateWorkflow(userID, "legacy-wf", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: `{"template_id":` + strconv.FormatInt(templateID, 10) + `}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	campaignID, _ := CreateCampaign(userID, "legacy", templateID, 0, "workflow", vid, "", "")

	MigrateLegacyCampaignWorkflowStepTemplates()

	mappings, err := GetCampaignWorkflowTemplates(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if mappings["send1"] != templateID {
		t.Fatalf("expected migrated template %d for send1, got %d", templateID, mappings["send1"])
	}

	_ = SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send1": templateID})
	MigrateLegacyCampaignWorkflowStepTemplates()
	mappings2, _ := GetCampaignWorkflowTemplates(campaignID)
	if len(mappings2) != 1 {
		t.Fatalf("expected 1 mapping after re-migrate, got %d", len(mappings2))
	}
}
