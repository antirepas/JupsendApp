package model

import (
	"database/sql"
	"fmt"
	"time"

	"emailtracker.com/db"
)

// WorkflowArchivePreview summarizes in-flight work before archiving.
type WorkflowArchivePreview struct {
	WorkflowName       string `json:"workflow_name"`
	WaitingInstances   int    `json:"waiting_instances"`
	CompletedInstances int    `json:"completed_instances"`
	QueuedSendJobs     int    `json:"queued_send_jobs"`
}

func GetWorkflowArchivePreview(workflowID, userID int64) (WorkflowArchivePreview, error) {
	w, err := GetWorkflowForUser(workflowID, userID)
	if err != nil {
		return WorkflowArchivePreview{}, err
	}
	preview := WorkflowArchivePreview{WorkflowName: w.Name}

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM workflow_instances wi
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ? AND wi.status IN ('active', 'waiting')
	`, workflowID).Scan(&preview.WaitingInstances)

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM workflow_instances wi
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ? AND wi.status = 'completed'
	`, workflowID).Scan(&preview.CompletedInstances)

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM send_jobs sj
		INNER JOIN workflow_instances wi ON wi.id = sj.workflow_instance_id
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ? AND sj.user_id = ? AND sj.status IN ('pending', 'processing')
	`, workflowID, userID).Scan(&preview.QueuedSendJobs)

	return preview, nil
}

// ArchiveWorkflow hides a workflow from new campaigns while in-flight instances keep running.
func ArchiveWorkflow(workflowID, userID int64, cancelQueued bool) error {
	w, err := GetWorkflowForUser(workflowID, userID)
	if err != nil {
		return err
	}
	if w.Status == "archived" {
		return fmt.Errorf("workflow is already archived")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if cancelQueued {
		if err := cancelPendingSendJobsForWorkflowTx(tx, workflowID, userID); err != nil {
			return err
		}
	}

	res, err := tx.Exec(`
		UPDATE workflows SET status = 'archived', updated_at = ?
		WHERE id = ? AND tenant_id = ? AND status = 'active'
	`, time.Now(), workflowID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workflow could not be archived")
	}
	return tx.Commit()
}

func cancelPendingSendJobsForWorkflowTx(tx *db.Tx, workflowID, userID int64) error {
	rows, err := tx.Query(`
		SELECT sj.id, sj.email_send_id
		FROM send_jobs sj
		INNER JOIN workflow_instances wi ON wi.id = sj.workflow_instance_id
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ? AND sj.user_id = ? AND sj.status IN ('pending', 'processing')
	`, workflowID, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type jobRow struct {
		id, emailSendID int64
		hasSend         bool
	}
	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		var sendID sql.NullInt64
		if err := rows.Scan(&j.id, &sendID); err != nil {
			return err
		}
		if sendID.Valid && sendID.Int64 > 0 {
			j.emailSendID = sendID.Int64
			j.hasSend = true
		}
		jobs = append(jobs, j)
	}

	const cancelMsg = "cancelled: workflow archived"
	for _, j := range jobs {
		if _, err := tx.Exec(`
			UPDATE send_jobs SET status = 'failed', last_error = ?, lock_token = NULL, updated_at = ?
			WHERE id = ? AND status IN ('pending', 'processing')
		`, cancelMsg, time.Now(), j.id); err != nil {
			return err
		}
		if j.hasSend {
			if _, err := tx.Exec(`
				UPDATE email_sends SET delivery_status = 'failed'
				WHERE id = ? AND delivery_status IN ('queued', 'sending')
			`, j.emailSendID); err != nil {
				return err
			}
		}
	}
	return nil
}

func WorkflowIsArchived(w Workflow) bool {
	return w.Status == "archived"
}
