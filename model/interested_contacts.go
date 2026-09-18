package model

import (
	"encoding/json"
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
	return ListInterestedContactsFiltered(userID, 0, limit)
}

func ListInterestedContactsFiltered(userID, campaignID int64, limit int) ([]InterestedContact, error) {
	if limit < 1 {
		limit = 100
	}

	type agg struct {
		email        string
		lastSignal   string
		lastActivity time.Time
		campaignName string
		campaignID   int64
		positive     bool
		neutral      bool
	}

	byContact := map[int64]*agg{}

	campSQL := ""
	args := []interface{}{userID}
	if campaignID > 0 {
		campSQL = " AND es.campaign_id = ?"
		args = append(args, campaignID)
	}

	replyRows, err := db.Query(`
		SELECT es.contact_id, c.email, es.campaign_id, COALESCE(camp.name, ''), ce.created_at,
			COALESCE(cm.reply_sentiment, ''), COALESCE(ce.metadata_json, '{}')
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		INNER JOIN contact c ON c.id = es.contact_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		LEFT JOIN conversation_messages cm
			ON cm.email_send_id = ce.email_send_id AND cm.contact_id = ce.contact_id AND cm.direction = 'inbound'
		WHERE es.user_id = ? AND ce.event_type = 'REPLY'
			AND ce.created_at >= CURRENT_TIMESTAMP - (90 * INTERVAL '1 day')`+campSQL+`
	`, args...)
	if err != nil {
		return nil, err
	}
	defer replyRows.Close()

	for replyRows.Next() {
		var contactID, campID int64
		var email, campaignName, sentRaw, metaRaw string
		var created time.Time
		if replyRows.Scan(&contactID, &email, &campID, &campaignName, &created, &sentRaw, &metaRaw) != nil {
			continue
		}
		s := NormalizeReplySentiment(sentRaw)
		if s == ReplySentimentPending || s == "" {
			meta := map[string]interface{}{}
			_ = json.Unmarshal([]byte(metaRaw), &meta)
			if v, ok := meta["sentiment"].(string); ok {
				s = NormalizeReplySentiment(v)
			}
		}
		if s == ReplySentimentNegative {
			continue
		}
		a := byContact[contactID]
		if a == nil {
			a = &agg{email: email, campaignName: campaignName, campaignID: campID}
			byContact[contactID] = a
		}
		if created.After(a.lastActivity) {
			a.lastActivity = created
			a.campaignName = campaignName
			a.campaignID = campID
			if s == ReplySentimentPositive {
				a.lastSignal = "positive_reply"
			} else {
				a.lastSignal = "reply"
			}
		}
		if s == ReplySentimentPositive {
			a.positive = true
		} else {
			a.neutral = true
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
		case a.positive:
			score = 100
			tier = "hot"
		case a.neutral:
			score = 50
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
