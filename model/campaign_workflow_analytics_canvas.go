package model

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
)

// CampaignWorkflowAnalyticsCanvas is a builder-style scrollable graph for analytics.
type CampaignWorkflowAnalyticsCanvas struct {
	Width  float64                       `json:"width"`
	Height float64                       `json:"height"`
	Nodes  []CampaignWorkflowCanvasNode  `json:"nodes"`
	Edges  []CampaignWorkflowCanvasEdge  `json:"edges"`
}

type CampaignWorkflowCanvasNode struct {
	NodeKey       string  `json:"node_key"`
	Label         string  `json:"label"`
	NodeType      string  `json:"node_type"`
	Description   string  `json:"description"`
	PositionX     float64 `json:"position_x"`
	PositionY     float64 `json:"position_y"`
	ContactsHere  int     `json:"contacts_here"`
	PassedThrough int     `json:"passed_through"`
	Opens         int     `json:"opens"`
	OpenRate      float64 `json:"open_rate"`
	StoppedHere   int     `json:"stopped_here"`
	IsMerge       bool    `json:"is_merge"`
	PathSummary   string  `json:"path_summary"`
}

type CampaignWorkflowCanvasEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	EdgeType string `json:"edge_type"`
	Label    string `json:"label"`
	Flow     int    `json:"flow"`
	Priority int    `json:"priority"`
}

// BuildCampaignWorkflowAnalyticsCanvas places live metrics on the published workflow layout.
func BuildCampaignWorkflowAnalyticsCanvas(
	versionID int64,
	enrolled int,
	steps []CampaignWorkflowStepAnalytics,
	edgeFlow map[string]int,
) (CampaignWorkflowAnalyticsCanvas, error) {
	out := CampaignWorkflowAnalyticsCanvas{}
	if versionID <= 0 {
		return out, fmt.Errorf("no workflow version")
	}
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return out, err
	}
	ExpandSkippedNodeEdgeFlows(graph, edgeFlow)

	byKey := map[string]CampaignWorkflowStepAnalytics{}
	for _, s := range steps {
		byKey[s.NodeKey] = s
	}

	const nodeW = 188.0
	const nodeH = 118.0
	maxX, maxY := 1200.0, 560.0

	for _, n := range graph.Nodes {
		label := n.Label
		if label == "" {
			label = LabelFromMap(labelsFromGraph(graph), n.NodeKey)
		}
		s := byKey[n.NodeKey]
		desc := describeWorkflowStep(n, graph)
		if s.Description != "" {
			desc = s.Description
		}
		cn := CampaignWorkflowCanvasNode{
			NodeKey:       n.NodeKey,
			Label:         label,
			NodeType:      n.NodeType,
			Description:   desc,
			PositionX:     n.PositionX,
			PositionY:     n.PositionY,
			ContactsHere:  s.ContactsHere,
			PassedThrough: s.PassedThrough,
			Opens:         s.Opens,
			OpenRate:      s.OpenRate,
			StoppedHere:   s.StoppedHere,
			IsMerge:       s.IsMerge,
			PathSummary:   s.PathSummary,
		}
		// Steps list skips triggers — still show them on the canvas.
		if n.NodeType == "trigger_campaign_started" && s.NodeKey == "" {
			cn.Description = "Campaign starts here"
		}
		out.Nodes = append(out.Nodes, cn)
		if n.PositionX+nodeW+80 > maxX {
			maxX = n.PositionX + nodeW + 80
		}
		if n.PositionY+nodeH+80 > maxY {
			maxY = n.PositionY + nodeH + 80
		}
	}

	for _, e := range graph.Edges {
		et := e.EdgeType
		if et == "" {
			et = "default"
		}
		lab := edgeBranchLabel(e)
		if lab == "" && e.Priority > 0 {
			lab = fmt.Sprintf("P%d", e.Priority)
		}
		out.Edges = append(out.Edges, CampaignWorkflowCanvasEdge{
			Source:   e.SourceNodeKey,
			Target:   e.TargetNodeKey,
			EdgeType: et,
			Label:    lab,
			Flow:     edgeFlow[e.SourceNodeKey+"|"+e.TargetNodeKey],
			Priority: e.Priority,
		})
	}

	out.Width = math.Max(maxX, 1200)
	out.Height = math.Max(maxY, 560)
	_ = enrolled
	return out, nil
}

// CanvasJSON is a compact JSON blob for the analytics canvas script (safe for <script>).
func (c CampaignWorkflowAnalyticsCanvas) CanvasJSON() template.JS {
	b, err := json.Marshal(c)
	if err != nil {
		return template.JS(`{"width":1200,"height":560,"nodes":[],"edges":[]}`)
	}
	return template.JS(b)
}
