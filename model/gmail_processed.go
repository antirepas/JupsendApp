package model

import (
	"emailtracker.com/db"
)

// GmailMessageAlreadyProcessed reports whether this Gmail message was already scanned.
func GmailMessageAlreadyProcessed(userID int64, messageKey string) (bool, error) {
	if userID <= 0 || messageKey == "" {
		return false, nil
	}
	var n int
	err := db.QueryRow(`
		SELECT 1 FROM gmail_processed_messages WHERE user_id = ? AND message_key = ? LIMIT 1
	`, userID, messageKey).Scan(&n)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// MarkGmailMessageProcessed records that a Gmail inbox message was scanned for bounce/reply.
func MarkGmailMessageProcessed(userID int64, messageKey string) error {
	if userID <= 0 || messageKey == "" {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO gmail_processed_messages (user_id, message_key)
		VALUES (?, ?)
		ON CONFLICT (user_id, message_key) DO NOTHING
	`, userID, messageKey)
	return err
}
