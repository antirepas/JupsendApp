package model

// NodeLabelMapForVersion returns node_key → display label for a workflow version (one graph load).
func NodeLabelMapForVersion(versionID int64) map[string]string {
	labels := map[string]string{}
	if versionID <= 0 {
		return labels
	}
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return labels
	}
	for _, n := range g.Nodes {
		if n.Label != "" {
			labels[n.NodeKey] = n.Label
			continue
		}
		switch n.NodeType {
		case "action_send_email":
			labels[n.NodeKey] = "Send email"
		case "action_wait":
			labels[n.NodeKey] = "Wait"
		case "condition_engagement":
			labels[n.NodeKey] = "Condition"
		case "condition_temperature":
			labels[n.NodeKey] = "Lead temperature"
		case "action_end":
			labels[n.NodeKey] = "End"
		case "trigger_campaign_started":
			labels[n.NodeKey] = "Start"
		default:
			labels[n.NodeKey] = n.NodeType
		}
	}
	return labels
}

func LabelFromMap(labels map[string]string, nodeKey string) string {
	if nodeKey == "" {
		return "—"
	}
	if labels != nil {
		if label, ok := labels[nodeKey]; ok && label != "" {
			return label
		}
	}
	return nodeKey
}
