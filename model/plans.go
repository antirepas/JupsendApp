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
	DefaultDailyLimit            = 50
	DefaultWarmupDailyCap        = 20 // legacy; unused (warmup removed)
	DefaultWarmupIncrementPerDay = 20 // legacy; unused (warmup removed)
	MailboxSourceShared          = "shared"
	MailboxSourceInboxKit        = "inboxkit"
	MailboxSourceManual          = "manual"
)

type planSpec struct {
	DailyEmailCap          int // 0 = no daily send cap
	AICreditsPerDay        int
	PerMinuteLimit         int
	MinSecondsBetweenSends int
	IncludedDomains        int
	IncludedMailboxes      int
	MaxContacts            int // 0 = unlimited
	MonthlyEmailCap        int // 0 = no monthly cap (Free uses daily only)
	MonthlyPriceUSD        int
	Label                  string
}

func PlanSpecForTier(tier PlanTier) (planSpec, error) {
	switch NormalizePlanTier(string(tier)) {
	case PlanTierFree:
		return planSpec{
			DailyEmailCap:          10,
			AICreditsPerDay:        50,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
			MaxContacts:            0,
			MonthlyEmailCap:        0,
			MonthlyPriceUSD:        0,
			Label:                  "Free",
		}, nil
	case PlanTierStarter:
		return planSpec{
			DailyEmailCap:          0,
			AICreditsPerDay:        10000,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
			MaxContacts:            5000,
			MonthlyEmailCap:        10000,
			MonthlyPriceUSD:        23,
			Label:                  "10K",
		}, nil
	case PlanTierGrowth:
		return planSpec{
			DailyEmailCap:          0,
			AICreditsPerDay:        10000,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
			MaxContacts:            10000,
			MonthlyEmailCap:        25000,
			MonthlyPriceUSD:        49,
			Label:                  "25K",
		}, nil
	case PlanTierScale:
		return planSpec{
			DailyEmailCap:          0,
			AICreditsPerDay:        10000,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
			MaxContacts:            20000,
			MonthlyEmailCap:        50000,
			MonthlyPriceUSD:        64,
			Label:                  "50K",
		}, nil
	case PlanTierHyper:
		return planSpec{
			DailyEmailCap:          0,
			AICreditsPerDay:        10000,
			PerMinuteLimit:         2,
			MinSecondsBetweenSends: 30,
			IncludedDomains:        0,
			IncludedMailboxes:      0,
			MaxContacts:            25000,
			MonthlyEmailCap:        100000,
			MonthlyPriceUSD:        87,
			Label:                  "100K",
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
	Tier            PlanTier
	Name            string
	Label           string
	DailyEmails     int // 0 = unlimited / no daily cap
	MonthlyEmails   int // 0 = no monthly cap
	MaxContacts     int // 0 = unlimited
	AICredits       int
	MonthlyPriceUSD int
	Domains         int
	Mailboxes       int
	Paid            bool
	Recommended     bool
}

func planDisplayName(tier PlanTier) string {
	switch NormalizePlanTier(string(tier)) {
	case PlanTierStarter:
		return "Starter"
	case PlanTierGrowth:
		return "Growth"
	case PlanTierScale:
		return "Scale"
	case PlanTierHyper:
		return "Hyper"
	default:
		return "Free"
	}
}

func PlanInfoForTier(tier PlanTier) PlanInfo {
	tier = NormalizePlanTier(string(tier))
	spec, err := PlanSpecForTier(tier)
	if err != nil {
		spec, _ = PlanSpecForTier(PlanTierFree)
		tier = PlanTierFree
	}
	return PlanInfo{
		Tier:            tier,
		Name:            planDisplayName(tier),
		Label:           spec.Label,
		DailyEmails:     spec.DailyEmailCap,
		MonthlyEmails:   spec.MonthlyEmailCap,
		MaxContacts:     spec.MaxContacts,
		AICredits:       spec.AICreditsPerDay,
		MonthlyPriceUSD: spec.MonthlyPriceUSD,
		Domains:         spec.IncludedDomains,
		Mailboxes:       spec.IncludedMailboxes,
		Paid:            IsPaidPlanTier(tier),
		Recommended:     tier == PlanTierGrowth,
	}
}

// CountUserContacts returns how many contacts the user owns.
func CountUserContacts(userID int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM contact WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// AssertCanCreateContact errors when the plan contact cap would be exceeded by one new contact.
func AssertCanCreateContact(userID int64) error {
	u, err := GetUserByID(userID)
	if err != nil {
		return err
	}
	spec, err := PlanSpecForTier(NormalizePlanTier(u.PlanTier))
	if err != nil || spec.MaxContacts <= 0 {
		return nil
	}
	n, err := CountUserContacts(userID)
	if err != nil {
		return err
	}
	if n >= spec.MaxContacts {
		return fmt.Errorf("contact limit reached (%d). Upgrade or delete contacts to add more", spec.MaxContacts)
	}
	return nil
}

// CountUserEmailsThisMonth counts delivered/attempted sends in the current calendar month (UTC).
func CountUserEmailsThisMonth(userID int64) (int, error) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM email_sends
		WHERE user_id = ?
		  AND sent_at IS NOT NULL
		  AND sent_at >= ?
	`, userID, start).Scan(&n)
	return n, err
}

// UserMonthlyEmailRemaining returns monthly cap and how many sends are left (ok=false when exhausted).
// Cap 0 means no monthly restriction.
func UserMonthlyEmailRemaining(userID int64) (cap int, remaining int, ok bool) {
	u, err := GetUserByID(userID)
	if err != nil {
		return 0, 0, false
	}
	spec, err := PlanSpecForTier(NormalizePlanTier(u.PlanTier))
	if err != nil || spec.MonthlyEmailCap <= 0 {
		return 0, 0, true
	}
	cap = spec.MonthlyEmailCap
	used, err := CountUserEmailsThisMonth(userID)
	if err != nil {
		return cap, 0, false
	}
	remaining = cap - used
	if remaining < 0 {
		remaining = 0
	}
	return cap, remaining, remaining > 0
}

// UserUnderMonthlyEmailCap reports whether the user may send another campaign email this month.
func UserUnderMonthlyEmailCap(userID int64) bool {
	_, _, ok := UserMonthlyEmailRemaining(userID)
	return ok
}

// PaidPlanTiers returns the purchasable email plans (Starter → Hyper).
func PaidPlanTiers() []PlanInfo {
	return []PlanInfo{
		PlanInfoForTier(PlanTierStarter),
		PlanInfoForTier(PlanTierGrowth),
		PlanInfoForTier(PlanTierScale),
		PlanInfoForTier(PlanTierHyper),
	}
}

// AllPlanTiers returns purchasable plans (Free is no longer offered).
func AllPlanTiers() []PlanInfo {
	return PaidPlanTiers()
}

func IsPaidPlanTier(tier PlanTier) bool {
	switch NormalizePlanTier(string(tier)) {
	case PlanTierStarter, PlanTierGrowth, PlanTierScale, PlanTierHyper:
		return true
	default:
		return false
	}
}

func NormalizePlanTier(s string) PlanTier {
	switch PlanTier(strings.ToLower(strings.TrimSpace(s))) {
	case PlanTierStarter:
		return PlanTierStarter
	case PlanTierGrowth:
		return PlanTierGrowth
	case PlanTierScale:
		return PlanTierScale
	case PlanTierHyper:
		return PlanTierHyper
	case PlanTierPro, PlanTierStandard:
		// Legacy Pro/Standard → Starter (same 10k/5k envelope).
		return PlanTierStarter
	case PlanTierFree:
		return PlanTierFree
	default:
		return PlanTierFree
	}
}

func IsValidSignupPlan(s string) bool {
	raw := PlanTier(strings.ToLower(strings.TrimSpace(s)))
	switch raw {
	case PlanTierStarter, PlanTierGrowth, PlanTierScale, PlanTierHyper:
		return true
	case PlanTierPro, PlanTierStandard:
		return true // legacy aliases → Starter
	default:
		return false
	}
}

// DefaultSignupPlan is used when signup omits a plan.
func DefaultSignupPlan() PlanTier {
	return PlanTierStarter
}

func UserIsPro(userID int64) bool {
	u, err := GetUserByID(userID)
	if err != nil {
		return false
	}
	if UserIsAdmin(u) {
		return true
	}
	return IsPaidPlanTier(NormalizePlanTier(u.PlanTier)) && UserHasAppAccess(u)
}

// EnsureAdminProAccess marks the user as admin (if needed) and applies Hyper limits.
func EnsureAdminProAccess(userID int64) error {
	u, err := GetUserByID(userID)
	if err != nil {
		return err
	}
	if !UserIsAdmin(u) {
		return nil
	}
	if !u.IsAdmin {
		_ = SetUserAdmin(userID, true)
	}
	if NormalizePlanTier(u.PlanTier) != PlanTierHyper {
		return ApplyPlanLimitsToUser(userID, PlanTierHyper)
	}
	return nil
}

// ApplyPlanLimitsToUser sets plan_tier + synchronizes smtp_accounts sending limits.
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

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE users SET plan_tier = ? WHERE id = ?`, string(tier), userID); err != nil {
		return err
	}

	if IsPaidPlanTier(tier) {
		_, _ = tx.Exec(`
			UPDATE smtp_accounts SET status='inactive', is_default=0, updated_at=?
			WHERE user_id=? AND mailbox_source=?
		`, now, userID, MailboxSourceShared)
	}

	// Sync limit fields only. Never reset sends_today/last_send_at here — RequireMailboxSetup
	// and billing webhooks call this often; wiping counters let Free users exceed the daily cap.
	// Day rollover is handled by ResetAccountDailyIfNeeded. Warmup ramp is retired.
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
			warmup_current_cap=0,
			warmup_earned_days=0,
			updated_at=?
		WHERE user_id=? AND COALESCE(mailbox_source,'') <> ?
	`, spec.DailyEmailCap, spec.PerMinuteLimit, spec.MinSecondsBetweenSends,
		now, userID, MailboxSourceShared); err != nil {
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
				warmup_current_cap=0,
				warmup_earned_days=0,
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

// EnsureFreeSharedMailbox attaches/refreshes legacy shared SMTP (no longer offered as a plan).
func EnsureFreeSharedMailbox(userID int64) error {
	spec, err := PlanSpecForTier(PlanTierFree)
	if err != nil {
		return err
	}
	return EnsureSharedSMTPAccountForUser(userID, spec)
}

// RevokePaidAccess clears paid entitlement after cancel. Does not require shared SMTP.
func RevokePaidAccess(userID int64) error {
	_, err := db.Exec(`UPDATE users SET plan_tier = ? WHERE id = ?`, string(PlanTierFree), userID)
	if err != nil {
		return err
	}
	return UpdateUserSubscription(userID, SubStatusCancelled, "", "", nil)
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

// MigrateLegacyStandardUsersToPro remaps plan_tier=standard|pro → starter and reapplies limits.
func MigrateLegacyStandardUsersToPro() {
	_, _ = db.Exec(`UPDATE users SET plan_tier = 'starter' WHERE LOWER(COALESCE(plan_tier,'')) IN ('standard', 'pro')`)
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
