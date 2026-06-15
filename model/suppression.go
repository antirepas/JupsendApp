package model

import (
	"time"

	"emailtracker.com/db"
)

type ContactSuppression struct {
	ContactID      int64
	ContactEmail   string
	Reason         string
	SourceMessage  string
	SMTPAccountID  int64
	CreatedAt      time.Time
}

func IsContactSuppressed(contactID int64) (bool, error) {
	var n int
	err := db.DB.QueryRow(`SELECT COUNT(*) FROM contact_suppressions WHERE contact_id=?`, contactID).Scan(&n)
	return n > 0, err
}

func SuppressContact(contactID int64, reason, source string, smtpAccountID int64) error {
	var accID interface{}
	if smtpAccountID > 0 {
		accID = smtpAccountID
	}
	_, err := db.DB.Exec(`
		INSERT OR REPLACE INTO contact_suppressions (contact_id, reason, source_message, smtp_account_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, contactID, reason, source, accID, time.Now())
	return err
}

func RemoveSuppression(contactID int64) error {
	_, err := db.DB.Exec(`DELETE FROM contact_suppressions WHERE contact_id=?`, contactID)
	return err
}

func ListSuppressions(userID int64) ([]ContactSuppression, error) {
	rows, err := db.DB.Query(`
		SELECT cs.contact_id, COALESCE(c.email,''), cs.reason, cs.source_message,
			COALESCE(cs.smtp_account_id, 0), cs.created_at
		FROM contact_suppressions cs
		INNER JOIN contact c ON c.id = cs.contact_id
		WHERE c.user_id = ?
		ORDER BY cs.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ContactSuppression
	for rows.Next() {
		var s ContactSuppression
		if err := rows.Scan(&s.ContactID, &s.ContactEmail, &s.Reason, &s.SourceMessage, &s.SMTPAccountID, &s.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, nil
}

func FilterSuppressedContactIDs(userID int64, contactIDs []int64) ([]int64, []int64, error) {
	if len(contactIDs) == 0 {
		return nil, nil, nil
	}
	rows, err := db.DB.Query(`
		SELECT cs.contact_id FROM contact_suppressions cs
		INNER JOIN contact c ON c.id = cs.contact_id
		WHERE c.user_id = ?
	`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	suppressed := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		suppressed[id] = true
	}
	var allowed, skipped []int64
	for _, id := range contactIDs {
		if suppressed[id] {
			skipped = append(skipped, id)
		} else {
			allowed = append(allowed, id)
		}
	}
	return allowed, skipped, nil
}
