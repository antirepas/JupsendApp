package outbound

import (
	"io"
	"log"
	"strings"
	"time"

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
		if acc.Status != "active" || acc.IMAPHost == "" || acc.IMAPUser == "" {
			continue
		}
		if err := pollAccountBounces(acc); err != nil {
			log.Printf("IMAP poll account %d: %v", acc.ID, err)
		}
	}
}

func pollAccountBounces(acc model.SMTPAccount) error {
	addr := acc.IMAPHost + ":" + acc.IMAPPort
	c, err := client.DialTLS(addr, nil)
	if err != nil {
		return err
	}
	defer c.Logout()

	pass := acc.IMAPPassword
	if pass == "" {
		pass = acc.SMTPPassword
	}
	if err := c.Login(acc.IMAPUser, pass); err != nil {
		return err
	}

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

	var bounceSeqNums []uint32
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
		if !IsBounceMessage(fromAddr, subject, body) {
			continue
		}
		handleBounce(acc, fromAddr, subject, body)
		bounceSeqNums = append(bounceSeqNums, msg.SeqNum)
	}

	if err := <-done; err != nil {
		return err
	}

	if len(bounceSeqNums) > 0 {
		mark := new(imap.SeqSet)
		for _, n := range bounceSeqNums {
			mark.AddNum(n)
		}
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		_ = c.Store(mark, item, []interface{}{imap.SeenFlag}, nil)
	}
	return nil
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
