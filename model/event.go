package model

import (
	"time"

	"emailtracker.com/db"
)

type Event struct {
	ID          int64
	EmailSendID int64
	Type        string
	Timestamp   time.Duration
	Metadata    interface{}
}

type EventRecord struct {
	ID          int64
	EmailSendID int64
	TrackingID  string
	EventType   string
	UserAgent   string
	IPAddress   string
	CreatedAt   time.Time
}

type DashboardStats struct {
	TotalSends    int
	TotalOpens    int
	TotalClicks   int
	TotalReplies  int
	OpenRate      float64
	ClickRate     float64
	ReplyRate     float64
}

type DailyStat struct {
	Date         string
	Sends        int
	Opens        int
	UniqueOpens  int
	Clicks       int
	UniqueClicks int
	Replies      int
}

func resolveEmailSendID(trackingID string) int64 {
	var emailSendID int64
	err := db.QueryRow(
		`SELECT id FROM email_sends WHERE tracking_id = ?`, trackingID,
	).Scan(&emailSendID)
	if err == nil {
		return emailSendID
	}

	err = db.QueryRow(
		`SELECT email_send_id FROM tracked_links WHERE tracking_id = ?`, trackingID,
	).Scan(&emailSendID)
	if err == nil {
		return emailSendID
	}
	return 0
}

func StoreEvent(trackingID, eventType, userAgent, ip string) error {
	emailSendID := resolveEmailSendID(trackingID)
	var sendID interface{}
	if emailSendID > 0 {
		sendID = emailSendID
	}
	query := `
		INSERT INTO email_events (email_send_id, tracking_id, event_type, user_agent, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, sendID, trackingID, eventType, userAgent, ip, time.Now())
	return err
}

func GetEventsForSend(emailSendID int64) ([]EventRecord, error) {
	query := `
		SELECT id, email_send_id, tracking_id, event_type, user_agent, ip_address, created_at
		FROM email_events
		WHERE email_send_id = ?
		ORDER BY created_at DESC
	`
	rows, err := db.Query(query, emailSendID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		var e EventRecord
		err := rows.Scan(&e.ID, &e.EmailSendID, &e.TrackingID, &e.EventType, &e.UserAgent, &e.IPAddress, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func GetDashboardStats(userID int64) (DashboardStats, error) {
	var stats DashboardStats

	err := db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE user_id = ?`, userID).Scan(&stats.TotalSends)
	if err != nil {
		return stats, err
	}

	err = db.QueryRow(`
		SELECT COUNT(*) FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'open' AND es.user_id = ?
	`, userID).Scan(&stats.TotalOpens)
	if err != nil {
		return stats, err
	}

	err = db.QueryRow(`
		SELECT COUNT(*) FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'click' AND es.user_id = ?
	`, userID).Scan(&stats.TotalClicks)
	if err != nil {
		return stats, err
	}

	err = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE ce.event_type = 'REPLY' AND es.user_id = ?
	`, userID).Scan(&stats.TotalReplies)
	if err != nil {
		return stats, err
	}

	if stats.TotalSends > 0 {
		stats.OpenRate = float64(stats.TotalOpens) / float64(stats.TotalSends) * 100
		stats.ClickRate = float64(stats.TotalClicks) / float64(stats.TotalSends) * 100
		stats.ReplyRate = float64(stats.TotalReplies) / float64(stats.TotalSends) * 100
	}
	return stats, nil
}

func GetRecentEvents(userID int64, limit int) ([]EventRecord, error) {
	query := `
		SELECT ee.id, ee.email_send_id, ee.tracking_id, ee.event_type, ee.user_agent, ee.ip_address, ee.created_at
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.user_id = ?
		ORDER BY ee.created_at DESC
		LIMIT ?
	`
	rows, err := db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		var e EventRecord
		err := rows.Scan(&e.ID, &e.EmailSendID, &e.TrackingID, &e.EventType, &e.UserAgent, &e.IPAddress, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

type ContactActivityItem struct {
	ContactID    int64
	ContactEmail string
	SendID       int64
	EventType    string
	LinkURL      string
	CampaignName string
	CreatedAt    time.Time
}

type ContactActivityPage struct {
	Items      []ContactActivityItem
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

const recentContactActivitySQL = `
	SELECT c.id, c.email, es.id AS send_id, ee.event_type,
		COALESCE(tl.original_url, '') AS link_url, COALESCE(camp.name, '') AS campaign_name, ee.created_at
	FROM email_events ee
	INNER JOIN email_sends es ON es.id = ee.email_send_id
	INNER JOIN contact c ON c.id = es.contact_id
	LEFT JOIN tracked_links tl ON tl.tracking_id = ee.tracking_id AND ee.event_type = 'click'
	LEFT JOIN campaigns camp ON camp.id = es.campaign_id
	WHERE es.user_id = ? AND ee.event_type IN ('open', 'click')
	UNION ALL
	SELECT c.id, c.email, COALESCE(ce.email_send_id, 0), 'reply',
		'', COALESCE(camp.name, ''), ce.occurred_at
	FROM contact_events ce
	INNER JOIN contact c ON c.id = ce.contact_id
	LEFT JOIN email_sends es ON es.id = ce.email_send_id
	LEFT JOIN campaigns camp ON camp.id = es.campaign_id
	WHERE c.user_id = ? AND ce.event_type = 'REPLY'
`

func GetRecentContactActivity(userID int64, limit int) ([]ContactActivityItem, error) {
	page, err := GetRecentContactActivityPage(userID, 1, limit)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func GetRecentContactActivityPage(userID int64, page, pageSize int) (ContactActivityPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	countQuery := `SELECT COUNT(*) FROM (` + recentContactActivitySQL + `) activity`
	var total int
	if err := db.QueryRow(countQuery, userID, userID).Scan(&total); err != nil {
		return ContactActivityPage{}, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize
	listQuery := `
		SELECT * FROM (` + recentContactActivitySQL + `) activity
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`
	rows, err := db.Query(listQuery, userID, userID, pageSize, offset)
	if err != nil {
		return ContactActivityPage{}, err
	}
	defer rows.Close()

	var items []ContactActivityItem
	for rows.Next() {
		var item ContactActivityItem
		if err := rows.Scan(&item.ContactID, &item.ContactEmail, &item.SendID, &item.EventType,
			&item.LinkURL, &item.CampaignName, &item.CreatedAt); err != nil {
			return ContactActivityPage{}, err
		}
		items = append(items, item)
	}

	return ContactActivityPage{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

func GetDailyStats(userID int64, days int) ([]DailyStat, error) {
	query := `
		SELECT (sent_at)::date as day, COUNT(*) as sends
		FROM email_sends
		WHERE user_id = ? AND sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (sent_at)::date
		ORDER BY day
	`
	rows, err := db.Query(query, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sendMap := make(map[string]int)
	for rows.Next() {
		var day string
		var sends int
		if err := rows.Scan(&day, &sends); err != nil {
			return nil, err
		}
		sendMap[day] = sends
	}

	openQuery := `
		SELECT (ee.created_at)::date as day, COUNT(*) as opens
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'open' AND es.user_id = ? AND ee.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (ee.created_at)::date
	`
	openRows, err := db.Query(openQuery, userID, days)
	if err != nil {
		return nil, err
	}
	defer openRows.Close()

	openMap := make(map[string]int)
	uniqueOpenMap := make(map[string]int)
	for openRows.Next() {
		var day string
		var opens int
		if err := openRows.Scan(&day, &opens); err != nil {
			return nil, err
		}
		openMap[day] = opens
	}

	uniqueOpenQuery := `
		SELECT (ee.created_at)::date as day, COUNT(DISTINCT es.contact_id) as unique_opens
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'open' AND es.user_id = ? AND ee.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (ee.created_at)::date
	`
	uniqueOpenRows, err := db.Query(uniqueOpenQuery, userID, days)
	if err == nil {
		defer uniqueOpenRows.Close()
		for uniqueOpenRows.Next() {
			var day string
			var n int
			if uniqueOpenRows.Scan(&day, &n) == nil {
				uniqueOpenMap[day] = n
			}
		}
	}

	clickMap := make(map[string]int)
	uniqueClickMap := make(map[string]int)
	clickQuery := `
		SELECT (ee.created_at)::date as day, COUNT(*) as clicks
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'click' AND es.user_id = ? AND ee.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (ee.created_at)::date
	`
	clickRows, err := db.Query(clickQuery, userID, days)
	if err == nil {
		defer clickRows.Close()
		for clickRows.Next() {
			var day string
			var n int
			if clickRows.Scan(&day, &n) == nil {
				clickMap[day] = n
			}
		}
	}

	uniqueClickQuery := `
		SELECT (ee.created_at)::date as day, COUNT(DISTINCT es.contact_id) as unique_clicks
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'click' AND es.user_id = ? AND ee.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (ee.created_at)::date
	`
	uniqueClickRows, err := db.Query(uniqueClickQuery, userID, days)
	if err == nil {
		defer uniqueClickRows.Close()
		for uniqueClickRows.Next() {
			var day string
			var n int
			if uniqueClickRows.Scan(&day, &n) == nil {
				uniqueClickMap[day] = n
			}
		}
	}

	replyMap := make(map[string]int)
	replyQuery := `
		SELECT (ce.created_at)::date as day, COUNT(DISTINCT ce.contact_id) as replies
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE ce.event_type = 'REPLY' AND es.user_id = ? AND ce.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
		GROUP BY (ce.created_at)::date
	`
	replyRows, err := db.Query(replyQuery, userID, days)
	if err == nil {
		defer replyRows.Close()
		for replyRows.Next() {
			var day string
			var replies int
			if replyRows.Scan(&day, &replies) == nil {
				replyMap[day] = replies
			}
		}
	}

	seen := make(map[string]bool)
	for day := range sendMap {
		seen[day] = true
	}
	for day := range openMap {
		seen[day] = true
	}
	for day := range uniqueOpenMap {
		seen[day] = true
	}
	for day := range clickMap {
		seen[day] = true
	}
	for day := range uniqueClickMap {
		seen[day] = true
	}
	for day := range replyMap {
		seen[day] = true
	}

	var stats []DailyStat
	for day := range seen {
		stats = append(stats, DailyStat{
			Date:         day,
			Sends:        sendMap[day],
			Opens:        openMap[day],
			UniqueOpens:  uniqueOpenMap[day],
			Clicks:       clickMap[day],
			UniqueClicks: uniqueClickMap[day],
			Replies:      replyMap[day],
		})
	}
	sortDailyStats(stats)
	return fillDailyGaps(stats, days), nil
}

func fillDailyGaps(stats []DailyStat, days int) []DailyStat {
	if days < 1 {
		return stats
	}
	byDate := make(map[string]DailyStat, len(stats))
	for _, s := range stats {
		byDate[normalizeStatDate(s.Date)] = s
	}
	out := make([]DailyStat, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if s, ok := byDate[d]; ok {
			s.Date = d
			out = append(out, s)
		} else {
			out = append(out, DailyStat{Date: d})
		}
	}
	return out
}

func normalizeStatDate(day string) string {
	if len(day) >= 10 {
		return day[:10]
	}
	return day
}

func sortDailyStats(stats []DailyStat) {
	for i := 0; i < len(stats); i++ {
		for j := i + 1; j < len(stats); j++ {
			if stats[j].Date < stats[i].Date {
				stats[i], stats[j] = stats[j], stats[i]
			}
		}
	}
}

type EntityCounts struct {
	Templates int
	Contacts  int
	Campaigns int
}

func GetEntityCounts(userID int64) (EntityCounts, error) {
	var c EntityCounts
	if err := db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ?`, userID).Scan(&c.Templates); err != nil {
		return c, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contact WHERE user_id = ?`, userID).Scan(&c.Contacts); err != nil {
		return c, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM campaigns WHERE user_id = ?`, userID).Scan(&c.Campaigns); err != nil {
		return c, err
	}
	return c, nil
}
