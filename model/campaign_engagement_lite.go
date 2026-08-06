package model

import "emailtracker.com/db"

type ContactEngagementLite struct {
	OpenCount  int
	ClickCount int
	SendID     int64
}

// GetCampaignContactEngagementLite returns per-contact engagement without loading full analytics.
func GetCampaignContactEngagementLite(campaignID int64) (map[int64]ContactEngagementLite, error) {
	rows, err := db.Query(`
		SELECT es.contact_id, es.id,
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id
		WHERE es.campaign_id = ?
		GROUP BY es.contact_id, es.id
		ORDER BY es.contact_id, es.id DESC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]ContactEngagementLite)
	for rows.Next() {
		var contactID, sendID int64
		var opens, clicks int
		if err := rows.Scan(&contactID, &sendID, &opens, &clicks); err != nil {
			return nil, err
		}
		if _, exists := out[contactID]; exists {
			// Keep latest send per contact (rows ordered by id desc).
			continue
		}
		out[contactID] = ContactEngagementLite{
			OpenCount:  opens,
			ClickCount: clicks,
			SendID:     sendID,
		}
	}
	return out, nil
}

// GetContactEmailsByIDs loads id→email for a set of contact IDs in one query.
func GetContactEmailsByIDs(userID int64, contactIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(contactIDs))
	if len(contactIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+1)
	args = append(args, userID)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `SELECT id, email FROM contact WHERE user_id = ? AND id IN (` + joinPlaceholders(placeholders) + `)`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, nil
}

func joinPlaceholders(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += "," + parts[i]
	}
	return s
}
