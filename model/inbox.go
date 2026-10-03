package model

import (
	"database/sql"
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
	Type     string // open | click
	At       time.Time
	URL      string
	Subject  string
	Campaign string
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

// InboxFolderCounts holds sidebar folder tallies without scanning full thread lists.
type InboxFolderCounts struct {
	All        int
	Unread     int
	Interested int
	Opened     int
	Clicked    int
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

func inboxSnippet(bodyText string, maxRunes int) string {
	s := strings.TrimSpace(bodyText)
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

func (c InboxFolderCounts) Map() map[string]int {
	return map[string]int{
		InboxFolderAll:        c.All,
		InboxFolderUnread:     c.Unread,
		InboxFolderInterested: c.Interested,
		InboxFolderOpened:     c.Opened,
		InboxFolderClicked:    c.Clicked,
	}
}

// CountInboxFolders returns cheap folder tallies.
func CountInboxFolders(userID int64) InboxFolderCounts {
	var c InboxFolderCounts
	c.Unread = CountInboxUnread(userID)
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT contact_id)::int
		FROM conversation_messages
		WHERE user_id = ? AND direction = 'inbound'
	`, userID).Scan(&c.All)
	_ = db.QueryRow(`
		SELECT COUNT(*)::int FROM (
			SELECT DISTINCT ON (contact_id) reply_sentiment
			FROM conversation_messages
			WHERE user_id = ? AND direction = 'inbound'
			ORDER BY contact_id, occurred_at DESC, id DESC
		) last_in
		WHERE lower(trim(reply_sentiment)) IN ('positive', 'pos', 'interested', 'yes', 'neutral')
	`, userID).Scan(&c.Interested)
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id)::int
		FROM email_sends es
		WHERE es.user_id = ?
		  AND EXISTS (
			SELECT 1 FROM email_events ee
			WHERE ee.email_send_id = es.id AND `+HumanOpenPredicate+`
		  )
		  AND EXISTS (
			SELECT 1 FROM conversation_messages cm
			WHERE cm.user_id = es.user_id AND cm.contact_id = es.contact_id
		  )
	`, userID).Scan(&c.Opened)
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id)::int
		FROM email_sends es
		WHERE es.user_id = ?
		  AND EXISTS (
			SELECT 1 FROM email_events ee
			WHERE ee.email_send_id = es.id AND ee.event_type = 'click'
		  )
		  AND EXISTS (
			SELECT 1 FROM conversation_messages cm
			WHERE cm.user_id = es.user_id AND cm.contact_id = es.contact_id
		  )
	`, userID).Scan(&c.Clicked)
	return c
}

func inboxNeedsEngFilter(folder string) bool {
	return folder == InboxFolderOpened || folder == InboxFolderClicked
}

func attachInboxEngagement(userID int64, threads []InboxThread) {
	if len(threads) == 0 {
		return
	}
	ids := make([]interface{}, 0, len(threads)+1)
	ids = append(ids, userID)
	placeholders := make([]string, 0, len(threads))
	index := map[int64]int{}
	for i, t := range threads {
		ids = append(ids, t.ContactID)
		placeholders = append(placeholders, "?")
		index[t.ContactID] = i
	}
	rows, err := db.Query(`
		SELECT es.contact_id,
			COUNT(*) FILTER (WHERE `+HumanOpenPredicate+`)::int,
			COUNT(*) FILTER (WHERE ee.event_type = 'click')::int
		FROM email_sends es
		INNER JOIN email_events ee ON ee.email_send_id = es.id
		WHERE es.user_id = ?
		  AND es.contact_id IN (`+strings.Join(placeholders, ",")+`)
		  AND (ee.event_type = 'click' OR (`+HumanOpenPredicate+`))
		GROUP BY es.contact_id
	`, ids...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var contactID int64
		var opens, clicks int
		if rows.Scan(&contactID, &opens, &clicks) != nil {
			continue
		}
		if i, ok := index[contactID]; ok {
			threads[i].OpenCount = opens
			threads[i].ClickCount = clicks
		}
	}
}

// ListInboxThreads returns contact-level threads newest-first for the given folder.
func ListInboxThreads(userID int64, folder, query string, limit int) ([]InboxThread, error) {
	folder = NormalizeInboxFolder(folder)
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	query = strings.TrimSpace(query)

	whereExtra := ""
	args := []interface{}{userID, userID, userID} // latest, inbound_last, flags
	engJoin := ""
	engSelectOpens := "0"
	engSelectClicks := "0"
	if inboxNeedsEngFilter(folder) {
		engJoin = `
		LEFT JOIN (
			SELECT
				es.contact_id,
				COUNT(*) FILTER (WHERE ` + HumanOpenPredicate + `)::int AS open_count,
				COUNT(*) FILTER (WHERE ee.event_type = 'click')::int AS click_count
			FROM email_sends es
			INNER JOIN email_events ee ON ee.email_send_id = es.id
			WHERE es.user_id = ?
			  AND (ee.event_type = 'click' OR (` + HumanOpenPredicate + `))
			GROUP BY es.contact_id
		) e ON e.contact_id = latest.contact_id`
		engSelectOpens = "COALESCE(e.open_count, 0)"
		engSelectClicks = "COALESCE(e.click_count, 0)"
	}

	switch folder {
	case InboxFolderAll:
		whereExtra = ` AND f.has_reply`
	case InboxFolderUnread:
		whereExtra = ` AND f.has_reply AND f.unread`
	case InboxFolderInterested:
		whereExtra = ` AND f.has_reply AND lower(trim(COALESCE(i.reply_sentiment,''))) IN ('positive','pos','interested','yes','neutral')`
	case InboxFolderOpened:
		whereExtra = ` AND COALESCE(e.open_count, 0) > 0`
	case InboxFolderClicked:
		whereExtra = ` AND COALESCE(e.click_count, 0) > 0`
	}

	// contact.user_id placeholder
	args = append(args, userID)
	if inboxNeedsEngFilter(folder) {
		args = append(args, userID)
	}
	if query != "" {
		whereExtra += ` AND (c.email ILIKE ? OR COALESCE(latest.subject,'') ILIKE ?)`
		like := "%" + query + "%"
		args = append(args, like, like)
	}
	args = append(args, limit)

	sqlText := `
		WITH latest AS (
			SELECT DISTINCT ON (contact_id)
				contact_id,
				subject,
				LEFT(COALESCE(body_text, ''), 400) AS body_text,
				occurred_at
			FROM conversation_messages
			WHERE user_id = ?
			ORDER BY contact_id, occurred_at DESC, id DESC
		),
		inbound_last AS (
			SELECT DISTINCT ON (contact_id)
				contact_id,
				reply_sentiment
			FROM conversation_messages
			WHERE user_id = ? AND direction = 'inbound'
			ORDER BY contact_id, occurred_at DESC, id DESC
		),
		flags AS (
			SELECT
				contact_id,
				BOOL_OR(direction = 'inbound') AS has_reply,
				BOOL_OR(direction = 'inbound' AND read_at IS NULL) AS unread
			FROM conversation_messages
			WHERE user_id = ?
			GROUP BY contact_id
		)
		SELECT
			c.id,
			COALESCE(c.email, ''),
			COALESCE(latest.subject, ''),
			COALESCE(latest.body_text, ''),
			latest.occurred_at,
			COALESCE(i.reply_sentiment, ''),
			COALESCE(f.unread, FALSE),
			COALESCE(f.has_reply, FALSE),
			` + engSelectOpens + `,
			` + engSelectClicks + `
		FROM latest
		INNER JOIN contact c ON c.id = latest.contact_id AND c.user_id = ?
		INNER JOIN flags f ON f.contact_id = latest.contact_id
		LEFT JOIN inbound_last i ON i.contact_id = latest.contact_id
		` + engJoin + `
		WHERE TRUE
		` + whereExtra + `
		ORDER BY latest.occurred_at DESC, c.id DESC
		LIMIT ?
	`

	rows, err := db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InboxThread, 0, 64)
	for rows.Next() {
		var t InboxThread
		var bodyText string
		if err := rows.Scan(
			&t.ContactID, &t.Email, &t.Subject, &bodyText, &t.LastAt,
			&t.Sentiment, &t.Unread, &t.HasReply, &t.OpenCount, &t.ClickCount,
		); err != nil {
			return nil, err
		}
		t.Sentiment = NormalizeReplySentiment(t.Sentiment)
		t.Snippet = inboxSnippet(bodyText, 120)
		if t.Subject == "" {
			t.Subject = "(no subject)"
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !inboxNeedsEngFilter(folder) {
		attachInboxEngagement(userID, out)
	}
	return out, nil
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
			COUNT(*) FILTER (WHERE `+HumanOpenPredicate+`)::int,
			COUNT(*) FILTER (WHERE ee.event_type = 'click')::int
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.user_id = ? AND es.contact_id = ?
		  AND (ee.event_type = 'click' OR (`+HumanOpenPredicate+`))
	`, userID, contactID).Scan(&opens, &clicks)
	return
}

func listInboxEngagementEvents(userID, contactID int64, limit int) ([]InboxEngagementEvent, error) {
	if limit <= 0 {
		limit = 12
	}
	if limit > 25 {
		limit = 25
	}
	rows, err := db.Query(`
		SELECT ee.event_type, ee.created_at,
			COALESCE(tl.original_url, ''),
			COALESCE(NULLIF(es.rendered_subject, ''), COALESCE(t.subject, '')),
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

func contactRepliedAt(userID, contactID int64) *time.Time {
	var replied sql.NullTime
	_ = db.QueryRow(`SELECT replied_at FROM contact WHERE id = ? AND user_id = ?`, contactID, userID).Scan(&replied)
	if !replied.Valid {
		return nil
	}
	t := replied.Time
	return &t
}

// GetInboxThread loads conversation + engagement for a contact and marks inbound read.
func GetInboxThread(userID, contactID int64) (*InboxThreadDetail, error) {
	contact, _, err := GetContactForUser(contactID, userID)
	if err != nil {
		return nil, err
	}
	EnsureInboundFromReplyEvents(userID, contactID)
	msgs, err := ListContactConversation(userID, contactID, 80)
	if err != nil {
		return nil, err
	}
	opens, clicks := contactEngagementCounts(userID, contactID)
	events, _ := listInboxEngagementEvents(userID, contactID, 12)

	sentiment := ""
	subject := ""
	var lastInboundAt time.Time
	for _, m := range msgs {
		if m.Direction != ConversationInbound {
			continue
		}
		if m.OccurredAt.After(lastInboundAt) {
			lastInboundAt = m.OccurredAt
			sentiment = m.ReplySentiment
			if strings.TrimSpace(m.Subject) != "" {
				subject = m.Subject
			}
		} else if subject == "" && strings.TrimSpace(m.Subject) != "" {
			subject = m.Subject
		}
	}

	_ = MarkInboxThreadRead(userID, contactID)

	detail := &InboxThreadDetail{
		Contact:       contact,
		Messages:      msgs,
		OpenCount:     opens,
		ClickCount:    clicks,
		RecentEvents:  events,
		NeedsRecovery: ContactNeedsInboundBody(userID, contactID),
		CanReply:      CanReplyInApp(userID, contactID, contactRepliedAt(userID, contactID)),
		Unread:        false,
		Sentiment:     NormalizeReplySentiment(sentiment),
		Subject:       subject,
	}
	if detail.Subject == "" {
		detail.Subject = "(no subject)"
	}
	return detail, nil
}
