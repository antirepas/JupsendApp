package model

import (
	"fmt"
	"testing"

	"emailtracker.com/db"
)

func TestDescribeConditionEngagementLastSend(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Intro", ConfigJSON: `{"template_id":1}`},
		},
	}
	desc := DescribeConditionEngagement(map[string]interface{}{
		"predicate": "has_opened",
		"params":    map[string]interface{}{"email_send_scope": "last_in_workflow"},
	}, graph)
	if desc != "If opened for the most recent email sent in this workflow" {
		t.Fatalf("got %q", desc)
	}
}

func TestDescribeConditionEngagementNotOpenedWait(t *testing.T) {
	desc := DescribeConditionEngagement(map[string]interface{}{
		"predicate": "has_not_opened",
		"params": map[string]interface{}{
			"email_send_scope": "last_in_workflow",
			"wait_days":        float64(3),
		},
	}, WorkflowGraph{})
	if desc != "If still did not open after 3 days for the most recent email sent in this workflow" {
		t.Fatalf("got %q", desc)
	}
}

func TestDescribeConditionEngagementLinkedNode(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Follow up", ConfigJSON: `{"template_id":2}`},
		},
	}
	desc := DescribeConditionEngagement(map[string]interface{}{
		"predicate": "click_count_gte",
		"params": map[string]interface{}{
			"email_send_scope": "node",
			"email_node_key":   "send1",
			"min":              float64(2),
		},
	}, graph)
	want := `If clicked at least 2 times for "Follow up"`
	if desc != want {
		t.Fatalf("got %q want %q", desc, want)
	}
}

func TestValidateConditionEmailRef(t *testing.T) {
	types := map[string]string{
		"send1": "action_send_email",
		"wait1": "action_wait",
	}
	n := WorkflowNode{
		NodeKey:    "cond1",
		NodeType:   "condition_engagement",
		ConfigJSON: `{"predicate":"has_opened","params":{"email_send_scope":"node","email_node_key":"send1"}}`,
	}
	if msg := validateConditionEmailRef(n, types); msg != "" {
		t.Fatalf("unexpected: %s", msg)
	}
	n.ConfigJSON = `{"predicate":"has_opened","params":{"email_send_scope":"node","email_node_key":"wait1"}}`
	if msg := validateConditionEmailRef(n, types); msg == "" {
		t.Fatal("expected error for non-send node ref")
	}
}

func TestGetSendIDForInstanceNode(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("wf-sendref@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	wid, err := CreateWorkflow(userID, "ref", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campaignID, err := CreateCampaign(userID, "c", templateID, 0, "workflow", w.CurrentVersionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c := Contact{Email: "sendref@test.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := CreateWorkflowInstance(w.CurrentVersionID, contactID, campaignID, "send1")
	if err != nil {
		t.Fatal(err)
	}
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, "track-sendref", campaignID, "", instID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateExecution(instID, "send1", fmt.Sprintf("exec-%d-send1", instID), "succeeded", fmt.Sprintf(`{"email_send_id":%d}`, sendID), "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := GetSendIDForInstanceNode(instID, "send1")
	if err != nil {
		t.Fatal(err)
	}
	if got != sendID {
		t.Fatalf("got send %d want %d", got, sendID)
	}
}

func TestValidateWorkflowGraphConditionEmailRef(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("wf-condval@test.com", "hash", "http://localhost")
	wid, _ := CreateWorkflow(userID, "cond", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID
	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Send", ConfigJSON: `{"template_id":1}`},
			{NodeKey: "cond", NodeType: "condition_engagement", Label: "Opened?", ConfigJSON: `{"predicate":"has_opened","params":{"email_send_scope":"node","email_node_key":"send"}}`},
			{NodeKey: "end_y", NodeType: "action_end", Label: "End Y", ConfigJSON: "{}"},
			{NodeKey: "end_n", NodeType: "action_end", Label: "End N", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "cond", EdgeType: "default"},
			{SourceNodeKey: "cond", TargetNodeKey: "end_y", EdgeType: "true"},
			{SourceNodeKey: "cond", TargetNodeKey: "end_n", EdgeType: "false"},
		},
	})
	errs := ValidateWorkflowGraph(vid)
	if len(errs) > 0 {
		t.Fatalf("expected valid graph, got %v", errs)
	}
}
