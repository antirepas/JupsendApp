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
	ContactEmail    string
	TrackingID      string
	SentAt          time.Time
	OpenCount       int
	ClickCount      int
	DeliveryStatus  string
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
	now := time.Now()
	query := `INSERT INTO email_sends (template_id, contact_id, tracking_id, sent_at, campaign_id, variant, workflow_instance_id, delivery_status, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?) RETURNING id`
	row := db.QueryRow(query, tId, cId, trackId, now, campID, variant, instID, userID)
	var id int64
	err := row.Scan(&id)
	return id, err
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

func ListEmailSends(userID int64) ([]EmailSendListItem, error) {
	query := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(c.email, ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(es.delivery_status, 'sent')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.user_id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, c.email
		ORDER BY es.sent_at DESC
	`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []EmailSendListItem
	for rows.Next() {
		var item EmailSendListItem
		err := rows.Scan(
			&item.ID, &item.TemplateID, &item.ContactID, &item.TrackingID, &item.SentAt,
			&item.TemplateName, &item.ContactEmail,
			&item.OpenCount, &item.ClickCount, &item.DeliveryStatus,
		)
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
			COALESCE(t.name, ''), COALESCE(c.email, ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(es.delivery_status, 'sent')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.user_id = ? AND es.contact_id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, c.email
		ORDER BY es.sent_at DESC
		LIMIT ?
	`
	rows, err := db.Query(query, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []EmailSendListItem
	for rows.Next() {
		var item EmailSendListItem
		if err := rows.Scan(
			&item.ID, &item.TemplateID, &item.ContactID, &item.TrackingID, &item.SentAt,
			&item.TemplateName, &item.ContactEmail,
			&item.OpenCount, &item.ClickCount, &item.DeliveryStatus,
		); err != nil {
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
			COALESCE(t.name, ''), COALESCE(c.email, ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(es.delivery_status, 'sent')
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.id = ?
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, t.name, c.email
	`
	row := db.QueryRow(query, id)
	var detail EmailSendDetail
	err := row.Scan(
		&detail.ID, &detail.TemplateID, &detail.ContactID, &detail.TrackingID, &detail.SentAt,
		&detail.TemplateName, &detail.ContactEmail,
		&detail.OpenCount, &detail.ClickCount, &detail.DeliveryStatus,
	)
	if err != nil {
		return EmailSendDetail{}, err
	}

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
