package outbound

import (
	"log"
	"strings"

	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// StartIMAPPoller previously scanned Gmail for bounces/replies.
// Disabled: gmail.readonly is a restricted scope (CASA). Sending uses gmail.send only.
func StartIMAPPoller() {
	log.Println("inbox bounce/reply polling disabled (gmail.send only; no gmail.readonly)")
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
		if acc.IsGoogleOAuth() {
			if err := pollGmailAPIAccount(acc); err != nil {
				log.Printf("Gmail API poll account %d: %v", acc.ID, err)
			}
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

func pollGmailAPIAccount(acc model.SMTPAccount) error {
	token, err := model.GmailAccessToken(acc)
	if err != nil {
		return err
	}
	ids, err := googleoauth.ListRecentMessageIDsForPolling(token, 50)
	if err != nil {
		return err
	}
	ownEmail := acc.GoogleEmail
	if ownEmail == "" {
		ownEmail = acc.SMTPUser
	}

	for _, id := range ids {
		// Raw MIME includes delivery-status + original message headers needed for bounces.
		msg, err := googleoauth.GetMessageRaw(token, id)
		if err != nil {
			log.Printf("Gmail API get message %s account %d: %v", id, acc.ID, err)
			continue
		}
		dedupeID := firstNonEmpty(normalizeMessageID(msg.MessageID), msg.ID)
		if dedupeID != "" {
			if done, _ := model.GmailMessageAlreadyProcessed(acc.UserID, dedupeID); done {
				continue
			}
			if exists, _ := model.ContactEventExistsByDedupe("gmail-msg:" + dedupeID); exists {
				_ = model.MarkGmailMessageProcessed(acc.UserID, dedupeID)
				continue
			}
			if exists, _ := model.ContactEventExistsByDedupe("imap-msg:" + dedupeID); exists {
				_ = model.MarkGmailMessageProcessed(acc.UserID, dedupeID)
				continue
			}
		}
		processInboxMessage(acc, ownEmail, inboxMessage{
			From:       googleoauth.ExtractEmailAddress(msg.From),
			Subject:    msg.Subject,
			Body:       msg.Body,
			MessageID:  msg.MessageID,
			InReplyTo:  msg.InReplyTo,
			References: msg.References,
			DedupeID:   dedupeID,
		})
		if dedupeID != "" {
			_ = model.MarkGmailMessageProcessed(acc.UserID, dedupeID)
		}
	}
	return nil
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
	addr := acc.IMAPHost + ":" + acc.IMAPPort
	c, err := client.DialTLS(addr, nil)
	if err != nil {
		return nil, err
	}
	pass := acc.IMAPPassword
	if pass == "" {
		pass = acc.SMTPPassword
	}
	if err := c.Login(acc.IMAPUser, pass); err != nil {
		c.Logout()
		return nil, err
	}
	return c, nil
}

// pollAccountBounces kept for tests that may reference it.
func pollAccountBounces(acc model.SMTPAccount) error {
	if acc.IsGoogleOAuth() {
		return pollGmailAPIAccount(acc)
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

	eventSource := "gmail-bounce"
	if trackingID != "" {
		_ = model.StoreEvent(trackingID, "bounce", eventSource, "")
	} else if sendID > 0 {
		detail, err := model.GetEmailSendDetail(sendID)
		if err == nil && detail.TrackingID != "" {
			_ = model.StoreEvent(detail.TrackingID, "bounce", eventSource, "")
		}
	}
}
