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
	SMTPHost              string
	SMTPPort              string
	SMTPUser              string
	SMTPPassword          string
	FromEmail             string
	FromName              string
	IMAPHost              string
	IMAPPort              string
	IMAPUser              string
	IMAPPassword          string
	Status                string
	DailyLimit            int
	PerMinuteLimit        int
	MinSecondsBetweenSends int
	WarmupEnabled         bool
	WarmupDailyCap        int
	WarmupTargetDailyCap  int
	WarmupIncrementPerDay int
	WarmupStartedAt       *time.Time
	SendsToday            int
	SendsTodayResetAt     *time.Time
	LastSendAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func scanSMTPAccount(row interface{ Scan(...interface{}) error }) (SMTPAccount, error) {
	var a SMTPAccount
	var userID sql.NullInt64
	var warmupStarted, lastSend, resetAt sql.NullTime
	var warmupEnabled int
	err := row.Scan(
		&a.ID, &userID, &a.Name, &a.SMTPHost, &a.SMTPPort, &a.SMTPUser, &a.SMTPPassword,
		&a.FromEmail, &a.FromName, &a.IMAPHost, &a.IMAPPort, &a.IMAPUser, &a.IMAPPassword,
		&a.Status, &a.DailyLimit, &a.PerMinuteLimit, &a.MinSecondsBetweenSends,
		&warmupEnabled, &a.WarmupDailyCap, &a.WarmupTargetDailyCap, &a.WarmupIncrementPerDay,
		&warmupStarted, &a.SendsToday, &resetAt, &lastSend, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return SMTPAccount{}, err
	}
	if userID.Valid {
		a.UserID = userID.Int64
	}
	a.WarmupEnabled = warmupEnabled == 1
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
	return a, nil
}

const smtpAccountCols = `
	id, user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
	imap_host, imap_port, imap_user, imap_password, status, daily_limit, per_minute_limit,
	min_seconds_between_sends, warmup_enabled, warmup_daily_cap, warmup_target_daily_cap,
	warmup_increment_per_day, warmup_started_at, sends_today, sends_today_reset_at,
	last_send_at, created_at, updated_at
`

func GetSMTPAccountByUserID(userID int64) (SMTPAccount, error) {
	row := db.DB.QueryRow(`SELECT `+smtpAccountCols+` FROM smtp_accounts WHERE user_id = ?`, userID)
	return scanSMTPAccount(row)
}

func GetActiveSMTPAccountForUser(userID int64) (SMTPAccount, error) {
	row := db.DB.QueryRow(`SELECT `+smtpAccountCols+` FROM smtp_accounts WHERE user_id = ? AND status = 'active'`, userID)
	return scanSMTPAccount(row)
}

func GetSMTPAccount(id int64) (SMTPAccount, error) {
	row := db.DB.QueryRow(`SELECT `+smtpAccountCols+` FROM smtp_accounts WHERE id = ?`, id)
	return scanSMTPAccount(row)
}

func ListActiveSMTPAccounts() ([]SMTPAccount, error) {
	rows, err := db.DB.Query(`SELECT `+smtpAccountCols+` FROM smtp_accounts WHERE status = 'active' ORDER BY id ASC`)
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
	_, err := db.DB.Exec(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email,
			status, warmup_started_at, sends_today_reset_at, created_at, updated_at
		) VALUES (?, 'Primary', 'smtp.gmail.com', '587', '', '', '', 'active', ?, ?, ?, ?)
	`, userID, now, now.Format("2006-01-02"), now, now)
	return err
}

func UpsertSMTPAccountForUser(userID int64, a SMTPAccount) error {
	existing, err := GetSMTPAccountByUserID(userID)
	if err == nil {
		a.ID = existing.ID
		a.UserID = userID
		return UpdateSMTPAccount(a)
	}
	a.UserID = userID
	a.Name = "Primary"
	a.Status = "active"
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
	row := db.DB.QueryRow(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
			imap_host, imap_port, imap_user, imap_password, status, daily_limit, per_minute_limit,
			min_seconds_between_sends, warmup_enabled, warmup_daily_cap, warmup_target_daily_cap,
			warmup_increment_per_day, warmup_started_at, sends_today_reset_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`,
		userID, a.Name, a.SMTPHost, a.SMTPPort, a.SMTPUser, a.SMTPPassword, a.FromEmail, a.FromName,
		a.IMAPHost, a.IMAPPort, a.IMAPUser, a.IMAPPassword, a.Status, a.DailyLimit, a.PerMinuteLimit,
		a.MinSecondsBetweenSends, warmup, a.WarmupDailyCap, a.WarmupTargetDailyCap, a.WarmupIncrementPerDay,
		warmupStart, now.Format("2006-01-02"), now, now,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func UpdateSMTPAccount(a SMTPAccount) error {
	warmup := 0
	if a.WarmupEnabled {
		warmup = 1
	}
	_, err := db.DB.Exec(`
		UPDATE smtp_accounts SET
			name=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, from_email=?, from_name=?,
			imap_host=?, imap_port=?, imap_user=?, imap_password=?, status=?, daily_limit=?,
			per_minute_limit=?, min_seconds_between_sends=?, warmup_enabled=?, warmup_daily_cap=?,
			warmup_target_daily_cap=?, warmup_increment_per_day=?, updated_at=?
		WHERE id=?
	`,
		a.Name, a.SMTPHost, a.SMTPPort, a.SMTPUser, a.SMTPPassword, a.FromEmail, a.FromName,
		a.IMAPHost, a.IMAPPort, a.IMAPUser, a.IMAPPassword, a.Status, a.DailyLimit,
		a.PerMinuteLimit, a.MinSecondsBetweenSends, warmup, a.WarmupDailyCap,
		a.WarmupTargetDailyCap, a.WarmupIncrementPerDay, time.Now(), a.ID,
	)
	return err
}

func SetSMTPAccountStatus(id int64, status string) error {
	_, err := db.DB.Exec(`UPDATE smtp_accounts SET status=?, updated_at=? WHERE id=?`, status, time.Now(), id)
	return err
}

func IncrementAccountSendCount(accountID int64) error {
	today := time.Now().Format("2006-01-02")
	_, err := db.DB.Exec(`
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
