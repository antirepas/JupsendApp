package model

import "fmt"

const (
	RecommendedOutreachWorkflowName = "Recommended outreach"
	RecommendedOutreachWorkflowDesc = "A/B cold intro → wait → temperature → value prop (warm/hot) or no-engagement → breakup (cold). Hot/warm at the end stop for a manual Loom + Calendly close."
)

// RecommendedOutreachGraph is the default jupsend outreach sequence.
// Campaign setup: use workflow A/B on the first send step; map templates to each send node;
// set lead temperature rules; leave stop-on-reply on. Loom/Calendly is always manual.
func RecommendedOutreachGraph() GraphSaveInput {
	const day = 86400.0
	wait3 := fmt.Sprintf(`{"duration_seconds":%d}`, int(3*day))

	n := func(key, typ, label string, x, y float64, cfg string) WorkflowNodeInput {
		if cfg == "" {
			cfg = "{}"
		}
		return WorkflowNodeInput{
			NodeKey: key, NodeType: typ, Label: label,
			ConfigJSON: cfg, PositionX: x, PositionY: y,
		}
	}
	e := func(from, to, edgeType string) WorkflowEdgeInput {
		if edgeType == "" {
			edgeType = "default"
		}
		return WorkflowEdgeInput{
			SourceNodeKey: from, TargetNodeKey: to,
			EdgeType: edgeType, Priority: 0, ConditionJSON: "{}",
		}
	}

	return GraphSaveInput{
		Nodes: []WorkflowNodeInput{
			n("start", "trigger_campaign_started", "Campaign started", 80, 280, "{}"),
			n("send_ab", "action_send_email", "1. Cold intro (A/B)", 280, 280, "{}"),
			n("wait_ab", "action_wait", "Wait 3 days", 480, 280, wait3),
			n("temp1", "condition_temperature", "Temperature after intro", 680, 280, "{}"),

			n("send_value", "action_send_email", "2. Value prop", 920, 120, "{}"),
			n("wait_value", "action_wait", "Wait 3 days", 1120, 120, wait3),
			n("temp2", "condition_temperature", "Temperature after value", 1320, 120, "{}"),
			n("end_manual", "action_end", "End — send Loom + Calendly", 1560, 40, "{}"),

			n("send_nudge", "action_send_email", "3. No-engagement nudge", 920, 440, "{}"),
			n("wait_nudge", "action_wait", "Wait 3 days", 1120, 440, wait3),
			n("send_breakup", "action_send_email", "4. Breakup", 1320, 440, "{}"),
			n("end_done", "action_end", "End — sequence done", 1560, 440, "{}"),
		},
		Edges: []WorkflowEdgeInput{
			e("start", "send_ab", "default"),
			e("send_ab", "wait_ab", "default"),
			e("wait_ab", "temp1", "default"),

			e("temp1", "send_value", "hot"),
			e("temp1", "send_value", "warm"),
			e("temp1", "send_nudge", "cold"),

			e("send_value", "wait_value", "default"),
			e("wait_value", "temp2", "default"),
			e("temp2", "end_manual", "hot"),
			e("temp2", "end_manual", "warm"),
			e("temp2", "send_nudge", "cold"),

			e("send_nudge", "wait_nudge", "default"),
			e("wait_nudge", "send_breakup", "default"),
			e("send_breakup", "end_done", "default"),
		},
	}
}

// CreateRecommendedOutreachWorkflow creates a draft workflow preloaded with RecommendedOutreachGraph.
func CreateRecommendedOutreachWorkflow(userID int64) (int64, error) {
	id, err := CreateWorkflow(userID, RecommendedOutreachWorkflowName, RecommendedOutreachWorkflowDesc)
	if err != nil {
		return 0, err
	}
	w, err := GetWorkflow(id)
	if err != nil {
		return id, err
	}
	if w.CurrentVersionID == 0 {
		return id, fmt.Errorf("workflow has no draft version")
	}
	if err := SaveWorkflowGraph(w.CurrentVersionID, RecommendedOutreachGraph()); err != nil {
		return id, err
	}
	return id, nil
}
