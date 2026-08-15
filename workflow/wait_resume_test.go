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
