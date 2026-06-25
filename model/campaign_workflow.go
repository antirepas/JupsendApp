package model

import (
	"fmt"

	"emailtracker.com/db"
)

type CampaignWorkflowOverview struct {
	TotalContacts  int
	NotStarted     int
	InProgress     int
	Completed      int
	Cancelled      int
	NotStartedPct  float64
	InProgressPct  float64
	CompletedPct   float64
	CancelledPct   float64
	Steps          []CampaignWorkflowStepStat
}

type CampaignWorkflowStepStat struct {
	NodeKey       string
	Label         string
	NodeType      string
	ContactsHere  int
	PassedThrough int
	BarPercent    float64
	StepIndex     int
}

type CampaignWorkflowStepDisplay struct {
	NodeKey     string
	Label       string
	NodeType    string
	Description string
	StepIndex   int
}

func GetCampaignWorkflowStepDisplay(versionID int64) ([]CampaignWorkflowStepDisplay, error) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}
	ordered := orderedWorkflowDisplayNodes(graph)
	var steps []CampaignWorkflowStepDisplay
	for i, n := range ordered {
		label := n.Label
		if label == "" {
			label = NodeLabelForKey(versionID, n.NodeKey)
		}
		steps = append(steps, CampaignWorkflowStepDisplay{
			NodeKey:     n.NodeKey,
			Label:       label,
			NodeType:    n.NodeType,
			Description: describeWorkflowStep(n),
			StepIndex:   i + 1,
		})
	}
	return steps, nil
}

func describeWorkflowStep(n WorkflowNode) string {
	switch n.NodeType {
	case "action_send_email":
		cfg := ParseNodeConfig(n.ConfigJSON)
		tid := int64(0)
		if v, ok := cfg["template_id"].(float64); ok {
			tid = int64(v)
		}
		name := fmt.Sprintf("template #%d", tid)
		if tid > 0 {
			if t, err := GetTemplate(tid); err == nil {
				name = t.Name
			}
		}
		return "Sends " + name
	case "action_wait":
		cfg := ParseNodeConfig(n.ConfigJSON)
		secs := 86400
		if v, ok := cfg["duration_seconds"].(float64); ok {
			secs = int(v)
		}
		if secs < 3600 {
			return fmt.Sprintf("Pauses for %d minutes", secs/60)
		}
		if secs < 86400 {
			return fmt.Sprintf("Pauses for %d hours", secs/3600)
		}
		days := secs / 86400
		if days == 1 {
			return "Pauses for 1 day"
		}
		return fmt.Sprintf("Pauses for %d days", days)
	case "condition_engagement":
		cfg := ParseNodeConfig(n.ConfigJSON)
		if cond, ok := cfg["condition"].(string); ok && cond != "" {
			switch cond {
			case "opened":
				return "Branches if the contact opened the email"
			case "clicked":
				return "Branches if the contact clicked a link"
			case "replied":
				return "Branches if the contact replied"
			}
		}
		return "Branches based on engagement"
	case "action_end":
		return "Workflow completes for this contact"
	default:
		return ""
	}
}

func GetCampaignWorkflowOverview(campaignID, versionID int64, totalContacts int) (CampaignWorkflowOverview, error) {
	overview := CampaignWorkflowOverview{TotalContacts: totalContacts}

	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return overview, err
	}

	nodeAtCount := map[string]int{}
	statusTotals := map[string]int{}
	instanceCount := 0

	rows, err := db.Query(`
		SELECT current_node_key, status, COUNT(*)
		FROM workflow_instances
		WHERE campaign_id = ?
		GROUP BY current_node_key, status
	`, campaignID)
	if err != nil {
		return overview, err
	}
	defer rows.Close()

	for rows.Next() {
		var nodeKey, status string
		var n int
		if err := rows.Scan(&nodeKey, &status, &n); err != nil {
			return overview, err
		}
		instanceCount += n
		statusTotals[status] += n
		if status == "active" || status == "waiting" {
			nodeAtCount[nodeKey] += n
		}
	}

	passedMap := map[string]int{}
	pRows, err := db.Query(`
		SELECT we.node_key, COUNT(*)
		FROM workflow_executions we
		INNER JOIN workflow_instances wi ON wi.id = we.instance_id
		WHERE wi.campaign_id = ? AND we.status = 'succeeded'
		GROUP BY we.node_key
	`, campaignID)
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var key string
			var n int
			if pRows.Scan(&key, &n) == nil {
				passedMap[key] = n
			}
		}
	}

	overview.NotStarted = totalContacts - instanceCount
	if overview.NotStarted < 0 {
		overview.NotStarted = 0
	}
	overview.InProgress = statusTotals["active"] + statusTotals["waiting"]
	overview.Completed = statusTotals["completed"]
	overview.Cancelled = statusTotals["cancelled"]
	if totalContacts > 0 {
		overview.NotStartedPct = float64(overview.NotStarted) / float64(totalContacts) * 100
		overview.InProgressPct = float64(overview.InProgress) / float64(totalContacts) * 100
		overview.CompletedPct = float64(overview.Completed) / float64(totalContacts) * 100
		overview.CancelledPct = float64(overview.Cancelled) / float64(totalContacts) * 100
	}

	ordered := orderedWorkflowDisplayNodes(graph)
	denom := totalContacts
	if denom < 1 {
		denom = 1
	}
	for i, n := range ordered {
		label := n.Label
		if label == "" {
			label = NodeLabelForKey(versionID, n.NodeKey)
		}
		here := nodeAtCount[n.NodeKey]
		overview.Steps = append(overview.Steps, CampaignWorkflowStepStat{
			NodeKey:       n.NodeKey,
			Label:         label,
			NodeType:      n.NodeType,
			ContactsHere:  here,
			PassedThrough: passedMap[n.NodeKey],
			BarPercent:    float64(here) / float64(denom) * 100,
			StepIndex:     i + 1,
		})
	}

	return overview, nil
}

func orderedWorkflowDisplayNodes(graph WorkflowGraph) []WorkflowNode {
	nodeMap := map[string]WorkflowNode{}
	for _, n := range graph.Nodes {
		nodeMap[n.NodeKey] = n
	}
	adj := map[string][]string{}
	for _, e := range graph.Edges {
		adj[e.SourceNodeKey] = append(adj[e.SourceNodeKey], e.TargetNodeKey)
	}

	var entryKey string
	for _, n := range graph.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entryKey = n.NodeKey
			break
		}
	}
	if entryKey == "" {
		return nil
	}

	var ordered []WorkflowNode
	seen := map[string]bool{}
	queue := []string{entryKey}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if seen[key] {
			continue
		}
		seen[key] = true
		n, ok := nodeMap[key]
		if !ok {
			continue
		}
		switch n.NodeType {
		case "action_send_email", "action_wait", "condition_engagement", "action_end":
			ordered = append(ordered, n)
		}
		for _, next := range adj[key] {
			if !seen[next] {
				queue = append(queue, next)
			}
		}
	}
	return ordered
}
