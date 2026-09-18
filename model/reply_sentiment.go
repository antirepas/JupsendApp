package model

import (
	"encoding/json"
	"strings"
	"time"

	"emailtracker.com/db"
)

const (
	ReplySentimentPositive = "positive"
	ReplySentimentNegative = "negative"
	ReplySentimentNeutral  = "neutral"
	ReplySentimentPending  = "pending"
)

func NormalizeReplySentiment(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ReplySentimentPositive, "pos", "interested", "yes":
		return ReplySentimentPositive
	case ReplySentimentNegative, "neg", "not_interested", "unsubscribe", "no":
		return ReplySentimentNegative
	case ReplySentimentNeutral:
		return ReplySentimentNeutral
	case ReplySentimentPending, "":
		return ReplySentimentPending
	default:
		return ReplySentimentPending
	}
}

func IsNonNegativeReplySentiment(s string) bool {
	switch NormalizeReplySentiment(s) {
	case ReplySentimentPositive, ReplySentimentNeutral, ReplySentimentPending:
		return true
	default:
		return false
	}
}

// SetConversationReplySentiment updates an inbound message sentiment and contact rollup.
func SetConversationReplySentiment(messageID int64, sentiment, source string) error {
	sentiment = NormalizeReplySentiment(sentiment)
	if sentiment == ReplySentimentPending {
		sentiment = ReplySentimentPending
	}
	var userID, contactID, emailSendID int64
	err := db.QueryRow(`
		SELECT user_id, contact_id, COALESCE(email_send_id, 0)
		FROM conversation_messages WHERE id = ? AND direction = 'inbound'
	`, messageID).Scan(&userID, &contactID, &emailSendID)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE conversation_messages SET reply_sentiment = ? WHERE id = ?`, sentiment, messageID)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE contact SET last_reply_sentiment = ? WHERE id = ?`, sentiment, contactID)
	if emailSendID > 0 {
		_ = patchReplyEventSentiment(emailSendID, contactID, sentiment, source)
	}
	return nil
}

func patchReplyEventSentiment(emailSendID, contactID int64, sentiment, source string) error {
	var metaRaw string
	var eventID int64
	err := db.QueryRow(`
		SELECT id, COALESCE(metadata_json, '{}') FROM contact_events
		WHERE contact_id = ? AND email_send_id = ? AND event_type = 'REPLY'
		ORDER BY id DESC LIMIT 1
	`, contactID, emailSendID).Scan(&eventID, &metaRaw)
	if err != nil {
		return err
	}
	meta := map[string]interface{}{}
	_ = json.Unmarshal([]byte(metaRaw), &meta)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["sentiment"] = sentiment
	if source != "" {
		meta["sentiment_source"] = source
	}
	b, _ := json.Marshal(meta)
	_, err = db.Exec(`UPDATE contact_events SET metadata_json = ? WHERE id = ?`, string(b), eventID)
	return err
}

// ApplyReplySentimentSideEffects stops outreach on negative replies and refreshes hot-stop.
func ApplyReplySentimentSideEffects(userID, contactID, campaignID, messageID int64, sentiment string) {
	sentiment = NormalizeReplySentiment(sentiment)
	if sentiment == ReplySentimentNegative {
		ApplyStopOnReplyForContact(contactID, campaignID)
		_ = SuppressContact(contactID, "negative_reply", "AI/manual negative reply sentiment", 0)
		return
	}
	if sentiment == ReplySentimentPositive && campaignID > 0 {
		MaybeStopWorkflowOnHot(campaignID, contactID)
	}
}

// CountCampaignContactReplySentiments returns reply sentiment tallies for temperature.
func CountCampaignContactReplySentiments(campaignID, contactID int64) (positive, negative, nonNegative, total int, err error) {
	if campaignID <= 0 || contactID <= 0 {
		return 0, 0, 0, 0, nil
	}
	rows, err := db.Query(`
		SELECT COALESCE(cm.reply_sentiment, ''), COALESCE(ce.metadata_json, '{}')
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		LEFT JOIN conversation_messages cm
			ON cm.email_send_id = ce.email_send_id AND cm.contact_id = ce.contact_id AND cm.direction = 'inbound'
		WHERE es.campaign_id = ? AND es.contact_id = ? AND ce.event_type = 'REPLY'
	`, campaignID, contactID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var sentRaw, metaRaw string
		if rows.Scan(&sentRaw, &metaRaw) != nil {
			continue
		}
		total++
		s := NormalizeReplySentiment(sentRaw)
		if s == ReplySentimentPending || s == "" {
			meta := map[string]interface{}{}
			_ = json.Unmarshal([]byte(metaRaw), &meta)
			if v, ok := meta["sentiment"].(string); ok {
				s = NormalizeReplySentiment(v)
			}
		}
		switch s {
		case ReplySentimentPositive:
			positive++
			nonNegative++
		case ReplySentimentNegative:
			negative++
		default:
			nonNegative++
		}
	}
	return positive, negative, nonNegative, total, nil
}

// MarkContactRepliedPending sets replied_at and pending sentiment when a reply arrives.
func MarkContactRepliedPending(contactID int64) error {
	now := time.Now()
	_, err := db.Exec(`
		UPDATE contact SET replied_at = COALESCE(replied_at, ?), last_reply_sentiment = CASE
			WHEN last_reply_sentiment IN ('positive', 'negative', 'neutral') THEN last_reply_sentiment
			ELSE 'pending'
		END
		WHERE id = ?
	`, now, contactID)
	return err
}

// ReplySentimentForSend returns the latest inbound sentiment for a send.
func ReplySentimentForSend(emailSendID, contactID int64) (string, error) {
	var sent string
	err := db.QueryRow(`
		SELECT COALESCE(reply_sentiment, '') FROM conversation_messages
		WHERE email_send_id = ? AND contact_id = ? AND direction = 'inbound'
		ORDER BY id DESC LIMIT 1
	`, emailSendID, contactID).Scan(&sent)
	if err != nil {
		return "", err
	}
	return sent, nil
}
