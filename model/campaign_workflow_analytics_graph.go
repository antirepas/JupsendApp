package model

import (
	"fmt"

	"emailtracker.com/db"
)

// CampaignWorkflowAnalyticsNode is the branching pipeline used on analytics pages.
type CampaignWorkflowAnalyticsNode struct {
	NodeKey       string
	Label         string
	NodeType      string
	Description   string
	IsForkBranch  bool
	EdgePriority  int
	EdgeType      string
	EdgeLabel     string
	EdgeFlow      int // contacts that took this incoming edge (from prior condition)
	ContactsHere  int
	PassedThrough int
	Opens         int
	OpenRate      float64
	StoppedHere   int
	BarPercent    float64
	IsMerge       bool
	PathSummary   string
	Children      []CampaignWorkflowAnalyticsNode
}

type analyticsGraphMaps struct {
	here     map[string]int
	passed   map[string]int
	opens    map[string]stepEngagement
	stopped  map[string]int
	edgeFlow map[string]int // "source|target" → count
	total    int
}

// BuildCampaignWorkflowAnalyticsTree builds the same branching tree as the campaign detail
// graph, annotated with live analytics counts so forks and merges are visible.
func BuildCampaignWorkflowAnalyticsTree(
	campaign Campaign,
	enrolled int,
	steps []CampaignWorkflowStepAnalytics,
	edgeFlow map[string]int,
) (CampaignWorkflowAnalyticsNode, error) {
	if campaign.WorkflowVersionID <= 0 {
		return CampaignWorkflowAnalyticsNode{}, fmt.Errorf("no workflow version")
	}
	tree, err := BuildCampaignWorkflowGraphTree(campaign, campaign.UserID)
	if err != nil {
		return CampaignWorkflowAnalyticsNode{}, err
	}

	here := map[string]int{}
	passed := map[string]int{}
	opens := map[string]stepEngagement{}
	stopped := map[string]int{}
	pathSummary := map[string]string{}
	isMerge := map[string]bool{}
	for _, s := range steps {
		here[s.NodeKey] = s.ContactsHere
		passed[s.NodeKey] = s.PassedThrough
		opens[s.NodeKey] = stepEngagement{Opens: s.Opens, Clicks: s.Clicks}
		stopped[s.NodeKey] = s.StoppedHere
		pathSummary[s.NodeKey] = s.PathSummary
		isMerge[s.NodeKey] = s.IsMerge
	}

	if enrolled < 1 {
		enrolled = 1
	}
	maps := analyticsGraphMaps{
		here:     here,
		passed:   passed,
		opens:    opens,
		stopped:  stopped,
		edgeFlow: edgeFlow,
		total:    enrolled,
	}

	return annotateAnalyticsTree(tree, maps, pathSummary, isMerge, ""), nil
}

func annotateAnalyticsTree(
	n CampaignWorkflowGraphNode,
	maps analyticsGraphMaps,
	pathSummary map[string]string,
	isMerge map[string]bool,
	parentKey string,
) CampaignWorkflowAnalyticsNode {
	eng := maps.opens[n.NodeKey]
	passed := maps.passed[n.NodeKey]
	here := maps.here[n.NodeKey]
	denom := maps.total
	if denom < 1 {
		denom = 1
	}
	out := CampaignWorkflowAnalyticsNode{
		NodeKey:       n.NodeKey,
		Label:         n.Label,
		NodeType:      n.NodeType,
		Description:   n.Description,
		IsForkBranch:  n.IsForkBranch,
		EdgePriority:  n.EdgePriority,
		EdgeType:      n.EdgeType,
		EdgeLabel:     n.EdgeLabel,
		ContactsHere:  here,
		PassedThrough: passed,
		Opens:         eng.Opens,
		StoppedHere:   maps.stopped[n.NodeKey],
		BarPercent:    float64(here) / float64(denom) * 100,
		IsMerge:       isMerge[n.NodeKey],
		PathSummary:   pathSummary[n.NodeKey],
	}
	if parentKey != "" {
		out.EdgeFlow = maps.edgeFlow[parentKey+"|"+n.NodeKey]
	}
	if passed > 0 && n.NodeType == "action_send_email" {
		out.OpenRate = float64(eng.Opens) / float64(passed) * 100
	}
	for _, child := range n.Children {
		out.Children = append(out.Children, annotateAnalyticsTree(child, maps, pathSummary, isMerge, n.NodeKey))
	}
	return out
}

// getCampaignBranchEdgeFlow counts how many times contacts moved from source → target
// after a successful node execution (used for Hot/Warm/Cold path volume).
func getCampaignBranchEdgeFlow(campaignID int64) map[string]int {
	result := map[string]int{}
	rows, err := db.Query(`
		WITH ranked AS (
			SELECT we.instance_id, we.node_key, we.id,
				LEAD(we.node_key) OVER (PARTITION BY we.instance_id ORDER BY we.id) AS next_node_key
			FROM workflow_executions we
			INNER JOIN workflow_instances wi ON wi.id = we.instance_id
			WHERE wi.campaign_id = ? AND we.status = 'succeeded'
		)
		SELECT node_key, next_node_key, COUNT(*)
		FROM ranked
		WHERE next_node_key IS NOT NULL
		GROUP BY node_key, next_node_key
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var src, dst string
		var n int
		if rows.Scan(&src, &dst, &n) == nil {
			result[src+"|"+dst] = n
		}
	}
	return result
}

// ExpandBranchEdgeFlowThroughSkippedConditions attributes hops that skipped an unrecorded
// temperature/engagement node (older runs) onto the condition→child edge for analytics badges.
func ExpandBranchEdgeFlowThroughSkippedConditions(graph WorkflowGraph, flow map[string]int) {
	if len(flow) == 0 {
		return
	}
	for _, n := range graph.Nodes {
		if n.NodeType != "condition_temperature" && n.NodeType != "condition_engagement" {
			continue
		}
		childSet := map[string]bool{}
		for _, child := range outgoingDisplayEdges(graph, n.NodeKey, n.NodeType) {
			childSet[child.TargetNodeKey] = true
		}
		if len(childSet) == 0 {
			continue
		}
		for key, c := range flow {
			src, dst, ok := splitEdgeKey(key)
			if !ok || c <= 0 || !childSet[dst] || src == n.NodeKey {
				continue
			}
			if !isGraphAncestor(graph, src, n.NodeKey, 4) {
				continue
			}
			condKey := n.NodeKey + "|" + dst
			if flow[condKey] < c {
				flow[condKey] = c
			}
		}
	}
}

func splitEdgeKey(key string) (src, dst string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}

func isGraphAncestor(graph WorkflowGraph, from, to string, maxDepth int) bool {
	if from == to {
		return true
	}
	adj := map[string][]string{}
	for _, e := range graph.Edges {
		adj[e.SourceNodeKey] = append(adj[e.SourceNodeKey], e.TargetNodeKey)
	}
	seen := map[string]bool{from: true}
	queue := []string{from}
	depth := map[string]int{from: 0}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if depth[cur] >= maxDepth {
			continue
		}
		for _, next := range adj[cur] {
			if next == to {
				return true
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			depth[next] = depth[cur] + 1
			queue = append(queue, next)
		}
	}
	return false
}

func pathLabelForHop(src, dst string, graph WorkflowGraph, edgeBySrcDst map[string]WorkflowEdge, condSet map[string]bool) string {
	if e, ok := edgeBySrcDst[src+"|"+dst]; ok {
		if lab := edgeDisplayLabel(e, true); lab != "" {
			return lab
		}
	}
	if condSet[src] {
		return ""
	}
	for _, n := range graph.Nodes {
		if !condSet[n.NodeKey] {
			continue
		}
		if !isGraphAncestor(graph, src, n.NodeKey, 4) {
			continue
		}
		if e, ok := edgeBySrcDst[n.NodeKey+"|"+dst]; ok {
			if lab := edgeDisplayLabel(e, true); lab != "" {
				return lab
			}
		}
	}
	return ""
}

// getCampaignLastPathLabels returns contact_id → last temperature/engagement path label (Hot, Cold, If yes…).
func getCampaignLastPathLabels(campaignID, versionID int64) map[int64]string {
	result := map[int64]string{}
	if versionID <= 0 {
		return result
	}
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return result
	}
	edgeBySrcDst := map[string]WorkflowEdge{}
	for _, e := range graph.Edges {
		edgeBySrcDst[e.SourceNodeKey+"|"+e.TargetNodeKey] = e
	}
	condSet := map[string]bool{}
	for _, n := range graph.Nodes {
		if n.NodeType == "condition_temperature" || n.NodeType == "condition_engagement" {
			condSet[n.NodeKey] = true
		}
	}
	if len(condSet) == 0 {
		return result
	}

	rows, err := db.Query(`
		WITH ranked AS (
			SELECT wi.contact_id, we.node_key, we.id,
				LEAD(we.node_key) OVER (PARTITION BY we.instance_id ORDER BY we.id) AS next_node_key
			FROM workflow_executions we
			INNER JOIN workflow_instances wi ON wi.id = we.instance_id
			WHERE wi.campaign_id = ? AND we.status = 'succeeded'
		)
		SELECT contact_id, node_key, next_node_key
		FROM ranked
		WHERE next_node_key IS NOT NULL
		ORDER BY contact_id, id DESC
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var contactID int64
		var src, dst string
		if rows.Scan(&contactID, &src, &dst) != nil {
			continue
		}
		if seen[contactID] {
			continue
		}
		lab := pathLabelForHop(src, dst, graph, edgeBySrcDst, condSet)
		if lab == "" {
			continue
		}
		seen[contactID] = true
		result[contactID] = lab
	}
	return result
}

// EnrichBranchPathLabels fills PathLabel for contacts that passed a temperature/engagement split.
func EnrichBranchPathLabels(campaignID, versionID int64, contacts []CampaignWorkflowContactAnalytics) {
	labels := getCampaignLastPathLabels(campaignID, versionID)
	if len(labels) == 0 {
		return
	}
	for i := range contacts {
		if lab, ok := labels[contacts[i].ContactID]; ok {
			contacts[i].PathLabel = lab
		}
	}
}

// GetCampaignLastPathLabelsForUI exposes path labels for campaign detail tables.
func GetCampaignLastPathLabelsForUI(campaignID, versionID int64) map[int64]string {
	return getCampaignLastPathLabels(campaignID, versionID)
}
