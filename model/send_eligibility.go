package model

import (
	"fmt"
	"time"

	"emailtracker.com/db"
)

const (
	SkipReasonSuppressed     = "suppressed"
	SkipReasonCooldown       = "cooldown"
	SkipReasonActiveCampaign = "active_campaign"
	SkipReasonInvalidEmail   = "invalid_email"
)

type SkipReason struct {
	ContactID int64
	Reason    string
}

func GetUserSendCooldownDays(userID int64) (int, error) {
	var days int
	err := db.QueryRow(`SELECT COALESCE(send_cooldown_days, 30) FROM users WHERE id = ?`, userID).Scan(&days)
	if err != nil {
		return 30, err
	}
	if days < 0 {
		days = 0
	}
	return days, nil
}

func UpdateUserSendCooldownDays(userID int64, days int) error {
	if days < 0 {
		days = 0
	}
	_, err := db.Exec(`UPDATE users SET send_cooldown_days = ? WHERE id = ?`, days, userID)
	return err
}

func FilterSendEligible(userID, campaignID int64, contactIDs []int64) ([]int64, []SkipReason, error) {
	if len(contactIDs) == 0 {
		return nil, nil, nil
	}

	cooldownDays, _ := GetUserSendCooldownDays(userID)

	suppressedSet := make(map[int64]bool)
	rows, err := db.Query(`
		SELECT cs.contact_id FROM contact_suppressions cs
		INNER JOIN contact c ON c.id = cs.contact_id
		WHERE c.user_id = ?
	`, userID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		suppressedSet[id] = true
	}
	rows.Close()

	cooldownSet := make(map[int64]bool)
	if cooldownDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cooldownDays)
		crows, err := db.Query(`
			SELECT contact_id FROM email_sends
			WHERE user_id = ? AND contact_id IS NOT NULL
				AND delivery_status = 'sent'
				AND sent_at IS NOT NULL AND sent_at > ?
			GROUP BY contact_id
		`, userID, cutoff)
		if err != nil {
			return nil, nil, err
		}
		for crows.Next() {
			var id int64
			if err := crows.Scan(&id); err != nil {
				crows.Close()
				return nil, nil, err
			}
			cooldownSet[id] = true
		}
		crows.Close()
	}

	activeSet := make(map[int64]bool)
	arows, err := db.Query(`
		SELECT cc.contact_id FROM campaign_contacts cc
		INNER JOIN campaigns c ON c.id = cc.campaign_id
		WHERE c.user_id = ? AND cc.campaign_id != ?
			AND (c.status = 'draft' OR COALESCE(c.is_sending, 0) = 1)
	`, userID, campaignID)
	if err != nil {
		return nil, nil, err
	}
	for arows.Next() {
		var id int64
		if err := arows.Scan(&id); err != nil {
			arows.Close()
			return nil, nil, err
		}
		activeSet[id] = true
	}
	arows.Close()

	invalidSet := make(map[int64]bool)
	irows, err := db.Query(`
		SELECT id FROM contact WHERE user_id = ? AND COALESCE(email_status, 'unknown') = 'invalid'
	`, userID)
	if err != nil {
		return nil, nil, err
	}
	for irows.Next() {
		var id int64
		if err := irows.Scan(&id); err != nil {
			irows.Close()
			return nil, nil, err
		}
		invalidSet[id] = true
	}
	irows.Close()

	var eligible []int64
	var skipped []SkipReason
	for _, id := range contactIDs {
		switch {
		case suppressedSet[id]:
			skipped = append(skipped, SkipReason{ContactID: id, Reason: SkipReasonSuppressed})
		case invalidSet[id]:
			skipped = append(skipped, SkipReason{ContactID: id, Reason: SkipReasonInvalidEmail})
		case cooldownSet[id]:
			skipped = append(skipped, SkipReason{ContactID: id, Reason: SkipReasonCooldown})
		case activeSet[id]:
			skipped = append(skipped, SkipReason{ContactID: id, Reason: SkipReasonActiveCampaign})
		default:
			eligible = append(eligible, id)
		}
	}
	return eligible, skipped, nil
}

func CountSkipReasons(skipped []SkipReason) map[string]int {
	counts := make(map[string]int)
	for _, s := range skipped {
		counts[s.Reason]++
	}
	return counts
}

func FormatSkipBreakdown(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	parts := []string{}
	if n := counts[SkipReasonSuppressed]; n > 0 {
		parts = append(parts, formatSkipN(n, "suppressed"))
	}
	if n := counts[SkipReasonCooldown]; n > 0 {
		parts = append(parts, formatSkipN(n, "cooldown"))
	}
	if n := counts[SkipReasonActiveCampaign]; n > 0 {
		parts = append(parts, formatSkipN(n, "active campaign"))
	}
	if n := counts[SkipReasonInvalidEmail]; n > 0 {
		parts = append(parts, formatSkipN(n, "invalid email"))
	}
	if n := counts["gmail_not_ready"]; n > 0 {
		parts = append(parts, formatSkipN(n, "Gmail not connected"))
	}
	if n := counts["enqueue_error"]; n > 0 {
		parts = append(parts, formatSkipN(n, "enqueue error"))
	}
	if len(parts) == 0 {
		return ""
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

func formatSkipN(n int, label string) string {
	return fmt.Sprintf("%d %s", n, label)
}
