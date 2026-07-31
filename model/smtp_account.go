package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

type SMTPAccount struct {
	ID                     int64
	UserID                 int64
	Name                   string
	SMTPHost               string
	SMTPPort               string
	SMTPUser               string
	SMTPPassword           string
	FromEmail              string
	FromName               string
	IMAPHost               string
	IMAPPort               string
	IMAPUser               string
	IMAPPassword           string
	AuthType               string
	OAuthRefreshToken      string
	OAuthAccessToken       string
	OAuthExpiry            time.Time
	GoogleEmail            string
	Status                 string
	DailyLimit             int
	PerMinuteLimit         int
	MinSecondsBetweenSends int
	WarmupEnabled          bool
	WarmupDailyCap         int
	WarmupTargetDailyCap   int
	WarmupIncrementPerDay  int
	WarmupStartedAt        *time.Time
	SendsToday             int
	SendsTodayResetAt      *time.Time
	LastSendAt             *time.Time
	InboxkitMailboxID      string
	IsDefault              bool
	MailboxSource          string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (a SMTPAccount) IsGoogleOAuth() bool {
	return a.AuthType == AuthTypeGoogleOAuth && a.OAuthRefreshToken != ""
}

// SenderEmail returns the address used for SMTP MAIL FROM and OAuth identity.
func (a SMTPAccount) SenderEmail() string {
	if a.IsGoogleOAuth() && a.GoogleEmail != "" {
		return a.GoogleEmail
	}
	if a.FromEmail != "" {
		return a.FromEmail
	}
	return a.SMTPUser
}

func scanSMTPAccount(row interface{ Scan(...interface{}) error }) (SMTPAccount, error) {
	var a SMTPAccount
	var userID sql.NullInt64
	var warmupStarted, lastSend, resetAt, oauthExpiry sql.NullTime
	var warmupEnabled, isDefault int
	err := row.Scan(
		&a.ID, &userID, &a.Name, &a.SMTPHost, &a.SMTPPort, &a.SMTPUser, &a.SMTPPassword,
		&a.FromEmail, &a.FromName, &a.IMAPHost, &a.IMAPPort, &a.IMAPUser, &a.IMAPPassword,
		&a.Status, &a.DailyLimit, &a.PerMinuteLimit, &a.MinSecondsBetweenSends,
		&warmupEnabled, &a.WarmupDailyCap, &a.WarmupTargetDailyCap, &a.WarmupIncrementPerDay,
		&warmupStarted, &a.SendsToday, &resetAt, &lastSend,
		&a.AuthType, &a.OAuthRefreshToken, &a.OAuthAccessToken, &oauthExpiry, &a.GoogleEmail,
		&a.InboxkitMailboxID, &isDefault, &a.MailboxSource,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return SMTPAccount{}, err
	}
	if userID.Valid {
		a.UserID = userID.Int64
	}
	a.WarmupEnabled = warmupEnabled == 1
	a.IsDefault = isDefault == 1
	if warmupStarted.Valid {
		t := warmupStarted.Time
		a.WarmupStartedAt = &t
	}
	if resetAt.Valid {
		t := resetAt.Time
		a.SendsTodayResetAt = &t
	}
	if lastSend.Valid {
		t := lastSend.Time
		a.LastSendAt = &t
	}
	if oauthExpiry.Valid {
		a.OAuthExpiry = oauthExpiry.Time
	}
	return a, nil
}

const smtpAccountCols = `
	id, user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
	imap_host, imap_port, imap_user, imap_password, status, daily_limit, per_minute_limit,
	min_seconds_between_sends, warmup_enabled, warmup_daily_cap, warmup_target_daily_cap,
	warmup_increment_per_day, warmup_started_at, sends_today, sends_today_reset_at,
	last_send_at, auth_type, oauth_refresh_token, oauth_access_token, oauth_expiry, google_email,
	COALESCE(inboxkit_mailbox_id, ''), COALESCE(is_default, 0), COALESCE(mailbox_source, ''),
	created_at, updated_at
`

func GetSMTPAccountByUserID(userID int64) (SMTPAccount, error) {
	row := db.QueryRow(`
		SELECT `+smtpAccountCols+` FROM smtp_accounts
		WHERE user_id = ?
		ORDER BY CASE WHEN COALESCE(is_default, 0) = 1 THEN 0 ELSE 1 END,
			CASE WHEN status = 'active' THEN 0 ELSE 1 END,
			id ASC
		LIMIT 1
	`, userID)
	return scanSMTPAccount(row)
}

func GetActiveSMTPAccountForUser(userID int64) (SMTPAccount, error) {
	row := db.QueryRow(`
		SELECT `+smtpAccountCols+` FROM smtp_accounts
		WHERE user_id = ? AND status = 'active'
		ORDER BY CASE WHEN COALESCE(is_default, 0) = 1 THEN 0 ELSE 1 END, id ASC
		LIMIT 1
	`, userID)
	return scanSMTPAccount(row)
}

func GetSMTPAccount(id int64) (SMTPAccount, error) {
	row := db.QueryRow(`SELECT `+smtpAccountCols+` FROM smtp_accounts WHERE id = ?`, id)
	return scanSMTPAccount(row)
}

func ListActiveSMTPAccounts() ([]SMTPAccount, error) {
	rows, err := db.Query(`SELECT ` + smtpAccountCols + ` FROM smtp_accounts WHERE status = 'active' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SMTPAccount
	for rows.Next() {
		a, err := scanSMTPAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, nil
}

func CreateDefaultSMTPAccountForUser(userID int64) error {
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email,
			status, auth_type, warmup_started_at, sends_today_reset_at, created_at, updated_at
		) VALUES (?, 'Gmail', 'smtp.gmail.com', '465', '', '', '', 'inactive', '', ?, ?, ?, ?)
	`, userID, now, now.Format("2006-01-02"), now, now)
	return err
}

func SaveGoogleOAuthAccount(userID int64, email, fromName, encRefresh, encAccess string, expiry time.Time) error {
	now := time.Now()
	warmup := 1
	var existingID int64
	_ = db.QueryRow(`
		SELECT id FROM smtp_accounts
		WHERE user_id = ? AND auth_type = ?
		ORDER BY id ASC
		LIMIT 1
	`, userID, AuthTypeGoogleOAuth).Scan(&existingID)
	if existingID > 0 {
		_, err := db.Exec(`
		UPDATE smtp_accounts SET
			name='Gmail', smtp_host='smtp.gmail.com', smtp_port='465', smtp_user=?, smtp_password='',
				from_email=?, from_name=?, imap_host='imap.gmail.com', imap_port='993', imap_user=?, imap_password='',
				auth_type=?, oauth_refresh_token=?, oauth_access_token=?, oauth_expiry=?, google_email=?,
				status='active', mailbox_source='gmail_oauth', updated_at=?
			WHERE id=?
		`, email, email, fromName, email, AuthTypeGoogleOAuth, encRefresh, encAccess, expiry, email, now, existingID)
		return err
	}
	_, err := db.Exec(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, from_email, from_name,
			imap_host, imap_port, imap_user, auth_type, oauth_refresh_token, oauth_access_token,
			oauth_expiry, google_email, status, warmup_enabled, warmup_started_at, sends_today_reset_at,
			mailbox_source, is_default, created_at, updated_at
		) VALUES (?, 'Gmail', 'smtp.gmail.com', '465', ?, ?, ?, 'imap.gmail.com', '993', ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 'gmail_oauth', 0, ?, ?)
	`, userID, email, email, fromName, email, AuthTypeGoogleOAuth, encRefresh, encAccess, expiry, email,
		warmup, now, now.Format("2006-01-02"), now, now)
	return err
}

func ClearGoogleOAuth(userID int64) error {
	_, err := db.Exec(`
		UPDATE smtp_accounts SET auth_type='', oauth_refresh_token='', oauth_access_token='',
			oauth_expiry=NULL, google_email='', smtp_user='', imap_user='', status='inactive', updated_at=?
		WHERE user_id=?
	`, time.Now(), userID)
	return err
}

func UpdateOAuthTokens(accountID int64, encAccess string, expiry time.Time) error {
	_, err := db.Exec(`UPDATE smtp_accounts SET oauth_access_token=?, oauth_expiry=?, updated_at=? WHERE id=?`,
		encAccess, expiry, time.Now(), accountID)
	return err
}

func UpsertSMTPAccountForUser(userID int64, a SMTPAccount) error {
	existing, err := GetSMTPAccountByUserID(userID)
	if err == nil {
		a.ID = existing.ID
		a.UserID = userID
		a.AuthType = existing.AuthType
		a.OAuthRefreshToken = existing.OAuthRefreshToken
		a.OAuthAccessToken = existing.OAuthAccessToken
		a.OAuthExpiry = existing.OAuthExpiry
		a.GoogleEmail = existing.GoogleEmail
		a.SMTPUser = existing.SMTPUser
		a.IMAPUser = existing.IMAPUser
		a.InboxkitMailboxID = existing.InboxkitMailboxID
		a.MailboxSource = existing.MailboxSource
		a.IsDefault = existing.IsDefault
		if a.FromEmail == "" {
			a.FromEmail = existing.FromEmail
		}
		if a.SMTPPassword == "" {
			a.SMTPPassword = existing.SMTPPassword
		}
		if a.SMTPHost == "" {
			a.SMTPHost = existing.SMTPHost
		}
		if a.SMTPPort == "" {
			a.SMTPPort = existing.SMTPPort
		}
		if a.IMAPHost == "" {
			a.IMAPHost = existing.IMAPHost
		}
		if a.IMAPPort == "" {
			a.IMAPPort = existing.IMAPPort
		}
		if a.IMAPPassword == "" {
			a.IMAPPassword = existing.IMAPPassword
		}
		return UpdateSMTPAccount(a)
	}
	a.UserID = userID
	a.Name = "Gmail"
	if a.Status == "" {
		a.Status = "inactive"
	}
	_, err = CreateSMTPAccountForUser(userID, a)
	return err
}

func CreateSMTPAccountForUser(userID int64, a SMTPAccount) (int64, error) {
	now := time.Now()
	warmup := 0
	if a.WarmupEnabled {
		warmup = 1
	}
	warmupStart := now
	if a.WarmupStartedAt != nil {
		warmupStart = *a.WarmupStartedAt
	}
	row := db.QueryRow(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
			imap_host, imap_port, imap_user, imap_password, status, daily_limit, per_minute_limit,
			min_seconds_between_sends, warmup_enabled, warmup_daily_cap, warmup_target_daily_cap,
			warmup_increment_per_day, warmup_started_at, sends_today_reset_at,
			auth_type, oauth_refresh_token, oauth_access_token, oauth_expiry, google_email,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`,
		userID, a.Name, a.SMTPHost, a.SMTPPort, a.SMTPUser, a.SMTPPassword, a.FromEmail, a.FromName,
		a.IMAPHost, a.IMAPPort, a.IMAPUser, a.IMAPPassword, a.Status, a.DailyLimit, a.PerMinuteLimit,
		a.MinSecondsBetweenSends, warmup, a.WarmupDailyCap, a.WarmupTargetDailyCap, a.WarmupIncrementPerDay,
		warmupStart, now.Format("2006-01-02"),
		a.AuthType, a.OAuthRefreshToken, a.OAuthAccessToken, nullTime(a.OAuthExpiry), a.GoogleEmail,
		now, now,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func UpdateSMTPAccount(a SMTPAccount) error {
	warmup := 0
	if a.WarmupEnabled {
		warmup = 1
	}
	_, err := db.Exec(`
		UPDATE smtp_accounts SET
			name=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, from_email=?, from_name=?,
			imap_host=?, imap_port=?, imap_user=?, imap_password=?, status=?, daily_limit=?,
			per_minute_limit=?, min_seconds_between_sends=?, warmup_enabled=?, warmup_daily_cap=?,
			warmup_target_daily_cap=?, warmup_increment_per_day=?,
			auth_type=?, oauth_refresh_token=?, oauth_access_token=?, oauth_expiry=?, google_email=?,
			updated_at=?
		WHERE id=?
	`,
		a.Name, a.SMTPHost, a.SMTPPort, a.SMTPUser, a.SMTPPassword, a.FromEmail, a.FromName,
		a.IMAPHost, a.IMAPPort, a.IMAPUser, a.IMAPPassword, a.Status, a.DailyLimit,
		a.PerMinuteLimit, a.MinSecondsBetweenSends, warmup, a.WarmupDailyCap,
		a.WarmupTargetDailyCap, a.WarmupIncrementPerDay,
		a.AuthType, a.OAuthRefreshToken, a.OAuthAccessToken, nullTime(a.OAuthExpiry), a.GoogleEmail,
		time.Now(), a.ID,
	)
	return err
}

func SetSMTPAccountStatus(id int64, status string) error {
	_, err := db.Exec(`UPDATE smtp_accounts SET status=?, updated_at=? WHERE id=?`, status, time.Now(), id)
	return err
}

func IncrementAccountSendCount(accountID int64) error {
	today := time.Now().Format("2006-01-02")
	_, err := db.Exec(`
		UPDATE smtp_accounts SET
			sends_today = CASE WHEN sends_today_reset_at = ? THEN sends_today + 1 ELSE 1 END,
			sends_today_reset_at = ?,
			last_send_at = ?,
			updated_at = ?
		WHERE id = ?
	`, today, today, time.Now(), time.Now(), accountID)
	return err
}

func ResetAccountDailyIfNeeded(a *SMTPAccount) {
	today := time.Now().Format("2006-01-02")
	if a.SendsTodayResetAt == nil || a.SendsTodayResetAt.Format("2006-01-02") != today {
		a.SendsToday = 0
	}
}
