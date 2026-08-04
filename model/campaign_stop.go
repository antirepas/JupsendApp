package model

import (
	"database/sql"
	"fmt"
	"strings"

	"emailtracker.com/db"
)

// StopCampaignResult summarizes cancelled work after a stop.
type StopCampaignResult struct {
	CancelledJobs       int
	CancelledInstances  int
	AlreadyStopped      bool
	HadNothingToCancel  bool
}

// CampaignIsStopped reports whether the campaign was stopped by the user.
func CampaignIsStopped(campaignID int64) bool {
	if campaignID <= 0 {
		return false
	}
	var status string
	err := db.QueryRow(`SELECT status FROM campaigns WHERE id = ?`, campaignID).Scan(&status)
	return err == nil && status == "stopped"
}

// CampaignHasCancellableWork is true when queued jobs or active workflow instances remain.
func CampaignHasCancellableWork(campaignID int64) (bool, error) {
	var jobs int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM send_jobs
		WHERE campaign_id = ? AND status IN ('pending', 'processing')
	`, campaignID).Scan(&jobs)
	if err != nil {
		return false, err
	}
	if jobs > 0 {
		return true, nil
	}
	var instances int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM workflow_instances
		WHERE campaign_id = ? AND status IN ('active', 'waiting')
	`, campaignID).Scan(&instances)
	if err != nil {
		return false, err
	}
	return instances > 0, nil
}

// StopCampaign marks the campaign stopped, deletes queued send jobs/sends, and cancels workflow instances.
func StopCampaign(campaignID, userID int64) (StopCampaignResult, error) {
	var result StopCampaignResult
	campaign, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return result, fmt.Errorf("campaign not found")
	}
	if campaign.Status == "stopped" {
		result.AlreadyStopped = true
		return result, nil
	}
	if campaign.Status == "draft" && !campaign.IsSending && campaign.ScheduledAt == nil {
		hasWork, err := CampaignHasCancellableWork(campaignID)
		if err != nil {
			return result, err
		}
		if !hasWork {
			result.HadNothingToCancel = true
			return result, fmt.Errorf("campaign is not running")
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()

	nJobs, err := cancelPendingSendJobsForCampaignTx(tx, campaignID, userID)
	if err != nil {
		return result, err
	}
	result.CancelledJobs = nJobs

	nInst, err := cancelWorkflowInstancesForCampaignTx(tx, campaignID)
	if err != nil {
		return result, err
	}
	result.CancelledInstances = nInst

	_, err = tx.Exec(`
		UPDATE campaigns SET status = 'stopped', scheduled_at = NULL, is_sending = 0
		WHERE id = ? AND user_id = ?
	`, campaignID, userID)
	if err != nil {
		return result, err
	}

	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func cancelPendingSendJobsForCampaignTx(tx *db.Tx, campaignID, userID int64) (int, error) {
	rows, err := tx.Query(`
		SELECT id, email_send_id
		FROM send_jobs
		WHERE campaign_id = ? AND user_id = ? AND status IN ('pending', 'processing')
	`, campaignID, userID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var jobIDs []int64
	var sendIDs []int64
	seenSend := map[int64]bool{}
	for rows.Next() {
		var jobID int64
		var sendID sql.NullInt64
		if err := rows.Scan(&jobID, &sendID); err != nil {
			return 0, err
		}
		jobIDs = append(jobIDs, jobID)
		if sendID.Valid && sendID.Int64 > 0 && !seenSend[sendID.Int64] {
			seenSend[sendID.Int64] = true
			sendIDs = append(sendIDs, sendID.Int64)
		}
	}
	if len(jobIDs) == 0 {
		return 0, nil
	}

	// Delete queue jobs first (FK to email_sends).
	jobPH := make([]string, len(jobIDs))
	jobArgs := make([]interface{}, len(jobIDs))
	for i, id := range jobIDs {
		jobPH[i] = "?"
		jobArgs[i] = id
	}
	if _, err := tx.Exec(`DELETE FROM send_jobs WHERE id IN (`+strings.Join(jobPH, ",")+`)`, jobArgs...); err != nil {
		return 0, err
	}

	if len(sendIDs) > 0 {
		sendPH := make([]string, len(sendIDs))
		sendArgs := make([]interface{}, 0, len(sendIDs)+1)
		for i, id := range sendIDs {
			sendPH[i] = "?"
			sendArgs = append(sendArgs, id)
		}
		inList := strings.Join(sendPH, ",")
		if _, err := tx.Exec(`DELETE FROM tracked_links WHERE email_send_id IN (`+inList+`)`, sendArgs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`UPDATE contact_events SET email_send_id = NULL WHERE email_send_id IN (`+inList+`)`, sendArgs...); err != nil {
			return 0, err
		}
		// Only purge never-delivered campaign queue rows.
		sendArgs = append(sendArgs, userID)
		if _, err := tx.Exec(`
			DELETE FROM email_sends
			WHERE id IN (`+inList+`)
				AND user_id = ?
				AND LOWER(COALESCE(delivery_status, '')) IN ('queued', 'sending', 'failed', 'cancelled')
		`, sendArgs...); err != nil {
			return 0, err
		}
	}
	return len(jobIDs), nil
}

func cancelWorkflowInstancesForCampaignTx(tx *db.Tx, campaignID int64) (int, error) {
	res, err := tx.Exec(`
		UPDATE workflow_instances SET status = 'cancelled', completed_at = CURRENT_TIMESTAMP,
			lock_token = NULL, lock_expires_at = NULL
		WHERE campaign_id = ? AND status IN ('active', 'waiting')
	`, campaignID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
