package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

type EmailSend struct {
	ID         int64
	TemplateID int64 `json:"template_id"`
	ContactID  int64 `json:"contact_id"`
	TrackID    string
	SentAt     time.Time
}

type EmailSendListItem struct {
	ID              int64
	TemplateID      int64
	ContactID       int64
	TemplateName    string
	TemplateSubject string
	ContactEmail    string
	SenderEmail     string
	TrackingID      string
	SentAt          time.Time
	OpenCount       int
	ClickCount      int
	DeliveryStatus  string
	DeliveryError   string
	JobStatus       string
}

type EmailSendDetail struct {
	EmailSendListItem
	Events []EventRecord
}

func SaveSendEmail(userID, tId, cId int64, trackId string, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	return CreateQueuedEmailSend(userID, tId, cId, trackId, campaignID, variant, workflowInstanceID)
}

func CreateQueuedEmailSend(userID, tId, cId int64, trackId string, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	var campID, instID interface{}
	if campaignID > 0 {
		campID = campaignID
	}
	if workflowInstanceID > 0 {
		instID = workflowInstanceID
	}
	query := `INSERT INTO email_sends (template_id, contact_id, tracking_id, campaign_id, variant, workflow_instance_id, delivery_status, user_id) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?) RETURNING id`
	row := db.QueryRow(query, tId, cId, trackId, campID, variant, instID, userID)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetEmailSendSentAt(sendID int64) (time.Time, error) {
	var sentAt sql.NullTime
	err := db.QueryRow(`SELECT sent_at FROM email_sends WHERE id = ?`, sendID).Scan(&sentAt)
	if err != nil {
		return time.Time{}, err
	}
	if !sentAt.Valid {
		return time.Time{}, nil
	}
	return sentAt.Time, nil
}

func MarkEmailSendSent(sendID, accountID, jobID int64) error {
	_, err := db.Exec(`
		UPDATE email_sends SET delivery_status='sent', sent_at=?, smtp_account_id=?, send_job_id=? WHERE id=?
	`, time.Now(), accountID, jobID, sendID)
	return err
}

func MarkEmailSendFailed(sendID int64) error {
	_, err := db.Exec(`UPDATE email_sends SET delivery_status='failed' WHERE id=?`, sendID)
	return err
}

func ResetEmailSendForRetry(sendID int64) error {
	_, err := db.Exec(`UPDATE email_sends SET delivery_status='queued' WHERE id=? AND delivery_status='sending'`, sendID)
	return err
}

func GetEmailSendDeliveryStatus(sendID int64) (string, error) {
	var status sql.NullString
	err := db.QueryRow(`SELECT delivery_status FROM email_sends WHERE id = ?`, sendID).Scan(&status)
	if err != nil {
		return "", err
	}
	if !status.Valid || status.String == "" {
		return "unknown", nil
	}
	return status.String, nil
}

func TryMarkEmailSendSending(sendID int64) (bool, error) {
	res, err := db.Exec(`
		UPDATE email_sends SET delivery_status='sending' WHERE id=? AND delivery_status='queued'
	`, sendID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func ReconcileEmailSendAlreadySent(sendID, accountID, jobID int64) error {
	if accountID == 0 {
		var id sql.NullInt64
		if err := db.QueryRow(`SELECT smtp_account_id FROM email_sends WHERE id = ?`, sendID).Scan(&id); err == nil && id.Valid {
			accountID = id.Int64
		}
	}
	return CompleteSendJob(jobID, accountID)
}

// GmailSendBlocked returns a user-facing message when OAuth sending is unavailable.
func GmailSendBlocked(userID int64) string {
	acc, err := GetSMTPAccountByUserID(userID)
	if err != nil || !acc.IsGoogleOAuth() {
		return ""
	}
	if _, err := GmailAccessToken(acc); err != nil {
		return "Gmail connection expired or invalid — reconnect in Settings to resume sending."
	}
	return ""
}

func LinkEmailSendJob(sendID, jobID int64) error {
	_, err := db.Exec(`UPDATE email_sends SET send_job_id=? WHERE id=?`, jobID, sendID)
	return err
}

func GetSendWorkflowInstanceID(sendID int64) int64 {
	var id sql.NullInt64
	err := db.QueryRow(`SELECT workflow_instance_id FROM email_sends WHERE id = ?`, sendID).Scan(&id)
	if err != nil || !id.Valid {
		return 0
	}
	return id.Int64
}

func GetEmailSendIDByTrackingID(trackingID string) (int64, error) {
	query := `SELECT id FROM email_sends WHERE tracking_id = ?`
	row := db.QueryRow(query, trackingID)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func scanEmailSendListItem(
	scan func(dest ...interface{}) error,
) (EmailSendListItem, error) {
	var item EmailSendListItem
	var sentAt sql.NullTime
	err := scan(
		&item.ID, &item.TemplateID, &item.ContactID, &item.TrackingID, &sentAt,
		&item.TemplateName, &item.TemplateSubject, &item.ContactEmail, &item.SenderEmail,
		&item.OpenCount, &item.ClickCount, &item.DeliveryStatus, &item.DeliveryError, &item.JobStatus,
	)
	if err != nil {
		return EmailSendListItem{}, err
	}
	if sentAt.Valid {
		item.SentAt = sentAt.Time
	}
	return item, nil
}

func ListEmailSends(userID int64) ([]EmailSendListItem, error) {
	query := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(t.subject, ''), COALESCE(c.email, ''),
			COALESCE(NULLIF(sa.google_email, ''), NULLIF(sa.from_email, ''), NULLIF(sa.smtp_user, ''), ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(NULLIF(es.delivery_status, ''), 'unknown'),
			COALESCE(sj.last_error, ''),
			COALESCE(sj.status, '')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN smtp_accounts sa ON sa.user_id = es.user_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, t.subject, c.email, sa.google_email, sa.from_email, sa.smtp_user, sj.last_error, sj.status
		ORDER BY es.id DESC
	`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []EmailSendListItem
	for rows.Next() {
		item, err := scanEmailSendListItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func ListEmailSendsForContact(userID, contactID int64, limit int) ([]EmailSendListItem, error) {
	if limit < 1 {
		limit = 10
	}
	query := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(t.subject, ''), COALESCE(c.email, ''),
			COALESCE(NULLIF(sa.google_email, ''), NULLIF(sa.from_email, ''), NULLIF(sa.smtp_user, ''), ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(NULLIF(es.delivery_status, ''), 'unknown'),
			COALESCE(sj.last_error, ''),
			COALESCE(sj.status, '')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN smtp_accounts sa ON sa.user_id = es.user_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ? AND es.contact_id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, t.subject, c.email, sa.google_email, sa.from_email, sa.smtp_user, sj.last_error, sj.status
		ORDER BY es.id DESC
		LIMIT ?
	`
	rows, err := db.Query(query, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []EmailSendListItem
	for rows.Next() {
		item, err := scanEmailSendListItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func GetEmailSendDetail(id int64) (EmailSendDetail, error) {
	query := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(t.subject, ''), COALESCE(c.email, ''),
			COALESCE(NULLIF(sa.google_email, ''), NULLIF(sa.from_email, ''), NULLIF(sa.smtp_user, ''), ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(NULLIF(es.delivery_status, ''), 'unknown'),
			COALESCE(sj.last_error, ''),
			COALESCE(sj.status, '')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN smtp_accounts sa ON sa.user_id = es.user_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, t.subject, c.email, sa.google_email, sa.from_email, sa.smtp_user, sj.last_error, sj.status
	`
	row := db.QueryRow(query, id)
	item, err := scanEmailSendListItem(row.Scan)
	if err != nil {
		return EmailSendDetail{}, err
	}
	detail := EmailSendDetail{EmailSendListItem: item}

	events, err := GetEventsForSend(id)
	if err != nil {
		return EmailSendDetail{}, err
	}
	detail.Events = events
	return detail, nil
}

func GetEmailSendDetailForUser(id, userID int64) (EmailSendDetail, error) {
	detail, err := GetEmailSendDetail(id)
	if err != nil {
		return EmailSendDetail{}, err
	}
	var owner int64
	err = db.QueryRow(`SELECT COALESCE(user_id, 0) FROM email_sends WHERE id = ?`, id).Scan(&owner)
	if err != nil {
		return EmailSendDetail{}, err
	}
	if userID > 0 && owner > 0 && owner != userID {
		return EmailSendDetail{}, sql.ErrNoRows
	}
	return detail, nil
}
