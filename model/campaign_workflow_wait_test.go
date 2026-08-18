package model

import (
	"testing"
	"time"
)

func TestFormatWaitRemaining(t *testing.T) {
	remaining, abs := FormatWaitRemaining(nil, "waiting")
	if remaining != "" || abs != "" {
		t.Fatalf("expected empty for nil wake, got %q %q", remaining, abs)
	}
	wake := time.Now().Add(90 * time.Minute)
	remaining, abs = FormatWaitRemaining(&wake, "waiting")
	if remaining == "" || abs == "" {
		t.Fatalf("expected remaining and absolute, got %q %q", remaining, abs)
	}
	remaining, _ = FormatWaitRemaining(&wake, "active")
	if remaining != "" {
		t.Fatalf("active should not show wait remaining")
	}
	past := time.Now().Add(-time.Minute)
	remaining, _ = FormatWaitRemaining(&past, "waiting")
	if remaining != "due now" {
		t.Fatalf("got %q", remaining)
	}
}

func TestIncomingPathMetaMerge(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "temp", NodeType: "condition_temperature"},
			{NodeKey: "nudge", NodeType: "action_send_email", Label: "Nudge"},
			{NodeKey: "value", NodeType: "action_send_email", Label: "Value"},
		},
		Edges: []WorkflowEdge{
			{SourceNodeKey: "temp", TargetNodeKey: "value", EdgeType: "hot"},
			{SourceNodeKey: "temp", TargetNodeKey: "value", EdgeType: "warm"},
			{SourceNodeKey: "temp", TargetNodeKey: "nudge", EdgeType: "cold"},
			{SourceNodeKey: "value", TargetNodeKey: "nudge", EdgeType: "default"},
		},
	}
	_, summary, merge := incomingPathMeta(graph, "nudge")
	if !merge {
		t.Fatal("expected merge at nudge")
	}
	if summary == "" {
		t.Fatal("expected path summary")
	}
}

func TestOrderedWorkflowIncludesTemperature(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "start", NodeType: "trigger_campaign_started"},
			{NodeKey: "send", NodeType: "action_send_email", Label: "Intro"},
			{NodeKey: "temp", NodeType: "condition_temperature", Label: "Temp"},
			{NodeKey: "end", NodeType: "action_end", Label: "End"},
		},
		Edges: []WorkflowEdge{
			{SourceNodeKey: "start", TargetNodeKey: "send", EdgeType: "default"},
			{SourceNodeKey: "send", TargetNodeKey: "temp", EdgeType: "default"},
			{SourceNodeKey: "temp", TargetNodeKey: "end", EdgeType: "hot"},
			{SourceNodeKey: "temp", TargetNodeKey: "end", EdgeType: "warm"},
			{SourceNodeKey: "temp", TargetNodeKey: "end", EdgeType: "cold"},
		},
	}
	ordered := orderedWorkflowDisplayNodes(graph)
	found := false
	for _, n := range ordered {
		if n.NodeKey == "temp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("temperature node missing from ordered steps: %+v", ordered)
	}
}
