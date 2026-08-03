package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/googleoauth"
)

const (
	DefaultWarmupDailyCap        = 20
	DefaultWarmupIncrementPerDay = 20
	MailboxSourceShared          = "shared"
	MailboxSourceInboxKit        = "inboxkit"
)

type planSpec struct {
	DailyEmailCap             int
	AICreditsPerDay           int
	WarmupEnabled             bool
	WarmupDailyCap            int
	WarmupTargetDailyCap      int
	WarmupIncrementPerDay     int
	PerMinuteLimit            int
	MinSecondsBetweenSends    int
	IncludedDomains           int
	IncludedMailboxes         int
}

func PlanSpecForTier(tier PlanTier) (planSpec, error) {
	switch NormalizePlanTier(string(tier)) {
	case PlanTierFree:
		return planSpec{
			DailyEmailCap:          10,
			AICreditsPerDay:        50,
			WarmupEnabled:          false,
			WarmupDailyCap:         0,
			WarmupTargetDailyCap:   0,
			WarmupIncrementPerDay:  0,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
		}, nil
	case PlanTierPro:
		return planSpec{
			DailyEmailCap:          250, // per mailbox
			AICreditsPerDay:        5000,
			WarmupEnabled:          true,
			WarmupDailyCap:         DefaultWarmupDailyCap,
			WarmupTargetDailyCap:   250,
			WarmupIncrementPerDay:  DefaultWarmupIncrementPerDay,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        1,
			IncludedMailboxes:      config.InboxKitIncludedMailboxCount(),
		}, nil
	default:
		return planSpec{}, fmt.Errorf("unknown plan tier %q", tier)
	}
}

func AICreditsCapForTier(tier PlanTier) int {
	s, err := PlanSpecForTier(tier)
	if err != nil {
		s, _ = PlanSpecForTier(PlanTierFree)
	}
	return s.AICreditsPerDay
}

// PlanInfo is a user-facing summary of a plan tier.
type PlanInfo struct {
	Tier        PlanTier
	Name        string
	DailyEmails int
	AICredits   int
	Warmup      bool
	Domains     int
	Mailboxes   int
}

func PlanInfoForTier(tier PlanTier) PlanInfo {
	tier = NormalizePlanTier(string(tier))
	spec, err := PlanSpecForTier(tier)
	if err != nil {
		spec, _ = PlanSpecForTier(PlanTierFree)
		tier = PlanTierFree
	}
	name := "Free"
	if tier == PlanTierPro {
		name = "Pro"
	}
	return PlanInfo{
		Tier:        tier,
		Name:        name,
		DailyEmails: spec.DailyEmailCap,
		AICredits:   spec.AICreditsPerDay,
		Warmup:      spec.WarmupEnabled,
		Domains:     spec.IncludedDomains,
		Mailboxes:   spec.IncludedMailboxes,
	}
}

func AllPlanTiers() []PlanInfo {
	return []PlanInfo{
		PlanInfoForTier(PlanTierFree),
		PlanInfoForTier(PlanTierPro),
	}
}

func NormalizePlanTier(s string) PlanTier {
	switch PlanTier(strings.ToLower(strings.TrimSpace(s))) {
	case PlanTierPro, PlanTierStandard:
		// Legacy "standard" maps to Pro.
		return PlanTierPro
	default:
		return PlanTierFree
	}
}

func UserIsPro(userID int64) bool {
	u, err := GetUserByID(userID)
	if err != nil {
		return false
	}
	return NormalizePlanTier(u.PlanTier) == PlanTierPro && UserHasAppAccess(u)
}

// ApplyPlanLimitsToUser sets plan_tier + synchronizes smtp_accounts sending limits and warmup behavior.
func ApplyPlanLimitsToUser(userID int64, tier PlanTier) error {
	tier = NormalizePlanTier(string(tier))
	spec, err := PlanSpecForTier(tier)
	if err != nil {
		return err
	}

	if tier == PlanTierFree {
		if err := EnsureSharedSMTPAccountForUser(userID, spec); err != nil {
			return err
		}
	} else if _, err := GetSMTPAccountByUserID(userID); err != nil {
		if err := CreateDefaultSMTPAccountForUser(userID); err != nil {
			return err
		}
	}

	now := time.Now()

	var warmupStartedAt any = nil
	if spec.WarmupEnabled {
		warmupStartedAt = now
	}
	warmupEnabledInt := 0
	if spec.WarmupEnabled {
		warmupEnabledInt = 1
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE users SET plan_tier = ? WHERE id = ?`, string(tier), userID); err != nil {
		return err
	}

	if tier == PlanTierPro {
		_, _ = tx.Exec(`
			UPDATE smtp_accounts SET status='inactive', is_default=0, updated_at=?
			WHERE user_id=? AND mailbox_source=?
		`, now, userID, MailboxSourceShared)
	}

	// Sync limit fields only. Never reset sends_today/last_send_at here — RequireMailboxSetup
	// and billing webhooks call this often; wiping counters let Free users exceed the daily cap.
	// Day rollover is handled by ResetAccountDailyIfNeeded.
	if _, err := tx.Exec(`
		UPDATE smtp_accounts SET
			daily_limit=?,
			per_minute_limit=?,
			min_seconds_between_sends=?,
			warmup_enabled=?,
			warmup_daily_cap=?,
			warmup_target_daily_cap=?,
			warmup_increment_per_day=?,
			warmup_started_at=CASE
				WHEN ? = 1 AND COALESCE(warmup_enabled, 0) = 0 THEN ?
				WHEN ? = 0 THEN NULL
				ELSE warmup_started_at
			END,
			updated_at=?
		WHERE user_id=? AND COALESCE(mailbox_source,'') <> ?
	`, spec.DailyEmailCap, spec.PerMinuteLimit, spec.MinSecondsBetweenSends,
		warmupEnabledInt, spec.WarmupDailyCap, spec.WarmupTargetDailyCap, spec.WarmupIncrementPerDay,
		warmupEnabledInt, warmupStartedAt, warmupEnabledInt, now, userID, MailboxSourceShared); err != nil {
		return err
	}

	if tier == PlanTierFree {
		if _, err := tx.Exec(`
			UPDATE smtp_accounts SET
				daily_limit=?,
				per_minute_limit=?,
				min_seconds_between_sends=?,
				warmup_enabled=0,
				warmup_daily_cap=0,
				warmup_target_daily_cap=0,
				warmup_increment_per_day=0,
				warmup_started_at=NULL,
				status='active',
				is_default=1,
				updated_at=?
			WHERE user_id=? AND mailbox_source=?
		`, spec.DailyEmailCap, spec.PerMinuteLimit, spec.MinSecondsBetweenSends, now, userID, MailboxSourceShared); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// EnsureFreeSharedMailbox attaches/refreshes the Free plan shared SMTP without resetting send counters.
func EnsureFreeSharedMailbox(userID int64) error {
	spec, err := PlanSpecForTier(PlanTierFree)
	if err != nil {
		return err
	}
	return EnsureSharedSMTPAccountForUser(userID, spec)
}

// EnsureSharedSMTPAccountForUser attaches the platform shared SMTP profile for Free users.
func EnsureSharedSMTPAccountForUser(userID int64, spec planSpec) error {
	if config.SMTPHost == "" || config.SMTPUser == "" || config.SMTPPass == "" {
		return fmt.Errorf("shared SMTP not configured: set SMTP_HOST, SMTP_USER, and APP_PASSWORD")
	}
	encPass, err := googleoauth.Encrypt(config.SMTPPass)
	if err != nil {
		return fmt.Errorf("encrypt shared smtp password: %w", err)
	}
	from := config.SMTPFrom
	if from == "" {
		from = config.SMTPUser
	}
	port := config.SMTPPort
	if port == "" {
		port = "587"
	}
	daily := spec.DailyEmailCap
	if daily <= 0 {
		daily = 10
	}
	now := time.Now()
	imapHost := config.SharedIMAPHost()
	imapPort := config.SharedIMAPPort()

	var existingID int64
	_ = db.QueryRow(`
		SELECT id FROM smtp_accounts
		WHERE user_id=? AND mailbox_source=?
		ORDER BY id ASC LIMIT 1
	`, userID, MailboxSourceShared).Scan(&existingID)

	_, _ = db.Exec(`UPDATE smtp_accounts SET is_default=0 WHERE user_id=?`, userID)

	if existingID > 0 {
		_, err = db.Exec(`
			UPDATE smtp_accounts SET
				name=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, from_email=?, from_name=?,
				imap_host=?, imap_port=?, imap_user=?, imap_password=?,
				status='active', auth_type='', oauth_refresh_token='', oauth_access_token='', google_email='',
				is_default=1, mailbox_source=?, daily_limit=?, per_minute_limit=?, min_seconds_between_sends=?,
				warmup_enabled=0, warmup_daily_cap=0, warmup_target_daily_cap=0, warmup_increment_per_day=0,
				warmup_started_at=NULL, updated_at=?
			WHERE id=?
		`, from, config.SMTPHost, port, config.SMTPUser, encPass, from, "jupsend",
			imapHost, imapPort, config.SMTPUser, encPass,
			MailboxSourceShared, daily, spec.PerMinuteLimit, spec.MinSecondsBetweenSends, now, existingID)
		return err
	}

	_, err = db.Exec(`
		INSERT INTO smtp_accounts (
			user_id, name, smtp_host, smtp_port, smtp_user, smtp_password, from_email, from_name,
			imap_host, imap_port, imap_user, imap_password,
			status, daily_limit, per_minute_limit, min_seconds_between_sends, warmup_enabled,
			auth_type, is_default, mailbox_source, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 0, '', 1, ?, ?, ?)
	`, userID, from, config.SMTPHost, port, config.SMTPUser, encPass, from, "jupsend",
		imapHost, imapPort, config.SMTPUser, encPass,
		daily, spec.PerMinuteLimit, spec.MinSecondsBetweenSends, MailboxSourceShared, now, now)
	return err
}

// MigrateLegacyStandardUsersToPro remaps plan_tier=standard → pro and reapplies limits.
func MigrateLegacyStandardUsersToPro() {
	_, _ = db.Exec(`UPDATE users SET plan_tier = 'pro' WHERE LOWER(COALESCE(plan_tier,'')) = 'standard'`)
	MigrateAllUsersToPlanLimits()
}

// MigrateAllUsersToPlanLimits applies plan-driven sending caps/warmup based on users.plan_tier.
func MigrateAllUsersToPlanLimits() {
	rows, err := db.Query(`SELECT id, COALESCE(plan_tier,'free') FROM users`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var tierStr string
		if err := rows.Scan(&id, &tierStr); err != nil {
			continue
		}
		_ = ApplyPlanLimitsToUser(id, NormalizePlanTier(tierStr))
	}
}
