package model

import (
	"fmt"
	"strings"

	"emailtracker.com/db"
)

type WorkflowSendStep struct {
	NodeKey string `json:"node_key"`
	Label   string `json:"label"`
}

// SaveCampaignWorkflowTemplates stores the template mapping for send steps in a workflow campaign.
// It replaces any existing mapping for the campaign.
func SaveCampaignWorkflowTemplates(campaignID int64, templates map[string]int64) error {
	if campaignID <= 0 {
		return fmt.Errorf("campaign_id required")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM campaign_workflow_step_templates WHERE campaign_id = ?`, campaignID); err != nil {
		return err
	}

	for nodeKey, templateID := range templates {
		if strings.TrimSpace(nodeKey) == "" {
			return fmt.Errorf("empty node_key in template mapping")
		}
		if templateID <= 0 {
			return fmt.Errorf("missing template_id for node %s", nodeKey)
		}
		if _, err := tx.Exec(
			`INSERT INTO campaign_workflow_step_templates (campaign_id, node_key, template_id) VALUES (?, ?, ?)`,
			campaignID, nodeKey, templateID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func GetCampaignWorkflowTemplates(campaignID int64) (map[string]int64, error) {
	rows, err := db.Query(`SELECT node_key, template_id FROM campaign_workflow_step_templates WHERE campaign_id = ?`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var nodeKey string
		var templateID int64
		if err := rows.Scan(&nodeKey, &templateID); err != nil {
			return nil, err
		}
		out[nodeKey] = templateID
	}
	return out, nil
}

func ListSendEmailSteps(versionID int64) ([]WorkflowSendStep, error) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}

	steps := listDisplayReachableSendSteps(graph)
	if len(steps) == 0 {
		return nil, fmt.Errorf("workflow has no send-email steps")
	}
	return steps, nil
}

// listDisplayReachableSendSteps returns send steps reachable via the same edges shown in the campaign manage UI.
func listDisplayReachableSendSteps(graph WorkflowGraph) []WorkflowSendStep {
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

	nodeMap := map[string]WorkflowNode{}
	for _, n := range graph.Nodes {
		nodeMap[n.NodeKey] = n
	}

	var steps []WorkflowSendStep
	seen := map[string]bool{}
	var walk func(nodeKey string, path map[string]bool)
	walk = func(nodeKey string, path map[string]bool) {
		if path == nil {
			path = map[string]bool{}
		}
		if path[nodeKey] {
			return
		}
		path[nodeKey] = true
		defer delete(path, nodeKey)

		n, ok := nodeMap[nodeKey]
		if !ok {
			return
		}
		if n.NodeType == "action_send_email" && !seen[nodeKey] {
			seen[nodeKey] = true
			label := n.Label
			if label == "" {
				label = "Send email"
			}
			steps = append(steps, WorkflowSendStep{NodeKey: nodeKey, Label: label})
		}

		children := outgoingDisplayEdges(graph, nodeKey, n.NodeType)
		if n.NodeType == "trigger_campaign_started" && len(children) == 1 {
			walk(children[0].TargetNodeKey, path)
			return
		}
		for _, e := range children {
			walk(e.TargetNodeKey, copyPath(path))
		}
	}
	walk(entryKey, nil)
	return steps
}

func GetFirstSendNodeKey(versionID int64) (string, error) {
	steps, err := ListSendEmailSteps(versionID)
	if err != nil {
		return "", err
	}
	if len(steps) == 0 {
		return "", fmt.Errorf("workflow has no send-email steps")
	}
	return steps[0].NodeKey, nil
}

// ResolveCampaignSendTemplate resolves which template should be used for a given workflow send step.
//
// execution_mode:
// - workflow: mapping must exist for every send node
// - workflow_ab: first send node uses TemplateAID/TemplateBID based on variant; other send nodes use mapping
//
// Backward compat:
// If no mapping exists, fall back to template_id in the workflow node config_json (if present).
func ResolveCampaignSendTemplate(campaignID int64, nodeKey string, variant string, workflowVersionID int64) (int64, error) {
	campaign, err := GetCampaign(campaignID)
	if err != nil {
		return 0, err
	}

	templates, err := GetCampaignWorkflowTemplates(campaignID)
	if err != nil {
		return 0, err
	}

	firstSendNodeKey, err := GetFirstSendNodeKey(workflowVersionID)
	if err != nil {
		return 0, err
	}

	execMode := strings.TrimSpace(campaign.ExecutionMode)
	if execMode == "workflow_ab" && nodeKey == firstSendNodeKey {
		if variant == "B" && campaign.TemplateBID > 0 {
			return campaign.TemplateBID, nil
		}
		return campaign.TemplateAID, nil
	}

	if t, ok := templates[nodeKey]; ok && t > 0 {
		return t, nil
	}

	// Backward compat: workflow nodes may still store template_id in config_json.
	graph, err := GetWorkflowGraph(workflowVersionID)
	if err != nil {
		return 0, err
	}
	for _, n := range graph.Nodes {
		if n.NodeKey != nodeKey || n.NodeType != "action_send_email" {
			continue
		}
		cfg := ParseNodeConfig(n.ConfigJSON)
		if v, ok := cfg["template_id"].(float64); ok && int64(v) > 0 {
			return int64(v), nil
		}
		if v, ok := cfg["template_id"].(int64); ok && v > 0 {
			return v, nil
		}
		if v, ok := cfg["template_id"].(int); ok && v > 0 {
			return int64(v), nil
		}
	}

	return 0, fmt.Errorf("missing template mapping for send step %s", nodeKey)
}

// MergeWorkflowTemplateMappings combines posted values with any existing campaign mappings.
func MergeWorkflowTemplateMappings(campaignID int64, incoming map[string]int64) map[string]int64 {
	merged := make(map[string]int64)
	existing, _ := GetCampaignWorkflowTemplates(campaignID)
	for k, v := range existing {
		if v > 0 {
			merged[k] = v
		}
	}
	for k, v := range incoming {
		if v > 0 {
			merged[k] = v
		}
	}
	return merged
}

func workflowNodeTemplateIDs(graph WorkflowGraph) map[string]int64 {
	nodeTemplates := make(map[string]int64)
	for _, n := range graph.Nodes {
		if n.NodeType != "action_send_email" {
			continue
		}
		cfg := ParseNodeConfig(n.ConfigJSON)
		if v, ok := cfg["template_id"].(float64); ok && int64(v) > 0 {
			nodeTemplates[n.NodeKey] = int64(v)
		} else if v, ok := cfg["template_id"].(int64); ok && v > 0 {
			nodeTemplates[n.NodeKey] = v
		} else if v, ok := cfg["template_id"].(int); ok && v > 0 {
			nodeTemplates[n.NodeKey] = int64(v)
		}
	}
	return nodeTemplates
}

// EnrichWorkflowTemplateMappings fills gaps from legacy node config template_id values.
func EnrichWorkflowTemplateMappings(versionID int64, mappings map[string]int64) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return
	}
	for nodeKey, tid := range workflowNodeTemplateIDs(graph) {
		if mappings[nodeKey] <= 0 {
			mappings[nodeKey] = tid
		}
	}
}

// ValidateWorkflowStepTemplateMappings checks that every visible send step has a template mapped.
func ValidateWorkflowStepTemplateMappings(campaign Campaign, mappings map[string]int64) error {
	sendSteps, err := ListSendEmailSteps(campaign.WorkflowVersionID)
	if err != nil {
		return err
	}

	graph, err := GetWorkflowGraph(campaign.WorkflowVersionID)
	if err != nil {
		return err
	}
	nodeTemplates := workflowNodeTemplateIDs(graph)

	execMode := strings.TrimSpace(campaign.ExecutionMode)
	firstSendNodeKey := ""
	if execMode == "workflow_ab" {
		firstSendNodeKey, err = GetFirstSendNodeKey(campaign.WorkflowVersionID)
		if err != nil {
			return err
		}
	}

	for _, step := range sendSteps {
		if execMode == "workflow_ab" && step.NodeKey == firstSendNodeKey {
			continue
		}
		if mappings[step.NodeKey] > 0 || nodeTemplates[step.NodeKey] > 0 {
			continue
		}
		if execMode == "workflow_ab" {
			return fmt.Errorf("template mapping required for follow-up send step %s", step.Label)
		}
		return fmt.Errorf("template mapping required for send step %s", step.Label)
	}
	return nil
}

func ValidateCampaignWorkflowReady(campaign Campaign) error {
	if campaign.WorkflowVersionID <= 0 {
		return fmt.Errorf("workflow_version_id required")
	}

	sendSteps, err := ListSendEmailSteps(campaign.WorkflowVersionID)
	if err != nil {
		return err
	}
	if len(sendSteps) == 0 {
		return fmt.Errorf("workflow has no send-email steps")
	}

	templates, err := GetCampaignWorkflowTemplates(campaign.ID)
	if err != nil {
		return err
	}

	graph, err := GetWorkflowGraph(campaign.WorkflowVersionID)
	if err != nil {
		return err
	}
	nodeTemplates := workflowNodeTemplateIDs(graph)

	execMode := strings.TrimSpace(campaign.ExecutionMode)
	switch execMode {
	case "workflow":
		for _, step := range sendSteps {
			if templates[step.NodeKey] > 0 || nodeTemplates[step.NodeKey] > 0 {
				continue
			}
			return fmt.Errorf("template mapping required for send step %s", step.NodeKey)
		}
		return nil
	case "workflow_ab":
		firstSendNodeKey, err := GetFirstSendNodeKey(campaign.WorkflowVersionID)
		if err != nil {
			return err
		}
		if campaign.TemplateAID <= 0 {
			return fmt.Errorf("Template A is required for hybrid workflow")
		}
		if campaign.TemplateBID <= 0 {
			return fmt.Errorf("Template B is required for hybrid workflow")
		}
		for _, step := range sendSteps {
			if step.NodeKey == firstSendNodeKey {
				continue
			}
			if templates[step.NodeKey] > 0 || nodeTemplates[step.NodeKey] > 0 {
				continue
			}
			return fmt.Errorf("template mapping required for send step %s", step.NodeKey)
		}
		return nil
	default:
		return nil
	}
}

// MigrateLegacyCampaignWorkflowStepTemplates copies template_id from workflow node configs
// into campaign_workflow_step_templates for campaigns that have no mappings yet.
func MigrateLegacyCampaignWorkflowStepTemplates() {
	rows, err := db.Query(`
		SELECT id, workflow_version_id FROM campaigns
		WHERE execution_mode IN ('workflow', 'workflow_ab') AND workflow_version_id > 0
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var campaignID, versionID int64
		if err := rows.Scan(&campaignID, &versionID); err != nil {
			continue
		}
		existing, err := GetCampaignWorkflowTemplates(campaignID)
		if err != nil || len(existing) > 0 {
			continue
		}
		graph, err := GetWorkflowGraph(versionID)
		if err != nil {
			continue
		}
		mappings := make(map[string]int64)
		for _, n := range graph.Nodes {
			if n.NodeType != "action_send_email" {
				continue
			}
			cfg := ParseNodeConfig(n.ConfigJSON)
			var tid int64
			if v, ok := cfg["template_id"].(float64); ok {
				tid = int64(v)
			} else if v, ok := cfg["template_id"].(int64); ok {
				tid = v
			} else if v, ok := cfg["template_id"].(int); ok {
				tid = int64(v)
			}
			if tid > 0 {
				mappings[n.NodeKey] = tid
			}
		}
		if len(mappings) == 0 {
			continue
		}
		_ = SaveCampaignWorkflowTemplates(campaignID, mappings)
	}
}

