package model

import (
	"database/sql"
	"html"
	htmltemplate "html/template"
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
	ID            int64
	UserID        int64
	ContactID     int64
	SMTPAccountID int64
	EmailSendID   int64
	Direction     string
	FromEmail     string
	ToEmail       string
	Subject       string
	BodyText      string
	BodyHTML      string
	MessageID     string
	InReplyTo     string
	OccurredAt    time.Time
	CreatedAt     time.Time
	// Display helpers (not always filled)
	CampaignName string
	OpenCount    int
	ClickCount   int
}

type ConversationMessageInput struct {
	UserID        int64
	ContactID     int64
	SMTPAccountID int64
	EmailSendID   int64
	Direction     string
	FromEmail     string
	ToEmail       string
	Subject       string
	BodyText      string
	BodyHTML      string
	MessageID     string
	InReplyTo     string
	OccurredAt    time.Time
}

func truncateConversationBody(s string) string {
	if len(s) <= MaxConversationBody {
		return s
	}
	// Avoid cutting mid-rune.
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

	if msgID != "" {
		var existing int64
		err := db.QueryRow(`
			SELECT id FROM conversation_messages WHERE user_id = ? AND message_id = ?
		`, in.UserID, msgID).Scan(&existing)
		if err == nil && existing > 0 {
			return existing, nil
		}
	}

	var id int64
	err := db.QueryRow(`
		INSERT INTO conversation_messages (
			user_id, contact_id, smtp_account_id, email_send_id, direction,
			from_email, to_email, subject, body_text, body_html,
			message_id, in_reply_to, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, in.UserID, in.ContactID, in.SMTPAccountID, in.EmailSendID, in.Direction,
		in.FromEmail, in.ToEmail, in.Subject, bodyText, bodyHTML,
		msgID, strings.TrimSpace(in.InReplyTo), occurred).Scan(&id)
	return id, err
}

func ListConversationMessages(userID, contactID int64, limit int) ([]ConversationMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT id, user_id, contact_id, COALESCE(smtp_account_id,0), COALESCE(email_send_id,0),
			direction, from_email, to_email, subject, body_text, body_html,
			COALESCE(message_id,''), COALESCE(in_reply_to,''), occurred_at, created_at
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
			&m.MessageID, &m.InReplyTo, &m.OccurredAt, &m.CreatedAt,
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
			COALESCE(message_id,''), COALESCE(in_reply_to,''), occurred_at, created_at
		FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, userID, contactID)
	var m ConversationMessage
	err := row.Scan(
		&m.ID, &m.UserID, &m.ContactID, &m.SMTPAccountID, &m.EmailSendID,
		&m.Direction, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.BodyHTML,
		&m.MessageID, &m.InReplyTo, &m.OccurredAt, &m.CreatedAt,
	)
	return m, err
}

func HasInboundConversation(userID, contactID int64) bool {
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound'
	`, userID, contactID).Scan(&n)
	return n > 0
}

// LatestSMTPAccountForContact returns the mailbox used for the most recent sent email.
func LatestSMTPAccountForContact(userID, contactID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT COALESCE(smtp_account_id, 0) FROM email_sends
		WHERE user_id = ? AND contact_id = ? AND delivery_status = 'sent' AND COALESCE(smtp_account_id,0) > 0
		ORDER BY sent_at DESC NULLS LAST, id DESC
		LIMIT 1
	`, userID, contactID).Scan(&id)
	return id, err
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

// sanitizeHTMLForDisplay strips scripts/styles for safe embedding (kept in model to avoid util↔model import cycle).
func sanitizeHTMLForDisplay(s string) string {
	if s == "" {
		return ""
	}
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
