package model

import (
	"strings"
	"time"

	"emailtracker.com/db"
)

// CampaignDeliverability surfaces bounce + opt-out health for a campaign.
type CampaignDeliverability struct {
	BounceSends      int // unique email_sends with a bounce event
	BounceContacts   int // unique contacts who bounced
	BounceRate       float64
	Health           string // healthy | watch | poor
	SuppressedTotal  int
	SuppressBounce   int
	SuppressUnsub    int
	SuppressNegative int
	SuppressManual   int
	SuppressOther    int
	SuppressRate     float64 // of enrolled contacts
	Contacts         []CampaignSuppressedContact
}

type CampaignSuppressedContact struct {
	ContactID     int64
	Email         string
	Reason        string
	SourceMessage string
	CreatedAt     time.Time
}

// ReasonLabel is a human-readable suppression reason.
func (c CampaignSuppressedContact) ReasonLabel() string {
	switch strings.ToLower(strings.TrimSpace(c.Reason)) {
	case "bounce":
		return "Bounce"
	case "unsubscribe":
		return "Opt-out"
	case "negative_reply":
		return "Negative reply"
	case "manual":
		return "Manual"
	case "complaint":
		return "Complaint"
	default:
		if c.Reason == "" {
			return "Suppressed"
		}
		return c.Reason
	}
}

// HealthLabel is a short human label for the bounce-health band.
func (d CampaignDeliverability) HealthLabel() string {
	switch d.Health {
	case "poor":
		return "Poor"
	case "watch":
		return "Needs attention"
	default:
		return "Healthy"
	}
}

func loadCampaignDeliverability(campaignID int64, enrolled, sentCount int) CampaignDeliverability {
	d := CampaignDeliverability{Health: "healthy"}
	if campaignID <= 0 {
		return d
	}
	if sentCount <= 0 {
		_ = db.QueryRow(`
			SELECT COUNT(*) FROM email_sends
			WHERE campaign_id = ?
			  AND LOWER(COALESCE(delivery_status, '')) = 'sent'
		`, campaignID).Scan(&sentCount)
	}

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.id), COUNT(DISTINCT es.contact_id)
		FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND ee.event_type = 'bounce'
		WHERE es.campaign_id = ?
		  AND LOWER(COALESCE(es.delivery_status, '')) = 'sent'
	`, campaignID).Scan(&d.BounceSends, &d.BounceContacts)

	if sentCount > 0 {
		d.BounceRate = float64(d.BounceSends) / float64(sentCount) * 100
	}
	switch {
	case d.BounceRate >= 5:
		d.Health = "poor"
	case d.BounceRate >= 2:
		d.Health = "watch"
	default:
		d.Health = "healthy"
	}

	rows, err := db.Query(`
		SELECT LOWER(COALESCE(cs.reason, '')), COUNT(*)
		FROM contact_suppressions cs
		INNER JOIN campaign_contacts cc ON cc.contact_id = cs.contact_id
		WHERE cc.campaign_id = ?
		GROUP BY LOWER(COALESCE(cs.reason, ''))
	`, campaignID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var reason string
			var n int
			if rows.Scan(&reason, &n) != nil {
				continue
			}
			d.SuppressedTotal += n
			switch reason {
			case "bounce":
				d.SuppressBounce = n
			case "unsubscribe":
				d.SuppressUnsub = n
			case "negative_reply":
				d.SuppressNegative = n
			case "manual", "complaint":
				d.SuppressManual += n
			default:
				d.SuppressOther += n
			}
		}
	}
	if enrolled > 0 {
		d.SuppressRate = float64(d.SuppressedTotal) / float64(enrolled) * 100
	}

	listRows, err := db.Query(`
		SELECT c.id, COALESCE(c.email, ''), COALESCE(cs.reason, ''), COALESCE(cs.source_message, ''), cs.created_at
		FROM contact_suppressions cs
		INNER JOIN campaign_contacts cc ON cc.contact_id = cs.contact_id
		INNER JOIN contact c ON c.id = cs.contact_id
		WHERE cc.campaign_id = ?
		ORDER BY cs.created_at DESC
		LIMIT 100
	`, campaignID)
	if err == nil {
		defer listRows.Close()
		for listRows.Next() {
			var row CampaignSuppressedContact
			if listRows.Scan(&row.ContactID, &row.Email, &row.Reason, &row.SourceMessage, &row.CreatedAt) != nil {
				continue
			}
			d.Contacts = append(d.Contacts, row)
		}
	}
	return d
}

// getCampaignDailyBounceStats returns bounce counts keyed by day (YYYY-MM-DD).
func getCampaignDailyBounceStats(campaignID int64) map[string]int {
	out := map[string]int{}
	rows, err := db.Query(`
		SELECT (ee.created_at)::date, COUNT(DISTINCT es.id)
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id OR ee.tracking_id = es.tracking_id
		WHERE es.campaign_id = ? AND ee.event_type = 'bounce'
		GROUP BY (ee.created_at)::date
	`, campaignID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		var n int
		if rows.Scan(&day, &n) == nil {
			out[day] = n
		}
	}
	return out
}
