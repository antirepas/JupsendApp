package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestBuildCampaignWorkflowGraphTreeForks(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("graph-tree@test.com", "hash", "http://localhost")

	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('T','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	wid, _ := CreateWorkflow(userID, "tree-wf", "")
	w, _ := GetWorkflow(wid)
	vid := w.CurrentVersionID

	_ = SaveWorkflowGraph(vid, GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			{NodeKey: "start", NodeType: "trigger_campaign_started", Label: "Start", ConfigJSON: "{}"},
			{NodeKey: "send1", NodeType: "action_send_email", Label: "Email", ConfigJSON: "{}"},
			{NodeKey: "cond_a", NodeType: "condition_engagement", Label: "Opened?", ConfigJSON: `{"predicate":"opened"}`},
			{NodeKey: "cond_b", NodeType: "condition_engagement", Label: "Replied?", ConfigJSON: `{"predicate":"replied"}`},
			{NodeKey: "send2", NodeType: "action_send_email", Label: "Follow-up", ConfigJSON: "{}"},
			{NodeKey: "end", NodeType: "action_end", Label: "End", ConfigJSON: "{}"},
		},
		Edges: []WorkflowEdgeInput{
			{SourceNodeKey: "start", TargetNodeKey: "send1", EdgeType: "default"},
			{SourceNodeKey: "send1", TargetNodeKey: "cond_a", EdgeType: "default", Priority: 1},
			{SourceNodeKey: "send1", TargetNodeKey: "cond_b", EdgeType: "default", Priority: 2},
			{SourceNodeKey: "cond_a", TargetNodeKey: "send2", EdgeType: "true"},
			{SourceNodeKey: "cond_a", TargetNodeKey: "end", EdgeType: "false"},
			{SourceNodeKey: "cond_b", TargetNodeKey: "end", EdgeType: "true"},
			{SourceNodeKey: "cond_b", TargetNodeKey: "end", EdgeType: "false"},
			{SourceNodeKey: "send2", TargetNodeKey: "end", EdgeType: "default"},
		},
	})

	campaignID, _ := CreateCampaign(userID, "tree", templateID, 0, "workflow", vid, "", "")
	_ = SaveCampaignWorkflowTemplates(campaignID, map[string]int64{"send1": templateID, "send2": templateID})
	campaign, _ := GetCampaign(campaignID)

	tree, err := BuildCampaignWorkflowGraphTree(campaign, userID)
	if err != nil {
		t.Fatal(err)
	}
	if tree.NodeKey != "send1" {
		t.Fatalf("expected root send1 (trigger skipped), got %s", tree.NodeKey)
	}
	if len(tree.Children) != 2 {
		t.Fatalf("expected 2 fork branches from send1, got %d", len(tree.Children))
	}
	if !tree.Children[0].IsForkBranch || tree.Children[0].EdgePriority != 1 {
		t.Fatalf("expected first branch priority 1, got %+v", tree.Children[0])
	}
	// Condition nodes should have yes/no children.
	condA := tree.Children[0]
	if condA.NodeKey != "cond_a" {
		t.Fatalf("expected cond_a, got %s", condA.NodeKey)
	}
	if len(condA.Children) != 2 {
		t.Fatalf("expected cond_a to have true/false branches, got %d", len(condA.Children))
	}
	if condA.Children[0].EdgeType != "true" || condA.Children[1].EdgeType != "false" {
		t.Fatalf("expected true/false edges, got %s / %s", condA.Children[0].EdgeType, condA.Children[1].EdgeType)
	}
}

func TestOutgoingDisplayEdgesCondition(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{{NodeKey: "c", NodeType: "condition_engagement"}},
		Edges: []WorkflowEdge{
			{SourceNodeKey: "c", TargetNodeKey: "yes", EdgeType: "true"},
			{SourceNodeKey: "c", TargetNodeKey: "no", EdgeType: "false"},
			{SourceNodeKey: "c", TargetNodeKey: "skip", EdgeType: "default"},
		},
	}
	edges := outgoingDisplayEdges(graph, "c", "condition_engagement")
	if len(edges) != 2 {
		t.Fatalf("expected 2 condition edges, got %d", len(edges))
	}
	if edges[0].EdgeType != "true" || edges[1].EdgeType != "false" {
		t.Fatalf("unexpected edge order: %s, %s", edges[0].EdgeType, edges[1].EdgeType)
	}
}
