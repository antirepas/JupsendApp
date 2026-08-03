package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"emailtracker.com/db"
)

// ContactTimelineItem is one engagement or delivery event on a contact's history.
type ContactTimelineItem struct {
	At           time.Time
	Type         string // open, click, reply, send, bounce, workflow_started, …
	Label        string
	Detail       string
	CampaignID   int64
	CampaignName string
	TemplateName string
	Subject      string
	URL          string
	SendID       int64
	Source       string // contact_events | email_events
}

func normalizeTimelineType(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	switch t {
	case "open", "opened":
		return "open"
	case "click", "clicked":
		return "click"
	case "reply", "replied":
		return "reply"
	case "send", "sent", "email_sent":
		return "send"
	case "bounce", "bounced":
		return "bounce"
	case "workflow_started":
		return "workflow_started"
	case "workflow_completed":
		return "workflow_completed"
	default:
		return t
	}
}

func timelineLabel(t string) string {
	switch t {
	case "open":
		return "Opened"
	case "click":
		return "Clicked"
	case "reply":
		return "Replied"
	case "send":
		return "Email sent"
	case "bounce":
		return "Bounced"
	case "workflow_started":
		return "Workflow started"
	case "workflow_completed":
		return "Workflow completed"
	default:
		if t == "" {
			return "Event"
		}
		return strings.ReplaceAll(t, "_", " ")
	}
}

func timelineBadgeClass(t string) string {
	switch t {
	case "open":
		return "badge-blue"
	case "click":
		return "badge-amber"
	case "reply":
		return "badge-violet"
	case "send":
		return "badge-gray"
	case "bounce":
		return "badge-red"
	case "workflow_started", "workflow_completed":
		return "badge-gray"
	default:
		return "badge-gray"
	}
}

// BadgeClass is used from templates.
func (i ContactTimelineItem) BadgeClass() string {
	return timelineBadgeClass(i.Type)
}

func metaString(metaJSON, key string) string {
	if strings.TrimSpace(metaJSON) == "" || metaJSON == "{}" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func buildTimelineDetail(campaignName, templateName, subject, url string) string {
	parts := make([]string, 0, 3)
	if campaignName != "" {
		parts = append(parts, campaignName)
	}
	if subject != "" {
		parts = append(parts, subject)
	} else if templateName != "" {
		parts = append(parts, templateName)
	}
	if url != "" {
		parts = append(parts, url)
	}
	return strings.Join(parts, " · ")
}

// ListContactTimeline returns opens, clicks, replies, sends, and related events for a contact.
func ListContactTimeline(userID, contactID int64, limit int) ([]ContactTimelineItem, error) {
	if _, _, err := GetContactForUser(contactID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 300 {
		limit = 300
	}

	items := make([]ContactTimelineItem, 0, limit)
	seen := map[string]bool{}

	add := func(it ContactTimelineItem) {
		it.Type = normalizeTimelineType(it.Type)
		if it.Type == "" {
			return
		}
		it.Label = timelineLabel(it.Type)
		if it.Detail == "" {
			it.Detail = buildTimelineDetail(it.CampaignName, it.TemplateName, it.Subject, it.URL)
		}
		// Prefer richer contact_events; skip email_events duplicates for same send+type+minute.
		key := fmt.Sprintf("%s|%d|%d", it.Type, it.SendID, it.At.Unix()/60)
		if it.SendID == 0 {
			key = fmt.Sprintf("%s|%s|%d", it.Type, it.Detail, it.At.Unix()/60)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, it)
	}

	rows, err := db.Query(`
		SELECT
			ce.event_type,
			ce.metadata_json,
			ce.occurred_at,
			COALESCE(ce.email_send_id, 0),
			COALESCE(ce.campaign_id, es.campaign_id, 0),
			COALESCE(NULLIF(camp.name, ''), NULLIF(camp2.name, ''), ''),
			COALESCE(t.name, ''),
			COALESCE(t.subject, '')
		FROM contact_events ce
		LEFT JOIN email_sends es ON es.id = ce.email_send_id
		LEFT JOIN campaigns camp ON camp.id = ce.campaign_id
		LEFT JOIN campaigns camp2 ON camp2.id = es.campaign_id
		LEFT JOIN template t ON t.id = es.template_id
		WHERE ce.contact_id = ?
		ORDER BY ce.occurred_at DESC
		LIMIT ?
	`, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ContactTimelineItem
		var meta string
		if err := rows.Scan(
			&it.Type, &meta, &it.At, &it.SendID, &it.CampaignID,
			&it.CampaignName, &it.TemplateName, &it.Subject,
		); err != nil {
			return nil, err
		}
		it.URL = metaString(meta, "clicked_url")
		if it.URL == "" {
			it.URL = metaString(meta, "url")
		}
		it.Source = "contact_events"
		add(it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Backfill opens/clicks/bounces from email_events (pixel/link tracking) when dual-write is missing.
	eeRows, err := db.Query(`
		SELECT
			ee.event_type,
			ee.created_at,
			COALESCE(es.id, 0),
			COALESCE(es.campaign_id, 0),
			COALESCE(camp.name, ''),
			COALESCE(t.name, ''),
			COALESCE(t.subject, ''),
			COALESCE(tl.original_url, '')
		FROM email_events ee
		INNER JOIN email_sends es ON (
			es.id = ee.email_send_id
			OR (ee.tracking_id IS NOT NULL AND ee.tracking_id <> '' AND es.tracking_id = ee.tracking_id)
		)
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN tracked_links tl ON tl.tracking_id = ee.tracking_id
		WHERE es.contact_id = ? AND es.user_id = ?
			AND ee.event_type IN ('open', 'click', 'bounce', 'reply')
		ORDER BY ee.created_at DESC
		LIMIT ?
	`, contactID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer eeRows.Close()
	for eeRows.Next() {
		var it ContactTimelineItem
		if err := eeRows.Scan(
			&it.Type, &it.At, &it.SendID, &it.CampaignID,
			&it.CampaignName, &it.TemplateName, &it.Subject, &it.URL,
		); err != nil {
			return nil, err
		}
		it.Source = "email_events"
		add(it)
	}
	if err := eeRows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].At.After(items[j].At)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
