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
	TotalSends  int
	TotalOpens  int
	TotalClicks int
	OpenRate    float64
	ClickRate   float64
}

type DailyStat struct {
	Date  string
	Sends int
	Opens int
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

	if stats.TotalSends > 0 {
		stats.OpenRate = float64(stats.TotalOpens) / float64(stats.TotalSends) * 100
		stats.ClickRate = float64(stats.TotalClicks) / float64(stats.TotalSends) * 100
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

func GetDailyStats(userID int64, days int) ([]DailyStat, error) {
	query := `
		SELECT date(sent_at) as day, COUNT(*) as sends
		FROM email_sends
		WHERE user_id = ? AND sent_at >= datetime('now', '-' || ? || ' days')
		GROUP BY date(sent_at)
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
		SELECT date(ee.created_at) as day, COUNT(*) as opens
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE ee.event_type = 'open' AND es.user_id = ? AND ee.created_at >= datetime('now', '-' || ? || ' days')
		GROUP BY date(ee.created_at)
	`
	openRows, err := db.Query(openQuery, userID, days)
	if err != nil {
		return nil, err
	}
	defer openRows.Close()

	openMap := make(map[string]int)
	for openRows.Next() {
		var day string
		var opens int
		if err := openRows.Scan(&day, &opens); err != nil {
			return nil, err
		}
		openMap[day] = opens
	}

	var stats []DailyStat
	for day, sends := range sendMap {
		stats = append(stats, DailyStat{
			Date:  day,
			Sends: sends,
			Opens: openMap[day],
		})
	}
	return stats, nil
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
