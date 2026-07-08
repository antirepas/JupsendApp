package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

// ConsumeAICredit increments today's AI credit usage for a user (if available).
// Returns: cap, remainingAfterConsume, ok.
func ConsumeAICredit(userID int64) (cap int, remaining int, ok bool) {
	now := time.Now()
	today := now.Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = tx.Rollback() }()

	var tierStr string
	var used int
	var resetAt sql.NullTime
	row := tx.QueryRow(`
		SELECT plan_tier, COALESCE(ai_credits_used_today, 0), ai_credits_reset_at
		FROM users
		WHERE id = ?
		FOR UPDATE
	`, userID)
	if err := row.Scan(&tierStr, &used, &resetAt); err != nil {
		return 0, 0, false
	}

	tier := PlanTier(tierStr)
	cap = AICreditsCapForTier(tier)

	resetNeeded := !resetAt.Valid || resetAt.Time.Format("2006-01-02") != today
	if resetNeeded {
		used = 0
	}

	if used >= cap {
		remaining = 0
		return cap, remaining, false
	}

	used++
	remaining = cap - used

	if _, err := tx.Exec(`
		UPDATE users
		SET ai_credits_used_today = ?, ai_credits_reset_at = ?
		WHERE id = ?
	`, used, today, userID); err != nil {
		return 0, 0, false
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, false
	}
	return cap, remaining, true
}

