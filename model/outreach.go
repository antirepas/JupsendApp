package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/googleoauth"
)

type OutreachDomain struct {
	ID               int64
	UserID           int64
	Domain           string
	InboxkitOrderID  string
	Status           string
	Included         bool
	RedirectURL      string
	NameserversJSON  string
	LastError        string
	LastSyncedAt     *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type OutreachMailbox struct {
	ID                 int64
	UserID             int64
	DomainID           int64
	SMTPAccountID      int64
	InboxkitMailboxID  string
	Email              string
	FirstName          string
	LastName           string
	Platform           string
	Status             string
	IsDefault          bool
	IsAdmin            bool
	Role               string
	ForwardingEmail    string
	LastError          string
	CancelledAt        *time.Time
	HealthJSON         string
	AnalyticsJSON      string
	Included           bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func UserHasReadyMailbox(userID int64) bool {
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM outreach_mailboxes
		WHERE user_id = ? AND status = 'ready'
	`, userID).Scan(&n)
	if n > 0 {
		return true
	}
	// Free shared SMTP (and any other send-ready profile) counts as ready without InboxKit rows.
	acc, err := GetSMTPAccountByUserID(userID)
	return err == nil && acc.IsSendReady()
}

func UserHasOutreachDomain(userID int64) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM outreach_domains WHERE user_id = ?`, userID).Scan(&n)
	return n > 0
}

// CountActiveIncludedDomains counts Pro included domains that are not failed/cancelled.
// Used to enforce the one included domain quota.
func CountActiveIncludedDomains(userID int64) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM outreach_domains
		WHERE user_id = ?
		  AND included = TRUE
		  AND lower(status) NOT IN ('error', 'cancelled', 'canceled')
	`, userID).Scan(&n)
	return n, err
}

// IncludedDomainQuotaRemaining returns how many included domains the user may still claim.
func IncludedDomainQuotaRemaining(userID int64) (int, error) {
	spec, err := PlanSpecForTier(PlanTierPro)
	if err != nil {
		return 0, err
	}
	n, err := CountActiveIncludedDomains(userID)
	if err != nil {
		return 0, err
	}
	left := spec.IncludedDomains - n
	if left < 0 {
		return 0, nil
	}
	return left, nil
}

func CreateOutreachDomain(userID int64, domain, orderID, redirect string, included bool) (int64, error) {
	now := time.Now()
	var id int64
	err := db.QueryRow(`
		INSERT INTO outreach_domains (user_id, domain, inboxkit_order_id, status, included, redirect_url, created_at, updated_at)
		VALUES (?, ?, ?, 'ordering', ?, ?, ?, ?)
		ON CONFLICT (user_id, domain) DO UPDATE SET
			inboxkit_order_id = EXCLUDED.inboxkit_order_id,
			status = 'ordering',
			included = EXCLUDED.included,
			redirect_url = EXCLUDED.redirect_url,
			last_error = '',
			updated_at = EXCLUDED.updated_at
		RETURNING id
	`, userID, strings.ToLower(domain), orderID, included, redirect, now, now).Scan(&id)
	return id, err
}

func DeleteOutreachDomain(id, userID int64) error {
	_, err := db.Exec(`DELETE FROM outreach_domains WHERE id=? AND user_id=?`, id, userID)
	return err
}

func UpdateOutreachDomainStatus(id int64, status, orderID string) error {
	_, err := db.Exec(`
		UPDATE outreach_domains SET status=?, inboxkit_order_id=COALESCE(NULLIF(?, ''), inboxkit_order_id), updated_at=?
		WHERE id=?
	`, status, orderID, time.Now(), id)
	return err
}

// ResetOutreachDomainForFulfill clears a manual queue marker so InboxKit place can run.
func ResetOutreachDomainForFulfill(id int64) error {
	_, err := db.Exec(`
		UPDATE outreach_domains
		SET status='ordering', inboxkit_order_id='', last_error='', updated_at=?
		WHERE id=?
	`, time.Now(), id)
	return err
}

func SetOutreachDomainError(id int64, status, lastError string) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE outreach_domains SET status=?, last_error=?, last_synced_at=?, updated_at=? WHERE id=?
	`, status, lastError, now, now, id)
	return err
}

func ClearOutreachDomainError(id int64) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE outreach_domains SET last_error='', last_synced_at=?, updated_at=? WHERE id=?
	`, now, now, id)
	return err
}

func TouchOutreachDomainSynced(id int64) error {
	now := time.Now()
	_, err := db.Exec(`UPDATE outreach_domains SET last_synced_at=?, updated_at=? WHERE id=?`, now, now, id)
	return err
}

func SetOutreachDomainNameservers(id int64, nameservers []string) error {
	raw := "[]"
	if len(nameservers) > 0 {
		b, err := json.Marshal(nameservers)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := db.Exec(`
		UPDATE outreach_domains SET nameservers_json=?, updated_at=? WHERE id=?
	`, raw, time.Now(), id)
	return err
}

func (d OutreachDomain) Nameservers() []string {
	if strings.TrimSpace(d.NameserversJSON) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(d.NameserversJSON), &out); err != nil {
		return nil
	}
	return out
}

func MarkOutreachDomainPaid(id int64) error {
	_, err := db.Exec(`UPDATE outreach_domains SET included=FALSE, updated_at=? WHERE id=?`, time.Now(), id)
	return err
}

func GetMailboxAnalyticsBySMTPAccountID(smtpAccountID int64) string {
	if db.DB == nil || smtpAccountID <= 0 {
		return "{}"
	}
	var raw string
	_ = db.QueryRow(`
		SELECT COALESCE(analytics_json,'{}') FROM outreach_mailboxes
		WHERE smtp_account_id=? ORDER BY id DESC LIMIT 1
	`, smtpAccountID).Scan(&raw)
	if raw == "" {
		return "{}"
	}
	return raw
}

func GetOutreachDomain(id, userID int64) (OutreachDomain, error) {
	row := db.QueryRow(`
		SELECT id, user_id, domain, COALESCE(inboxkit_order_id,''), status, included, COALESCE(redirect_url,''),
			COALESCE(nameservers_json,''), COALESCE(last_error,''), last_synced_at, created_at, updated_at
		FROM outreach_domains WHERE id=? AND user_id=?
	`, id, userID)
	return scanOutreachDomain(row)
}

func GetOutreachDomainByName(userID int64, domain string) (OutreachDomain, error) {
	row := db.QueryRow(`
		SELECT id, user_id, domain, COALESCE(inboxkit_order_id,''), status, included, COALESCE(redirect_url,''),
			COALESCE(nameservers_json,''), COALESCE(last_error,''), last_synced_at, created_at, updated_at
		FROM outreach_domains WHERE user_id=? AND domain=?
	`, userID, strings.ToLower(domain))
	return scanOutreachDomain(row)
}

func ListOutreachDomains(userID int64) ([]OutreachDomain, error) {
	rows, err := db.Query(`
		SELECT id, user_id, domain, COALESCE(inboxkit_order_id,''), status, included, COALESCE(redirect_url,''),
			COALESCE(nameservers_json,''), COALESCE(last_error,''), last_synced_at, created_at, updated_at
		FROM outreach_domains WHERE user_id=? ORDER BY id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutreachDomain
	for rows.Next() {
		d, err := scanOutreachDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func scanOutreachDomain(row interface{ Scan(...interface{}) error }) (OutreachDomain, error) {
	var d OutreachDomain
	var lastSynced sql.NullTime
	err := row.Scan(&d.ID, &d.UserID, &d.Domain, &d.InboxkitOrderID, &d.Status, &d.Included, &d.RedirectURL,
		&d.NameserversJSON, &d.LastError, &lastSynced, &d.CreatedAt, &d.UpdatedAt)
	if lastSynced.Valid {
		t := lastSynced.Time
		d.LastSyncedAt = &t
	}
	return d, err
}

// UpsertOutreachMailbox inserts or updates by (user_id, email) to avoid duplicates on retry.
func UpsertOutreachMailbox(m OutreachMailbox) (int64, error) {
	now := time.Now()
	email := strings.ToLower(strings.TrimSpace(m.Email))
	m.Email = email
	var existing int64
	_ = db.QueryRow(`SELECT id FROM outreach_mailboxes WHERE user_id=? AND lower(email)=lower(?)`, m.UserID, email).Scan(&existing)
	if existing > 0 {
		_, err := db.Exec(`
			UPDATE outreach_mailboxes SET
				domain_id=COALESCE(NULLIF(?,0), domain_id),
				inboxkit_mailbox_id=COALESCE(NULLIF(?, ''), inboxkit_mailbox_id),
				first_name=CASE WHEN ? <> '' THEN ? ELSE first_name END,
				last_name=CASE WHEN ? <> '' THEN ? ELSE last_name END,
				platform=CASE WHEN ? <> '' THEN ? ELSE platform END,
				status=CASE WHEN status='ready' THEN status ELSE ? END,
				is_admin=is_admin OR ?,
				role=CASE WHEN ? <> '' THEN ? ELSE role END,
				forwarding_email=CASE WHEN ? <> '' THEN ? ELSE forwarding_email END,
				updated_at=?
			WHERE id=?
		`, m.DomainID, m.InboxkitMailboxID, m.FirstName, m.FirstName, m.LastName, m.LastName,
			m.Platform, m.Platform, m.Status, m.IsAdmin, m.Role, m.Role, m.ForwardingEmail, m.ForwardingEmail, now, existing)
		return existing, err
	}
	return CreateOutreachMailbox(m)
}

func CreateOutreachMailbox(m OutreachMailbox) (int64, error) {
	now := time.Now()
	var id int64
	email := strings.ToLower(strings.TrimSpace(m.Email))
	err := db.QueryRow(`
		INSERT INTO outreach_mailboxes (
			user_id, domain_id, smtp_account_id, inboxkit_mailbox_id, email, first_name, last_name,
			platform, status, is_default, is_admin, role, forwarding_email, last_error,
			health_json, analytics_json, included, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, m.UserID, nullInt64(m.DomainID), nullInt64(m.SMTPAccountID), m.InboxkitMailboxID, email, m.FirstName, m.LastName,
		m.Platform, m.Status, m.IsDefault, m.IsAdmin, m.Role, m.ForwardingEmail, m.LastError,
		coalesceJSON(m.HealthJSON), coalesceJSON(m.AnalyticsJSON), m.Included, now, now,
	).Scan(&id)
	return id, err
}

func UpdateOutreachMailboxReady(id int64, inboxkitID string, smtpAccountID int64, status string) error {
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET inboxkit_mailbox_id=?, smtp_account_id=?, status=?, last_error='', updated_at=?
		WHERE id=?
	`, inboxkitID, nullInt64(smtpAccountID), status, time.Now(), id)
	return err
}

func SetOutreachMailboxError(id int64, lastError string) error {
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET last_error=?, updated_at=? WHERE id=?
	`, lastError, time.Now(), id)
	return err
}

func SetOutreachMailboxForwarding(id int64, forwardingEmail string) error {
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET forwarding_email=?, updated_at=? WHERE id=?
	`, strings.TrimSpace(forwardingEmail), time.Now(), id)
	return err
}

func SetOutreachMailboxMeta(id int64, isAdmin bool, role, forwardingEmail, status string) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET
			is_admin=?, role=COALESCE(NULLIF(?, ''), role),
			forwarding_email=CASE WHEN ? <> '' THEN ? ELSE forwarding_email END,
			status=CASE WHEN ? <> '' THEN ? ELSE status END,
			updated_at=?
		WHERE id=?
	`, isAdmin, role, forwardingEmail, forwardingEmail, status, status, now, id)
	return err
}

func MarkOutreachMailboxCancelled(id int64) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET status='scheduled_cancel', cancelled_at=?, updated_at=? WHERE id=?
	`, now, now, id)
	return err
}

// CountCampaignsUsingSMTP counts distinct campaigns that queued sends via this SMTP account.
func CountCampaignsUsingSMTP(smtpAccountID int64) int {
	if smtpAccountID <= 0 {
		return 0
	}
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT campaign_id) FROM send_jobs
		WHERE smtp_account_id=? AND COALESCE(campaign_id,0) > 0
	`, smtpAccountID).Scan(&n)
	return n
}

// UpdateMailboxWarmupSettings toggles local jupsend warmup on the linked SMTP account.
func UpdateMailboxWarmupSettings(userID, mailboxID int64, enabled bool, dailyLimit int) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.SMTPAccountID <= 0 {
		return fmt.Errorf("mailbox has no SMTP account yet")
	}
	acc, err := GetSMTPAccount(m.SMTPAccountID)
	if err != nil || acc.UserID != userID {
		return fmt.Errorf("smtp account not found")
	}
	acc.WarmupEnabled = enabled
	if dailyLimit > 0 {
		acc.DailyLimit = dailyLimit
		if enabled && acc.WarmupTargetDailyCap <= 0 {
			acc.WarmupTargetDailyCap = dailyLimit
		}
	}
	return UpdateSMTPAccount(acc)
}

// DecryptMailboxPassword returns the plaintext SMTP password for the mailbox owner (reveal/copy).
func DecryptMailboxPassword(userID, mailboxID int64) (string, error) {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return "", err
	}
	if m.SMTPAccountID <= 0 {
		return "", fmt.Errorf("no credentials")
	}
	acc, err := GetSMTPAccount(m.SMTPAccountID)
	if err != nil || acc.UserID != userID {
		return "", fmt.Errorf("smtp account not found")
	}
	if acc.MailboxSource == MailboxSourceShared {
		return "", fmt.Errorf("shared Free mailbox credentials are not revealable")
	}
	if strings.TrimSpace(acc.SMTPPassword) == "" {
		return "", fmt.Errorf("no password stored")
	}
	return DecryptSMTPPassword(acc)
}

func SetMailboxAnalytics(id int64, health, analytics string) error {
	_, err := db.Exec(`
		UPDATE outreach_mailboxes SET health_json=?, analytics_json=?, updated_at=? WHERE id=?
	`, coalesceJSON(health), coalesceJSON(analytics), time.Now(), id)
	return err
}

func ListOutreachMailboxes(userID int64) ([]OutreachMailbox, error) {
	rows, err := db.Query(`
		SELECT id, user_id, COALESCE(domain_id,0), COALESCE(smtp_account_id,0), COALESCE(inboxkit_mailbox_id,''),
			email, COALESCE(first_name,''), COALESCE(last_name,''), platform, status, is_default,
			COALESCE(is_admin,FALSE), COALESCE(role,''), COALESCE(forwarding_email,''), COALESCE(last_error,''),
			cancelled_at, COALESCE(health_json,'{}'), COALESCE(analytics_json,'{}'), included, created_at, updated_at
		FROM outreach_mailboxes WHERE user_id=? ORDER BY is_default DESC, id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutreachMailbox
	for rows.Next() {
		m, err := scanOutreachMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func GetOutreachMailbox(id, userID int64) (OutreachMailbox, error) {
	row := db.QueryRow(`
		SELECT id, user_id, COALESCE(domain_id,0), COALESCE(smtp_account_id,0), COALESCE(inboxkit_mailbox_id,''),
			email, COALESCE(first_name,''), COALESCE(last_name,''), platform, status, is_default,
			COALESCE(is_admin,FALSE), COALESCE(role,''), COALESCE(forwarding_email,''), COALESCE(last_error,''),
			cancelled_at, COALESCE(health_json,'{}'), COALESCE(analytics_json,'{}'), included, created_at, updated_at
		FROM outreach_mailboxes WHERE id=? AND user_id=?
	`, id, userID)
	return scanOutreachMailbox(row)
}

func scanOutreachMailbox(row interface{ Scan(...interface{}) error }) (OutreachMailbox, error) {
	var m OutreachMailbox
	var cancelled sql.NullTime
	err := row.Scan(
		&m.ID, &m.UserID, &m.DomainID, &m.SMTPAccountID, &m.InboxkitMailboxID,
		&m.Email, &m.FirstName, &m.LastName, &m.Platform, &m.Status, &m.IsDefault,
		&m.IsAdmin, &m.Role, &m.ForwardingEmail, &m.LastError, &cancelled,
		&m.HealthJSON, &m.AnalyticsJSON, &m.Included, &m.CreatedAt, &m.UpdatedAt,
	)
	if cancelled.Valid {
		t := cancelled.Time
		m.CancelledAt = &t
	}
	return m, err
}

func ClearDefaultMailboxes(userID int64) error {
	_, err := db.Exec(`UPDATE outreach_mailboxes SET is_default=FALSE WHERE user_id=?`, userID)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE smtp_accounts SET is_default=0 WHERE user_id=?`, userID)
	return err
}

func SetDefaultOutreachMailbox(userID, mailboxID int64) error {
	if err := ClearDefaultMailboxes(userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE outreach_mailboxes SET is_default=TRUE, updated_at=? WHERE id=? AND user_id=?`, time.Now(), mailboxID, userID)
	if err != nil {
		return err
	}
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.SMTPAccountID > 0 {
		_, err = db.Exec(`UPDATE smtp_accounts SET is_default=1, updated_at=? WHERE id=?`, time.Now(), m.SMTPAccountID)
	}
	return err
}

// UpdateMailboxFromName sets the display name recipients see in From (and stores first/last on the outreach row).
func UpdateMailboxFromName(userID, mailboxID int64, fromName string) error {
	fromName = strings.TrimSpace(fromName)
	if fromName == "" {
		return fmt.Errorf("from name is required")
	}
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	parts := strings.Fields(fromName)
	fn, ln := fromName, ""
	if len(parts) >= 1 {
		fn = parts[0]
	}
	if len(parts) >= 2 {
		ln = strings.Join(parts[1:], " ")
	}
	now := time.Now()
	if _, err := db.Exec(`
		UPDATE outreach_mailboxes SET first_name=?, last_name=?, updated_at=? WHERE id=? AND user_id=?
	`, fn, ln, now, mailboxID, userID); err != nil {
		return err
	}
	if m.SMTPAccountID > 0 {
		_, err = db.Exec(`UPDATE smtp_accounts SET from_name=?, updated_at=? WHERE id=? AND user_id=?`, fromName, now, m.SMTPAccountID, userID)
		return err
	}
	return nil
}

// UpdateMailboxCredentials updates SMTP/IMAP login for a mailbox (manual or InboxKit-linked).
// Empty password keeps the existing password. Hosts/ports/username update when non-empty.
func UpdateMailboxCredentials(userID, mailboxID int64, smtpHost, smtpPort, imapHost, imapPort, username, password string) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.SMTPAccountID <= 0 {
		return fmt.Errorf("this mailbox has no SMTP credentials to update")
	}
	acc, err := GetSMTPAccount(m.SMTPAccountID)
	if err != nil || acc.UserID != userID {
		return fmt.Errorf("smtp account not found")
	}
	if acc.MailboxSource == MailboxSourceShared {
		return fmt.Errorf("shared Free mailbox credentials are managed by the server")
	}
	if smtpHost == "" {
		smtpHost = acc.SMTPHost
	}
	if smtpPort == "" {
		smtpPort = acc.SMTPPort
	}
	if imapHost == "" {
		imapHost = acc.IMAPHost
	}
	if imapPort == "" {
		imapPort = acc.IMAPPort
	}
	if username == "" {
		username = acc.SMTPUser
	}
	encPass := acc.SMTPPassword
	if strings.TrimSpace(password) != "" {
		password = config.NormalizeAppPassword(password)
		enc, err := googleoauth.Encrypt(password)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		encPass = enc
	}
	now := time.Now()
	_, err = db.Exec(`
		UPDATE smtp_accounts SET
			smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?,
			imap_host=?, imap_port=?, imap_user=?, imap_password=?,
			auth_type='', status='active', updated_at=?
		WHERE id=? AND user_id=?
	`, smtpHost, smtpPort, username, encPass, imapHost, imapPort, username, encPass, now, acc.ID, userID)
	return err
}

// DeleteOutreachMailbox removes a mailbox from the account. Deletes the linked SMTP row
// when it is not shared and unused by other outreach mailboxes.
func DeleteOutreachMailbox(userID, mailboxID int64) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	smtpID := m.SMTPAccountID
	wasDefault := m.IsDefault
	res, err := db.Exec(`DELETE FROM outreach_mailboxes WHERE id=? AND user_id=?`, mailboxID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("mailbox not found")
	}
	if smtpID > 0 {
		acc, err := GetSMTPAccount(smtpID)
		if err == nil && acc.UserID == userID && acc.MailboxSource != MailboxSourceShared {
			var refs int
			_ = db.QueryRow(`SELECT COUNT(*) FROM outreach_mailboxes WHERE smtp_account_id=?`, smtpID).Scan(&refs)
			if refs == 0 {
				_, _ = db.Exec(`DELETE FROM smtp_accounts WHERE id=? AND user_id=?`, smtpID, userID)
			}
		}
	}
	if wasDefault {
		var nextID int64
		_ = db.QueryRow(`
			SELECT id FROM outreach_mailboxes
			WHERE user_id=? AND status='ready'
			ORDER BY id ASC LIMIT 1
		`, userID).Scan(&nextID)
		if nextID > 0 {
			_ = SetDefaultOutreachMailbox(userID, nextID)
		}
	}
	return nil
}

// UpsertInboxKitSMTPAccount creates/updates an smtp_accounts row for plain SMTP sending.
func UpsertInboxKitSMTPAccount(userID int64, email, host, port, user, password, fromName, inboxkitID string, isDefault bool, dailyLimit int, imapHost, imapPort string) (int64, error) {
	password = config.NormalizeAppPassword(password)
	encPass, err := googleoauth.Encrypt(password)
	if err != nil {
		return 0, fmt.Errorf("encrypt smtp password: %w", err)
	}
	if host == "" {
		host = "smtp.gmail.com"
	}
	if port == "" {
		port = "587"
	}
	port = normalizeGmailSMTPPort(host, port)
	if user == "" {
		user = email
	}
	if imapHost == "" {
		imapHost = "imap.gmail.com"
	}
	if imapPort == "" {
		imapPort = "993"
	}
	if dailyLimit <= 0 {
		dailyLimit = 50
	}
	now := time.Now()
	def := 0
	if isDefault {
		def = 1
		_, _ = db.Exec(`UPDATE smtp_accounts SET is_default=0 WHERE user_id=?`, userID)
	}

	var existingID int64
	_ = db.QueryRow(`SELECT id FROM smtp_accounts WHERE user_id=? AND inboxkit_mailbox_id=?`, userID, inboxkitID).Scan(&existingID)
	if existingID > 0 {
		_, err = db.Exec(`
			UPDATE smtp_accounts SET
				name=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, from_email=?, from_name=?,
				imap_host=?, imap_port=?, imap_user=?, imap_password=?,
				status='active', auth_type='', oauth_refresh_token='', oauth_access_token='', google_email='',
				is_default=?, mailbox_source='inboxkit', daily_limit=?, updated_at=?
			WHERE id=?
		`, email, host, port, user, encPass, email, fromName, imapHost, imapPort, user, encPass, def, dailyLimit, now, existingID)
		return existingID, err
	}

	var id int64
	err = db.QueryRow(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
			imap_host, imap_port, imap_user, imap_password,
			status, daily_limit, per_minute_limit, min_seconds_between_sends, warmup_enabled,
			auth_type, inboxkit_mailbox_id, is_default, mailbox_source, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, 2, 30, 0, '', ?, ?, 'inboxkit', ?, ?)
		RETURNING id
	`, userID, email, host, port, user, encPass, email, fromName, imapHost, imapPort, user, encPass, dailyLimit, inboxkitID, def, now, now).Scan(&id)
	return id, err
}

// DecryptSMTPPassword returns plaintext password for InboxKit/shared/manual-sourced accounts.
func DecryptSMTPPassword(acc SMTPAccount) (string, error) {
	if acc.MailboxSource != MailboxSourceInboxKit && acc.MailboxSource != MailboxSourceShared && acc.MailboxSource != MailboxSourceManual && acc.AuthType != "" {
		return config.NormalizeAppPassword(acc.SMTPPassword), nil
	}
	if acc.SMTPPassword == "" {
		return "", nil
	}
	plain, err := googleoauth.Decrypt(acc.SMTPPassword)
	if err != nil {
		// Legacy plaintext only when the stored value is clearly not ciphertext.
		if looksLikeEncryptedSecret(acc.SMTPPassword) {
			return "", fmt.Errorf("could not decrypt stored SMTP password — re-enter it under Mailboxes → Manage → Credentials")
		}
		return config.NormalizeAppPassword(acc.SMTPPassword), nil
	}
	return config.NormalizeAppPassword(plain), nil
}

func looksLikeEncryptedSecret(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 32 {
		return false
	}
	// Our Encrypt() output is standard base64 of nonce+ciphertext.
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			continue
		}
		return false
	}
	return true
}

func normalizeGmailSMTPPort(host, port string) string {
	port = strings.TrimSpace(port)
	if !strings.EqualFold(strings.TrimSpace(host), "smtp.gmail.com") {
		if port == "" {
			return "587"
		}
		return port
	}
	if port == "" || port == "465" {
		return "587"
	}
	return port
}

// AttachManualSendingMailbox attaches SMTP/IMAP credentials for a mailbox you already operate
// (admin/Pro escape hatch when not provisioning via InboxKit purchase).
func AttachManualSendingMailbox(userID int64, email, fromName, smtpHost, smtpPort, imapHost, imapPort, username, password string, isDefault bool) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return 0, fmt.Errorf("valid email is required")
	}
	if password == "" {
		return 0, fmt.Errorf("password is required")
	}
	password = config.NormalizeAppPassword(password)
	if password == "" {
		return 0, fmt.Errorf("password is required")
	}
	if username == "" {
		username = email
	}
	if smtpHost == "" {
		smtpHost = "smtp.gmail.com"
	}
	if smtpPort == "" {
		smtpPort = "587"
	}
	smtpPort = normalizeGmailSMTPPort(smtpHost, smtpPort)
	if imapHost == "" {
		imapHost = "imap.gmail.com"
	}
	if imapPort == "" {
		imapPort = "993"
	}
	if fromName == "" {
		fromName = email
	}
	encPass, err := googleoauth.Encrypt(password)
	if err != nil {
		return 0, fmt.Errorf("encrypt password: %w", err)
	}
	now := time.Now()
	def := 0
	if isDefault {
		def = 1
		_, _ = db.Exec(`UPDATE smtp_accounts SET is_default=0 WHERE user_id=?`, userID)
	}
	daily := 50
	if spec, err := PlanSpecForTier(PlanTierPro); err == nil && spec.DailyEmailCap > 0 {
		daily = spec.DailyEmailCap
	}

	var existingID int64
	_ = db.QueryRow(`
		SELECT id FROM smtp_accounts
		WHERE user_id=? AND LOWER(from_email)=? AND mailbox_source=?
		ORDER BY id ASC LIMIT 1
	`, userID, email, MailboxSourceManual).Scan(&existingID)
	if existingID > 0 {
		_, err = db.Exec(`
			UPDATE smtp_accounts SET
				name=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, from_email=?, from_name=?,
				imap_host=?, imap_port=?, imap_user=?, imap_password=?,
				status='active', auth_type='', is_default=?, mailbox_source=?, daily_limit=?,
				warmup_enabled=1, updated_at=?
			WHERE id=?
		`, email, smtpHost, smtpPort, username, encPass, email, fromName,
			imapHost, imapPort, username, encPass, def, MailboxSourceManual, daily, now, existingID)
		if err != nil {
			return 0, err
		}
		_ = ensureManualOutreachMailbox(userID, email, fromName, existingID, isDefault)
		return existingID, nil
	}

	var id int64
	err = db.QueryRow(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
			imap_host, imap_port, imap_user, imap_password,
			status, daily_limit, per_minute_limit, min_seconds_between_sends, warmup_enabled,
			auth_type, is_default, mailbox_source, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, 2, 30, 1, '', ?, ?, ?, ?)
		RETURNING id
	`, userID, email, smtpHost, smtpPort, username, encPass, email, fromName,
		imapHost, imapPort, username, encPass, daily, def, MailboxSourceManual, now, now).Scan(&id)
	if err != nil {
		return 0, err
	}
	_ = ensureManualOutreachMailbox(userID, email, fromName, id, isDefault)
	return id, nil
}

func ensureManualOutreachMailbox(userID int64, email, fromName string, smtpAccountID int64, isDefault bool) error {
	parts := strings.Fields(fromName)
	fn, ln := "Manual", "Mailbox"
	if len(parts) >= 1 {
		fn = parts[0]
	}
	if len(parts) >= 2 {
		ln = strings.Join(parts[1:], " ")
	}
	var mbID int64
	_ = db.QueryRow(`
		SELECT id FROM outreach_mailboxes
		WHERE user_id=? AND LOWER(email)=?
		ORDER BY id ASC LIMIT 1
	`, userID, email).Scan(&mbID)
	if mbID > 0 {
		if err := UpdateOutreachMailboxReady(mbID, "", smtpAccountID, "ready"); err != nil {
			return err
		}
		if isDefault {
			return SetDefaultOutreachMailbox(userID, mbID)
		}
		return nil
	}
	id, err := CreateOutreachMailbox(OutreachMailbox{
		UserID:        userID,
		SMTPAccountID: smtpAccountID,
		Email:         email,
		FirstName:     fn,
		LastName:      ln,
		Platform:      "MANUAL",
		Status:        "ready",
		IsDefault:     false,
		Included:      false,
	})
	if err != nil {
		return err
	}
	if isDefault {
		return SetDefaultOutreachMailbox(userID, id)
	}
	return nil
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func coalesceJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

type MailboxPurchase struct {
	ID               int64
	UserID           int64
	DomainID         int64
	Quantity         int
	Status           string
	WhopCheckoutID   string
	PayloadJSON      string
	ErrorMessage     string
	InboxkitOrderID  string
}

func CreateMailboxPurchase(userID, domainID int64, quantity int, payload string) (int64, error) {
	var id int64
	err := db.QueryRow(`
		INSERT INTO mailbox_purchases (user_id, domain_id, quantity, status, payload_json, created_at, updated_at)
		VALUES (?, ?, ?, 'pending_payment', ?, ?, ?)
		RETURNING id
	`, userID, nullInt64(domainID), quantity, coalesceJSON(payload), time.Now(), time.Now()).Scan(&id)
	return id, err
}

func GetMailboxPurchase(id int64) (MailboxPurchase, error) {
	var p MailboxPurchase
	var domainID sql.NullInt64
	err := db.QueryRow(`
		SELECT id, user_id, domain_id, quantity, status, COALESCE(whop_checkout_id,''), COALESCE(payload_json,'{}'),
			COALESCE(error_message,''), COALESCE(inboxkit_order_id,'')
		FROM mailbox_purchases WHERE id=?
	`, id).Scan(&p.ID, &p.UserID, &domainID, &p.Quantity, &p.Status, &p.WhopCheckoutID, &p.PayloadJSON, &p.ErrorMessage, &p.InboxkitOrderID)
	if domainID.Valid {
		p.DomainID = domainID.Int64
	}
	return p, err
}

func UpdateMailboxPurchase(id int64, status, checkoutID, orderID, errMsg string) error {
	_, err := db.Exec(`
		UPDATE mailbox_purchases SET status=?, whop_checkout_id=COALESCE(NULLIF(?,''), whop_checkout_id),
			inboxkit_order_id=COALESCE(NULLIF(?,''), inboxkit_order_id),
			error_message=?, updated_at=?
		WHERE id=?
	`, status, checkoutID, orderID, errMsg, time.Now(), id)
	return err
}

func GetPendingMailboxPurchaseByCheckout(checkoutID string) (MailboxPurchase, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM mailbox_purchases WHERE whop_checkout_id=? ORDER BY id DESC LIMIT 1`, checkoutID).Scan(&id)
	if err != nil {
		return MailboxPurchase{}, err
	}
	return GetMailboxPurchase(id)
}
