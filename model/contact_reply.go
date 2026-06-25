package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

func MarkContactReplied(contactID int64) error {
	_, err := db.Exec(`
		UPDATE contact SET replied_at = COALESCE(replied_at, CURRENT_TIMESTAMP) WHERE id = ?
	`, contactID)
	return err
}

func CancelActiveInstancesForContact(contactID int64) error {
	rows, err := db.Query(`
		SELECT id FROM workflow_instances
		WHERE contact_id = ? AND status IN ('active', 'waiting')
	`, contactID)
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
	return nil
}

func FindRecentSendToContact(userID, contactID int64, withinDays int) (int64, error) {
	if withinDays <= 0 {
		withinDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -withinDays)
	var id int64
	err := db.QueryRow(`
		SELECT id FROM email_sends
		WHERE user_id = ? AND contact_id = ? AND sent_at >= ?
		ORDER BY sent_at DESC LIMIT 1
	`, userID, contactID, cutoff).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func GetContactEmailStatus(contactID int64) (status, reason string, err error) {
	err = db.QueryRow(`
		SELECT COALESCE(email_status, 'unknown'), COALESCE(email_status_reason, '')
		FROM contact WHERE id = ?
	`, contactID).Scan(&status, &reason)
	return status, reason, err
}

func SetContactEmailStatus(contactID int64, status, reason string) error {
	_, err := db.Exec(`
		UPDATE contact SET email_status = ?, email_status_reason = ? WHERE id = ?
	`, status, reason, contactID)
	return err
}

func UserIncludeUnsubscribeLink(userID int64) bool {
	var include bool
	err := db.QueryRow(`
		SELECT COALESCE(include_unsubscribe_link, TRUE) FROM users WHERE id = ?
	`, userID).Scan(&include)
	if err != nil {
		return true
	}
	return include
}

func UpdateUserIncludeUnsubscribeLink(userID int64, include bool) error {
	_, err := db.Exec(`UPDATE users SET include_unsubscribe_link = ? WHERE id = ?`, include, userID)
	return err
}
