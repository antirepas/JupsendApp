package model

import (
	"database/sql"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

type User struct {
	ID                      int64
	Email                   string
	PasswordHash            string
	BaseURL                 string
	SubscriptionStatus      string
	PlanTier                string
	WhopMembershipID        string
	WhopMemberID            string
	SubscriptionEndsAt      *time.Time
	IsAdmin                 bool
	AIcreditsUsedToday      int
	AIcreditsResetAt        *time.Time
	SendCooldownDays             int
	IncludeUnsubscribeLink  bool
	GoalMeetingsPerMonth    int
	GoalReplyToMeetingPct   int
	GoalDailySendCap        int
	WizardDismissedAt       *time.Time
	CreatedAt               time.Time
}

const (
	SubStatusNone      = "none"
	SubStatusActive    = "active"
	SubStatusPendingPayment = "pending_payment"
	SubStatusPastDue   = "past_due"
	SubStatusCancelled = "cancelled"
)

type PlanTier string

const (
	PlanTierFree     PlanTier = "free"
	PlanTierStandard PlanTier = "standard"
	PlanTierPro      PlanTier = "pro"
)

func CreateUser(email, passwordHash, baseURL string) (int64, error) {
	if baseURL == "" {
		baseURL = config.BaseURL
	}
	email = strings.TrimSpace(strings.ToLower(email))
	isAdmin := config.IsAdminEmail(email)
	planTier := string(PlanTierFree)
	if isAdmin {
		planTier = string(PlanTierPro)
	}
	row := db.QueryRow(`
		INSERT INTO users (email, password_hash, base_url, is_admin, plan_tier) VALUES (?, ?, ?, ?, ?) RETURNING id
	`, email, passwordHash, strings.TrimRight(baseURL, "/"), isAdmin, planTier)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		return 0, err
	}
	if isAdmin {
		_ = ApplyPlanLimitsToUser(id, PlanTierPro)
	}
	return id, nil
}

func GetUserByEmail(email string) (User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''),
			COALESCE(subscription_status, 'none'), COALESCE(whop_membership_id, ''), COALESCE(whop_member_id, ''),
			subscription_ends_at, is_admin, COALESCE(send_cooldown_days, 30), COALESCE(include_unsubscribe_link, TRUE),
			COALESCE(plan_tier, 'free'),
			COALESCE(ai_credits_used_today, 0), ai_credits_reset_at,
			COALESCE(goal_meetings_per_month, 0), COALESCE(goal_reply_to_meeting_pct, 50), COALESCE(goal_daily_send_cap, 0),
			wizard_dismissed_at, created_at
		FROM users WHERE email = ?
	`, strings.TrimSpace(strings.ToLower(email)))
	return scanUser(row)
}

func GetUserByID(id int64) (User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, COALESCE(base_url, ''),
			COALESCE(subscription_status, 'none'), COALESCE(whop_membership_id, ''), COALESCE(whop_member_id, ''),
			subscription_ends_at, is_admin, COALESCE(send_cooldown_days, 30), COALESCE(include_unsubscribe_link, TRUE),
			COALESCE(plan_tier, 'free'),
			COALESCE(ai_credits_used_today, 0), ai_credits_reset_at,
			COALESCE(goal_meetings_per_month, 0), COALESCE(goal_reply_to_meeting_pct, 50), COALESCE(goal_daily_send_cap, 0),
			wizard_dismissed_at, created_at
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func scanUser(row interface{ Scan(...interface{}) error }) (User, error) {
	var u User
	var ends sql.NullTime
	var resetAt sql.NullTime
	var wizardDismissed sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.BaseURL,
		&u.SubscriptionStatus, &u.WhopMembershipID, &u.WhopMemberID, &ends, &u.IsAdmin, &u.SendCooldownDays, &u.IncludeUnsubscribeLink,
		&u.PlanTier,
		&u.AIcreditsUsedToday, &resetAt,
		&u.GoalMeetingsPerMonth, &u.GoalReplyToMeetingPct, &u.GoalDailySendCap,
		&wizardDismissed, &u.CreatedAt)
	if ends.Valid {
		t := ends.Time
		u.SubscriptionEndsAt = &t
	}
	if resetAt.Valid {
		t := resetAt.Time
		u.AIcreditsResetAt = &t
	}
	if wizardDismissed.Valid {
		t := wizardDismissed.Time
		u.WizardDismissedAt = &t
	}
	return u, err
}

// UserWizardDismissed reports whether the optional getting-started banner should stay hidden.
func UserWizardDismissed(u User) bool {
	return u.WizardDismissedAt != nil
}

func SetWizardDismissed(userID int64) error {
	_, err := db.Exec(`UPDATE users SET wizard_dismissed_at = COALESCE(wizard_dismissed_at, ?) WHERE id = ?`, time.Now(), userID)
	return err
}

func ClearWizardDismissed(userID int64) error {
	_, err := db.Exec(`UPDATE users SET wizard_dismissed_at = NULL WHERE id = ?`, userID)
	return err
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
	if UserIsAdmin(u) {
		return true
	}
	// Free is a real plan — no paid membership required.
	if NormalizePlanTier(u.PlanTier) == PlanTierFree {
		return true
	}
	return UserHasActiveSubscription(u)
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
		_ = EnsureAdminProAccess(u.ID)
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
	_ = userID
	return config.NormalizeTrackingBaseURL(config.BaseURL)
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
