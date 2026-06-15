package model

import (
	"database/sql"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	BaseURL      string
	CreatedAt    time.Time
}

func CreateUser(email, passwordHash, baseURL string) (int64, error) {
	if baseURL == "" {
		baseURL = config.BaseURL
	}
	row := db.DB.QueryRow(`
		INSERT INTO users (email, password_hash, base_url) VALUES (?, ?, ?) RETURNING id
	`, strings.TrimSpace(strings.ToLower(email)), passwordHash, strings.TrimRight(baseURL, "/"))
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetUserByEmail(email string) (User, error) {
	row := db.DB.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''), created_at FROM users WHERE email = ?
	`, strings.TrimSpace(strings.ToLower(email)))
	return scanUser(row)
}

func GetUserByID(id int64) (User, error) {
	row := db.DB.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''), created_at FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func scanUser(row interface{ Scan(...interface{}) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.BaseURL, &u.CreatedAt)
	return u, err
}

func EmailExists(email string) (bool, error) {
	var n int
	err := db.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ?`, strings.TrimSpace(strings.ToLower(email))).Scan(&n)
	return n > 0, err
}

func UpdateUserPassword(userID int64, passwordHash string) error {
	_, err := db.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

func UpdateUserBaseURL(userID int64, baseURL string) error {
	_, err := db.DB.Exec(`UPDATE users SET base_url = ? WHERE id = ?`, strings.TrimRight(strings.TrimSpace(baseURL), "/"), userID)
	return err
}

func UserBaseURL(userID int64) string {
	u, err := GetUserByID(userID)
	if err != nil || u.BaseURL == "" {
		return config.BaseURL
	}
	return u.BaseURL
}

func AssignOrphanDataToUser(userID int64) error {
	tables := []struct {
		table string
		col   string
	}{
		{"template", "user_id"},
		{"contact", "user_id"},
		{"campaigns", "user_id"},
		{"email_sends", "user_id"},
		{"send_jobs", "user_id"},
	}
	for _, t := range tables {
		if _, err := db.DB.Exec(`UPDATE `+t.table+` SET `+t.col+` = ? WHERE `+t.col+` IS NULL`, userID); err != nil {
			return err
		}
	}
	_, _ = db.DB.Exec(`UPDATE workflows SET tenant_id = ? WHERE tenant_id IS NULL`, userID)
	_, _ = db.DB.Exec(`UPDATE smtp_accounts SET user_id = ? WHERE user_id IS NULL`, userID)
	return nil
}

func GetUserIDForContact(contactID int64) (int64, error) {
	var uid sql.NullInt64
	err := db.DB.QueryRow(`SELECT user_id FROM contact WHERE id = ?`, contactID).Scan(&uid)
	if err != nil {
		return 0, err
	}
	if !uid.Valid {
		return 0, sql.ErrNoRows
	}
	return uid.Int64, nil
}
