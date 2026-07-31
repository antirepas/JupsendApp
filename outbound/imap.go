package outbound

import (
	"log"
	"strings"
	"time"

	"emailtracker.com/model"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// StartIMAPPoller scans InboxKit mailboxes for bounces/replies via plain IMAP.
// Google OAuth accounts are not polled (gmail.readonly is a restricted scope).
func StartIMAPPoller() {
	LoadConfig()
	go func() {
		pollAllAccounts()
		ticker := time.NewTicker(IMAPPollInterval)
		defer ticker.Stop()
		for range ticker.C {
			pollAllAccounts()
		}
	}()
	log.Printf("IMAP poller started (interval=%s, inboxkit mailboxes only)", IMAPPollInterval)
}

func pollAllAccounts() {
	accounts, err := model.ListActiveSMTPAccounts()
	if err != nil {
		return
	}
	for _, acc := range accounts {
		if acc.Status != "active" {
			continue
		}
		if acc.IsGoogleOAuth() || acc.MailboxSource != "inboxkit" {
			continue
		}
		if acc.IMAPHost == "" || acc.IMAPUser == "" {
			continue
		}
		if err := pollIMAPAccount(acc); err != nil {
			log.Printf("IMAP poll account %d: %v", acc.ID, err)
		}
	}
}

type inboxMessage struct {
	From       string
	Subject    string
	Body       string
	MessageID  string
	InReplyTo  string
	References string
	DedupeID   string
}

func processInboxMessage(acc model.SMTPAccount, ownEmail string, msg inboxMessage) {
	replyRefs := []string{}
	if msg.InReplyTo != "" {
		replyRefs = append(replyRefs, msg.InReplyTo)
	}
	if msg.References != "" {
		replyRefs = append(replyRefs, msg.References)
	}
	if msg.MessageID != "" && len(replyRefs) == 0 {
		replyRefs = append(replyRefs, msg.MessageID)
	}

	if IsBounceMessage(msg.From, msg.Subject, msg.Body) {
		handleBounce(acc, msg.From, msg.Subject, msg.Body)
		return
	}
	if match, ok := MatchReply(acc.UserID, msg.From, msg.Subject, msg.Body, replyRefs, ownEmail); ok {
		handleReply(acc.UserID, match, msg.MessageID)
	}
}

func pollIMAPAccount(acc model.SMTPAccount) error {
	c, err := dialIMAP(acc)
	if err != nil {
		return err
	}
	defer c.Logout()

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return err
	}
	if mbox.Messages == 0 {
		return nil
	}

	from := uint32(1)
	if mbox.Messages > 50 {
		from = mbox.Messages - 49
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem(), imap.FetchFlags}
	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	ownEmail := acc.IMAPUser
	if ownEmail == "" {
		ownEmail = acc.SMTPUser
	}

	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		fromAddr := ""
		if len(msg.Envelope.From) > 0 {
			fromAddr = msg.Envelope.From[0].Address()
		}
		subject := msg.Envelope.Subject
		body := readIMAPMessageBody(msg, section)
		inReplyTo := msg.Envelope.InReplyTo
		processInboxMessage(acc, ownEmail, inboxMessage{
			From:      fromAddr,
			Subject:   subject,
			Body:      body,
			MessageID: msg.Envelope.MessageId,
			InReplyTo: inReplyTo,
			DedupeID:  normalizeMessageID(msg.Envelope.MessageId),
		})
	}

	return <-done
}

func dialIMAP(acc model.SMTPAccount) (*client.Client, error) {
	port := acc.IMAPPort
	if port == "" {
		port = "993"
	}
	addr := acc.IMAPHost + ":" + port
	c, err := client.DialTLS(addr, nil)
	if err != nil {
		return nil, err
	}
	user := acc.IMAPUser
	if user == "" {
		user = acc.SMTPUser
	}
	pass := acc.IMAPPassword
	if pass == "" {
		pass = acc.SMTPPassword
	}
	if acc.MailboxSource == "inboxkit" {
		dec, err := model.DecryptSMTPPassword(acc)
		if err != nil {
			c.Logout()
			return nil, err
		}
		if dec != "" {
			pass = dec
		}
	}
	if err := c.Login(user, pass); err != nil {
		c.Logout()
		return nil, err
	}
	return c, nil
}

// pollAccountBounces kept for tests that may reference it.
func pollAccountBounces(acc model.SMTPAccount) error {
	if acc.IsGoogleOAuth() || acc.MailboxSource != "inboxkit" {
		return nil
	}
	return pollIMAPAccount(acc)
}

func readIMAPMessageBody(msg *imap.Message, section *imap.BodySectionName) string {
	if msg == nil {
		return ""
	}
	r := msg.GetBody(section)
	if r == nil {
		return ""
	}
	b := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(b)
}

func handleBounce(acc model.SMTPAccount, from, subject, body string) {
	sendID := ExtractSendIDFromBounce(body)
	contactID := int64(0)
	trackingID := ""

	if sendID > 0 {
		detail, err := model.GetEmailSendDetail(sendID)
		if err == nil {
			contactID = detail.ContactID
			trackingID = detail.TrackingID
		}
	}

	if contactID == 0 {
		failed := ExtractFailedRecipient(body)
		if failed != "" {
			if c, err := model.FindContactByEmail(acc.UserID, failed); err == nil {
				contactID = c.ID
			}
		}
	}

	if contactID > 0 {
		source := strings.TrimSpace(subject)
		if len(source) > 200 {
			source = source[:200]
		}
		_ = model.SuppressContact(contactID, "bounce", source, acc.ID)
	}

	eventSource := "imap-bounce"
	if trackingID != "" {
		_ = model.StoreEvent(trackingID, "bounce", eventSource, "")
	} else if sendID > 0 {
		detail, err := model.GetEmailSendDetail(sendID)
		if err == nil && detail.TrackingID != "" {
			_ = model.StoreEvent(detail.TrackingID, "bounce", eventSource, "")
		}
	}
}
