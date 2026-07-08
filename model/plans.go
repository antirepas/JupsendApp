package model

import (
	"fmt"
	"time"

	"emailtracker.com/db"
)

type planSpec struct {
	DailyEmailCap              int
	AICreditsPerDay            int
	WarmupEnabled              bool
	WarmupDailyCap             int
	WarmupTargetDailyCap       int
	WarmupIncrementPerDay      int
	PerMinuteLimit             int
	MinSecondsBetweenSends    int
}

func PlanSpecForTier(tier PlanTier) (planSpec, error) {
	switch tier {
	case PlanTierFree:
		return planSpec{
			DailyEmailCap:           10,
			AICreditsPerDay:         50,
			WarmupEnabled:           false,
			WarmupDailyCap:          0,
			WarmupTargetDailyCap:    0,
			WarmupIncrementPerDay:   0,
			PerMinuteLimit:          2,
			MinSecondsBetweenSends: 30,
		}, nil
	case PlanTierStandard:
		return planSpec{
			DailyEmailCap:           250,
			AICreditsPerDay:         2000,
			WarmupEnabled:           true,
			WarmupDailyCap:          5,
			WarmupTargetDailyCap:    250,
			WarmupIncrementPerDay:   5,
			PerMinuteLimit:          2,
			MinSecondsBetweenSends: 30,
		}, nil
	case PlanTierPro:
		return planSpec{
			DailyEmailCap:           500,
			AICreditsPerDay:         10000,
			WarmupEnabled:           true,
			WarmupDailyCap:          5,
			WarmupTargetDailyCap:    500,
			WarmupIncrementPerDay:   5,
			PerMinuteLimit:          2,
			MinSecondsBetweenSends: 30,
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

// ApplyPlanLimitsToUser sets plan_tier + synchronizes smtp_accounts sending limits and warmup behavior.
// This is the source of truth for plan-driven throughput.
func ApplyPlanLimitsToUser(userID int64, tier PlanTier) error {
	spec, err := PlanSpecForTier(tier)
	if err != nil {
		return err
	}

	// Ensure smtp account exists so the worker can immediately use updated limits.
	if _, err := GetSMTPAccountByUserID(userID); err != nil {
		if err := CreateDefaultSMTPAccountForUser(userID); err != nil {
			return err
		}
	}

	now := time.Now()
	today := now.Format("2006-01-02")

	var warmupStartedAt any = nil
	if spec.WarmupEnabled {
		// Restart warmup ramp when applying plan.
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

	// Reset daily pacing counters so the new plan limits take effect immediately for the current day.
	if _, err := tx.Exec(`
		UPDATE smtp_accounts SET
			daily_limit=?,
			per_minute_limit=?,
			min_seconds_between_sends=?,
			warmup_enabled=?,
			warmup_daily_cap=?,
			warmup_target_daily_cap=?,
			warmup_increment_per_day=?,
			warmup_started_at=?,
			sends_today=0,
			sends_today_reset_at=?,
			last_send_at=NULL,
			updated_at=?
		WHERE user_id=?
	`, spec.DailyEmailCap, spec.PerMinuteLimit, spec.MinSecondsBetweenSends,
		warmupEnabledInt, spec.WarmupDailyCap, spec.WarmupTargetDailyCap, spec.WarmupIncrementPerDay,
		warmupStartedAt, today, now, userID); err != nil {
		return err
	}

	return tx.Commit()
}

// MigrateExistingActiveUsersToStandard.
// Older versions of this app only had one paid plan; we treat those active subscribers as Standard.
func MigrateExistingActiveUsersToStandard() {
	rows, err := db.Query(`SELECT id FROM users WHERE subscription_status = ?`, SubStatusActive)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		// Best-effort: sync limits to Standard tier.
		_ = ApplyPlanLimitsToUser(id, PlanTierStandard)
	}
}

// MigrateAllUsersToPlanLimits applies plan-driven sending caps/warmup based on users.plan_tier.
// This ensures pre-existing users (previously using user-configurable limits) get standardized plan limits.
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
		_ = ApplyPlanLimitsToUser(id, PlanTier(tierStr))
	}
}

