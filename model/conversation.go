package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	htmltemplate "html/template"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"emailtracker.com/db"
)

const (
	ConversationInbound  = "inbound"
	ConversationOutbound = "outbound"
	MaxConversationBody  = 200 * 1024
)

type ConversationMessage struct {
	ID             int64
	UserID         int64
	ContactID      int64
	SMTPAccountID  int64
	EmailSendID    int64
	Direction      string
	FromEmail      string
	ToEmail        string
	Subject        string
	BodyText       string
	BodyHTML       string
	MessageID      string
	InReplyTo      string
	ReplySentiment string
	OccurredAt     time.Time
	CreatedAt      time.Time
	// Display helpers (not always filled)
	CampaignName string
	OpenCount    int
	ClickCount   int
}

type ConversationMessageInput struct {
	UserID         int64
	ContactID      int64
	SMTPAccountID  int64
	EmailSendID    int64
	Direction      string
	FromEmail      string
	ToEmail        string
	Subject        string
	BodyText       string
	BodyHTML       string
	MessageID      string
	InReplyTo      string
	ReplySentiment string
	OccurredAt     time.Time
}

func truncateConversationBody(s string) string {
	// IMAP bodies sometimes include Windows-1252 bytes (e.g. en-dash 0x96) that
	// are invalid UTF-8 and reject Postgres inserts.
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if len(s) <= MaxConversationBody {
		return s
	}
	s = s[:MaxConversationBody]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func InsertConversationMessage(in ConversationMessageInput) (int64, error) {
	if in.Direction != ConversationInbound && in.Direction != ConversationOutbound {
		in.Direction = ConversationInbound
	}
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	msgID := strings.TrimSpace(in.MessageID)
	bodyText := truncateConversationBody(in.BodyText)
	bodyHTML := truncateConversationBody(in.BodyHTML)
	in.Subject = strings.ToValidUTF8(in.Subject, "�")
	in.FromEmail = strings.ToValidUTF8(in.FromEmail, "�")
	in.ToEmail = strings.ToValidUTF8(in.ToEmail, "�")

	if msgID != "" {
		var existing int64
		var existingDir string
		err := db.QueryRow(`
			SELECT id, direction FROM conversation_messages WHERE user_id = ? AND message_id = ?
		`, in.UserID, msgID).Scan(&existing, &existingDir)
		if err == nil && existing > 0 {
			if existingDir == in.Direction {
				return existing, nil
			}
			// Same Message-ID claimed by the other direction (broken IMAP / In-Reply-To
			// reused as Message-ID). Keep uniqueness without dropping the inbound row.
			if in.Direction == ConversationInbound {
				msgID = msgID + "#inbound"
			} else {
				msgID = msgID + "#outbound"
			}
		}
	}

	var id int64
	sentiment := strings.TrimSpace(in.ReplySentiment)
	err := db.QueryRow(`
		INSERT INTO conversation_messages (
			user_id, contact_id, smtp_account_id, email_send_id, direction,
			from_email, to_email, subject, body_text, body_html,
			message_id, in_reply_to, reply_sentiment, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, in.UserID, in.ContactID, in.SMTPAccountID, in.EmailSendID, in.Direction,
		in.FromEmail, in.ToEmail, in.Subject, bodyText, bodyHTML,
		msgID, strings.TrimSpace(in.InReplyTo), sentiment, occurred).Scan(&id)
	if err != nil && msgID != "" {
		// Race on unique index: return the winner if same direction.
		var existing int64
		var existingDir string
		if qErr := db.QueryRow(`
			SELECT id, direction FROM conversation_messages WHERE user_id = ? AND message_id = ?
		`, in.UserID, msgID).Scan(&existing, &existingDir); qErr == nil && existing > 0 && existingDir == in.Direction {
			return existing, nil
		}
	}
	return id, err
}

func ListConversationMessages(userID, contactID int64, limit int) ([]ConversationMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT id, user_id, contact_id, COALESCE(smtp_account_id,0), COALESCE(email_send_id,0),
			direction, from_email, to_email, subject, body_text, body_html,
			COALESCE(message_id,''), COALESCE(in_reply_to,''), COALESCE(reply_sentiment,''), occurred_at, created_at
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ?
		ORDER BY occurred_at ASC, id ASC
		LIMIT ?
	`, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationMessage
	for rows.Next() {
		var m ConversationMessage
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.ContactID, &m.SMTPAccountID, &m.EmailSendID,
			&m.Direction, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.BodyHTML,
			&m.MessageID, &m.InReplyTo, &m.ReplySentiment, &m.OccurredAt, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ListContactConversation merges conversation_messages with outbound email_sends snapshots
// that were never stored as conversation rows (legacy sends).
func ListContactConversation(userID, contactID int64, limit int) ([]ConversationMessage, error) {
	msgs, err := ListConversationMessages(userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	seenSend := make(map[int64]bool)
	for _, m := range msgs {
		if m.EmailSendID > 0 {
			seenSend[m.EmailSendID] = true
		}
	}

	rows, err := db.Query(`
		SELECT es.id, COALESCE(es.smtp_account_id,0), COALESCE(es.rendered_subject,''),
			COALESCE(es.rendered_html,''), COALESCE(es.rendered_text,''),
			es.sent_at,
			COALESCE(c.email,''), COALESCE(sa.from_email,''), COALESCE(t.subject,'')
		FROM email_sends es
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN smtp_accounts sa ON sa.id = es.smtp_account_id
		LEFT JOIN template t ON t.id = es.template_id
		WHERE es.user_id = ? AND es.contact_id = ? AND es.delivery_status = 'sent'
		ORDER BY es.sent_at ASC NULLS LAST, es.id ASC
	`, userID, contactID)
	if err != nil {
		return msgs, nil
	}
	defer rows.Close()

	var extras []ConversationMessage
	for rows.Next() {
		var sendID, smtpID int64
		var subj, html, text string
		var sentAt sql.NullTime
		var toEmail, fromEmail, tmplSubj string
		if err := rows.Scan(&sendID, &smtpID, &subj, &html, &text, &sentAt, &toEmail, &fromEmail, &tmplSubj); err != nil {
			continue
		}
		if seenSend[sendID] {
			continue
		}
		if subj == "" {
			subj = tmplSubj
		}
		if html == "" && text == "" && subj == "" {
			continue
		}
		occurred := time.Now()
		if sentAt.Valid {
			occurred = sentAt.Time
		}
		extras = append(extras, ConversationMessage{
			UserID:        userID,
			ContactID:     contactID,
			SMTPAccountID: smtpID,
			EmailSendID:   sendID,
			Direction:     ConversationOutbound,
			FromEmail:     fromEmail,
			ToEmail:       toEmail,
			Subject:       subj,
			BodyText:      text,
			BodyHTML:      html,
			OccurredAt:    occurred,
		})
	}

	if len(extras) == 0 {
		return msgs, nil
	}
	merged := append(msgs, extras...)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].OccurredAt.Equal(merged[j].OccurredAt) {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].OccurredAt.Before(merged[j].OccurredAt)
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	hydrateConversationFromSends(userID, merged)
	return merged, nil
}

// hydrateConversationFromSends overlays email_sends.rendered_* onto outbound messages
// so the thread shows exactly what was delivered (variables already filled).
func hydrateConversationFromSends(userID int64, msgs []ConversationMessage) {
	for i := range msgs {
		m := &msgs[i]
		if m.Direction != ConversationOutbound || m.EmailSendID <= 0 {
			continue
		}
		var subj, htmlBody, textBody string
		err := db.QueryRow(`
			SELECT COALESCE(rendered_subject,''), COALESCE(rendered_html,''), COALESCE(rendered_text,'')
			FROM email_sends WHERE id = ? AND user_id = ?
		`, m.EmailSendID, userID).Scan(&subj, &htmlBody, &textBody)
		if err != nil {
			continue
		}
		if subj == "" && htmlBody == "" && textBody == "" {
			continue
		}
		if subj != "" {
			m.Subject = subj
		}
		if htmlBody != "" {
			m.BodyHTML = htmlBody
		}
		if textBody != "" {
			m.BodyText = textBody
		}
	}
}

func LatestInboundMessage(userID, contactID int64) (ConversationMessage, error) {
	row := db.QueryRow(`
		SELECT id, user_id, contact_id, COALESCE(smtp_account_id,0), COALESCE(email_send_id,0),
			direction, from_email, to_email, subject, body_text, body_html,
			COALESCE(message_id,''), COALESCE(in_reply_to,''), COALESCE(reply_sentiment,''), occurred_at, created_at
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, userID, contactID)
	var m ConversationMessage
	err := row.Scan(
		&m.ID, &m.UserID, &m.ContactID, &m.SMTPAccountID, &m.EmailSendID,
		&m.Direction, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.BodyHTML,
		&m.MessageID, &m.InReplyTo, &m.ReplySentiment, &m.OccurredAt, &m.CreatedAt,
	)
	return m, err
}

func GetConversationMessageForUser(userID, contactID, messageID int64) (ConversationMessage, error) {
	row := db.QueryRow(`
		SELECT id, user_id, contact_id, COALESCE(smtp_account_id,0), COALESCE(email_send_id,0),
			direction, from_email, to_email, subject, body_text, body_html,
			COALESCE(message_id,''), COALESCE(in_reply_to,''), COALESCE(reply_sentiment,''), occurred_at, created_at
		FROM conversation_messages
		WHERE id = ? AND user_id = ? AND contact_id = ?
	`, messageID, userID, contactID)
	var m ConversationMessage
	err := row.Scan(
		&m.ID, &m.UserID, &m.ContactID, &m.SMTPAccountID, &m.EmailSendID,
		&m.Direction, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.BodyHTML,
		&m.MessageID, &m.InReplyTo, &m.ReplySentiment, &m.OccurredAt, &m.CreatedAt,
	)
	return m, err
}

// ListReplyTargets returns messages in this contact thread that can be replied to
// (newest first). Prefers inbound; includes outbound with a Message-ID for threading.
func ListReplyTargets(userID, contactID int64, limit int) ([]ConversationMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, user_id, contact_id, COALESCE(smtp_account_id,0), COALESCE(email_send_id,0),
			direction, from_email, to_email, subject, body_text, body_html,
			COALESCE(message_id,''), COALESCE(in_reply_to,''), COALESCE(reply_sentiment,''), occurred_at, created_at
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationMessage
	for rows.Next() {
		var m ConversationMessage
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.ContactID, &m.SMTPAccountID, &m.EmailSendID,
			&m.Direction, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.BodyHTML,
			&m.MessageID, &m.InReplyTo, &m.ReplySentiment, &m.OccurredAt, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func HasInboundConversation(userID, contactID int64) bool {
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
	`, userID, contactID).Scan(&n)
	return n > 0
}

const stubInboundBodyMarker = "Full message body was not stored"

// IsStubInboundBody reports placeholder text used when a reply event existed without MIME body.
func IsStubInboundBody(bodyText, bodyHTML string) bool {
	combined := bodyText + "\n" + bodyHTML
	return strings.Contains(combined, stubInboundBodyMarker)
}

// ContactNeedsInboundBody is true when the contact has a reply signal but no real inbound body.
func ContactNeedsInboundBody(userID, contactID int64) bool {
	if userID <= 0 || contactID <= 0 {
		return false
	}
	var replied sql.NullTime
	_ = db.QueryRow(`SELECT replied_at FROM contact WHERE id = ? AND user_id = ?`, contactID, userID).Scan(&replied)
	hasReplySignal := replied.Valid
	if !hasReplySignal {
		var n int
		_ = db.QueryRow(`
			SELECT COUNT(*) FROM contact_events ce
			INNER JOIN email_sends es ON es.id = ce.email_send_id
			WHERE ce.contact_id = ? AND es.user_id = ? AND ce.event_type = 'REPLY'
		`, contactID, userID).Scan(&n)
		hasReplySignal = n > 0
	}
	if !hasReplySignal {
		return false
	}
	rows, err := db.Query(`
		SELECT COALESCE(body_text,''), COALESCE(body_html,''), COALESCE(message_id,'')
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
		ORDER BY occurred_at DESC, id DESC
	`, userID, contactID)
	if err != nil {
		return true
	}
	defer rows.Close()
	for rows.Next() {
		var text, html, msgID string
		if rows.Scan(&text, &html, &msgID) != nil {
			continue
		}
		if strings.HasPrefix(msgID, "reply-event:") || IsStubInboundBody(text, html) {
			continue
		}
		if strings.TrimSpace(text) != "" || strings.TrimSpace(html) != "" {
			return false
		}
	}
	return true
}

// UpsertInboundReply inserts an inbound message, or upgrades a stub/empty inbound for this contact.
func UpsertInboundReply(in ConversationMessageInput) (int64, error) {
	in.Direction = ConversationInbound
	in.Subject = strings.ToValidUTF8(in.Subject, "�")
	in.FromEmail = strings.ToValidUTF8(in.FromEmail, "�")
	in.ToEmail = strings.ToValidUTF8(in.ToEmail, "�")
	bodyText := truncateConversationBody(in.BodyText)
	bodyHTML := truncateConversationBody(in.BodyHTML)
	if strings.TrimSpace(bodyText) == "" && strings.TrimSpace(bodyHTML) == "" {
		return InsertConversationMessage(in)
	}

	msgID := strings.TrimSpace(in.MessageID)
	if msgID != "" {
		var existing int64
		var existingDir, existingText, existingHTML string
		err := db.QueryRow(`
			SELECT id, direction, COALESCE(body_text,''), COALESCE(body_html,'')
			FROM conversation_messages WHERE user_id = ? AND message_id = ?
		`, in.UserID, msgID).Scan(&existing, &existingDir, &existingText, &existingHTML)
		if err == nil && existing > 0 && existingDir == ConversationInbound {
			if IsStubInboundBody(existingText, existingHTML) || (strings.TrimSpace(existingText) == "" && strings.TrimSpace(existingHTML) == "") {
				_, err = db.Exec(`
					UPDATE conversation_messages SET
						body_text = ?, body_html = ?, subject = COALESCE(NULLIF(?, ''), subject),
						from_email = COALESCE(NULLIF(?, ''), from_email),
						to_email = COALESCE(NULLIF(?, ''), to_email),
						smtp_account_id = CASE WHEN ? > 0 THEN ? ELSE smtp_account_id END,
						email_send_id = CASE WHEN ? > 0 THEN ? ELSE email_send_id END,
						in_reply_to = COALESCE(NULLIF(?, ''), in_reply_to)
					WHERE id = ?
				`, bodyText, bodyHTML, in.Subject, in.FromEmail, in.ToEmail,
					in.SMTPAccountID, in.SMTPAccountID, in.EmailSendID, in.EmailSendID,
					strings.TrimSpace(in.InReplyTo), existing)
				return existing, err
			}
			return existing, nil
		}
	}

	// Upgrade stub created from contact_events (message_id reply-event:N).
	var stubID int64
	var stubText, stubHTML string
	err := db.QueryRow(`
		SELECT id, COALESCE(body_text,''), COALESCE(body_html,'')
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
			AND (message_id LIKE 'reply-event:%' OR body_text LIKE '%' || ? || '%')
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, in.UserID, in.ContactID, stubInboundBodyMarker).Scan(&stubID, &stubText, &stubHTML)
	if err == nil && stubID > 0 {
		newMsgID := msgID
		if newMsgID == "" {
			newMsgID = fmt.Sprintf("recovered:%d", stubID)
		}
		// Avoid unique collision with an outbound that reused this Message-ID.
		var clashDir string
		_ = db.QueryRow(`SELECT direction FROM conversation_messages WHERE user_id = ? AND message_id = ?`, in.UserID, newMsgID).Scan(&clashDir)
		if clashDir == ConversationOutbound {
			newMsgID = newMsgID + "#inbound"
		}
		_, err = db.Exec(`
			UPDATE conversation_messages SET
				body_text = ?, body_html = ?, subject = COALESCE(NULLIF(?, ''), subject),
				from_email = COALESCE(NULLIF(?, ''), from_email),
				to_email = COALESCE(NULLIF(?, ''), to_email),
				message_id = ?,
				in_reply_to = COALESCE(NULLIF(?, ''), in_reply_to),
				smtp_account_id = CASE WHEN ? > 0 THEN ? ELSE smtp_account_id END,
				email_send_id = CASE WHEN ? > 0 THEN ? ELSE email_send_id END
			WHERE id = ?
		`, bodyText, bodyHTML, in.Subject, in.FromEmail, in.ToEmail, newMsgID,
			strings.TrimSpace(in.InReplyTo),
			in.SMTPAccountID, in.SMTPAccountID, in.EmailSendID, in.EmailSendID, stubID)
		return stubID, err
	}

	return InsertConversationMessage(in)
}

// EnsureInboundFromReplyEvents creates stub inbound conversation rows when IMAP
// recorded a REPLY contact_event but conversation_messages insert was skipped
// (dedupe early-return or Message-ID collision). Safe to call on every contact view.
func EnsureInboundFromReplyEvents(userID, contactID int64) {
	if userID <= 0 || contactID <= 0 || HasInboundConversation(userID, contactID) {
		return
	}
	rows, err := db.Query(`
		SELECT ce.id, COALESCE(ce.email_send_id, 0), COALESCE(ce.metadata_json, '{}'), ce.occurred_at,
			COALESCE(c.email, ''), COALESCE(sa.from_email, ''), COALESCE(es.smtp_account_id, 0)
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		INNER JOIN contact c ON c.id = ce.contact_id
		LEFT JOIN smtp_accounts sa ON sa.id = es.smtp_account_id
		WHERE ce.contact_id = ? AND es.user_id = ? AND ce.event_type = 'REPLY'
		ORDER BY ce.occurred_at ASC, ce.id ASC
	`, contactID, userID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var eventID, sendID, smtpID int64
		var metaJSON, contactEmail, fromEmail string
		var occurred time.Time
		if rows.Scan(&eventID, &sendID, &metaJSON, &occurred, &contactEmail, &fromEmail, &smtpID) != nil {
			continue
		}
		subject := ""
		snippet := ""
		sentiment := ReplySentimentPending
		if metaJSON != "" && metaJSON != "{}" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(metaJSON), &meta) == nil {
				if s, ok := meta["subject"].(string); ok {
					subject = strings.TrimSpace(s)
				}
				if s, ok := meta["body_snippet"].(string); ok {
					snippet = strings.TrimSpace(s)
				}
				if s, ok := meta["sentiment"].(string); ok && strings.TrimSpace(s) != "" {
					sentiment = NormalizeReplySentiment(s)
				}
			}
		}
		if subject == "" {
			subject = "(reply)"
		}
		bodyText := snippet
		if bodyText == "" {
			bodyText = "Reply detected. Full message body was not stored — open Reply to continue the thread."
		}
		dedupeMsgID := fmt.Sprintf("reply-event:%d", eventID)
		_, _ = InsertConversationMessage(ConversationMessageInput{
			UserID:         userID,
			ContactID:      contactID,
			SMTPAccountID:  smtpID,
			EmailSendID:    sendID,
			Direction:      ConversationInbound,
			FromEmail:      contactEmail,
			ToEmail:        fromEmail,
			Subject:        subject,
			BodyText:       bodyText,
			MessageID:      dedupeMsgID,
			ReplySentiment: sentiment,
			OccurredAt:     occurred,
		})
	}
}

// LatestSMTPAccountForContact returns the mailbox used for the most recent outbound
// to this contact. Only delivered / in-flight sends and conversation history count —
// pending queue pins must not lock a contact onto a seat before the first real send.
func LatestSMTPAccountForContact(userID, contactID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT smtp_account_id FROM (
			SELECT COALESCE(smtp_account_id, 0) AS smtp_account_id,
				COALESCE(sent_at, TIMESTAMPTZ 'epoch') AS ts, id
			FROM email_sends
			WHERE user_id = ? AND contact_id = ? AND COALESCE(smtp_account_id, 0) > 0
			  AND delivery_status IN ('sent', 'sending')
			UNION ALL
			SELECT COALESCE(smtp_account_id, 0), occurred_at, id
			FROM conversation_messages
			WHERE user_id = ? AND contact_id = ? AND direction = 'outbound'
			  AND COALESCE(smtp_account_id, 0) > 0
			UNION ALL
			SELECT COALESCE(smtp_account_id, 0), COALESCE(updated_at, created_at), id
			FROM send_jobs
			WHERE user_id = ? AND contact_id = ? AND status IN ('sent', 'processing')
			  AND COALESCE(smtp_account_id, 0) > 0
		) t
		ORDER BY ts DESC NULLS LAST, id DESC
		LIMIT 1
	`, userID, contactID, userID, contactID, userID, contactID).Scan(&id)
	return id, err
}

// ContactHasDeliveredOutbound reports whether this contact has already received mail
// from us (sticky From must be preserved). Queued-only history does not count.
func ContactHasDeliveredOutbound(userID, contactID int64) bool {
	if userID <= 0 || contactID <= 0 {
		return false
	}
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM email_sends
			WHERE user_id = ? AND contact_id = ? AND delivery_status = 'sent'
			LIMIT 1
		) s
	`, userID, contactID).Scan(&n)
	if n > 0 {
		return true
	}
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM conversation_messages
			WHERE user_id = ? AND contact_id = ? AND direction = 'outbound'
			LIMIT 1
		) m
	`, userID, contactID).Scan(&n)
	return n > 0
}

func (m ConversationMessage) DisplayHTML() htmltemplate.HTML {
	if m.BodyHTML != "" {
		return htmltemplate.HTML(sanitizeHTMLForDisplay(m.BodyHTML))
	}
	if m.BodyText == "" {
		return ""
	}
	return htmltemplate.HTML("<p>" + html.EscapeString(m.BodyText) + "</p>")
}

// DisplaySrcDoc returns sanitized HTML for an iframe srcdoc attribute (string so the
// template engine attribute-escapes it; the browser then renders it as HTML).
func (m ConversationMessage) DisplaySrcDoc() string {
	return string(m.DisplayHTML())
}

// InboxPlainBody returns plain text suitable for the inbox reading pane.
// Empty means the caller should fall back to HTML display.
func (m ConversationMessage) InboxPlainBody() string {
	text := strings.TrimSpace(m.BodyText)
	if text == "" || IsStubInboundBody(m.BodyText, m.BodyHTML) {
		return ""
	}
	return text
}

// sanitizeHTMLForDisplay strips scripts/styles/tracking for safe embedding
// (kept in model to avoid util↔model import cycle).
func sanitizeHTMLForDisplay(s string) string {
	if s == "" {
		return ""
	}
	s = RewriteTrackedClicksForDisplay(s)
	s = stripTrackingForDisplay(s)
	lower := strings.ToLower(s)
	for {
		start := strings.Index(lower, "<script")
		if start < 0 {
			break
		}
		end := strings.Index(lower[start:], "</script>")
		if end < 0 {
			s = s[:start]
			break
		}
		end = start + end + len("</script>")
		s = s[:start] + s[end:]
		lower = strings.ToLower(s)
	}
	for {
		start := strings.Index(lower, "<style")
		if start < 0 {
			break
		}
		end := strings.Index(lower[start:], "</style>")
		if end < 0 {
			s = s[:start]
			break
		}
		end = start + end + len("</style>")
		s = s[:start] + s[end:]
		lower = strings.ToLower(s)
	}
	return strings.ReplaceAll(s, "javascript:", "")
}

var (
	convTrackImgRe   = regexp.MustCompile(`(?is)<img\b[^>]*\bsrc\s*=\s*["'][^"']*/track/open/[^"']*["'][^>]*/?>`)
	convTrackBgRe    = regexp.MustCompile(`(?is)background-image\s*:\s*url\(\s*['"]?[^)'"]*/track/open/[^)'"]*['"]?\s*\)\s*;?`)
	convTrackDivRe   = regexp.MustCompile(`(?is)<div[^>]*(?:aria-hidden\s*=\s*["']true["']|max-height\s*:\s*0|display\s*:\s*none)[^>]*>\s*(?:<img\b[^>]*\bsrc\s*=\s*["'][^"']*/track/open/[^"']*["'][^>]*/?>)?\s*</div>`)
	convTrackClickRe = regexp.MustCompile(`(?is)(\bhref\s*=\s*)(["'])([^"']*/track/click/[^"']*)(["'])`)
)

func stripTrackingForDisplay(html string) string {
	if html == "" {
		return html
	}
	out := html
	lower := strings.ToLower(out)
	if strings.Contains(lower, "/track/open/") {
		out = convTrackDivRe.ReplaceAllString(out, "")
		out = convTrackImgRe.ReplaceAllString(out, "")
		out = convTrackBgRe.ReplaceAllString(out, "")
	}
	if strings.Contains(strings.ToLower(out), "/track/click/") {
		out = convTrackClickRe.ReplaceAllString(out, `${1}${2}#${4}`)
	}
	return out
}

func (m ConversationMessage) IsInbound() bool {
	return m.Direction == ConversationInbound
}

// CanReplyInApp is true when the contact has inbound mail or a replied flag, and a mailbox exists.
func CanReplyInApp(userID, contactID int64, repliedAt *time.Time) bool {
	if repliedAt != nil || HasInboundConversation(userID, contactID) {
		return true
	}
	// Also allow reply if we've sent them something from a known mailbox.
	id, err := LatestSMTPAccountForContact(userID, contactID)
	return err == nil && id > 0
}

// OutboundThreadHeaders holds SMTP threading fields for follow-up sends.
type OutboundThreadHeaders struct {
	InReplyTo   string
	References  string
	RootSubject string // first outbound subject in the thread (for Re: follow-ups)
	HasPrior    bool
}

// ResolveOutboundThread finds the previous outbound message to thread against.
// Prefers the same workflow instance; falls back to the same campaign + contact.
func ResolveOutboundThread(userID, contactID, workflowInstanceID, campaignID, excludeSendID int64) (OutboundThreadHeaders, error) {
	var out OutboundThreadHeaders
	if contactID <= 0 || userID <= 0 {
		return out, nil
	}

	var priorMsgID, priorInReplyTo, priorSubject string
	var err error
	switch {
	case workflowInstanceID > 0:
		err = db.QueryRow(`
			SELECT cm.message_id, COALESCE(cm.in_reply_to,''), COALESCE(cm.subject,'')
			FROM conversation_messages cm
			INNER JOIN email_sends es ON es.id = cm.email_send_id
			WHERE cm.user_id = ? AND cm.contact_id = ? AND cm.direction = 'outbound'
				AND COALESCE(cm.message_id,'') <> ''
				AND es.workflow_instance_id = ?
				AND es.delivery_status = 'sent'
				AND cm.email_send_id <> ?
			ORDER BY cm.occurred_at DESC, cm.id DESC
			LIMIT 1
		`, userID, contactID, workflowInstanceID, excludeSendID).Scan(&priorMsgID, &priorInReplyTo, &priorSubject)
	case campaignID > 0:
		err = db.QueryRow(`
			SELECT cm.message_id, COALESCE(cm.in_reply_to,''), COALESCE(cm.subject,'')
			FROM conversation_messages cm
			INNER JOIN email_sends es ON es.id = cm.email_send_id
			WHERE cm.user_id = ? AND cm.contact_id = ? AND cm.direction = 'outbound'
				AND COALESCE(cm.message_id,'') <> ''
				AND es.campaign_id = ?
				AND es.delivery_status = 'sent'
				AND cm.email_send_id <> ?
			ORDER BY cm.occurred_at DESC, cm.id DESC
			LIMIT 1
		`, userID, contactID, campaignID, excludeSendID).Scan(&priorMsgID, &priorInReplyTo, &priorSubject)
	default:
		return out, nil
	}
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	priorMsgID = normalizeAngleAddr(priorMsgID)
	if priorMsgID == "" {
		return out, nil
	}

	out.HasPrior = true
	out.InReplyTo = priorMsgID
	if priorInReplyTo != "" {
		out.References = strings.TrimSpace(normalizeAngleAddr(priorInReplyTo) + " " + priorMsgID)
	} else {
		out.References = priorMsgID
	}

	var rootSubj string
	switch {
	case workflowInstanceID > 0:
		_ = db.QueryRow(`
			SELECT COALESCE(cm.subject,'')
			FROM conversation_messages cm
			INNER JOIN email_sends es ON es.id = cm.email_send_id
			WHERE cm.user_id = ? AND cm.contact_id = ? AND cm.direction = 'outbound'
				AND es.workflow_instance_id = ?
				AND es.delivery_status = 'sent'
				AND cm.email_send_id <> ?
			ORDER BY cm.occurred_at ASC, cm.id ASC
			LIMIT 1
		`, userID, contactID, workflowInstanceID, excludeSendID).Scan(&rootSubj)
	case campaignID > 0:
		_ = db.QueryRow(`
			SELECT COALESCE(cm.subject,'')
			FROM conversation_messages cm
			INNER JOIN email_sends es ON es.id = cm.email_send_id
			WHERE cm.user_id = ? AND cm.contact_id = ? AND cm.direction = 'outbound'
				AND es.campaign_id = ?
				AND es.delivery_status = 'sent'
				AND cm.email_send_id <> ?
			ORDER BY cm.occurred_at ASC, cm.id ASC
			LIMIT 1
		`, userID, contactID, campaignID, excludeSendID).Scan(&rootSubj)
	}
	if rootSubj == "" {
		rootSubj = priorSubject
	}
	out.RootSubject = rootSubj
	return out, nil
}

// FollowUpSubject returns a Re: subject based on the thread root (classic follow-up threading).
func FollowUpSubject(rootSubject, currentSubject string) string {
	root := strings.TrimSpace(rootSubject)
	if root == "" {
		return currentSubject
	}
	for {
		lower := strings.ToLower(root)
		switch {
		case strings.HasPrefix(lower, "re:"):
			root = strings.TrimSpace(root[3:])
		case strings.HasPrefix(lower, "fwd:"):
			root = strings.TrimSpace(root[4:])
		case strings.HasPrefix(lower, "fw:"):
			root = strings.TrimSpace(root[3:])
		default:
			return "Re: " + root
		}
	}
}

func normalizeAngleAddr(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if !strings.HasPrefix(id, "<") {
		id = "<" + id
	}
	if !strings.HasSuffix(id, ">") {
		id = id + ">"
	}
	return id
}
