package model

import (
	"database/sql"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

type User struct {
	ID                 int64
	Email              string
	PasswordHash       string
	BaseURL            string
	SubscriptionStatus string
	WhopMembershipID   string
	WhopMemberID       string
	SubscriptionEndsAt *time.Time
	IsAdmin            bool
	SendCooldownDays        int
	IncludeUnsubscribeLink  bool
	GoalMeetingsPerMonth    int
	GoalReplyToMeetingPct   int
	GoalDailySendCap        int
	CreatedAt               time.Time
}

const (
	SubStatusNone      = "none"
	SubStatusActive    = "active"
	SubStatusPastDue   = "past_due"
	SubStatusCancelled = "cancelled"
)

func CreateUser(email, passwordHash, baseURL string) (int64, error) {
	if baseURL == "" {
		baseURL = config.BaseURL
	}
	email = strings.TrimSpace(strings.ToLower(email))
	isAdmin := config.IsAdminEmail(email)
	row := db.QueryRow(`
		INSERT INTO users (email, password_hash, base_url, is_admin) VALUES (?, ?, ?, ?) RETURNING id
	`, email, passwordHash, strings.TrimRight(baseURL, "/"), isAdmin)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetUserByEmail(email string) (User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''),
			COALESCE(subscription_status, 'none'), COALESCE(whop_membership_id, ''), COALESCE(whop_member_id, ''),
			subscription_ends_at, is_admin, COALESCE(send_cooldown_days, 30), COALESCE(include_unsubscribe_link, TRUE),
			COALESCE(goal_meetings_per_month, 0), COALESCE(goal_reply_to_meeting_pct, 50), COALESCE(goal_daily_send_cap, 0),
			created_at
		FROM users WHERE email = ?
	`, strings.TrimSpace(strings.ToLower(email)))
	return scanUser(row)
}

func GetUserByID(id int64) (User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''),
			COALESCE(subscription_status, 'none'), COALESCE(whop_membership_id, ''), COALESCE(whop_member_id, ''),
			subscription_ends_at, is_admin, COALESCE(send_cooldown_days, 30), COALESCE(include_unsubscribe_link, TRUE),
			COALESCE(goal_meetings_per_month, 0), COALESCE(goal_reply_to_meeting_pct, 50), COALESCE(goal_daily_send_cap, 0),
			created_at
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func scanUser(row interface{ Scan(...interface{}) error }) (User, error) {
	var u User
	var ends sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.BaseURL,
		&u.SubscriptionStatus, &u.WhopMembershipID, &u.WhopMemberID, &ends, &u.IsAdmin, &u.SendCooldownDays, &u.IncludeUnsubscribeLink,
		&u.GoalMeetingsPerMonth, &u.GoalReplyToMeetingPct, &u.GoalDailySendCap, &u.CreatedAt)
	if ends.Valid {
		t := ends.Time
		u.SubscriptionEndsAt = &t
	}
	return u, err
}

func EmailExists(email string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ?`, strings.TrimSpace(strings.ToLower(email))).Scan(&n)
	return n > 0, err
}

func UpdateUserPassword(userID int64, passwordHash string) error {
	_, err := db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

func UpdateUserBaseURL(userID int64, baseURL string) error {
	_, err := db.Exec(`UPDATE users SET base_url = ? WHERE id = ?`, strings.TrimRight(strings.TrimSpace(baseURL), "/"), userID)
	return err
}

func UserHasActiveSubscription(u User) bool {
	if u.SubscriptionStatus == SubStatusActive {
		return true
	}
	if u.SubscriptionEndsAt != nil && u.SubscriptionEndsAt.After(time.Now()) {
		return u.SubscriptionStatus != SubStatusNone && u.SubscriptionStatus != SubStatusCancelled
	}
	return false
}

func UserIsAdmin(u User) bool {
	return u.IsAdmin || config.IsAdminEmail(u.Email)
}

func UserHasAppAccess(u User) bool {
	return UserIsAdmin(u) || UserHasActiveSubscription(u)
}

func SetUserAdmin(userID int64, admin bool) error {
	_, err := db.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, admin, userID)
	return err
}

func SyncAdminEmailsFromConfig() {
	for email := range config.AdminEmails {
		u, err := GetUserByEmail(email)
		if err != nil {
			continue
		}
		if !u.IsAdmin {
			_ = SetUserAdmin(u.ID, true)
		}
	}
}

func UpdateUserSubscription(userID int64, status, membershipID, memberID string, endsAt *time.Time) error {
	var ends interface{}
	if endsAt != nil {
		ends = *endsAt
	}
	_, err := db.Exec(`
		UPDATE users SET subscription_status = ?, whop_membership_id = ?, whop_member_id = ?, subscription_ends_at = ?
		WHERE id = ?
	`, status, membershipID, memberID, ends, userID)
	return err
}

func UpdateUserSubscriptionByEmail(email, status, membershipID, memberID string, endsAt *time.Time) error {
	u, err := GetUserByEmail(email)
	if err != nil {
		return err
	}
	return UpdateUserSubscription(u.ID, status, membershipID, memberID, endsAt)
}

func UserBaseURL(userID int64) string {
	u, err := GetUserByID(userID)
	if err != nil || u.BaseURL == "" {
		return config.BaseURL
	}
	return u.BaseURL
}

func UpdateUserOutreachGoals(userID int64, meetings, replyPct, dailyCap int) error {
	if replyPct <= 0 {
		replyPct = 50
	}
	_, err := db.Exec(`
		UPDATE users SET goal_meetings_per_month = ?, goal_reply_to_meeting_pct = ?, goal_daily_send_cap = ?
		WHERE id = ?
	`, meetings, replyPct, dailyCap, userID)
	return err
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
		if _, err := db.Exec(`UPDATE `+t.table+` SET `+t.col+` = ? WHERE `+t.col+` IS NULL`, userID); err != nil {
			return err
		}
	}
	_, _ = db.Exec(`UPDATE workflows SET tenant_id = ? WHERE tenant_id IS NULL`, userID)
	_, _ = db.Exec(`UPDATE smtp_accounts SET user_id = ? WHERE user_id IS NULL`, userID)
	return nil
}

func GetUserIDForContact(contactID int64) (int64, error) {
	var uid sql.NullInt64
	err := db.QueryRow(`SELECT user_id FROM contact WHERE id = ?`, contactID).Scan(&uid)
	if err != nil {
		return 0, err
	}
	if !uid.Valid {
		return 0, sql.ErrNoRows
	}
	return uid.Int64, nil
}
