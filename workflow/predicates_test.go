package workflow

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

func TestPickNextNode(t *testing.T) {
	adj := map[string][]model.WorkflowEdge{
		"a": {{SourceNodeKey: "a", TargetNodeKey: "b", EdgeType: "true"}},
	}
	k, ok := pickNextNode(adj, "a", "true")
	if !ok || k != "b" {
		t.Fatalf("expected b, got %s ok=%v", k, ok)
	}
}

func TestPickNextNodeDefault(t *testing.T) {
	adj := map[string][]model.WorkflowEdge{
		"a": {{SourceNodeKey: "a", TargetNodeKey: "c", EdgeType: "default"}},
	}
	k, ok := pickNextNode(adj, "a", "default")
	if !ok || k != "c" {
		t.Fatalf("expected c, got %s", k)
	}
}

func TestEvaluateConditionBySendNode(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser("pred-node@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	wid, err := model.CreateWorkflow(userID, "pred", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := model.GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campaignID, err := model.CreateCampaign(userID, "c", templateID, 0, "workflow", w.CurrentVersionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "pred@test.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := model.CreateWorkflowInstance(w.CurrentVersionID, contactID, campaignID, "send1")
	if err != nil {
		t.Fatal(err)
	}
	sendID, err := model.CreateQueuedEmailSend(userID, templateID, contactID, "track-pred", campaignID, "", instID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.CreateExecution(instID, "send1", fmt.Sprintf("exec-%d", instID), "succeeded", fmt.Sprintf(`{"email_send_id":%d}`, sendID), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.InsertContactEvent(model.ContactEventInput{
		ContactID:   contactID,
		EmailSendID: sendID,
		EventType:   "OPEN",
	})
	if err != nil {
		t.Fatal(err)
	}

	inst, err := model.GetWorkflowInstance(instID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := EvaluateCondition("has_opened", map[string]interface{}{
		"email_send_scope": "node",
		"email_node_key":   "send1",
	}, inst, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected open for linked send node")
	}
}

func TestNegativePredicateWaitStillWaiting(t *testing.T) {
	db.OpenTestDB(t)
	userID, instID, sendID := setupPredicateFixture(t)
	sentAt := time.Now().Add(-24 * time.Hour)
	markSendSent(t, sendID, sentAt)

	inst, _ := model.GetWorkflowInstance(instID)
	wake, edge, err := NegativePredicateWait("has_not_opened", map[string]interface{}{
		"email_send_scope": "node",
		"email_node_key":   "send1",
		"wait_days":        float64(3),
	}, inst)
	if err != nil {
		t.Fatal(err)
	}
	if edge != "" {
		t.Fatalf("unexpected early edge %q", edge)
	}
	if wake == nil {
		t.Fatal("expected wake time while grace period active")
	}
	if wake.Before(sentAt.Add(2 * 24 * time.Hour)) {
		t.Fatalf("wake too soon: %v", wake)
	}
	_ = userID
}

func TestNegativePredicateWaitEarlyOpen(t *testing.T) {
	_, instID, sendID := setupPredicateFixture(t)
	markSendSent(t, sendID, time.Now().Add(-time.Hour))
	_, err := model.InsertContactEvent(model.ContactEventInput{
		ContactID:   mustContactID(t, instID),
		EmailSendID: sendID,
		EventType:   "OPEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := model.GetWorkflowInstance(instID)
	_, edge, err := NegativePredicateWait("has_not_opened", map[string]interface{}{
		"email_send_scope": "node",
		"email_node_key":   "send1",
		"wait_days":        float64(3),
	}, inst)
	if err != nil {
		t.Fatal(err)
	}
	if edge != "false" {
		t.Fatalf("expected false branch when opened early, got %q", edge)
	}
}

func TestNegativePredicateWaitAfterGrace(t *testing.T) {
	_, instID, sendID := setupPredicateFixture(t)
	markSendSent(t, sendID, time.Now().Add(-4*24*time.Hour))
	inst, _ := model.GetWorkflowInstance(instID)
	wake, edge, err := NegativePredicateWait("has_not_opened", map[string]interface{}{
		"email_send_scope": "node",
		"email_node_key":   "send1",
		"wait_days":        float64(3),
	}, inst)
	if err != nil {
		t.Fatal(err)
	}
	if wake != nil || edge != "" {
		t.Fatalf("expected immediate evaluation, wake=%v edge=%q", wake, edge)
	}
}

func setupPredicateFixture(t *testing.T) (userID, instID, sendID int64) {
	t.Helper()
	db.OpenTestDB(t)
	var err error
	userID, err = model.CreateUser("pred-wait@test.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	wid, err := model.CreateWorkflow(userID, "pred", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := model.GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campaignID, err := model.CreateCampaign(userID, "c", templateID, 0, "workflow", w.CurrentVersionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "pred-wait@test.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err = model.CreateWorkflowInstance(w.CurrentVersionID, contactID, campaignID, "cond1")
	if err != nil {
		t.Fatal(err)
	}
	sendID, err = model.CreateQueuedEmailSend(userID, templateID, contactID, "track-wait", campaignID, "", instID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.CreateExecution(instID, "send1", fmt.Sprintf("exec-%d", instID), "succeeded", fmt.Sprintf(`{"email_send_id":%d}`, sendID), "")
	if err != nil {
		t.Fatal(err)
	}
	return userID, instID, sendID
}

func markSendSent(t *testing.T, sendID int64, sentAt time.Time) {
	t.Helper()
	_, err := db.Exec(`UPDATE email_sends SET delivery_status='sent', sent_at=? WHERE id=?`, sentAt, sendID)
	if err != nil {
		t.Fatal(err)
	}
}

func mustContactID(t *testing.T, instID int64) int64 {
	t.Helper()
	inst, err := model.GetWorkflowInstance(instID)
	if err != nil {
		t.Fatal(err)
	}
	return inst.ContactID
}
