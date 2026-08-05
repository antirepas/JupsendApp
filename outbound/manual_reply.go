package outbound

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

// ManualReplyInput is a freeform reply from the contact conversation UI.
type ManualReplyInput struct {
	UserID            int64
	ContactID         int64
	Subject           string
	BodyText          string
	BodyHTML          string
	ReplyToMessageID  int64 // conversation_messages.id to thread against
}

// SendManualReply sends from the mailbox that last emailed this contact (or default).
// Subject/body may contain {{variables}}; they are rendered with this lead's data before send.
func SendManualReply(in ManualReplyInput) (int64, error) {
	contact, contactVars, err := model.GetContactForUser(in.ContactID, in.UserID)
	if err != nil {
		return 0, fmt.Errorf("contact not found")
	}
	subject := strings.TrimSpace(in.Subject)
	bodyHTML := strings.TrimSpace(in.BodyHTML)
	bodyText := strings.TrimSpace(in.BodyText)
	if subject == "" {
		return 0, fmt.Errorf("subject is required")
	}
	if bodyHTML == "" && bodyText == "" {
		return 0, fmt.Errorf("message body is required")
	}

	sourceBody := bodyHTML
	if sourceBody == "" {
		sourceBody = "<p>" + html.EscapeString(bodyText) + "</p>"
	}

	renderedSubj, renderedBody, missing, err := util.RenderEmail(subject, sourceBody, contactVars, util.RenderOptions{
		UserID: in.UserID,
		Ctx:    context.Background(),
	})
	if err != nil {
		return 0, fmt.Errorf("render reply: %w", err)
	}
	if len(missing) > 0 {
		return 0, fmt.Errorf("missing required variable: %s", strings.Join(missing, ", "))
	}
	subject = renderedSubj
	bodyHTML = util.WrapHTMLBody(renderedBody)
	bodyText = util.StripHTML(bodyHTML)
	if strings.TrimSpace(bodyText) == "" {
		return 0, fmt.Errorf("message body is required")
	}

	accountID, _ := model.LatestSMTPAccountForContact(in.UserID, in.ContactID)
	var account model.SMTPAccount
	if accountID > 0 {
		account, err = model.GetSMTPAccount(accountID)
		if err != nil || account.UserID != in.UserID {
			account, err = model.GetSendReadyAccountForUser(in.UserID)
		}
	} else {
		account, err = model.GetSendReadyAccountForUser(in.UserID)
	}
	if err != nil {
		return 0, fmt.Errorf("no sending mailbox available: %w", err)
	}
	model.ResetAccountDailyIfNeeded(&account)
	if !AccountCanSendNow(account) {
		return 0, fmt.Errorf("mailbox daily or rate limit reached — try again later")
	}

	inbound, inboundErr := model.LatestInboundMessage(in.UserID, in.ContactID)
	if in.ReplyToMessageID > 0 {
		if msg, err := model.GetConversationMessageForUser(in.UserID, in.ContactID, in.ReplyToMessageID); err == nil {
			inbound = msg
			inboundErr = nil
		}
	}
	inReplyTo := ""
	references := ""
	if inboundErr == nil {
		inReplyTo = inbound.MessageID
		if !strings.HasPrefix(inReplyTo, "<") && inReplyTo != "" {
			inReplyTo = "<" + inReplyTo + ">"
		}
		references = inReplyTo
		if inbound.InReplyTo != "" {
			ref := inbound.InReplyTo
			if !strings.HasPrefix(ref, "<") {
				ref = "<" + ref + ">"
			}
			references = ref + " " + inReplyTo
		}
	}

	trackID := fmt.Sprintf("%d", util.GenerateID())
	emailSendID, err := model.CreateManualReplyEmailSend(in.UserID, in.ContactID, trackID, account.ID)
	if err != nil {
		return 0, fmt.Errorf("create send: %w", err)
	}

	from := account.SenderEmail()
	if from == "" {
		return 0, fmt.Errorf("mailbox has no sender email")
	}
	smtpPass := account.SMTPPassword
	if account.MailboxSource == "inboxkit" || account.MailboxSource == model.MailboxSourceShared || account.MailboxSource == model.MailboxSourceManual {
		dec, err := model.DecryptSMTPPassword(account)
		if err != nil {
			return 0, fmt.Errorf("mailbox credentials: %w", err)
		}
		smtpPass = dec
	}
	sender := util.NewEmailSender(account.SMTPHost, account.SMTPPort, account.SMTPUser, smtpPass, from)
	messageID := fmt.Sprintf("<%s@%s>", trackID, messageIDDomain(from))
	meta := util.SendMeta{
		MessageID:          messageID,
		EmailTrackerSendID: fmt.Sprintf("%d", emailSendID),
		FromName:           account.FromName,
		InReplyTo:          inReplyTo,
		References:         references,
	}

	var sendErr error
	if account.IsGoogleOAuth() {
		token, err := model.GmailAccessToken(account)
		if err != nil {
			_ = model.MarkEmailSendFailed(emailSendID)
			return 0, fmt.Errorf("gmail oauth: %w", err)
		}
		sendErr = sender.SendWithMetaOAuth(contact.Email, subject, bodyText, bodyHTML, meta, token)
	} else {
		sendErr = sender.SendWithMeta(contact.Email, subject, bodyText, bodyHTML, meta)
	}
	if sendErr != nil {
		_ = model.MarkEmailSendFailed(emailSendID)
		return 0, sendErr
	}

	_ = model.MarkEmailSendSent(emailSendID, account.ID, 0)
	_ = model.SaveEmailSendRenderedContent(emailSendID, subject, bodyHTML, bodyText)
	_, _ = model.InsertConversationMessage(model.ConversationMessageInput{
		UserID:        in.UserID,
		ContactID:     in.ContactID,
		SMTPAccountID: account.ID,
		EmailSendID:   emailSendID,
		Direction:     model.ConversationOutbound,
		FromEmail:     from,
		ToEmail:       contact.Email,
		Subject:       subject,
		BodyText:      bodyText,
		BodyHTML:      bodyHTML,
		MessageID:     messageID,
		InReplyTo:     inReplyTo,
		OccurredAt:    time.Now(),
	})
	MarkAccountSent(account.ID)
	return emailSendID, nil
}
