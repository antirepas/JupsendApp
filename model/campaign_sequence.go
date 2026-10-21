package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

// CampaignSequenceStep is one send in a Manage-configured linear sequence.
// Position 0 is the first email (template_id 0 = use campaign A/B templates).
// DelaySecondsBefore is wait time before this send (0 for the first step).
type CampaignSequenceStep struct {
	ID                 int64
	CampaignID         int64
	Position           int
	TemplateID         int64
	DelaySecondsBefore int
}

// DelayDays is wait days before this step (for templates).
func (s CampaignSequenceStep) DelayDays() int {
	if s.DelaySecondsBefore <= 0 {
		return 0
	}
	return s.DelaySecondsBefore / 86400
}

// IsSequenceMode reports whether the campaign runs a Manage sequence.
func IsSequenceMode(executionMode string) bool {
	switch strings.TrimSpace(executionMode) {
	case "sequence", "sequence_ab":
		return true
	default:
		return false
	}
}

// IsLegacyWorkflowMode is the old send→wait→send workflow attachment.
func IsLegacyWorkflowMode(executionMode string) bool {
	switch strings.TrimSpace(executionMode) {
	case "workflow", "workflow_ab":
		return true
	default:
		return false
	}
}

// ListCampaignSequenceSteps returns ordered steps for a campaign.
func ListCampaignSequenceSteps(campaignID int64) ([]CampaignSequenceStep, error) {
	rows, err := db.Query(`
		SELECT id, campaign_id, position, COALESCE(template_id, 0), COALESCE(delay_seconds_before, 0)
		FROM campaign_sequence_steps
		WHERE campaign_id = ?
		ORDER BY position ASC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignSequenceStep
	for rows.Next() {
		var s CampaignSequenceStep
		if err := rows.Scan(&s.ID, &s.CampaignID, &s.Position, &s.TemplateID, &s.DelaySecondsBefore); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveCampaignSequenceSteps replaces all steps for a campaign (draft only — caller checks).
func SaveCampaignSequenceSteps(campaignID int64, steps []CampaignSequenceStep) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM campaign_sequence_steps WHERE campaign_id = ?`, campaignID); err != nil {
		return err
	}
	for i, s := range steps {
		pos := s.Position
		if pos <= 0 {
			pos = i + 1
		}
		delay := s.DelaySecondsBefore
		if delay < 0 {
			delay = 0
		}
		if _, err := tx.Exec(`
			INSERT INTO campaign_sequence_steps (campaign_id, position, template_id, delay_seconds_before)
			VALUES (?, ?, ?, ?)
		`, campaignID, pos, s.TemplateID, delay); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EnsureDefaultSequenceSteps creates a single first-email step if none exist.
func EnsureDefaultSequenceSteps(campaignID int64, templateAID int64) error {
	steps, err := ListCampaignSequenceSteps(campaignID)
	if err != nil {
		return err
	}
	if len(steps) > 0 {
		return nil
	}
	if templateAID <= 0 {
		return fmt.Errorf("campaign has no template A")
	}
	return SaveCampaignSequenceSteps(campaignID, []CampaignSequenceStep{
		{CampaignID: campaignID, Position: 1, TemplateID: 0, DelaySecondsBefore: 0},
	})
}

// ApplyRecommendedSequence inserts a simple 3-touch sequence using folder templates.
// first = A/B (template_id 0), then wait 3d + tpl2, wait 3d + tpl3 when available.
func ApplyRecommendedSequence(campaignID int64, templateIDs []int64) error {
	if len(templateIDs) == 0 {
		return fmt.Errorf("need at least one template")
	}
	const day = 86400
	steps := []CampaignSequenceStep{
		{Position: 1, TemplateID: 0, DelaySecondsBefore: 0},
	}
	pos := 2
	for i := 1; i < len(templateIDs) && i < 3; i++ {
		steps = append(steps, CampaignSequenceStep{
			Position:           pos,
			TemplateID:         templateIDs[i],
			DelaySecondsBefore: 3 * day,
		})
		pos++
	}
	return SaveCampaignSequenceSteps(campaignID, steps)
}

// ValidateCampaignSequenceReady checks sequence campaigns can launch.
func ValidateCampaignSequenceReady(c Campaign) error {
	if !IsSequenceMode(c.ExecutionMode) {
		return nil
	}
	if c.TemplateAID <= 0 {
		return fmt.Errorf("set template A for the first email")
	}
	if c.ExecutionMode == "sequence_ab" && c.TemplateBID <= 0 {
		return fmt.Errorf("set template B for A/B first email")
	}
	steps, err := ListCampaignSequenceSteps(c.ID)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return fmt.Errorf("add at least one email in the sequence")
	}
	for i, s := range steps {
		if i == 0 {
			continue
		}
		if s.TemplateID <= 0 {
			return fmt.Errorf("follow-up step %d needs a template", i+1)
		}
		if _, _, err := GetTemplateByID(s.TemplateID, c.UserID); err != nil {
			return fmt.Errorf("follow-up step %d: invalid template", i+1)
		}
	}
	return nil
}

// ResolveSequenceStepTemplate picks the template for a step + contact variant.
func ResolveSequenceStepTemplate(c Campaign, step CampaignSequenceStep, stepIndex int, variant string) (int64, error) {
	if stepIndex == 0 || step.Position == 1 {
		if c.ExecutionMode == "sequence_ab" && c.TemplateBID > 0 && strings.EqualFold(variant, "B") {
			return c.TemplateBID, nil
		}
		if c.TemplateAID <= 0 {
			return 0, fmt.Errorf("no template A")
		}
		return c.TemplateAID, nil
	}
	if step.TemplateID <= 0 {
		return 0, fmt.Errorf("step has no template")
	}
	return step.TemplateID, nil
}

// CampaignSequenceInstance tracks one contact through a Manage sequence.
type CampaignSequenceInstance struct {
	ID              int64
	CampaignID      int64
	ContactID       int64
	Status          string
	StepIndex       int
	Variant         string
	LastEmailSendID int64
	NextWakeAt      *time.Time
	ContextJSON     string
}

// CreateSequenceInstance starts a contact on a sequence (idempotent per campaign+contact).
func CreateSequenceInstance(campaignID, contactID int64, variant string) (id int64, created bool, err error) {
	variant = strings.TrimSpace(variant)
	now := time.Now()
	res, err := db.Exec(`
		INSERT INTO campaign_sequence_instances
			(campaign_id, contact_id, status, step_index, variant, next_wake_at, created_at, updated_at)
		VALUES (?, ?, 'active', 0, ?, ?, ?, ?)
		ON CONFLICT (campaign_id, contact_id) DO NOTHING
	`, campaignID, contactID, variant, now, now, now)
	if err != nil {
		return 0, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var existing int64
		_ = db.QueryRow(`SELECT id FROM campaign_sequence_instances WHERE campaign_id = ? AND contact_id = ?`,
			campaignID, contactID).Scan(&existing)
		return existing, false, nil
	}
	var newID int64
	err = db.QueryRow(`SELECT id FROM campaign_sequence_instances WHERE campaign_id = ? AND contact_id = ?`,
		campaignID, contactID).Scan(&newID)
	return newID, true, err
}

// ClaimDueSequenceInstances locks due active/waiting instances for processing.
func ClaimDueSequenceInstances(limit int) ([]CampaignSequenceInstance, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now()
	rows, err := db.Query(`
		SELECT id, campaign_id, contact_id, status, step_index, COALESCE(variant, ''),
			COALESCE(last_email_send_id, 0), next_wake_at, COALESCE(context_json, '{}')
		FROM campaign_sequence_instances
		WHERE status = 'active'
		   OR (status = 'waiting' AND next_wake_at IS NOT NULL AND next_wake_at <= ?)
		ORDER BY COALESCE(next_wake_at, created_at) ASC
		LIMIT ?
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignSequenceInstance
	for rows.Next() {
		var inst CampaignSequenceInstance
		var wake *time.Time
		if err := rows.Scan(&inst.ID, &inst.CampaignID, &inst.ContactID, &inst.Status, &inst.StepIndex,
			&inst.Variant, &inst.LastEmailSendID, &wake, &inst.ContextJSON); err != nil {
			return nil, err
		}
		inst.NextWakeAt = wake
		out = append(out, inst)
	}
	return out, rows.Err()
}

// UpdateSequenceInstance advances or parks an instance.
func UpdateSequenceInstance(id int64, status string, stepIndex int, lastSendID int64, wakeAt *time.Time) error {
	_, err := db.Exec(`
		UPDATE campaign_sequence_instances
		SET status = ?, step_index = ?, last_email_send_id = ?, next_wake_at = ?, updated_at = ?
		WHERE id = ?
	`, status, stepIndex, lastSendID, wakeAt, time.Now(), id)
	return err
}

// CompleteSequenceInstance marks done.
func CompleteSequenceInstance(id int64) error {
	return UpdateSequenceInstance(id, "completed", 0, 0, nil)
}

// CancelSequenceInstance cancels one instance.
func CancelSequenceInstance(id int64) error {
	_, err := db.Exec(`
		UPDATE campaign_sequence_instances
		SET status = 'cancelled', next_wake_at = NULL, updated_at = ?
		WHERE id = ? AND status IN ('active', 'waiting')
	`, time.Now(), id)
	return err
}

// CancelSequenceInstancesForContactCampaign cancels sequence progress + pending jobs.
func CancelSequenceInstancesForContactCampaign(contactID, campaignID int64) error {
	if contactID <= 0 || campaignID <= 0 {
		return nil
	}
	_, _ = db.Exec(`
		UPDATE campaign_sequence_instances
		SET status = 'cancelled', next_wake_at = NULL, updated_at = ?
		WHERE contact_id = ? AND campaign_id = ? AND status IN ('active', 'waiting')
	`, time.Now(), contactID, campaignID)
	_, _ = db.Exec(`
		UPDATE send_jobs SET status = 'cancelled', last_error = 'stopped by campaign policy',
			lock_token = NULL, updated_at = ?
		WHERE contact_id = ? AND campaign_id = ? AND status IN ('pending', 'processing')
	`, time.Now(), contactID, campaignID)
	_, _ = db.Exec(`
		UPDATE email_sends SET delivery_status = 'cancelled'
		WHERE contact_id = ? AND campaign_id = ?
		  AND LOWER(COALESCE(delivery_status, '')) IN ('queued', 'sending')
	`, contactID, campaignID)
	_, _ = db.Exec(`
		UPDATE reply_auto_jobs SET status = 'cancelled', updated_at = ?
		WHERE contact_id = ? AND campaign_id = ? AND status = 'pending'
	`, time.Now(), contactID, campaignID)
	return nil
}

// CountActiveSequenceInstances returns active+waiting count for a campaign.
func CountActiveSequenceInstances(campaignID int64) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM campaign_sequence_instances
		WHERE campaign_id = ? AND status IN ('active', 'waiting')
	`, campaignID).Scan(&n)
	return n, err
}
