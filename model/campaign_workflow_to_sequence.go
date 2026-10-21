package model

import (
	"log"
	"strings"

	"emailtracker.com/db"
)

// MigrateLegacyWorkflowCampaignsToSequences converts outreach workflow / workflow_ab
// campaigns into Manage sequences (hard cut — no legacy runtime).
func MigrateLegacyWorkflowCampaignsToSequences() {
	rows, err := db.Query(`
		SELECT id, user_id, COALESCE(execution_mode, 'bulk'), COALESCE(workflow_version_id, 0),
		       COALESCE(template_a_id, 0), COALESCE(template_b_id, 0)
		FROM campaigns
		WHERE execution_mode IN ('workflow', 'workflow_ab')
	`)
	if err != nil {
		log.Printf("migrate workflow→sequence: list: %v", err)
		return
	}
	defer rows.Close()

	type row struct {
		id, userID, versionID, templateA, templateB int64
		mode                                       string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userID, &r.mode, &r.versionID, &r.templateA, &r.templateB); err != nil {
			log.Printf("migrate workflow→sequence: scan: %v", err)
			return
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("migrate workflow→sequence: rows: %v", err)
		return
	}

	for _, r := range list {
		if err := ConvertCampaignWorkflowToSequence(r.id, r.userID, r.mode, r.versionID, r.templateA, r.templateB); err != nil {
			log.Printf("migrate workflow→sequence campaign=%d: %v", r.id, err)
		}
	}
	if len(list) > 0 {
		log.Printf("migrated %d legacy workflow campaign(s) to sequence", len(list))
	}
}

// ConvertCampaignWorkflowToSequence builds sequence steps from the outreach graph and flips mode.
func ConvertCampaignWorkflowToSequence(campaignID, userID int64, mode string, versionID, templateA, templateB int64) error {
	newMode := "sequence"
	if mode == "workflow_ab" || templateB > 0 {
		newMode = "sequence_ab"
	}

	steps := []CampaignSequenceStep{
		{Position: 1, TemplateID: 0, DelaySecondsBefore: 0},
	}
	if versionID > 0 {
		if extracted, err := SequenceStepsFromOutreachWorkflow(versionID, campaignID); err == nil && len(extracted) > 0 {
			steps = extracted
		}
	}
	if err := SaveCampaignSequenceSteps(campaignID, steps); err != nil {
		return err
	}
	_, err := db.Exec(`
		UPDATE campaigns
		SET execution_mode = ?, workflow_version_id = NULL
		WHERE id = ? AND user_id = ?
	`, newMode, campaignID, userID)
	return err
}

// SequenceStepsFromOutreachWorkflow walks the primary path (default / yes / hot) and
// emits linear send steps with waits between them.
func SequenceStepsFromOutreachWorkflow(versionID, campaignID int64) ([]CampaignSequenceStep, error) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}
	mappings, _ := GetCampaignWorkflowTemplates(campaignID)
	nodeMap := map[string]WorkflowNode{}
	for _, n := range graph.Nodes {
		nodeMap[n.NodeKey] = n
	}

	var entry string
	for _, n := range graph.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entry = n.NodeKey
			break
		}
	}
	if entry == "" {
		return nil, nil
	}

	var steps []CampaignSequenceStep
	pendingWait := 0
	seen := map[string]bool{}
	cur := entry
	for cur != "" && !seen[cur] {
		seen[cur] = true
		n, ok := nodeMap[cur]
		if !ok {
			break
		}
		switch n.NodeType {
		case "action_wait":
			pendingWait += waitSecondsFromConfig(n.ConfigJSON)
		case "action_send_email":
			tid := mappings[n.NodeKey]
			if tid <= 0 {
				tid = templateIDFromNodeConfig(n.ConfigJSON)
			}
			pos := len(steps) + 1
			stepTid := tid
			delay := pendingWait
			if pos == 1 {
				stepTid = 0 // first email uses campaign A/B
				delay = 0
			}
			steps = append(steps, CampaignSequenceStep{
				Position:           pos,
				TemplateID:         stepTid,
				DelaySecondsBefore: delay,
			})
			pendingWait = 0
		case "action_end":
			return steps, nil
		}
		cur = nextPrimaryPathNode(graph, cur, n.NodeType)
	}
	return steps, nil
}

func waitSecondsFromConfig(configJSON string) int {
	cfg := ParseNodeConfig(configJSON)
	if v, ok := cfg["duration_seconds"].(float64); ok && v > 0 {
		return int(v)
	}
	return 0
}

func templateIDFromNodeConfig(configJSON string) int64 {
	cfg := ParseNodeConfig(configJSON)
	switch v := cfg["template_id"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func nextPrimaryPathNode(graph WorkflowGraph, sourceKey, sourceType string) string {
	edges := outgoingDisplayEdges(graph, sourceKey, sourceType)
	if len(edges) == 0 {
		return ""
	}
	// Prefer the "happy" branch when splitting.
	prefer := "default"
	switch sourceType {
	case "condition_engagement":
		prefer = "true"
	case "condition_temperature":
		prefer = "hot"
	}
	for _, e := range edges {
		if e.EdgeType == prefer || (prefer == "default" && (e.EdgeType == "default" || e.EdgeType == "")) {
			return e.TargetNodeKey
		}
	}
	return edges[0].TargetNodeKey
}

// TemplateIDFromSendConfig is exported for watcher validation.
func TemplateIDFromSendConfig(configJSON string) int64 {
	return templateIDFromNodeConfig(configJSON)
}

// NormalizeLegacyModeForDisplay maps leftover workflow modes to sequence labels after migration.
func NormalizeLegacyModeForDisplay(mode string) string {
	switch strings.TrimSpace(mode) {
	case "workflow":
		return "sequence"
	case "workflow_ab":
		return "sequence_ab"
	default:
		return mode
	}
}
