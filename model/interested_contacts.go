package model

import (
	"sort"
	"time"

	"emailtracker.com/db"
)

type InterestedContact struct {
	ContactID    int64
	Email        string
	Score        int
	Tier         string
	LastSignal   string
	LastActivity time.Time
	CampaignName string
	CampaignID   int64
}

func ListInterestedContacts(userID int64, limit int) ([]InterestedContact, error) {
	if limit < 1 {
		limit = 100
	}

	type agg struct {
		email        string
		score        int
		lastSignal   string
		lastActivity time.Time
		campaignName string
		campaignID   int64
		openCount    int
		hasClick     bool
		hasReply     bool
	}

	byContact := map[int64]*agg{}

	rows, err := db.Query(`
		SELECT es.contact_id, c.email, es.campaign_id, COALESCE(camp.name, ''),
			ee.event_type, ee.created_at
		FROM email_sends es
		INNER JOIN contact c ON c.id = es.contact_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		LEFT JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
			AND ee.event_type IN ('open', 'click')
		WHERE es.user_id = ? AND es.sent_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var contactID, campaignID int64
		var email, campaignName, eventType string
		var createdAt *time.Time
		if err := rows.Scan(&contactID, &email, &campaignID, &campaignName, &eventType, &createdAt); err != nil {
			continue
		}
		a := byContact[contactID]
		if a == nil {
			a = &agg{email: email, campaignName: campaignName, campaignID: campaignID}
			byContact[contactID] = a
		}
		if createdAt != nil && createdAt.After(a.lastActivity) {
			a.lastActivity = *createdAt
			a.lastSignal = eventType
			if campaignName != "" {
				a.campaignName = campaignName
				a.campaignID = campaignID
			}
		}
		switch eventType {
		case "open":
			a.openCount++
		case "click":
			a.hasClick = true
		}
	}

	replyRows, err := db.Query(`
		SELECT es.contact_id, c.email, es.campaign_id, COALESCE(camp.name, ''), ce.created_at
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		INNER JOIN contact c ON c.id = es.contact_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		WHERE es.user_id = ? AND ce.event_type = 'REPLY'
			AND ce.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')
	`, userID)
	if err == nil {
		defer replyRows.Close()
		for replyRows.Next() {
			var contactID, campaignID int64
			var email, campaignName string
			var createdAt time.Time
			if replyRows.Scan(&contactID, &email, &campaignID, &campaignName, &createdAt) != nil {
				continue
			}
			a := byContact[contactID]
			if a == nil {
				a = &agg{email: email}
				byContact[contactID] = a
			}
			a.hasReply = true
			if createdAt.After(a.lastActivity) {
				a.lastActivity = createdAt
				a.lastSignal = "replied"
				a.campaignName = campaignName
				a.campaignID = campaignID
			}
		}
	}

	var list []InterestedContact
	dismissed, _ := dismissedInterestedSet(userID)
	for cid, a := range byContact {
		if dismissed[cid] {
			continue
		}
		score := 0
		tier := "cold"
		switch {
		case a.hasReply:
			score = 100
			tier = "hot"
		case a.hasClick:
			score = 40
			tier = "warm"
		case a.openCount >= 2:
			score = 25
			tier = "warm"
		case a.openCount >= 1:
			score = 10
			tier = "warm"
		default:
			continue
		}
		list = append(list, InterestedContact{
			ContactID:    cid,
			Email:        a.email,
			Score:        score,
			Tier:         tier,
			LastSignal:   a.lastSignal,
			LastActivity: a.lastActivity,
			CampaignName: a.campaignName,
			CampaignID:   a.campaignID,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}
		return list[i].LastActivity.After(list[j].LastActivity)
	})

	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func CountInterestedContacts(userID int64) int {
	list, err := ListInterestedContacts(userID, 10000)
	if err != nil {
		return 0
	}
	return len(list)
}
