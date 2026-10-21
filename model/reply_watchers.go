package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

// Known reply intents for watcher automation.
var ReplyIntents = []string{
	"interested",
	"not_now",
	"ooo",
	"unsubscribe",
	"wrong_person",
	"referral",
	"meeting_ask",
	"pricing",
}

func NormalizeReplyIntent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, k := range ReplyIntents {
		if s == k {
			return k
		}
	}
	return ""
}

// IntentToSentiment maps fine-grained intent to coarse sentiment for temperature UI.
func IntentToSentiment(intent string) string {
	switch NormalizeReplyIntent(intent) {
	case "interested", "meeting_ask", "pricing", "referral":
		return ReplySentimentPositive
	case "not_now", "unsubscribe", "wrong_person":
		return ReplySentimentNegative
	case "ooo":
		return ReplySentimentNeutral
	default:
		return ReplySentimentNeutral
	}
}

// CampaignReplyWatcher maps an intent to a template for auto-reply.
type CampaignReplyWatcher struct {
	ID         int64
	CampaignID int64
	Intent     string
	TemplateID int64
	Enabled    bool
}

// ListCampaignReplyWatchers returns all watcher rules for a campaign.
func ListCampaignReplyWatchers(campaignID int64) ([]CampaignReplyWatcher, error) {
	rows, err := db.Query(`
		SELECT id, campaign_id, intent, template_id, COALESCE(enabled, TRUE)
		FROM campaign_reply_watchers
		WHERE campaign_id = ?
		ORDER BY intent
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignReplyWatcher
	for rows.Next() {
		var w CampaignReplyWatcher
		if err := rows.Scan(&w.ID, &w.CampaignID, &w.Intent, &w.TemplateID, &w.Enabled); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SaveCampaignReplyWatchers replaces watcher rules (enabled list).
func SaveCampaignReplyWatchers(campaignID int64, watchers []CampaignReplyWatcher) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM campaign_reply_watchers WHERE campaign_id = ?`, campaignID); err != nil {
		return err
	}
	for _, w := range watchers {
		intent := NormalizeReplyIntent(w.Intent)
		if intent == "" || w.TemplateID <= 0 {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO campaign_reply_watchers (campaign_id, intent, template_id, enabled)
			VALUES (?, ?, ?, ?)
		`, campaignID, intent, w.TemplateID, w.Enabled); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FindCampaignReplyWatcher returns an enabled watcher for intent, if any.
func FindCampaignReplyWatcher(campaignID int64, intent string) (CampaignReplyWatcher, bool) {
	intent = NormalizeReplyIntent(intent)
	if intent == "" || campaignID <= 0 {
		return CampaignReplyWatcher{}, false
	}
	var w CampaignReplyWatcher
	err := db.QueryRow(`
		SELECT id, campaign_id, intent, template_id, COALESCE(enabled, TRUE)
		FROM campaign_reply_watchers
		WHERE campaign_id = ? AND intent = ? AND COALESCE(enabled, TRUE) = TRUE
	`, campaignID, intent).Scan(&w.ID, &w.CampaignID, &w.Intent, &w.TemplateID, &w.Enabled)
	if err != nil {
		return CampaignReplyWatcher{}, false
	}
	return w, true
}

// ReplyAutoJob is a delayed verified auto-reply.
type ReplyAutoJob struct {
	ID             int64
	CampaignID     int64
	ContactID      int64
	MessageID      int64
	Intent         string
	TemplateID     int64
	Status         string
	RunAt          time.Time
	VerifyDecision string
	LastError      string
	EmailSendID    int64
}

// EnqueueReplyAutoJob schedules a delayed auto-reply (idempotent per message+intent).
func EnqueueReplyAutoJob(campaignID, contactID, messageID int64, intent string, templateID int64, runAt time.Time) (int64, error) {
	intent = NormalizeReplyIntent(intent)
	if intent == "" || templateID <= 0 {
		return 0, fmt.Errorf("invalid watcher job")
	}
	now := time.Now()
	if runAt.IsZero() {
		runAt = now.Add(90 * time.Second)
	}
	_, err := db.Exec(`
		INSERT INTO reply_auto_jobs (campaign_id, contact_id, message_id, intent, template_id, status, run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?)
		ON CONFLICT (message_id, intent) DO NOTHING
	`, campaignID, contactID, messageID, intent, templateID, runAt, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	_ = db.QueryRow(`SELECT id FROM reply_auto_jobs WHERE message_id = ? AND intent = ?`, messageID, intent).Scan(&id)
	return id, nil
}

// ClaimDueReplyAutoJobs returns pending jobs ready to run.
func ClaimDueReplyAutoJobs(limit int) ([]ReplyAutoJob, error) {
	if limit <= 0 {
		limit = 40
	}
	now := time.Now()
	rows, err := db.Query(`
		SELECT id, campaign_id, contact_id, message_id, intent, template_id, status, run_at,
			COALESCE(verify_decision,''), COALESCE(last_error,''), COALESCE(email_send_id, 0)
		FROM reply_auto_jobs
		WHERE status = 'pending' AND run_at <= ?
		ORDER BY run_at ASC
		LIMIT ?
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReplyAutoJob
	for rows.Next() {
		var j ReplyAutoJob
		if err := rows.Scan(&j.ID, &j.CampaignID, &j.ContactID, &j.MessageID, &j.Intent, &j.TemplateID,
			&j.Status, &j.RunAt, &j.VerifyDecision, &j.LastError, &j.EmailSendID); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// UpdateReplyAutoJob sets terminal or in-progress state.
func UpdateReplyAutoJob(id int64, status, decision, lastErr string, emailSendID int64) error {
	_, err := db.Exec(`
		UPDATE reply_auto_jobs
		SET status = ?, verify_decision = ?, last_error = ?, email_send_id = ?, updated_at = ?
		WHERE id = ?
	`, status, decision, lastErr, emailSendID, time.Now(), id)
	return err
}

// GetConversationMessageBody loads inbound message text for verify.
func GetConversationMessageText(messageID int64) (subject, body string, contactID, userID int64, err error) {
	err = db.QueryRow(`
		SELECT COALESCE(subject,''), COALESCE(body_text,''), contact_id, user_id
		FROM conversation_messages WHERE id = ? AND direction = 'inbound'
	`, messageID).Scan(&subject, &body, &contactID, &userID)
	return
}

// InsertSystemConversationNote adds a non-email note into the thread (verify skip, etc.).
func InsertSystemConversationNote(userID, contactID int64, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || userID <= 0 || contactID <= 0 {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO conversation_messages (
			user_id, contact_id, direction, from_email, to_email, subject, body_text, occurred_at
		) VALUES (?, ?, 'system', '', '', '[System]', ?, ?)
	`, userID, contactID, body, time.Now())
	return err
}
