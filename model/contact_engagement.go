package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

// ContactEngagement is last-touch campaign + signal for a contact.
type ContactEngagement struct {
	LastCampaignID   int64
	LastCampaignName string
	LastSignal       string // open, click, reply, none
	LastActivity     *time.Time
}

// ContactCampaignRef is a campaign linked to a contact (membership or sends).
type ContactCampaignRef struct {
	ID   int64
	Name string
}

// EnrichContactsEngagement loads last campaign + last engagement signal for many contacts.
func EnrichContactsEngagement(userID int64, contactIDs []int64) (map[int64]ContactEngagement, error) {
	out := make(map[int64]ContactEngagement, len(contactIDs))
	if userID <= 0 || len(contactIDs) == 0 {
		return out, nil
	}
	for _, id := range contactIDs {
		out[id] = ContactEngagement{LastSignal: "none"}
	}

	placeholders, args := int64Placeholders(contactIDs, userID)
	rows, err := db.Query(`
		SELECT es.contact_id, COALESCE(es.campaign_id, 0), COALESCE(camp.name, ''), es.sent_at
		FROM email_sends es
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		WHERE es.user_id = ? AND es.contact_id IN (`+placeholders+`) AND es.sent_at IS NOT NULL
		ORDER BY es.sent_at DESC
	`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	seenSend := map[int64]bool{}
	for rows.Next() {
		var cid, campID int64
		var campName string
		var sentAt time.Time
		if rows.Scan(&cid, &campID, &campName, &sentAt) != nil {
			continue
		}
		if seenSend[cid] {
			continue
		}
		seenSend[cid] = true
		e := out[cid]
		e.LastCampaignID = campID
		e.LastCampaignName = campName
		if e.LastActivity == nil || sentAt.After(*e.LastActivity) {
			t := sentAt
			e.LastActivity = &t
		}
		out[cid] = e
	}

	eventRows, err := db.Query(`
		SELECT es.contact_id, ee.event_type, ee.created_at,
			COALESCE(es.campaign_id, 0), COALESCE(camp.name, '')
		FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND (ee.event_type = 'click' OR (ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0))
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		WHERE es.user_id = ? AND es.contact_id IN (`+placeholders+`)
	`, args...)
	if err == nil {
		defer eventRows.Close()
		for eventRows.Next() {
			var cid, campID int64
			var eventType, campName string
			var createdAt time.Time
			if eventRows.Scan(&cid, &eventType, &createdAt, &campID, &campName) != nil {
				continue
			}
			e := out[cid]
			if e.LastActivity == nil || createdAt.After(*e.LastActivity) {
				t := createdAt
				e.LastActivity = &t
				e.LastSignal = eventType
				if campID > 0 {
					e.LastCampaignID = campID
					e.LastCampaignName = campName
				}
			}
			out[cid] = e
		}
	}

	replyRows, err := db.Query(`
		SELECT es.contact_id, ce.created_at, COALESCE(es.campaign_id, 0), COALESCE(camp.name, '')
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		WHERE es.user_id = ? AND es.contact_id IN (`+placeholders+`) AND ce.event_type = 'REPLY'
	`, args...)
	if err == nil {
		defer replyRows.Close()
		for replyRows.Next() {
			var cid, campID int64
			var campName string
			var createdAt time.Time
			if replyRows.Scan(&cid, &createdAt, &campID, &campName) != nil {
				continue
			}
			e := out[cid]
			if e.LastActivity == nil || createdAt.After(*e.LastActivity) {
				t := createdAt
				e.LastActivity = &t
				e.LastSignal = "reply"
				if campID > 0 {
					e.LastCampaignID = campID
					e.LastCampaignName = campName
				}
			}
			out[cid] = e
		}
	}

	return out, nil
}

func int64Placeholders(ids []int64, userID int64) (string, []interface{}) {
	parts := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, userID)
	for i, id := range ids {
		parts[i] = "?"
		args = append(args, id)
	}
	return strings.Join(parts, ","), args
}

func engagementFilterSQL(engagement string) (clause string, extraArgs []interface{}) {
	switch strings.TrimSpace(engagement) {
	case "replied":
		return "c.replied_at IS NOT NULL", nil
	case "opened_no_reply":
		return `
			c.replied_at IS NULL
			AND EXISTS (
				SELECT 1 FROM email_sends es
				INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
				WHERE es.contact_id = c.id AND es.user_id = c.user_id AND ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0
					AND ee.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
			)`, nil
	case "clicked_no_reply":
		return `
			c.replied_at IS NULL
			AND EXISTS (
				SELECT 1 FROM email_sends es
				INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
				WHERE es.contact_id = c.id AND es.user_id = c.user_id AND ee.event_type = 'click'
					AND ee.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
			)`, nil
	case "interested":
		return `
			EXISTS (
				SELECT 1 FROM email_sends es
				INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
				WHERE es.contact_id = c.id AND es.user_id = c.user_id
					AND (ee.event_type = 'click' OR (ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0))
					AND ee.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
			)
			OR EXISTS (
				SELECT 1 FROM contact_events ce
				INNER JOIN email_sends es ON es.id = ce.email_send_id
				WHERE es.contact_id = c.id AND es.user_id = c.user_id AND ce.event_type = 'REPLY'
					AND ce.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
			)`, nil
	default:
		return "", nil
	}
}

func campaignMembershipFilterSQL() string {
	return `(
		c.id IN (SELECT contact_id FROM campaign_contacts WHERE campaign_id = ?)
		OR c.id IN (SELECT contact_id FROM email_sends WHERE campaign_id = ? AND user_id = c.user_id)
	)`
}

// FormatLastSignalLabel returns a short display label.
func FormatLastSignalLabel(signal string) string {
	switch signal {
	case "open":
		return "opened"
	case "click":
		return "clicked"
	case "reply", "replied":
		return "replied"
	case "none", "":
		return "—"
	default:
		return signal
	}
}

// DismissInterestedContacts hides contacts from the interested queue for a user.
func DismissInterestedContacts(userID int64, contactIDs []int64) (int, error) {
	n := 0
	for _, cid := range contactIDs {
		if _, _, err := GetContactForUser(cid, userID); err != nil {
			continue
		}
		_, err := db.Exec(`
			INSERT INTO contact_interested_dismissed (user_id, contact_id)
			VALUES (?, ?) ON CONFLICT DO NOTHING
		`, userID, cid)
		if err == nil {
			n++
		}
	}
	return n, nil
}

func dismissedInterestedSet(userID int64) (map[int64]bool, error) {
	rows, err := db.Query(`SELECT contact_id FROM contact_interested_dismissed WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out, nil
}

// BulkSuppressContacts suppresses many contacts for a user.
func BulkSuppressContacts(userID int64, contactIDs []int64, reason string) (int, error) {
	if reason == "" {
		reason = "manual"
	}
	n := 0
	for _, cid := range contactIDs {
		if _, _, err := GetContactForUser(cid, userID); err != nil {
			continue
		}
		if err := SuppressContact(cid, reason, "interested_bulk", 0); err == nil {
			n++
		}
	}
	return n, nil
}

// ValidateEngagementPreset returns an error if engagement is unknown.
func ValidateEngagementPreset(s string) error {
	switch strings.TrimSpace(s) {
	case "", "replied", "opened_no_reply", "clicked_no_reply", "interested":
		return nil
	default:
		return fmt.Errorf("unknown engagement filter %q", s)
	}
}
