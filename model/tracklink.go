package model

import "emailtracker.com/db"

type TrackLink struct {
	ID          int64
	EmailSendID int64
	TrackingID  string
	OriginalURL string
}

func GetOriginalURL(trackingID string) (string, error) {
	query := `SELECT original_url FROM tracked_links WHERE tracking_id = ?`
	row := db.DB.QueryRow(query, trackingID)
	var og string
	err := row.Scan(&og)
	if err != nil {
		return "", err
	}
	return og, nil
}
func SaveTrackLink(emailsendID int64, trackingId string, ogUrl string) (int64, error) {
	query := `INSERT INTO tracked_links (email_send_id, tracking_id, original_url) VALUES (?, ?, ?) RETURNING id`
	row := db.DB.QueryRow(query, emailsendID, trackingId, ogUrl)
	var id int64
	err := row.Scan(&id)
	return id, err
}
