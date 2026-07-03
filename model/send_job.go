package model

import (
	"database/sql"
	"strings"
	"time"

	"emailtracker.com/db"
)

type SendJob struct {
	ID                 int64
	UserID             int64
	SMTPAccountID      int64
	ContactID          int64
	TemplateID         int64
	CampaignID         int64
	Variant            string
	WorkflowInstanceID int64
	EmailSendID        int64
	Status             string
	Priority           int
	ScheduledAt        time.Time
	ClaimedAt          *time.Time
	LockToken          string
	LockExpiresAt      *time.Time
	Attempts           int
	MaxAttempts        int
	LastError          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SendJobCounts struct {
	Pending int
	Sent    int
	Failed  int
	Dead    int
}

func scanSendJob(row interface{ Scan(...interface{}) error }) (SendJob, error) {
	var j SendJob
	var smtpID, campID, wfID, sendID sql.NullInt64
	var claimed, lockExp sql.NullTime
	var lockToken sql.NullString
	var userID sql.NullInt64
	err := row.Scan(
		&j.ID, &userID, &smtpID, &j.ContactID, &j.TemplateID, &campID, &j.Variant, &wfID, &sendID,
		&j.Status, &j.Priority, &j.ScheduledAt, &claimed, &lockToken, &lockExp,
		&j.Attempts, &j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return SendJob{}, err
	}
	if userID.Valid {
		j.UserID = userID.Int64
	}
	if smtpID.Valid {
		j.SMTPAccountID = smtpID.Int64
	}
	if campID.Valid {
		j.CampaignID = campID.Int64
	}
	if wfID.Valid {
		j.WorkflowInstanceID = wfID.Int64
	}
	if sendID.Valid {
		j.EmailSendID = sendID.Int64
	}
	if claimed.Valid {
		t := claimed.Time
		j.ClaimedAt = &t
	}
	if lockToken.Valid {
		j.LockToken = lockToken.String
	}
	if lockExp.Valid {
		t := lockExp.Time
		j.LockExpiresAt = &t
	}
	return j, nil
}

const sendJobCols = `
	id, user_id, smtp_account_id, contact_id, template_id, campaign_id, variant, workflow_instance_id,
	email_send_id, status, priority, scheduled_at, claimed_at, lock_token, lock_expires_at,
	attempts, max_attempts, last_error, created_at, updated_at
`

func CreateSendJob(j SendJob) (int64, error) {
	var campID, wfID, sendID interface{}
	if j.CampaignID > 0 {
		campID = j.CampaignID
	}
	if j.WorkflowInstanceID > 0 {
		wfID = j.WorkflowInstanceID
	}
	if j.EmailSendID > 0 {
		sendID = j.EmailSendID
	}
	now := time.Now()
	scheduled := j.ScheduledAt
	if scheduled.IsZero() {
		scheduled = now
	}
	maxAttempts := j.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}
	row := db.QueryRow(`
		INSERT INTO send_jobs (
			user_id, contact_id, template_id, campaign_id, variant, workflow_instance_id, email_send_id,
			status, priority, scheduled_at, max_attempts, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?)
		RETURNING id
	`, j.UserID, j.ContactID, j.TemplateID, campID, j.Variant, wfID, sendID,
		j.Priority, scheduled, maxAttempts, now, now)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetSendJob(id int64) (SendJob, error) {
	row := db.QueryRow(`SELECT `+sendJobCols+` FROM send_jobs WHERE id = ?`, id)
	return scanSendJob(row)
}

func LinkSendJobEmailSend(jobID, emailSendID int64) error {
	_, err := db.Exec(`UPDATE send_jobs SET email_send_id=?, updated_at=? WHERE id=?`, emailSendID, time.Now(), jobID)
	return err
}

func ClaimSendJob(jobID int64, lockToken string, lockDuration time.Duration) (bool, error) {
	now := time.Now()
	exp := now.Add(lockDuration)
	res, err := db.Exec(`
		UPDATE send_jobs SET status='processing', lock_token=?, claimed_at=?, lock_expires_at=?, updated_at=?
		WHERE id=? AND status='pending' AND scheduled_at <= ?
	`, lockToken, now, exp, now, jobID, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func CompleteSendJob(jobID int64, accountID int64) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE send_jobs SET status='sent', smtp_account_id=?, lock_token=NULL, updated_at=? WHERE id=?
	`, accountID, now, jobID)
	return err
}

func FailSendJob(jobID int64, errMsg string, status string) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE send_jobs SET status=?, last_error=?, lock_token=NULL, updated_at=? WHERE id=?
	`, status, errMsg, now, jobID)
	return err
}

func RetrySendJob(jobID int64, attempts int, scheduledAt time.Time, errMsg string) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE send_jobs SET status='pending', attempts=?, scheduled_at=?, last_error=?, lock_token=NULL, updated_at=?
		WHERE id=?
	`, attempts, scheduledAt, errMsg, now, jobID)
	return err
}

func RescheduleSendJob(jobID int64, scheduledAt time.Time, errMsg string) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE send_jobs SET status='pending', scheduled_at=?, last_error=?, lock_token=NULL, updated_at=?
		WHERE id=?
	`, scheduledAt, errMsg, now, jobID)
	return err
}

func ReleaseStaleJobs() error {
	return ReleaseStaleJobsReconciled()
}

const staleDeliveryUncertainMsg = "delivery uncertain after worker timeout — not retried"

func ReleaseStaleJobsReconciled() error {
	now := time.Now()

	rows, err := db.Query(`
		SELECT sj.id, COALESCE(es.smtp_account_id, 0), COALESCE(es.delivery_status, 'queued')
		FROM send_jobs sj
		LEFT JOIN email_sends es ON es.id = sj.email_send_id
		WHERE sj.status = 'processing'
			AND sj.lock_expires_at IS NOT NULL AND sj.lock_expires_at < ?
	`, now)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var jobID, accountID int64
		var deliveryStatus string
		if err := rows.Scan(&jobID, &accountID, &deliveryStatus); err != nil {
			return err
		}
		switch deliveryStatus {
		case "sent":
			_ = CompleteSendJob(jobID, accountID)
		case "sending":
			var sendID int64
			_ = db.QueryRow(`SELECT email_send_id FROM send_jobs WHERE id = ?`, jobID).Scan(&sendID)
			if sendID > 0 {
				_ = MarkEmailSendFailed(sendID)
			}
			_ = FailSendJob(jobID, staleDeliveryUncertainMsg, "dead")
		default:
			_, _ = db.Exec(`
				UPDATE send_jobs SET status='pending', last_error='worker timeout', lock_token=NULL, updated_at=?
				WHERE id=? AND status='processing'
			`, now, jobID)
		}
	}
	return rows.Err()
}

type GlobalSendJobStats struct {
	Pending                 int
	Processing              int
	Dead                    int
	Failed                  int
	OldestPendingAgeSeconds int64
}

func GetGlobalSendJobStats() (GlobalSendJobStats, error) {
	var stats GlobalSendJobStats
	row := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM send_jobs
	`)
	err := row.Scan(&stats.Pending, &stats.Processing, &stats.Dead, &stats.Failed)
	if err != nil {
		return stats, err
	}

	var oldest sql.NullTime
	err = db.QueryRow(`
		SELECT MIN(scheduled_at) FROM send_jobs WHERE status = 'pending'
	`).Scan(&oldest)
	if err != nil && err != sql.ErrNoRows {
		return stats, err
	}
	if oldest.Valid {
		age := time.Since(oldest.Time)
		if age > 0 {
			stats.OldestPendingAgeSeconds = int64(age.Seconds())
		}
	}
	return stats, nil
}

func GetSendJobStatsForUsers(userIDs []int64) (GlobalSendJobStats, error) {
	var stats GlobalSendJobStats
	if len(userIDs) == 0 {
		return stats, nil
	}
	placeholders := make([]string, len(userIDs))
	args := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")
	row := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM send_jobs WHERE user_id IN (`+inClause+`)
	`, args...)
	err := row.Scan(&stats.Pending, &stats.Processing, &stats.Dead, &stats.Failed)
	if err != nil {
		return stats, err
	}
	oldestArgs := append([]interface{}{}, args...)
	row = db.QueryRow(`
		SELECT MIN(scheduled_at) FROM send_jobs
		WHERE status = 'pending' AND scheduled_at <= CURRENT_TIMESTAMP AND user_id IN (`+inClause+`)
	`, oldestArgs...)
	var oldest sql.NullTime
	if err := row.Scan(&oldest); err != nil && err != sql.ErrNoRows {
		return stats, err
	}
	if oldest.Valid {
		age := time.Since(oldest.Time)
		if age > 0 {
			stats.OldestPendingAgeSeconds = int64(age.Seconds())
		}
	}
	return stats, nil
}

func PendingSendJobIDs(limit int) ([]int64, error) {
	rows, err := db.Query(`
		SELECT id FROM send_jobs
		WHERE status='pending' AND scheduled_at <= ?
		ORDER BY priority DESC, scheduled_at ASC
		LIMIT ?
	`, time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func CountSendJobsByCampaign(campaignID int64) (SendJobCounts, error) {
	var c SendJobCounts
	row := db.QueryRow(`
		SELECT
			SUM(CASE WHEN status IN ('pending','processing') THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END)
		FROM send_jobs WHERE campaign_id = ?
	`, campaignID)
	err := row.Scan(&c.Pending, &c.Sent, &c.Failed, &c.Dead)
	return c, err
}

func HasActiveCampaignJobs(campaignID int64) (bool, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM send_jobs
		WHERE campaign_id=? AND status IN ('pending','processing')
	`, campaignID).Scan(&n)
	return n > 0, err
}

func CountPendingJobsForCampaign(campaignID int64) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM send_jobs WHERE campaign_id=? AND status IN ('pending','processing')
	`, campaignID).Scan(&n)
	return n, err
}
