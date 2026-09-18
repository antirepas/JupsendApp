package model

import (
	"strconv"

	"emailtracker.com/db"
)

// MailboxSendRates aggregates engagement for one SMTP mailbox over a rolling window.
type MailboxSendRates struct {
	PeriodDays  int
	TotalSends  int
	OpenRate    float64
	ClickRate   float64
	ReplyRate   float64
	BounceRate  float64
	HealthLabel string // Healthy, Watch, Poor, Insufficient data
	HealthNote  string
}

// GetMailboxSendRates computes open/click/reply/bounce rates for sent mail from smtpAccountID.
// Deliverability probe sends (variant deliverability_probe) are excluded.
func GetMailboxSendRates(userID, smtpAccountID int64, periodDays int) MailboxSendRates {
	if periodDays < 1 {
		periodDays = 30
	}
	r := MailboxSendRates{PeriodDays: periodDays}
	if userID <= 0 || smtpAccountID <= 0 {
		r.HealthLabel = "Insufficient data"
		r.HealthNote = "No mailbox linked for metrics."
		return r
	}

	var uniqueOpens, uniqueClicks, uniqueReplies, uniqueBounces int

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM email_sends
		WHERE user_id = ? AND smtp_account_id = ?
			AND delivery_status = 'sent'
			AND COALESCE(variant, '') <> ?
			AND sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, smtpAccountID, DeliverabilityProbeVariant, periodDays).Scan(&r.TotalSends)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0
		WHERE es.user_id = ? AND es.smtp_account_id = ?
			AND es.delivery_status = 'sent'
			AND COALESCE(es.variant, '') <> ?
			AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, smtpAccountID, DeliverabilityProbeVariant, periodDays).Scan(&uniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND ee.event_type = 'click'
		WHERE es.user_id = ? AND es.smtp_account_id = ?
			AND es.delivery_status = 'sent'
			AND COALESCE(es.variant, '') <> ?
			AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, smtpAccountID, DeliverabilityProbeVariant, periodDays).Scan(&uniqueClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.user_id = ? AND es.smtp_account_id = ?
			AND es.delivery_status = 'sent'
			AND COALESCE(es.variant, '') <> ?
			AND ce.event_type = 'REPLY'
			AND ce.created_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, smtpAccountID, DeliverabilityProbeVariant, periodDays).Scan(&uniqueReplies)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND ee.event_type = 'bounce'
		WHERE es.user_id = ? AND es.smtp_account_id = ?
			AND es.delivery_status = 'sent'
			AND COALESCE(es.variant, '') <> ?
			AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, smtpAccountID, DeliverabilityProbeVariant, periodDays).Scan(&uniqueBounces)

	if r.TotalSends > 0 {
		r.OpenRate = float64(uniqueOpens) / float64(r.TotalSends) * 100
		r.ClickRate = float64(uniqueClicks) / float64(r.TotalSends) * 100
		r.ReplyRate = float64(uniqueReplies) / float64(r.TotalSends) * 100
		r.BounceRate = float64(uniqueBounces) / float64(r.TotalSends) * 100
	}

	r.HealthLabel, r.HealthNote = mailboxHealthFromRates(r)
	return r
}

func mailboxHealthFromRates(r MailboxSendRates) (label, note string) {
	const minSample = 20
	if r.TotalSends < minSample {
		return "Insufficient data", "Need at least 20 sends in the last " + strconv.Itoa(r.PeriodDays) + " days for a reliable read."
	}
	if r.BounceRate >= 5 {
		return "Poor", "Bounce rate is high — clean lists and pause volume until it drops."
	}
	if r.BounceRate >= 2 || (r.ReplyRate < 1 && r.TotalSends >= minSample) {
		return "Watch", "Bounces or reply rates need attention before scaling volume."
	}
	return "Healthy", "Bounce and reply rates look solid for this mailbox."
}
