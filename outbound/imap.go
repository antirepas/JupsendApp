package outbound

import (
	"io"
	"log"
	"strings"
	"time"

	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func StartIMAPPoller() {
	LoadConfig()
	go func() {
		ticker := time.NewTicker(IMAPPollInterval)
		for range ticker.C {
			pollAllAccounts()
		}
	}()
}

func pollAllAccounts() {
	accounts, err := model.ListActiveSMTPAccounts()
	if err != nil {
		return
	}
	for _, acc := range accounts {
		if acc.Status != "active" || acc.IMAPHost == "" {
			continue
		}
		if !acc.IsGoogleOAuth() && acc.IMAPUser == "" {
			continue
		}
		if err := pollAccount(acc); err != nil {
			log.Printf("IMAP poll account %d: %v", acc.ID, err)
		}
	}
}

func pollAccount(acc model.SMTPAccount) error {
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

	ownEmail := acc.GoogleEmail
	if ownEmail == "" {
		ownEmail = acc.IMAPUser
	}
	if ownEmail == "" {
		ownEmail = acc.SMTPUser
	}

	var seenSeqNums []uint32
	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		fromAddr := ""
		if len(msg.Envelope.From) > 0 {
			fromAddr = msg.Envelope.From[0].Address()
		}
		subject := msg.Envelope.Subject
		body := readMessageBody(msg, section)
		inReplyTo := msg.Envelope.InReplyTo
		if inReplyTo == "" && msg.Envelope.MessageId != "" {
			inReplyTo = msg.Envelope.MessageId
		}
		replyRefs := []string{}
		if inReplyTo != "" {
			replyRefs = append(replyRefs, inReplyTo)
		}

		if IsBounceMessage(fromAddr, subject, body) {
			handleBounce(acc, fromAddr, subject, body)
			seenSeqNums = append(seenSeqNums, msg.SeqNum)
			continue
		}
		if match, ok := MatchReply(acc.UserID, fromAddr, subject, body, replyRefs, ownEmail); ok {
			handleReply(acc.UserID, match)
			seenSeqNums = append(seenSeqNums, msg.SeqNum)
		}
	}

	if err := <-done; err != nil {
		return err
	}

	if len(seenSeqNums) > 0 {
		mark := new(imap.SeqSet)
		for _, n := range seenSeqNums {
			mark.AddNum(n)
		}
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		_ = c.Store(mark, item, []interface{}{imap.SeenFlag}, nil)
	}
	return nil
}

func dialIMAP(acc model.SMTPAccount) (*client.Client, error) {
	addr := acc.IMAPHost + ":" + acc.IMAPPort
	c, err := client.DialTLS(addr, nil)
	if err != nil {
		return nil, err
	}
	if acc.IsGoogleOAuth() {
		token, err := model.GmailAccessToken(acc)
		if err != nil {
			c.Logout()
			return nil, err
		}
		email := acc.GoogleEmail
		if email == "" {
			email = acc.IMAPUser
		}
		if err := c.Authenticate(googleoauth.IMAPAuth(email, token)); err != nil {
			c.Logout()
			return nil, err
		}
	} else {
		pass := acc.IMAPPassword
		if pass == "" {
			pass = acc.SMTPPassword
		}
		if err := c.Login(acc.IMAPUser, pass); err != nil {
			c.Logout()
			return nil, err
		}
	}
	return c, nil
}

// pollAccountBounces kept for tests that may reference it.
func pollAccountBounces(acc model.SMTPAccount) error {
	return pollAccount(acc)
}

func readMessageBody(msg *imap.Message, section *imap.BodySectionName) string {
	if msg == nil {
		return ""
	}
	r := msg.GetBody(section)
	if r == nil {
		return ""
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return ""
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

	if trackingID != "" {
		_ = model.StoreEvent(trackingID, "bounce", "imap-bounce", "")
	} else if sendID > 0 {
		detail, err := model.GetEmailSendDetail(sendID)
		if err == nil && detail.TrackingID != "" {
			_ = model.StoreEvent(detail.TrackingID, "bounce", "imap-bounce", "")
		}
	}
}
