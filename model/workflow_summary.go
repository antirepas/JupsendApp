package model

import "fmt"

type WorkflowVersionInfo struct {
	WorkflowID   int64
	WorkflowName string
	VersionID    int64
}

func GetWorkflowForVersion(versionID int64) (WorkflowVersionInfo, error) {
	v, err := GetWorkflowVersion(versionID)
	if err != nil {
		return WorkflowVersionInfo{}, err
	}
	w, err := GetWorkflow(v.WorkflowID)
	if err != nil {
		return WorkflowVersionInfo{}, err
	}
	return WorkflowVersionInfo{
		WorkflowID:   w.ID,
		WorkflowName: w.Name,
		VersionID:    versionID,
	}, nil
}

func CountWorkflowSteps(versionID int64) int {
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return 0
	}
	n := 0
	for _, node := range g.Nodes {
		switch node.NodeType {
		case "action_send_email", "action_wait", "condition_engagement":
			n++
		}
	}
	return n
}

func SummarizeWorkflowSteps(versionID int64) []string {
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil
	}
	var lines []string
	for _, n := range g.Nodes {
		switch n.NodeType {
		case "action_send_email":
			cfg := ParseNodeConfig(n.ConfigJSON)
			tid := int64(0)
			if v, ok := cfg["template_id"].(float64); ok {
				tid = int64(v)
			}
			name := fmt.Sprintf("Template #%d", tid)
			if tid > 0 {
				if t, err := GetTemplate(tid); err == nil {
					name = t.Name
				}
			}
			if n.Label != "" && n.Label != "Send" {
				lines = append(lines, n.Label+": "+name)
			} else {
				lines = append(lines, "Send: "+name)
			}
		case "action_wait":
			cfg := ParseNodeConfig(n.ConfigJSON)
			secs := 86400
			if v, ok := cfg["duration_seconds"].(float64); ok {
				secs = int(v)
			}
			days := secs / 86400
			if days < 1 {
				days = 1
			}
			if n.Label != "" {
				lines = append(lines, n.Label)
			} else {
				lines = append(lines, fmt.Sprintf("Wait %d days", days))
			}
		case "condition_engagement":
			label := n.Label
			if label == "" {
				label = "Condition"
			}
			lines = append(lines, label)
		case "trigger_campaign_started":
			// skip — entry point, not shown as a user step
		case "action_end":
			// skip terminal nodes
		}
	}
	return lines
}

func NodeLabelForKey(versionID int64, nodeKey string) string {
	if nodeKey == "" {
		return "—"
	}
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nodeKey
	}
	for _, n := range g.Nodes {
		if n.NodeKey == nodeKey {
			if n.Label != "" {
				return n.Label
			}
			switch n.NodeType {
			case "action_send_email":
				return "Send email"
			case "action_wait":
				return "Wait"
			case "condition_engagement":
				return "Condition"
			case "action_end":
				return "End"
			case "trigger_campaign_started":
				return "Start"
			default:
				return n.NodeType
			}
		}
	}
	return nodeKey
}

func GetCampaignInstanceMap(campaignID int64) (map[int64]WorkflowInstance, error) {
	instances, err := ListInstancesForCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]WorkflowInstance, len(instances))
	for _, inst := range instances {
		m[inst.ContactID] = inst
	}
	return m, nil
}
