package workflow

import (
	"testing"

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
