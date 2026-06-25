package model

import (
	"fmt"
	"testing"

	"emailtracker.com/db"
)

func TestGetCampaignWorkflowOverview(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("wf-overview@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	wid, err := CreateWorkflow(userID, "overview test", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	vid := w.CurrentVersionID

	err = SaveWorkflowGraph(vid, GraphSaveInput{
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
	if err != nil {
		t.Fatal(err)
	}

	var templateID int64
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}
	campaignID, err := CreateCampaign(userID, "WF campaign", templateID, 0, "workflow", vid)
	if err != nil {
		t.Fatal(err)
	}

	var contactIDs []int64
	for i := 0; i < 5; i++ {
		c := Contact{Email: fmt.Sprintf("wf%d@test.com", i)}
		cid, err := c.SaveContact(userID, nil)
		if err != nil {
			t.Fatal(err)
		}
		contactIDs = append(contactIDs, cid)
	}
	if err := AddContactsToCampaign(campaignID, contactIDs); err != nil {
		t.Fatal(err)
	}

	// active at send
	inst1, err := CreateWorkflowInstance(vid, contactIDs[0], campaignID, "send")
	if err != nil {
		t.Fatal(err)
	}
	// waiting at wait
	inst2, err := CreateWorkflowInstance(vid, contactIDs[1], campaignID, "wait")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE workflow_instances SET status = 'waiting' WHERE id = ?`, inst2)
	if err != nil {
		t.Fatal(err)
	}
	// completed at end
	inst3, err := CreateWorkflowInstance(vid, contactIDs[2], campaignID, "end")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE workflow_instances SET status = 'completed' WHERE id = ?`, inst3)
	if err != nil {
		t.Fatal(err)
	}
	// cancelled
	inst4, err := CreateWorkflowInstance(vid, contactIDs[3], campaignID, "send")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE workflow_instances SET status = 'cancelled' WHERE id = ?`, inst4)
	if err != nil {
		t.Fatal(err)
	}
	// contactIDs[4] has no instance (not started)

	_, err = CreateExecution(inst1, "send", fmt.Sprintf("exec-%d", inst1), "succeeded", "{}", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateExecution(inst3, "send", fmt.Sprintf("exec-%d-send", inst3), "succeeded", "{}", "")
	if err != nil {
		t.Fatal(err)
	}

	overview, err := GetCampaignWorkflowOverview(campaignID, vid, 5)
	if err != nil {
		t.Fatal(err)
	}

	if overview.TotalContacts != 5 {
		t.Fatalf("TotalContacts=%d want 5", overview.TotalContacts)
	}
	if overview.NotStarted != 1 {
		t.Fatalf("NotStarted=%d want 1", overview.NotStarted)
	}
	if overview.InProgress != 2 {
		t.Fatalf("InProgress=%d want 2", overview.InProgress)
	}
	if overview.Completed != 1 {
		t.Fatalf("Completed=%d want 1", overview.Completed)
	}
	if overview.Cancelled != 1 {
		t.Fatalf("Cancelled=%d want 1", overview.Cancelled)
	}
	if len(overview.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(overview.Steps))
	}
	if overview.Steps[0].NodeKey != "send" || overview.Steps[0].ContactsHere != 1 {
		t.Fatalf("send step: %+v", overview.Steps[0])
	}
	if overview.Steps[0].PassedThrough != 2 {
		t.Fatalf("send PassedThrough=%d want 2", overview.Steps[0].PassedThrough)
	}
	if overview.Steps[1].NodeKey != "wait" || overview.Steps[1].ContactsHere != 1 {
		t.Fatalf("wait step: %+v", overview.Steps[1])
	}
	if overview.Steps[2].NodeKey != "end" {
		t.Fatalf("end step key=%q", overview.Steps[2].NodeKey)
	}
}

func TestOrderedWorkflowDisplayNodes(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "start", NodeType: "trigger_campaign_started"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Email"},
			{NodeKey: "wait", NodeType: "action_wait", Label: "Wait"},
			{NodeKey: "cond", NodeType: "condition_engagement", Label: "Opened?"},
			{NodeKey: "end", NodeType: "action_end", Label: "Done"},
		},
		Edges: []WorkflowEdge{
			{SourceNodeKey: "start", TargetNodeKey: "send"},
			{SourceNodeKey: "send", TargetNodeKey: "wait"},
			{SourceNodeKey: "wait", TargetNodeKey: "cond"},
			{SourceNodeKey: "cond", TargetNodeKey: "end"},
		},
	}
	ordered := orderedWorkflowDisplayNodes(graph)
	if len(ordered) != 4 {
		t.Fatalf("got %d nodes", len(ordered))
	}
	want := []string{"send", "wait", "cond", "end"}
	for i, key := range want {
		if ordered[i].NodeKey != key {
			t.Fatalf("step %d: got %q want %q", i, ordered[i].NodeKey, key)
		}
	}
}
