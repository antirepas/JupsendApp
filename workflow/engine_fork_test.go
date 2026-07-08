package workflow

import (
	"testing"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

type stubMailer struct{}

func (stubMailer) SendWorkflowEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	return 1, nil
}

func TestEngineForksOnMultipleDefaultEdges(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := model.CreateUser("fork@test.com", "hash", "http://localhost")

	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('T','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	wid, _ := model.CreateWorkflow(userID, "fork-wf", "")
	w, _ := model.GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = model.SaveWorkflowGraph(vid, model.GraphSaveInput{
		Nodes: []model.WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: "{}"},
			{NodeKey: "wait_a", NodeType: "action_wait", Label: "Wait A", ConfigJSON: `{"duration_seconds":3600}`},
			{NodeKey: "wait_b", NodeType: "action_wait", Label: "Wait B", ConfigJSON: `{"duration_seconds":7200}`},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []model.WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "wait_a", EdgeType: "default", Priority: 1},
			{SourceNodeKey: "send1", TargetNodeKey: "wait_b", EdgeType: "default", Priority: 10},
			{SourceNodeKey: "wait_a", TargetNodeKey: "end", EdgeType: "default"},
			{SourceNodeKey: "wait_b", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	campaignID, _ := model.CreateCampaign(userID, "fork campaign", templateID, 0, "workflow", vid, "", "")
	_ = model.SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send1": templateID})

	var contactID int64
	_ = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('fork@example.com', ?) RETURNING id`, userID).Scan(&contactID)

	entry, _ := model.GetEntryNodeKey(vid)
	rootID, err := model.CreateWorkflowInstance(vid, contactID, campaignID, entry)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(stubMailer{})
	if ok, _ := model.ClaimInstance(rootID); !ok {
		t.Fatal("could not claim root instance")
	}
	if err := engine.ProcessInstance(rootID); err != nil {
		t.Fatalf("process instance: %v", err)
	}

	instances, err := model.ListInstancesForCampaign(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances after fork, got %d", len(instances))
	}

	priorities := map[int]bool{}
	var forkCount int
	for _, inst := range instances {
		priorities[inst.BranchPriority] = true
		if inst.ForkRootID != nil && *inst.ForkRootID == rootID {
			forkCount++
		}
	}
	if forkCount != 1 {
		t.Fatalf("expected 1 forked sibling, got %d", forkCount)
	}
	if !priorities[1] || !priorities[10] {
		t.Fatalf("expected branch priorities 1 and 10, got %#v", priorities)
	}
}

func TestReplyCancelsForkSiblings(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := model.CreateUser("fork-cancel@test.com", "hash", "http://localhost")

	wid, _ := model.CreateWorkflow(userID, "fork-cancel", "")
	w, _ := model.GetWorkflow(wid)
	vid := w.CurrentVersionID

	var contactID int64
	_ = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('fc@example.com', ?) RETURNING id`, userID).Scan(&contactID)

	campaignID, _ := model.CreateCampaign(userID, "c", 1, 0, "workflow", vid, "", "")

	rootID, _ := model.CreateWorkflowInstance(vid, contactID, campaignID, "start")
	forkID, _ := model.CreateForkedWorkflowInstance(vid, contactID, campaignID, rootID, "wait_b", "{}", 10)

	if err := model.CancelActiveInstancesForContact(contactID); err != nil {
		t.Fatal(err)
	}

	root, _ := model.GetWorkflowInstance(rootID)
	fork, _ := model.GetWorkflowInstance(forkID)
	if root.Status != "cancelled" || fork.Status != "cancelled" {
		t.Fatalf("expected both cancelled, got root=%s fork=%s", root.Status, fork.Status)
	}
}
