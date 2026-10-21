package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"emailtracker.com/db"
)

// CampaignWatcherWorkflow is a published reply-watcher workflow attached to a campaign.
type CampaignWatcherWorkflow struct {
	WorkflowID      int64
	WorkflowName    string
	VersionID       int64
	Enabled         bool
	TriggerIntents  []string
	SendSteps       []WorkflowSendStep
}

// ListFolderReplyWorkflows returns published workflows in a folder that have reply-intent triggers.
func ListFolderReplyWorkflows(userID, folderID int64) ([]CampaignWatcherWorkflow, error) {
	if folderID <= 0 {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT w.id, w.name, w.current_version_id
		FROM workflows w
		WHERE w.tenant_id = ? AND w.folder_id = ? AND w.status = 'active'
		ORDER BY w.created_at ASC, w.id ASC
	`, userID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignWatcherWorkflow
	for rows.Next() {
		var w CampaignWatcherWorkflow
		var tip int64
		if err := rows.Scan(&w.WorkflowID, &w.WorkflowName, &tip); err != nil {
			return nil, err
		}
		pub, err := LatestPublishedVersionID(w.WorkflowID)
		if err != nil || pub <= 0 {
			continue
		}
		w.VersionID = pub
		intents, err := ListReplyTriggerIntents(pub)
		if err != nil || len(intents) == 0 {
			continue
		}
		w.TriggerIntents = intents
		steps, _ := ListSendEmailSteps(pub)
		w.SendSteps = steps
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListReplyTriggerIntents returns distinct intents configured on trigger_reply_intent nodes.
func ListReplyTriggerIntents(versionID int64) ([]string, error) {
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range g.Nodes {
		if n.NodeType != "trigger_reply_intent" {
			continue
		}
		intent := ReplyIntentFromNodeConfig(n.ConfigJSON)
		if intent == "" || seen[intent] {
			continue
		}
		seen[intent] = true
		out = append(out, intent)
	}
	return out, nil
}

// ReplyIntentFromNodeConfig reads intent from trigger config JSON.
func ReplyIntentFromNodeConfig(configJSON string) string {
	cfg := ParseNodeConfig(configJSON)
	if v, ok := cfg["intent"].(string); ok {
		return NormalizeReplyIntent(v)
	}
	return ""
}

// IsReplyWatcherWorkflow reports whether the published (or tip) graph is reply-triggered.
func IsReplyWatcherWorkflow(versionID int64) bool {
	intents, err := ListReplyTriggerIntents(versionID)
	return err == nil && len(intents) > 0
}

// ListCampaignWatcherWorkflowIDs returns attached watcher workflow IDs.
func ListCampaignWatcherWorkflowIDs(campaignID int64) ([]int64, error) {
	rows, err := db.Query(`SELECT workflow_id FROM campaign_watcher_workflows WHERE campaign_id = ?`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SaveCampaignWatcherWorkflows replaces which watcher workflows are attached.
func SaveCampaignWatcherWorkflows(campaignID int64, workflowIDs []int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM campaign_watcher_workflows WHERE campaign_id = ?`, campaignID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, wid := range workflowIDs {
		if wid <= 0 || seen[wid] {
			continue
		}
		seen[wid] = true
		if _, err := tx.Exec(`
			INSERT INTO campaign_watcher_workflows (campaign_id, workflow_id) VALUES (?, ?)
		`, campaignID, wid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FindReplyWatcherTriggersForCampaign finds published trigger nodes matching intent on attached workflows.
type ReplyWatcherMatch struct {
	WorkflowID   int64
	VersionID    int64
	TriggerKey   string
	TriggerLabel string
	Intent       string
}

func FindReplyWatcherTriggersForCampaign(campaignID int64, intent string) ([]ReplyWatcherMatch, error) {
	intent = NormalizeReplyIntent(intent)
	if intent == "" || campaignID <= 0 {
		return nil, nil
	}
	ids, err := ListCampaignWatcherWorkflowIDs(campaignID)
	if err != nil {
		return nil, err
	}
	var out []ReplyWatcherMatch
	for _, wid := range ids {
		pub, err := LatestPublishedVersionID(wid)
		if err != nil || pub <= 0 {
			continue
		}
		g, err := GetWorkflowGraph(pub)
		if err != nil {
			continue
		}
		for _, n := range g.Nodes {
			if n.NodeType != "trigger_reply_intent" {
				continue
			}
			ni := ReplyIntentFromNodeConfig(n.ConfigJSON)
			if ni != intent {
				continue
			}
			label := n.Label
			if label == "" {
				label = intent
			}
			out = append(out, ReplyWatcherMatch{
				WorkflowID: wid, VersionID: pub, TriggerKey: n.NodeKey,
				TriggerLabel: label, Intent: intent,
			})
		}
	}
	return out, nil
}

// CreateReplyWatcherInstance starts a contact at a reply-intent trigger (allows concurrent with completed outreach).
func CreateReplyWatcherInstance(versionID, contactID, campaignID int64, triggerNodeKey, contextJSON string) (int64, error) {
	if strings.TrimSpace(contextJSON) == "" {
		contextJSON = "{}"
	}
	var camp interface{}
	if campaignID > 0 {
		camp = campaignID
	}
	// Avoid duplicate active instance on the same trigger for this contact+campaign.
	var existing int64
	err := db.QueryRow(`
		SELECT id FROM workflow_instances
		WHERE campaign_id = ? AND contact_id = ? AND workflow_version_id = ?
		  AND current_node_key = ? AND status IN ('active', 'waiting')
		  AND fork_root_id IS NULL
		ORDER BY id ASC LIMIT 1
	`, campaignID, contactID, versionID, triggerNodeKey).Scan(&existing)
	if err == nil && existing > 0 {
		return existing, nil
	}
	row := db.QueryRow(`
		INSERT INTO workflow_instances (
			workflow_version_id, contact_id, campaign_id, current_node_key, status, context_json
		) VALUES (?, ?, ?, ?, 'active', ?) RETURNING id
	`, versionID, contactID, camp, triggerNodeKey, contextJSON)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// WorkflowHasCampaignStart reports legacy outreach graphs.
func WorkflowHasCampaignStart(versionID int64) bool {
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return false
	}
	for _, n := range g.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			return true
		}
	}
	return false
}

// FormatReplyIntentLabel is a short UI label.
func FormatReplyIntentLabel(intent string) string {
	intent = NormalizeReplyIntent(intent)
	if intent == "" {
		return "reply"
	}
	return strings.ReplaceAll(intent, "_", " ")
}

// MarshalIntentContext builds instance context for a reply-triggered run.
func MarshalIntentContext(intent string, messageID int64) string {
	b, _ := json.Marshal(map[string]interface{}{
		"reply_intent":     NormalizeReplyIntent(intent),
		"reply_message_id": messageID,
		"watcher":          true,
	})
	return string(b)
}

// ValidateReplyTriggerConfig ensures intent is set.
func ValidateReplyTriggerConfig(configJSON string) error {
	if ReplyIntentFromNodeConfig(configJSON) == "" {
		return fmt.Errorf("reply trigger needs an intent")
	}
	return nil
}
