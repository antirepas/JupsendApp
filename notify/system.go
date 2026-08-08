package notify

import (
	"fmt"
	"html"
	"log"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/util"
)

// SystemSender builds a mailer from env SMTP (shared Free / transactional).
func SystemSender() (*util.EmailSender, error) {
	if strings.TrimSpace(config.SMTPHost) == "" || strings.TrimSpace(config.SMTPUser) == "" || strings.TrimSpace(config.SMTPPass) == "" {
		return nil, fmt.Errorf("system SMTP is not configured (SMTP_HOST / SMTP_USER / APP_PASSWORD)")
	}
	from := config.SMTPFrom
	if from == "" {
		from = config.SMTPUser
	}
	return util.NewEmailSender(config.SMTPHost, config.SMTPPort, config.SMTPUser, config.SMTPPass, from), nil
}

func sendSystem(to, subject, plain, htmlBody string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("empty recipient")
	}
	s, err := SystemSender()
	if err != nil {
		return err
	}
	return s.SendWithMeta(to, subject, plain, htmlBody, util.SendMeta{FromName: "jupsend"})
}

// Async runs fn in a goroutine and logs errors.
func Async(label string, fn func() error) {
	go func() {
		if err := fn(); err != nil {
			log.Printf("notify %s: %v", label, err)
		}
	}()
}

func esc(s string) string { return html.EscapeString(s) }

// NotifyProvisionQueued emails the customer (~2h ETA) and the support inbox.
func NotifyProvisionQueued(userEmail, userLabel, kind, domain string, mailboxEmails []string) {
	Async("provision-queued", func() error {
		return notifyProvisionQueued(userEmail, userLabel, kind, domain, mailboxEmails)
	})
}

func notifyProvisionQueued(userEmail, userLabel, kind, domain string, mailboxEmails []string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "domain & mailboxes"
	}
	domain = strings.TrimSpace(domain)
	mbList := strings.Join(mailboxEmails, ", ")
	if mbList == "" {
		mbList = "(none listed yet)"
	}

	adminSubj := fmt.Sprintf("[jupsend] Manual provision: %s — %s", kind, domain)
	adminPlain := fmt.Sprintf(
		"A Pro customer needs InboxKit fulfillment (~2h queue).\n\nCustomer: %s <%s>\nKind: %s\nDomain: %s\nMailboxes: %s\n\n1) Top up InboxKit wallet\n2) Open %s/ops/provisioning\n3) Click Fulfill\n",
		userLabel, userEmail, kind, domain, mbList, strings.TrimRight(config.BaseURL, "/"),
	)
	adminHTML := fmt.Sprintf(
		`<p>A Pro customer needs InboxKit fulfillment (<strong>~2 hour</strong> queue).</p>
<ul>
<li><strong>Customer:</strong> %s &lt;%s&gt;</li>
<li><strong>Kind:</strong> %s</li>
<li><strong>Domain:</strong> %s</li>
<li><strong>Mailboxes:</strong> %s</li>
</ul>
<ol>
<li>Top up the InboxKit wallet</li>
<li>Open <a href="%s/ops/provisioning">%s/ops/provisioning</a></li>
<li>Click <strong>Fulfill</strong></li>
</ol>`,
		esc(userLabel), esc(userEmail), esc(kind), esc(domain), esc(mbList),
		esc(strings.TrimRight(config.BaseURL, "/")), esc(strings.TrimRight(config.BaseURL, "/")),
	)
	if err := sendSystem(config.SupportEmail, adminSubj, adminPlain, adminHTML); err != nil {
		log.Printf("notify admin provision queued: %v", err)
	}

	if strings.TrimSpace(userEmail) == "" {
		return nil
	}
	userSubj := "We're setting up your jupsend domain (~2 hours)"
	userPlain := fmt.Sprintf(
		"Hi%s,\n\nThanks for choosing Pro. We're provisioning %s for %s now.\n\nThis usually takes about 2 hours. We'll email you again when mailboxes are ready to send.\n\n— jupsend\n",
		nameSuffix(userLabel), kind, domain,
	)
	userHTML := fmt.Sprintf(
		`<p>Hi%s,</p>
<p>Thanks for choosing Pro. We're provisioning <strong>%s</strong> for <strong>%s</strong> now.</p>
<p>This usually takes about <strong>2 hours</strong>. We'll email you again when your mailboxes are ready to send.</p>
<p>Questions? Reply to this email or write <a href="mailto:%s">%s</a>.</p>
<p>— jupsend</p>`,
		esc(nameSuffix(userLabel)), esc(kind), esc(domain), esc(config.SupportEmail), esc(config.SupportEmail),
	)
	return sendSystem(userEmail, userSubj, userPlain, userHTML)
}

// NotifyProvisionReady emails the customer when setup is ready.
func NotifyProvisionReady(userEmail, userLabel, domain string) {
	Async("provision-ready", func() error {
		return notifyProvisionReady(userEmail, userLabel, domain)
	})
}

func notifyProvisionReady(userEmail, userLabel, domain string) error {
	userEmail = strings.TrimSpace(userEmail)
	if userEmail == "" {
		return nil
	}
	domain = strings.TrimSpace(domain)
	subj := "Your jupsend mailboxes are ready"
	plain := fmt.Sprintf(
		"Hi%s,\n\nGood news — setup for %s is complete. Your mailboxes are ready to send.\n\nOpen Mailboxes: %s/mailboxes\n\n— jupsend\n",
		nameSuffix(userLabel), domain, strings.TrimRight(config.BaseURL, "/"),
	)
	htmlBody := fmt.Sprintf(
		`<p>Hi%s,</p>
<p>Good news — setup for <strong>%s</strong> is complete. Your mailboxes are ready to send.</p>
<p><a href="%s/mailboxes">Open Mailboxes</a></p>
<p>— jupsend</p>`,
		esc(nameSuffix(userLabel)), esc(domain), esc(strings.TrimRight(config.BaseURL, "/")),
	)
	return sendSystem(userEmail, subj, plain, htmlBody)
}

func nameSuffix(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return " " + label
}
