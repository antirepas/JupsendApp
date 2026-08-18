package model

import (
	"fmt"
	"sort"

	"emailtracker.com/db"
)

type CampaignWorkflowOverview struct {
	TotalContacts int
	NotStarted    int
	InProgress    int
	Completed     int
	Cancelled     int
	NotStartedPct float64
	InProgressPct float64
	CompletedPct  float64
	CancelledPct  float64
	Steps         []CampaignWorkflowStepStat
}

type CampaignWorkflowStepStat struct {
	NodeKey       string
	Label         string
	NodeType      string
	Description   string
	ContactsHere  int
	PassedThrough int
	BarPercent    float64
	StepIndex     int
	// PathLabel is how contacts arrive (Hot / Cold / If yes). Empty on the first step.
	PathLabel string
	// PathSummary lists all incoming branch labels when paths recombine.
	PathSummary string
	IsMerge     bool
}

type CampaignWorkflowStepDisplay struct {
	NodeKey      string
	Label        string
	NodeType     string
	Description  string
	StepIndex    int
	TemplateID   int64
	TemplateName string
	IsHybridAB   bool
}

func GetCampaignWorkflowStepDisplay(versionID int64) ([]CampaignWorkflowStepDisplay, error) {
	return getCampaignWorkflowStepDisplay(versionID, Campaign{}, 0)
}

func GetCampaignWorkflowStepDisplayForCampaign(campaign Campaign, userID int64) ([]CampaignWorkflowStepDisplay, error) {
	return getCampaignWorkflowStepDisplay(campaign.WorkflowVersionID, campaign, userID)
}

func getCampaignWorkflowStepDisplay(versionID int64, campaign Campaign, userID int64) ([]CampaignWorkflowStepDisplay, error) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}

	var mappings map[string]int64
	var firstSendKey string
	isHybrid := campaign.ExecutionMode == "workflow_ab"
	if campaign.ID > 0 {
		mappings, _ = GetCampaignWorkflowTemplates(campaign.ID)
		firstSendKey, _ = GetFirstSendNodeKey(versionID)
	}

	ordered := orderedWorkflowDisplayNodes(graph)
	var steps []CampaignWorkflowStepDisplay
	for i, n := range ordered {
		label := n.Label
		if label == "" {
			label = NodeLabelForKey(versionID, n.NodeKey)
		}
		step := CampaignWorkflowStepDisplay{
			NodeKey:     n.NodeKey,
			Label:       label,
			NodeType:    n.NodeType,
			Description: describeWorkflowStep(n, graph),
			StepIndex:   i + 1,
		}
		if n.NodeType == "action_send_email" && campaign.ID > 0 {
			if isHybrid && n.NodeKey == firstSendKey {
				step.IsHybridAB = true
				if campaign.TemplateAID > 0 {
					if t, _, err := GetTemplateByID(campaign.TemplateAID, userID); err == nil {
						step.TemplateName = t.Name + " / "
					}
				}
				if campaign.TemplateBID > 0 {
					if t, _, err := GetTemplateByID(campaign.TemplateBID, userID); err == nil {
						step.TemplateName += t.Name
					}
				}
				step.Description = "A/B first email: " + step.TemplateName
			} else if tid := mappings[n.NodeKey]; tid > 0 {
				step.TemplateID = tid
				if t, _, err := GetTemplateByID(tid, userID); err == nil {
					step.TemplateName = t.Name
					step.Description = "Sends " + t.Name
				}
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func describeWorkflowStep(n WorkflowNode, graph WorkflowGraph) string {
	switch n.NodeType {
	case "action_send_email":
		label := n.Label
		if label == "" {
			label = "email"
		}
		return "Sends: " + label
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
		return DescribeConditionEngagement(ParseNodeConfig(n.ConfigJSON), graph)
	case "condition_temperature":
		return "Branches by campaign lead temperature (hot / warm / cold)"
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
	labelMap := labelsFromGraph(graph)
	denom := totalContacts
	if denom < 1 {
		denom = 1
	}
	for i, n := range ordered {
		label := n.Label
		if label == "" {
			label = LabelFromMap(labelMap, n.NodeKey)
		}
		here := nodeAtCount[n.NodeKey]
		pathLabel, pathSummary, isMerge := incomingPathMeta(graph, n.NodeKey)
		overview.Steps = append(overview.Steps, CampaignWorkflowStepStat{
			NodeKey:       n.NodeKey,
			Label:         label,
			NodeType:      n.NodeType,
			Description:   describeWorkflowStep(n, graph),
			ContactsHere:  here,
			PassedThrough: passedMap[n.NodeKey],
			BarPercent:    float64(here) / float64(denom) * 100,
			StepIndex:     i + 1,
			PathLabel:     pathLabel,
			PathSummary:   pathSummary,
			IsMerge:       isMerge,
		})
	}

	return overview, nil
}

func labelsFromGraph(graph WorkflowGraph) map[string]string {
	labels := map[string]string{}
	for _, n := range graph.Nodes {
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
		case "action_send_email", "action_wait", "condition_engagement", "condition_temperature", "action_end":
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

// incomingPathMeta returns branch labels into a node and whether multiple paths merge there.
func incomingPathMeta(graph WorkflowGraph, nodeKey string) (pathLabel, pathSummary string, isMerge bool) {
	var labels []string
	seenLabel := map[string]bool{}
	sourceCount := 0
	seenSource := map[string]bool{}
	for _, e := range graph.Edges {
		if e.TargetNodeKey != nodeKey {
			continue
		}
		if !seenSource[e.SourceNodeKey] {
			seenSource[e.SourceNodeKey] = true
			sourceCount++
		}
		lab := edgeBranchLabel(e)
		if lab == "" {
			continue
		}
		if !seenLabel[lab] {
			seenLabel[lab] = true
			labels = append(labels, lab)
		}
	}
	isMerge = sourceCount > 1
	if len(labels) == 0 {
		if isMerge {
			return "", "Paths recombine", true
		}
		return "", "", false
	}
	sort.Strings(labels)
	pathLabel = labels[0]
	if isMerge {
		pathSummary = "Merge · " + joinPathLabels(labels)
	} else {
		pathSummary = pathLabel
	}
	return pathLabel, pathSummary, isMerge
}

// edgeBranchLabel is for analytics path columns (Hot/Cold/If yes) — not fork priority numbers.
func edgeBranchLabel(e WorkflowEdge) string {
	switch e.EdgeType {
	case "true":
		return "If yes"
	case "false":
		return "If no"
	case "hot":
		return "Hot"
	case "warm":
		return "Warm"
	case "cold":
		return "Cold"
	default:
		return ""
	}
}

func joinPathLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	if len(labels) == 1 {
		return labels[0]
	}
	out := labels[0]
	for i := 1; i < len(labels); i++ {
		out += " + " + labels[i]
	}
	return out
}

type CampaignWorkflowGraphNode struct {
	NodeKey      string
	Label        string
	NodeType     string
	Description  string
	TemplateID   int64
	TemplateName string
	IsHybridAB   bool
	IsForkBranch bool
	EdgePriority int
	EdgeType     string
	EdgeLabel    string
	Children     []CampaignWorkflowGraphNode
}

// BuildCampaignWorkflowGraphTree renders the workflow as a branching tree (not a flat BFS list).
func BuildCampaignWorkflowGraphTree(campaign Campaign, userID int64) (CampaignWorkflowGraphNode, error) {
	graph, err := GetWorkflowGraph(campaign.WorkflowVersionID)
	if err != nil {
		return CampaignWorkflowGraphNode{}, err
	}
	mappings, _ := GetCampaignWorkflowTemplates(campaign.ID)
	firstSendKey, _ := GetFirstSendNodeKey(campaign.WorkflowVersionID)
	isHybrid := campaign.ExecutionMode == "workflow_ab"

	var entryKey string
	for _, n := range graph.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entryKey = n.NodeKey
			break
		}
	}
	if entryKey == "" {
		return CampaignWorkflowGraphNode{}, fmt.Errorf("no entry node")
	}

	templateNames, _ := TemplateNameMapForUser(userID)
	nameFor := func(id int64) string {
		if id <= 0 {
			return ""
		}
		return templateNames[id]
	}

	ctx := graphTreeContext{
		graph:        graph,
		campaign:     campaign,
		userID:       userID,
		mappings:     mappings,
		firstSendKey: firstSendKey,
		isHybrid:     isHybrid,
		nameFor:      nameFor,
		labelMap:     labelsFromGraph(graph),
	}
	return buildGraphTreeNode(entryKey, ctx, nil), nil
}

type graphTreeContext struct {
	graph        WorkflowGraph
	campaign     Campaign
	userID       int64
	mappings     map[string]int64
	firstSendKey string
	isHybrid     bool
	nameFor      func(int64) string
	labelMap     map[string]string
}

func buildGraphTreeNode(nodeKey string, ctx graphTreeContext, path map[string]bool) CampaignWorkflowGraphNode {
	if path == nil {
		path = map[string]bool{}
	}
	if path[nodeKey] {
		return CampaignWorkflowGraphNode{
			NodeKey:     nodeKey,
			Label:       "…",
			NodeType:    "action_end",
			Description: "Merge point (cycle trimmed)",
		}
	}
	path[nodeKey] = true
	defer delete(path, nodeKey)

	nodeMap := map[string]WorkflowNode{}
	for _, n := range ctx.graph.Nodes {
		nodeMap[n.NodeKey] = n
	}
	n, ok := nodeMap[nodeKey]
	if !ok {
		return CampaignWorkflowGraphNode{NodeKey: nodeKey, Label: nodeKey, NodeType: "action_end"}
	}

	label := n.Label
	if label == "" {
		label = LabelFromMap(ctx.labelMap, n.NodeKey)
	}

	out := CampaignWorkflowGraphNode{
		NodeKey:     n.NodeKey,
		Label:       label,
		NodeType:    n.NodeType,
		Description: describeWorkflowStep(n, ctx.graph),
	}

	if n.NodeType == "action_send_email" {
		if ctx.isHybrid && n.NodeKey == ctx.firstSendKey {
			out.IsHybridAB = true
			aName := ctx.nameFor(ctx.campaign.TemplateAID)
			bName := ctx.nameFor(ctx.campaign.TemplateBID)
			out.TemplateName = aName + " / " + bName
			out.Description = "A/B first email"
		} else if tid := ctx.mappings[n.NodeKey]; tid > 0 {
			out.TemplateID = tid
			out.TemplateName = ctx.nameFor(tid)
			out.Description = "Sends " + out.TemplateName
		}
	}

	if n.NodeType == "trigger_campaign_started" {
		children := outgoingDisplayEdges(ctx.graph, nodeKey, n.NodeType)
		if len(children) == 1 {
			child := buildGraphTreeNode(children[0].TargetNodeKey, ctx, path)
			child.EdgePriority = children[0].Priority
			child.EdgeType = children[0].EdgeType
			child.EdgeLabel = edgeDisplayLabel(children[0], len(children) > 1)
			return child
		}
		for _, e := range children {
			child := buildGraphTreeNode(e.TargetNodeKey, ctx, copyPath(path))
			child.IsForkBranch = len(children) > 1
			child.EdgePriority = e.Priority
			child.EdgeType = e.EdgeType
			child.EdgeLabel = edgeDisplayLabel(e, len(children) > 1)
			out.Children = append(out.Children, child)
		}
		return out
	}

	children := outgoingDisplayEdges(ctx.graph, nodeKey, n.NodeType)
	for _, e := range children {
		child := buildGraphTreeNode(e.TargetNodeKey, ctx, copyPath(path))
		child.IsForkBranch = len(children) > 1
		child.EdgePriority = e.Priority
		child.EdgeType = e.EdgeType
		child.EdgeLabel = edgeDisplayLabel(e, len(children) > 1)
		out.Children = append(out.Children, child)
	}
	return out
}

func copyPath(path map[string]bool) map[string]bool {
	out := make(map[string]bool, len(path))
	for k, v := range path {
		out[k] = v
	}
	return out
}

func defaultChildEdges(graph WorkflowGraph, sourceKey string) []WorkflowEdge {
	return outgoingDisplayEdges(graph, sourceKey, "")
}

func outgoingDisplayEdges(graph WorkflowGraph, sourceKey, sourceNodeType string) []WorkflowEdge {
	nodeType := sourceNodeType
	if nodeType == "" {
		for _, n := range graph.Nodes {
			if n.NodeKey == sourceKey {
				nodeType = n.NodeType
				break
			}
		}
	}

	var edges []WorkflowEdge
	for _, e := range graph.Edges {
		if e.SourceNodeKey != sourceKey {
			continue
		}
		if nodeType == "condition_engagement" {
			if e.EdgeType == "true" || e.EdgeType == "false" {
				edges = append(edges, e)
			}
			continue
		}
		if nodeType == "condition_temperature" {
			if e.EdgeType == "hot" || e.EdgeType == "warm" || e.EdgeType == "cold" {
				edges = append(edges, e)
			}
			continue
		}
		if e.EdgeType == "default" || e.EdgeType == "" {
			edges = append(edges, e)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		order := func(t string) int {
			switch t {
			case "true", "hot":
				return 0
			case "warm":
				return 1
			case "false", "cold":
				return 2
			default:
				return 3
			}
		}
		if order(edges[i].EdgeType) != order(edges[j].EdgeType) {
			return order(edges[i].EdgeType) < order(edges[j].EdgeType)
		}
		if edges[i].Priority != edges[j].Priority {
			return edges[i].Priority < edges[j].Priority
		}
		return edges[i].TargetNodeKey < edges[j].TargetNodeKey
	})
	return edges
}

func edgeDisplayLabel(e WorkflowEdge, isFork bool) string {
	switch e.EdgeType {
	case "true":
		return "If yes"
	case "false":
		return "If no"
	case "hot":
		return "Hot"
	case "warm":
		return "Warm"
	case "cold":
		return "Cold"
	}
	if isFork && e.Priority > 0 {
		return fmt.Sprintf("Priority %d", e.Priority)
	}
	return ""
}
