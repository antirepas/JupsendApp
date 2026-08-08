package model

import (
	"log"
	"time"

	"emailtracker.com/db"
)

// CancelActiveInstancesForContactCampaign cancels active/waiting workflow instances
// for one contact in one campaign, and drops pending queue jobs for that pair.
func CancelActiveInstancesForContactCampaign(contactID, campaignID int64) error {
	if contactID <= 0 || campaignID <= 0 {
		return nil
	}
	rows, err := db.Query(`
		SELECT id FROM workflow_instances
		WHERE contact_id = ? AND campaign_id = ? AND status IN ('active', 'waiting')
	`, contactID, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			_ = CancelInstance(id)
		}
	}
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
	return nil
}

// ApplyStopOnReplyForContact cancels workflow progress for campaigns that opted into stop-on-reply.
// preferCampaignID (from the matched send) is checked first; other active campaigns for the contact are also gated by their own flags.
func ApplyStopOnReplyForContact(contactID, preferCampaignID int64) {
	if contactID <= 0 {
		return
	}
	seen := map[int64]bool{}
	if preferCampaignID > 0 {
		seen[preferCampaignID] = true
		if camp, err := GetCampaign(preferCampaignID); err == nil && camp.StopOnReply {
			if err := CancelActiveInstancesForContactCampaign(contactID, preferCampaignID); err != nil {
				log.Printf("stop-on-reply campaign=%d contact=%d: %v", preferCampaignID, contactID, err)
			}
		}
	}
	rows, err := db.Query(`
		SELECT DISTINCT COALESCE(campaign_id, 0) FROM workflow_instances
		WHERE contact_id = ? AND status IN ('active', 'waiting') AND COALESCE(campaign_id, 0) > 0
	`, contactID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var campaignID int64
		if rows.Scan(&campaignID) != nil || campaignID <= 0 || seen[campaignID] {
			continue
		}
		seen[campaignID] = true
		camp, err := GetCampaign(campaignID)
		if err != nil || !camp.StopOnReply {
			continue
		}
		if err := CancelActiveInstancesForContactCampaign(contactID, campaignID); err != nil {
			log.Printf("stop-on-reply campaign=%d contact=%d: %v", campaignID, contactID, err)
		}
	}
}

// MaybeStopWorkflowOnHot cancels remaining workflow steps for this contact when they become hot
// and the campaign has stop_on_hot enabled.
func MaybeStopWorkflowOnHot(campaignID, contactID int64) {
	if campaignID <= 0 || contactID <= 0 {
		return
	}
	camp, err := GetCampaign(campaignID)
	if err != nil || !camp.StopOnHot {
		return
	}
	tier, err := ResolveLeadTemperature(campaignID, contactID)
	if err != nil || tier != "hot" {
		return
	}
	if err := CancelActiveInstancesForContactCampaign(contactID, campaignID); err != nil {
		log.Printf("stop-on-hot campaign=%d contact=%d: %v", campaignID, contactID, err)
	}
}

// ShouldBlockWorkflowSend reports whether a send step should be skipped for campaign stop policies.
func ShouldBlockWorkflowSend(campaignID, contactID int64) (block bool, reason string) {
	if campaignID <= 0 || contactID <= 0 {
		return false, ""
	}
	camp, err := GetCampaign(campaignID)
	if err != nil {
		return false, ""
	}
	if camp.StopOnReply {
		var replied bool
		_ = db.QueryRow(`SELECT replied_at IS NOT NULL FROM contact WHERE id = ?`, contactID).Scan(&replied)
		if replied {
			return true, "contact replied (stop on reply)"
		}
	}
	if camp.StopOnHot {
		tier, err := ResolveLeadTemperature(campaignID, contactID)
		if err == nil && tier == "hot" {
			return true, "lead is hot (stop on hot)"
		}
	}
	return false, ""
}
