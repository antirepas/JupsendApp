package model

import (
	"database/sql"
	"encoding/json"
	"time"

	"emailtracker.com/db"
)

type ContactEvent struct {
	ID                 int64
	TenantID           *int64
	ContactID          int64
	CampaignID         *int64
	WorkflowID         *int64
	WorkflowInstanceID *int64
	EmailSendID        *int64
	EventType          string
	MetadataJSON       string
	OccurredAt         time.Time
	CreatedAt          time.Time
	DedupeKey          string
}

type ContactEventInput struct {
	ContactID          int64
	CampaignID         int64
	WorkflowID         int64
	WorkflowInstanceID int64
	EmailSendID        int64
	EventType          string
	Metadata           map[string]interface{}
	OccurredAt         time.Time
	DedupeKey          string
}

func InsertContactEvent(in ContactEventInput) (int64, error) {
	meta := "{}"
	if in.Metadata != nil {
		b, _ := json.Marshal(in.Metadata)
		meta = string(b)
	}
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}

	var campID, wfID, instID, sendID interface{}
	if in.CampaignID > 0 {
		campID = in.CampaignID
	}
	if in.WorkflowID > 0 {
		wfID = in.WorkflowID
	}
	if in.WorkflowInstanceID > 0 {
		instID = in.WorkflowInstanceID
	}
	if in.EmailSendID > 0 {
		sendID = in.EmailSendID
	}

	var dedupe interface{}
	if in.DedupeKey != "" {
		dedupe = in.DedupeKey
	}

	row := db.QueryRow(`
		INSERT INTO contact_events (
			contact_id, campaign_id, workflow_id, workflow_instance_id, email_send_id,
			event_type, metadata_json, occurred_at, dedupe_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(dedupe_key) DO NOTHING
		RETURNING id
	`, in.ContactID, campID, wfID, instID, sendID, in.EventType, meta, occurred, dedupe)

	var id int64
	err := row.Scan(&id)
	if err != nil {
		// conflict on dedupe — not an error for tracking
		if in.DedupeKey != "" {
			err = db.QueryRow(`SELECT id FROM contact_events WHERE dedupe_key = ?`, in.DedupeKey).Scan(&id)
		}
	}
	return id, err
}

func GetContactEvents(contactID int64, limit int) ([]ContactEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, contact_id, campaign_id, workflow_id, workflow_instance_id, email_send_id,
			event_type, metadata_json, occurred_at, created_at, dedupe_key
		FROM contact_events WHERE contact_id = ? ORDER BY occurred_at DESC LIMIT ?
	`, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ContactEvent
	for rows.Next() {
		var e ContactEvent
		var camp, wf, inst, send sqlNullInt64
		var dedupe sql.NullString
		if err := rows.Scan(&e.ID, &e.ContactID, &camp, &wf, &inst, &send,
			&e.EventType, &e.MetadataJSON, &e.OccurredAt, &e.CreatedAt, &dedupe); err != nil {
			return nil, err
		}
		if camp.Valid {
			v := camp.Int64
			e.CampaignID = &v
		}
		if wf.Valid {
			v := wf.Int64
			e.WorkflowID = &v
		}
		if inst.Valid {
			v := inst.Int64
			e.WorkflowInstanceID = &v
		}
		if send.Valid {
			v := send.Int64
			e.EmailSendID = &v
		}
		if dedupe.Valid {
			e.DedupeKey = dedupe.String
		}
		events = append(events, e)
	}
	return events, nil
}

type sqlNullInt64 struct {
	Int64 int64
	Valid bool
}

func (n *sqlNullInt64) Scan(value interface{}) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	switch v := value.(type) {
	case int64:
		n.Int64 = v
		n.Valid = true
	case int:
		n.Int64 = int64(v)
		n.Valid = true
	}
	return nil
}

func CountContactEventsForSend(emailSendID int64, eventType string) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM contact_events WHERE email_send_id = ? AND event_type = ?
	`, emailSendID, eventType).Scan(&n)
	return n, err
}

func HasContactEventForSend(emailSendID int64, eventType string) (bool, error) {
	n, err := CountContactEventsForSend(emailSendID, eventType)
	return n > 0, err
}

func GetLastSendIDForInstance(instanceID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT id FROM email_sends WHERE workflow_instance_id = ? ORDER BY sent_at DESC LIMIT 1
	`, instanceID).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
