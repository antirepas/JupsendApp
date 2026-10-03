package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"emailtracker.com/db"
)

const (
	InboxFolderAll        = "all"
	InboxFolderUnread     = "unread"
	InboxFolderInterested = "interested"
	InboxFolderOpened     = "opened"
	InboxFolderClicked    = "clicked"
)

// InboxThread is one contact-level conversation row in the unified inbox.
type InboxThread struct {
	ContactID  int64
	Email      string
	Subject    string
	Snippet    string
	LastAt     time.Time
	Unread     bool
	Sentiment  string
	OpenCount  int
	ClickCount int
	HasReply   bool
}

// InboxEngagementEvent is a recent open/click shown in the reading pane.
type InboxEngagementEvent struct {
	Type      string // open | click
	At        time.Time
	URL       string
	Subject   string
	Campaign  string
}

// InboxThreadDetail is the reading-pane payload for one contact.
type InboxThreadDetail struct {
	Contact       Contact
	Messages      []ConversationMessage
	OpenCount     int
	ClickCount    int
	RecentEvents  []InboxEngagementEvent
	NeedsRecovery bool
	CanReply      bool
	Unread        bool
	Sentiment     string
	Subject       string
}

// NormalizeInboxFolder returns a known inbox folder key.
func NormalizeInboxFolder(folder string) string {
	switch strings.ToLower(strings.TrimSpace(folder)) {
	case InboxFolderUnread:
		return InboxFolderUnread
	case InboxFolderInterested:
		return InboxFolderInterested
	case InboxFolderOpened:
		return InboxFolderOpened
	case InboxFolderClicked:
		return InboxFolderClicked
	default:
		return InboxFolderAll
	}
}

func inboxSnippet(bodyText, bodyHTML string, maxRunes int) string {
	s := strings.TrimSpace(bodyText)
	if s == "" {
		s = strings.TrimSpace(stripTagsApprox(bodyHTML))
	}
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 {
		maxRunes = 120
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

func stripTagsApprox(html string) string {
	if html == "" {
		return ""
	}
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func contactInitial(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "?"
	}
	r, _ := utf8.DecodeRuneInString(email)
	if r == utf8.RuneError {
		return "?"
	}
	return strings.ToUpper(string(r))
}

// Initial is used from templates for avatar letters.
func (t InboxThread) Initial() string {
	return contactInitial(t.Email)
}

// ListInboxThreads returns contact-level threads newest-first for the given folder.
func ListInboxThreads(userID int64, folder, query string, limit int) ([]InboxThread, error) {
	folder = NormalizeInboxFolder(folder)
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	query = strings.TrimSpace(query)

	rows, err := db.Query(`
		SELECT
			c.id,
			COALESCE(c.email, ''),
			COALESCE(latest.subject, ''),
			COALESCE(latest.body_text, ''),
			COALESCE(latest.body_html, ''),
			latest.occurred_at,
			COALESCE(inbound_sent.reply_sentiment, ''),
			EXISTS (
				SELECT 1 FROM conversation_messages u
				WHERE u.user_id = ? AND u.contact_id = c.id
				  AND u.direction = 'inbound' AND u.read_at IS NULL
			) AS unread,
			EXISTS (
				SELECT 1 FROM conversation_messages r
				WHERE r.user_id = ? AND r.contact_id = c.id AND r.direction = 'inbound'
			) AS has_reply,
			COALESCE((
				SELECT COUNT(*)::int FROM email_events ee
				INNER JOIN email_sends es ON es.id = ee.email_send_id
				WHERE es.user_id = ? AND es.contact_id = c.id AND `+HumanOpenPredicate+`
			), 0) AS open_count,
			COALESCE((
				SELECT COUNT(*)::int FROM email_events ee
				INNER JOIN email_sends es ON es.id = ee.email_send_id
				WHERE es.user_id = ? AND es.contact_id = c.id AND ee.event_type = 'click'
			), 0) AS click_count
		FROM contact c
		INNER JOIN LATERAL (
			SELECT subject, body_text, body_html, occurred_at
			FROM conversation_messages cm
			WHERE cm.user_id = ? AND cm.contact_id = c.id
			ORDER BY cm.occurred_at DESC, cm.id DESC
			LIMIT 1
		) latest ON TRUE
		LEFT JOIN LATERAL (
			SELECT reply_sentiment
			FROM conversation_messages cm
			WHERE cm.user_id = ? AND cm.contact_id = c.id AND cm.direction = 'inbound'
			ORDER BY cm.occurred_at DESC, cm.id DESC
			LIMIT 1
		) inbound_sent ON TRUE
		WHERE c.user_id = ?
		  AND EXISTS (
			SELECT 1 FROM conversation_messages cm0
			WHERE cm0.user_id = ? AND cm0.contact_id = c.id
		  )
		ORDER BY latest.occurred_at DESC, c.id DESC
	`, userID, userID, userID, userID, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InboxThread, 0, 64)
	for rows.Next() {
		var t InboxThread
		var bodyText, bodyHTML string
		var lastAt time.Time
		if err := rows.Scan(
			&t.ContactID, &t.Email, &t.Subject, &bodyText, &bodyHTML, &lastAt,
			&t.Sentiment, &t.Unread, &t.HasReply, &t.OpenCount, &t.ClickCount,
		); err != nil {
			return nil, err
		}
		t.LastAt = lastAt
		t.Sentiment = NormalizeReplySentiment(t.Sentiment)
		t.Snippet = inboxSnippet(bodyText, bodyHTML, 120)
		if t.Subject == "" {
			t.Subject = "(no subject)"
		}

		if query != "" {
			q := strings.ToLower(query)
			hay := strings.ToLower(t.Email + " " + t.Subject + " " + t.Snippet)
			if !strings.Contains(hay, q) {
				continue
			}
		}

		switch folder {
		case InboxFolderAll:
			if !t.HasReply {
				continue
			}
		case InboxFolderUnread:
			if !t.Unread || !t.HasReply {
				continue
			}
		case InboxFolderInterested:
			if !t.HasReply {
				continue
			}
			s := NormalizeReplySentiment(t.Sentiment)
			if s != ReplySentimentPositive && s != ReplySentimentNeutral {
				continue
			}
		case InboxFolderOpened:
			if t.OpenCount <= 0 {
				continue
			}
		case InboxFolderClicked:
			if t.ClickCount <= 0 {
				continue
			}
		}

		out = append(out, t)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// CountInboxUnread returns contacts with at least one unread inbound message.
func CountInboxUnread(userID int64) int {
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT contact_id)::int
		FROM conversation_messages
		WHERE user_id = ? AND direction = 'inbound' AND read_at IS NULL
	`, userID).Scan(&n)
	return n
}

// MarkInboxThreadRead sets read_at on all unread inbound messages for the contact.
func MarkInboxThreadRead(userID, contactID int64) error {
	if userID <= 0 || contactID <= 0 {
		return fmt.Errorf("invalid ids")
	}
	if _, _, err := GetContactForUser(contactID, userID); err != nil {
		return err
	}
	_, err := db.Exec(`
		UPDATE conversation_messages
		SET read_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND contact_id = ? AND direction = 'inbound' AND read_at IS NULL
	`, userID, contactID)
	return err
}

func contactEngagementCounts(userID, contactID int64) (opens, clicks int) {
	_ = db.QueryRow(`
		SELECT
			COALESCE((
				SELECT COUNT(*)::int FROM email_events ee
				INNER JOIN email_sends es ON es.id = ee.email_send_id
				WHERE es.user_id = ? AND es.contact_id = ? AND `+HumanOpenPredicate+`
			), 0),
			COALESCE((
				SELECT COUNT(*)::int FROM email_events ee
				INNER JOIN email_sends es ON es.id = ee.email_send_id
				WHERE es.user_id = ? AND es.contact_id = ? AND ee.event_type = 'click'
			), 0)
	`, userID, contactID, userID, contactID).Scan(&opens, &clicks)
	return
}

func listInboxEngagementEvents(userID, contactID int64, limit int) ([]InboxEngagementEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT ee.event_type, ee.created_at,
			COALESCE(tl.original_url, ''),
			COALESCE(es.rendered_subject, t.subject, ''),
			COALESCE(camp.name, '')
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		LEFT JOIN tracked_links tl ON tl.tracking_id = ee.tracking_id
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
		WHERE es.user_id = ? AND es.contact_id = ?
		  AND (
			(`+HumanOpenPredicate+`)
			OR ee.event_type = 'click'
		  )
		ORDER BY ee.created_at DESC
		LIMIT ?
	`, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxEngagementEvent
	for rows.Next() {
		var e InboxEngagementEvent
		if err := rows.Scan(&e.Type, &e.At, &e.URL, &e.Subject, &e.Campaign); err != nil {
			return nil, err
		}
		e.Type = strings.ToLower(strings.TrimSpace(e.Type))
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetInboxThread loads conversation + engagement for a contact and marks inbound read.
func GetInboxThread(userID, contactID int64) (*InboxThreadDetail, error) {
	contact, _, err := GetContactForUser(contactID, userID)
	if err != nil {
		return nil, err
	}
	EnsureInboundFromReplyEvents(userID, contactID)
	msgs, err := ListContactConversation(userID, contactID, 200)
	if err != nil {
		return nil, err
	}
	opens, clicks := contactEngagementCounts(userID, contactID)
	events, _ := listInboxEngagementEvents(userID, contactID, 25)

	unread := false
	sentiment := ""
	subject := ""
	var lastInboundAt time.Time
	for _, m := range msgs {
		if m.Direction == ConversationInbound {
			if subject == "" && strings.TrimSpace(m.Subject) != "" {
				subject = m.Subject
			}
			if m.OccurredAt.After(lastInboundAt) {
				lastInboundAt = m.OccurredAt
				sentiment = m.ReplySentiment
				if strings.TrimSpace(m.Subject) != "" {
					subject = m.Subject
				}
			}
		}
	}
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM conversation_messages
			WHERE user_id = ? AND contact_id = ? AND direction = 'inbound' AND read_at IS NULL
		)
	`, userID, contactID).Scan(&unread)

	if err := MarkInboxThreadRead(userID, contactID); err != nil {
		return nil, err
	}
	unread = false

	summary, _ := GetContactSummary(userID, contactID)
	repliedAt := summary.RepliedAt
	detail := &InboxThreadDetail{
		Contact:       contact,
		Messages:      msgs,
		OpenCount:     opens,
		ClickCount:    clicks,
		RecentEvents:  events,
		NeedsRecovery: ContactNeedsInboundBody(userID, contactID),
		CanReply:      CanReplyInApp(userID, contactID, repliedAt),
		Unread:        unread,
		Sentiment:     NormalizeReplySentiment(sentiment),
		Subject:       subject,
	}
	if detail.Subject == "" {
		detail.Subject = "(no subject)"
	}
	return detail, nil
}
