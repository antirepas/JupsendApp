package workflow

import (
	"testing"
	"time"

	"emailtracker.com/model"
)

func TestWaitExecutorResumesInsteadOfRearming(t *testing.T) {
	ex := WaitExecutor{}
	node := model.WorkflowNode{
		NodeKey:    "wait1",
		NodeType:   "action_wait",
		ConfigJSON: `{"duration_seconds":86400}`,
	}

	// Fresh active instance → schedule a wait.
	active := model.WorkflowInstance{ID: 1, Status: "active", CurrentNodeKey: "wait1"}
	res, err := ex.Execute(ExecutionContext{Instance: active, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	if res.WakeAt == nil {
		t.Fatal("expected WakeAt when starting wait")
	}
	if res.NextEdgeType != "" {
		t.Fatalf("should not advance yet, got edge %q", res.NextEdgeType)
	}

	// Due wake: status waiting → advance, do not schedule another day.
	waiting := model.WorkflowInstance{ID: 1, Status: "waiting", CurrentNodeKey: "wait1"}
	past := time.Now().Add(-time.Minute)
	waiting.NextWakeAt = &past
	res2, err := ex.Execute(ExecutionContext{Instance: waiting, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	if res2.WakeAt != nil {
		t.Fatal("resuming wait must not re-arm WakeAt")
	}
	if res2.NextEdgeType != "default" {
		t.Fatalf("expected default edge, got %q", res2.NextEdgeType)
	}
}

func TestConfigDurationSecondsHelpers(t *testing.T) {
	if got := configDurationSeconds(map[string]interface{}{"duration_seconds": float64(3600)}); got != 3600 {
		t.Fatalf("seconds=%d", got)
	}
	if got := configDurationSeconds(map[string]interface{}{"duration_hours": float64(24)}); got != 86400 {
		t.Fatalf("hours=%d", got)
	}
	if got := configDurationSeconds(map[string]interface{}{"duration_days": float64(2)}); got != 172800 {
		t.Fatalf("days=%d", got)
	}
}

// Regression: after wait#1 completes, wait#2 must arm a fresh full delay when entered
// as active — not immediately advance because status is still "waiting" from wait#1.
func TestWaitExecutorArmsEachWaitIndependently(t *testing.T) {
	ex := WaitExecutor{}
	wait1 := model.WorkflowNode{NodeKey: "wait1", NodeType: "action_wait", ConfigJSON: `{"duration_days":2}`}
	wait2 := model.WorkflowNode{NodeKey: "wait2", NodeType: "action_wait", ConfigJSON: `{"duration_days":2}`}

	// Resume wait1 (due).
	waiting := model.WorkflowInstance{ID: 42, Status: "waiting", CurrentNodeKey: "wait1"}
	past := time.Now().Add(-time.Minute)
	waiting.NextWakeAt = &past
	res1, err := ex.Execute(ExecutionContext{Instance: waiting, Node: wait1})
	if err != nil {
		t.Fatal(err)
	}
	if res1.WakeAt != nil || res1.NextEdgeType != "default" {
		t.Fatalf("wait1 resume: WakeAt=%v edge=%q", res1.WakeAt, res1.NextEdgeType)
	}

	// Engine sets status=active before entering wait2 (see ProcessInstance).
	active := model.WorkflowInstance{ID: 42, Status: "active", CurrentNodeKey: "wait2"}
	before := time.Now()
	res2, err := ex.Execute(ExecutionContext{Instance: active, Node: wait2})
	if err != nil {
		t.Fatal(err)
	}
	if res2.WakeAt == nil {
		t.Fatal("wait2 must arm a new WakeAt")
	}
	minWake := before.Add(2*24*time.Hour - time.Minute)
	if res2.WakeAt.Before(minWake) {
		t.Fatalf("wait2 WakeAt too soon: got %v want around +2d", res2.WakeAt)
	}
	if res2.NextEdgeType != "" {
		t.Fatalf("wait2 must not advance yet, edge=%q", res2.NextEdgeType)
	}
}
