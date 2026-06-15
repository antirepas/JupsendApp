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
	ID            int64
	TemplateID    int64
	ContactID     int64
	TemplateName  string
	ContactEmail  string
	TrackingID    string
	SentAt        time.Time
	OpenCount     int
	ClickCount    int
}

type EmailSendDetail struct {
	EmailSendListItem
	Events []EventRecord
}

func SaveSendEmail(tId, cId int64, trackId string, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	var campID, instID interface{}
	if campaignID > 0 {
		campID = campaignID
	}
	if workflowInstanceID > 0 {
		instID = workflowInstanceID
	}
	query := `INSERT INTO email_sends (template_id, contact_id, tracking_id, sent_at, campaign_id, variant, workflow_instance_id) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`
	row := db.DB.QueryRow(query, tId, cId, trackId, time.Now(), campID, variant, instID)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetSendWorkflowInstanceID(sendID int64) int64 {
	var id sql.NullInt64
	err := db.DB.QueryRow(`SELECT workflow_instance_id FROM email_sends WHERE id = ?`, sendID).Scan(&id)
	if err != nil || !id.Valid {
		return 0
	}
	return id.Int64
}

func GetEmailSendIDByTrackingID(trackingID string) (int64, error) {
	query := `SELECT id FROM email_sends WHERE tracking_id = ?`
	row := db.DB.QueryRow(query, trackingID)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func ListEmailSends() ([]EmailSendListItem, error) {
	query := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(c.email, ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		GROUP BY es.id
		ORDER BY es.sent_at DESC
	`
	rows, err := db.DB.Query(query)
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
			&item.OpenCount, &item.ClickCount,
		)
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
			COALESCE(t.name, ''), COALESCE(c.email, ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.id = ?
		GROUP BY es.id
	`
	row := db.DB.QueryRow(query, id)
	var detail EmailSendDetail
	err := row.Scan(
		&detail.ID, &detail.TemplateID, &detail.ContactID, &detail.TrackingID, &detail.SentAt,
		&detail.TemplateName, &detail.ContactEmail,
		&detail.OpenCount, &detail.ClickCount,
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
