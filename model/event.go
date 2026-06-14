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
	err := db.DB.QueryRow(
		`SELECT id FROM email_sends WHERE tracking_id = ?`, trackingID,
	).Scan(&emailSendID)
	if err == nil {
		return emailSendID
	}

	err = db.DB.QueryRow(
		`SELECT email_send_id FROM tracked_links WHERE tracking_id = ?`, trackingID,
	).Scan(&emailSendID)
	if err == nil {
		return emailSendID
	}
	return 0
}

func StoreEvent(trackingID, eventType, userAgent, ip string) error {
	emailSendID := resolveEmailSendID(trackingID)
	query := `
		INSERT INTO email_events (email_send_id, tracking_id, event_type, user_agent, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := db.DB.Exec(query, emailSendID, trackingID, eventType, userAgent, ip, time.Now())
	return err
}

func GetEventsForSend(emailSendID int64) ([]EventRecord, error) {
	query := `
		SELECT id, email_send_id, tracking_id, event_type, user_agent, ip_address, created_at
		FROM email_events
		WHERE email_send_id = ?
		ORDER BY created_at DESC
	`
	rows, err := db.DB.Query(query, emailSendID)
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

func GetDashboardStats() (DashboardStats, error) {
	var stats DashboardStats

	err := db.DB.QueryRow(`SELECT COUNT(*) FROM email_sends`).Scan(&stats.TotalSends)
	if err != nil {
		return stats, err
	}

	err = db.DB.QueryRow(`SELECT COUNT(*) FROM email_events WHERE event_type = 'open'`).Scan(&stats.TotalOpens)
	if err != nil {
		return stats, err
	}

	err = db.DB.QueryRow(`SELECT COUNT(*) FROM email_events WHERE event_type = 'click'`).Scan(&stats.TotalClicks)
	if err != nil {
		return stats, err
	}

	if stats.TotalSends > 0 {
		stats.OpenRate = float64(stats.TotalOpens) / float64(stats.TotalSends) * 100
		stats.ClickRate = float64(stats.TotalClicks) / float64(stats.TotalSends) * 100
	}
	return stats, nil
}

func GetRecentEvents(limit int) ([]EventRecord, error) {
	query := `
		SELECT id, email_send_id, tracking_id, event_type, user_agent, ip_address, created_at
		FROM email_events
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := db.DB.Query(query, limit)
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

func GetDailyStats(days int) ([]DailyStat, error) {
	query := `
		SELECT date(sent_at) as day, COUNT(*) as sends
		FROM email_sends
		WHERE sent_at >= datetime('now', '-' || ? || ' days')
		GROUP BY date(sent_at)
		ORDER BY day
	`
	rows, err := db.DB.Query(query, days)
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
		SELECT date(created_at) as day, COUNT(*) as opens
		FROM email_events
		WHERE event_type = 'open' AND created_at >= datetime('now', '-' || ? || ' days')
		GROUP BY date(created_at)
	`
	openRows, err := db.DB.Query(openQuery, days)
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
