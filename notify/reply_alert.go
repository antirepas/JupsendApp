package notify

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"emailtracker.com/config"
	"emailtracker.com/model"
)

// ReplyAlertInput is a new inbound reply to notify the account owner about.
type ReplyAlertInput struct {
	UserID        int64
	ContactID     int64
	FromEmail     string
	Subject       string
	BodySnippet   string
	MailboxEmail  string
}

// NotifyReplyAlert emails the user's signup address when notify_on_reply is enabled.
func NotifyReplyAlert(in ReplyAlertInput) {
	if in.UserID <= 0 || !model.UserNotifyOnReply(in.UserID) {
		return
	}
	Async("reply-alert", func() error {
		return notifyReplyAlert(in)
	})
}

func notifyReplyAlert(in ReplyAlertInput) error {
	user, err := model.GetUserByID(in.UserID)
	if err != nil {
		return err
	}
	to := strings.TrimSpace(user.Email)
	if to == "" {
		return nil
	}

	contactEmail := ""
	if in.ContactID > 0 {
		if c, _, err := model.GetContact(in.ContactID); err == nil {
			contactEmail = c.Email
		}
	}
	from := strings.TrimSpace(in.FromEmail)
	if from == "" {
		from = contactEmail
	}
	subjLine := strings.TrimSpace(in.Subject)
	if subjLine == "" {
		subjLine = "(no subject)"
	}
	snippet := compactSnippet(in.BodySnippet, 280)
	base := strings.TrimRight(model.UserBaseURL(in.UserID), "/")
	if base == "" {
		base = strings.TrimRight(config.BaseURL, "/")
	}
	contactURL := fmt.Sprintf("%s/contacts/%d", base, in.ContactID)
	mailbox := strings.TrimSpace(in.MailboxEmail)

	alertSubj := fmt.Sprintf("New reply from %s", from)
	plain := fmt.Sprintf(
		"You got a reply on jupsend.\n\nFrom: %s\nSubject: %s\nMailbox: %s\n\n%s\n\nOpen conversation: %s\n\n— jupsend\n",
		from, subjLine, mailboxOrDash(mailbox), snippet, contactURL,
	)
	htmlBody := fmt.Sprintf(
		`<p>You got a reply on jupsend.</p>
<ul>
<li><strong>From:</strong> %s</li>
<li><strong>Subject:</strong> %s</li>
<li><strong>Mailbox:</strong> %s</li>
</ul>
%s
<p><a href="%s">Open conversation</a></p>
<p>— jupsend</p>`,
		esc(from), esc(subjLine), esc(mailboxOrDash(mailbox)),
		snippetHTML(snippet),
		esc(contactURL),
	)
	return sendSystem(to, alertSubj, plain, htmlBody)
}

func mailboxOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func compactSnippet(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func snippetHTML(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "<p style=\"white-space:pre-wrap;border-left:3px solid #e5e7eb;padding-left:12px;color:#374151;\">" + esc(s) + "</p>"
}
