package model

import "testing"

func TestLayoutAnalyticsCanvasNoOverlap(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", PositionX: 0, PositionY: 0},
			{NodeKey: "ab", NodeType: "action_send_email", Label: "AB", PositionX: 10, PositionY: 10},
			{NodeKey: "wait1", NodeType: "action_wait", Label: "Wait", PositionX: 20, PositionY: 20},
			{NodeKey: "temp", NodeType: "condition_temperature", Label: "Temp", PositionX: 30, PositionY: 30},
			{NodeKey: "hot", NodeType: "action_wait", Label: "Hot wait", PositionX: 40, PositionY: 5},
			{NodeKey: "warm", NodeType: "action_wait", Label: "Warm wait", PositionX: 40, PositionY: 40},
			{NodeKey: "cold", NodeType: "action_send_email", Label: "Cold", PositionX: 40, PositionY: 80},
			{NodeKey: "merge", NodeType: "action_wait", Label: "Merge wait", PositionX: 50, PositionY: 40},
			{NodeKey: "email2", NodeType: "action_send_email", Label: "Email 2", PositionX: 60, PositionY: 40},
			{NodeKey: "end", NodeType: "action_end", Label: "End", PositionX: 70, PositionY: 40},
		},
		Edges: []WorkflowEdge{
			{SourceNodeKey: "start", TargetNodeKey: "ab", EdgeType: "default"},
			{SourceNodeKey: "ab", TargetNodeKey: "wait1", EdgeType: "default"},
			{SourceNodeKey: "wait1", TargetNodeKey: "temp", EdgeType: "default"},
			{SourceNodeKey: "temp", TargetNodeKey: "hot", EdgeType: "hot"},
			{SourceNodeKey: "temp", TargetNodeKey: "warm", EdgeType: "warm"},
			{SourceNodeKey: "temp", TargetNodeKey: "cold", EdgeType: "cold"},
			{SourceNodeKey: "hot", TargetNodeKey: "merge", EdgeType: "default"},
			{SourceNodeKey: "warm", TargetNodeKey: "merge", EdgeType: "default"},
			{SourceNodeKey: "cold", TargetNodeKey: "merge", EdgeType: "default"},
			{SourceNodeKey: "merge", TargetNodeKey: "email2", EdgeType: "default"},
			{SourceNodeKey: "email2", TargetNodeKey: "end", EdgeType: "default"},
		},
	}

	pos, w, h := layoutAnalyticsCanvasPositions(graph)
	if len(pos) != len(graph.Nodes) {
		t.Fatalf("expected %d positions, got %d", len(graph.Nodes), len(pos))
	}
	if w < 400 || h < 200 {
		t.Fatalf("canvas too small: %.0fx%.0f", w, h)
	}

	// Merge must be to the right of branch arms.
	if pos["merge"][0] <= pos["hot"][0] || pos["merge"][0] <= pos["cold"][0] {
		t.Fatalf("merge x=%v should be right of branches hot=%v cold=%v", pos["merge"], pos["hot"], pos["cold"])
	}
	// Temperature after wait.
	if pos["temp"][0] <= pos["wait1"][0] {
		t.Fatalf("temp should be right of wait1")
	}

	// No box overlaps using layout card size.
	keys := make([]string, 0, len(pos))
	for k := range pos {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			a, b := pos[keys[i]], pos[keys[j]]
			if boxesOverlap(a[0], a[1], b[0], b[1], analyticsNodeW, analyticsNodeH, analyticsColGap*0.25, analyticsRowGap*0.25) {
				t.Fatalf("overlap %s (%.0f,%.0f) vs %s (%.0f,%.0f)", keys[i], a[0], a[1], keys[j], b[0], b[1])
			}
		}
	}
}

func boxesOverlap(ax, ay, bx, by, w, h, slackX, slackY float64) bool {
	return ax < bx+w+slackX && ax+w+slackX > bx && ay < by+h+slackY && ay+h+slackY > by
}
