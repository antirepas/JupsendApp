package workflow

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

type countingMailer struct {
	calls atomic.Int64
}

func (m *countingMailer) SendWorkflowEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64, openTracking, clickTracking bool) (int64, error) {
	m.calls.Add(1)
	time.Sleep(20 * time.Millisecond) // widen race window
	return m.calls.Load(), nil
}

func TestSendEmailExecutorClaimPreventsDuplicate(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("wf-dedupe-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "dedupe@lead.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wid, err := model.CreateWorkflow(userID, "dedupe", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := model.GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	vid := w.CurrentVersionID
	if err := model.SaveWorkflowGraph(vid, model.GraphSaveInput{
		Nodes: []model.WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: fmt.Sprintf(`{"template_id":%d}`, templateID)},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []model.WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "end", EdgeType: "default"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := model.PublishWorkflowVersion(wid, vid); err != nil {
		t.Fatal(err)
	}
	campID, err := model.CreateCampaign(userID, "dedupe", templateID, 0, "workflow", vid, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = model.SaveCampaignWorkflowTemplates(campID, map[string]int64{"send1": templateID})
	instID, _, err := model.CreateWorkflowInstance(vid, contactID, campID, "send1", "{}")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := model.GetWorkflowInstance(instID)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := model.GetWorkflowGraph(vid)
	if err != nil {
		t.Fatal(err)
	}
	var node model.WorkflowNode
	for _, n := range graph.Nodes {
		if n.NodeKey == "send1" {
			node = n
			break
		}
	}
	if node.NodeKey == "" {
		t.Fatal("send1 node missing")
	}
	mailer := &countingMailer{}
	ex := SendEmailExecutor{}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ex.Execute(ExecutionContext{
				Instance: inst,
				Node:     node,
				Graph:    graph,
				Mailer:   mailer,
			})
		}()
	}
	wg.Wait()

	if got := mailer.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 send, got %d", got)
	}
}

func TestTryBeginExecutionOnlyOnce(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("wf-claim-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "claim@lead.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wid, err := model.CreateWorkflow(userID, "claim", "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := model.GetWorkflow(wid)
	if err != nil {
		t.Fatal(err)
	}
	vid := w.CurrentVersionID
	if err := model.SaveWorkflowGraph(vid, model.GraphSaveInput{
		Nodes: []model.WorkflowNodeInput{
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	instID, _, err := model.CreateWorkflowInstance(vid, contactID, 0, "send1", "{}")
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%d:send1:send", instID)
	ok1, err := model.TryBeginExecution(instID, "send1", key)
	if err != nil || !ok1 {
		t.Fatalf("first claim: ok=%v err=%v", ok1, err)
	}
	ok2, err := model.TryBeginExecution(instID, "send1", key)
	if err != nil || ok2 {
		t.Fatalf("second claim should fail: ok=%v err=%v", ok2, err)
	}
	model.AbortStartedExecution(key)
	ok3, err := model.TryBeginExecution(instID, "send1", key)
	if err != nil || !ok3 {
		t.Fatalf("after abort should reclaim: ok=%v err=%v", ok3, err)
	}
}
