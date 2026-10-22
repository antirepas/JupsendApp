package model

import "emailtracker.com/db"

// DashboardCampaignBar is one campaign row for the home comparison chart.
type DashboardCampaignBar struct {
	ID        int64
	Name      string
	Sends     int
	Replies   int
	ReplyRate float64
}

// ListDashboardCampaignBars returns recent launched campaigns with send/reply totals
// for dashboard charts (newest first, capped).
func ListDashboardCampaignBars(userID int64, limit int) []DashboardCampaignBar {
	if limit < 1 {
		limit = 8
	}
	rows, err := db.Query(`
		SELECT c.id, c.name,
			COUNT(es.id)::int AS sends,
			COUNT(DISTINCT ce.contact_id)::int AS replies
		FROM campaigns c
		LEFT JOIN email_sends es ON es.campaign_id = c.id AND es.user_id = c.user_id
		LEFT JOIN contact_events ce ON ce.email_send_id = es.id AND ce.event_type = 'REPLY'
		WHERE c.user_id = ?
		  AND COALESCE(c.status, '') <> 'draft'
		GROUP BY c.id, c.name, c.created_at
		HAVING COUNT(es.id) > 0
		ORDER BY c.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []DashboardCampaignBar
	for rows.Next() {
		var b DashboardCampaignBar
		if err := rows.Scan(&b.ID, &b.Name, &b.Sends, &b.Replies); err != nil {
			continue
		}
		if b.Sends > 0 {
			b.ReplyRate = float64(b.Replies) / float64(b.Sends) * 100
		}
		out = append(out, b)
	}
	return out
}
