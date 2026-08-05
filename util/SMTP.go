package util

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/googleoauth"
)

// DefaultSMTPSendTimeout caps how long a send may block the worker.
var DefaultSMTPSendTimeout = 30 * time.Second

// ProbeSMTPSendTimeout is used for interactive SMTP checks (must finish before reverse-proxy timeouts).
var ProbeSMTPSendTimeout = 10 * time.Second

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

type EmailSender struct {
	Config SMTPConfig
}

func NewEmailSender(host, port, username, password, from string) *EmailSender {
	return &EmailSender{
		Config: SMTPConfig{
			Host:     host,
			Port:     port,
			Username: username,
			Password: password,
			From:     from,
		},
	}
}

type SendMeta struct {
	MessageID          string
	EmailTrackerSendID string
	FromName           string
	InReplyTo          string
	References         string
}

func (s *EmailSender) Send(to, subject, plainBody, htmlBody string) error {
	return s.SendWithMeta(to, subject, plainBody, htmlBody, SendMeta{})
}

func (s *EmailSender) SendWithMeta(to, subject, plainBody, htmlBody string, meta SendMeta) error {
	return s.SendWithMetaAuth(to, subject, plainBody, htmlBody, meta, nil)
}

func (s *EmailSender) SendWithMetaOAuth(to, subject, plainBody, htmlBody string, meta SendMeta, accessToken string) error {
	auth := googleoauth.SMTPAuth(s.Config.From, accessToken)
	return s.sendWithAuth(to, subject, plainBody, htmlBody, meta, auth)
}

func (s *EmailSender) SendWithMetaAuth(to, subject, plainBody, htmlBody string, meta SendMeta, auth smtp.Auth) error {
	if auth == nil {
		auth = smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)
	}
	return s.sendWithAuth(to, subject, plainBody, htmlBody, meta, auth)
}

func (s *EmailSender) sendWithAuth(to, subject, plainBody, htmlBody string, meta SendMeta, auth smtp.Auth) error {
	msg := BuildMultipartEmail(s.Config.From, meta.FromName, to, subject, plainBody, htmlBody, meta)
	return sendMailWithTimeout(s.Config.Host, s.Config.Port, auth, s.Config.From, []string{to}, msg)
}

// ProbeSMTPAuth verifies OAuth SMTP login without sending a message.
func ProbeSMTPAuth(host, port, from, accessToken string) error {
	auth := googleoauth.SMTPAuth(from, accessToken)
	return sendMailAttempt(host, port, auth, from, nil, nil, ProbeSMTPSendTimeout)
}

// ProbeSMTPPlain verifies username/password SMTP login without sending a message.
func ProbeSMTPPlain(host, port, username, password, from string) error {
	password = config.NormalizeAppPassword(password)
	username = strings.TrimSpace(username)
	from = strings.TrimSpace(from)
	if username == "" {
		username = from
	}
	auth := smtp.PlainAuth("", username, password, host)
	return sendMailAttempt(host, port, auth, from, nil, nil, ProbeSMTPSendTimeout)
}

func sendMailWithTimeout(host, port string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return sendMailAttempt(host, port, auth, from, to, msg, DefaultSMTPSendTimeout)
}

func sendMailAttempt(host, port string, auth smtp.Auth, from string, to []string, msg []byte, timeout time.Duration) error {
	port = strings.TrimSpace(port)
	if port == "" {
		port = "587"
	}

	// Prefer the account's configured port first (Free shared SMTP usually 587).
	err := sendMail(host, port, portUsesImplicitTLS(port), auth, from, to, msg, timeout)
	if err == nil {
		return nil
	}

	// Many VPS hosts (and broken IPv6 routes) fail on 465 while 587 STARTTLS works,
	// or the reverse. Always try the alternate Gmail submission port on dial/network errors.
	if shouldTryAlternateSMTPPort(err) {
		altPort, altTLS := alternateSMTPPort(port)
		if altPort != port {
			if err2 := sendMail(host, altPort, altTLS, auth, from, to, msg, timeout); err2 == nil {
				return nil
			}
		}
	}
	return err
}

func portUsesImplicitTLS(port string) bool {
	return port == "465"
}

func alternateSMTPPort(port string) (string, bool) {
	if port == "465" {
		return "587", false
	}
	return "465", true
}

func isGmailHost(host string) bool {
	return strings.EqualFold(host, "smtp.gmail.com")
}

func shouldTryAlternateSMTPPort(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "tls:")
}

// PreferIPv4SMTP avoids dialing Google SMTP over broken IPv6 routes common on some VPS hosts.
var PreferIPv4SMTP = true

func sendMail(host, port string, implicitTLS bool, auth smtp.Auth, from string, to []string, msg []byte, timeout time.Duration) error {
	if port == "" {
		port = "587"
	}
	if timeout <= 0 {
		timeout = DefaultSMTPSendTimeout
	}
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: timeout}

	conn, err := dialSMTP(dialer, addr, host, implicitTLS)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if msg == nil {
		return client.Quit()
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func dialSMTP(dialer *net.Dialer, addr, serverName string, implicitTLS bool) (net.Conn, error) {
	networks := []string{"tcp"}
	if PreferIPv4SMTP {
		// Try IPv4 first — Hostinger and similar hosts often have IPv6 that times out to Google.
		networks = []string{"tcp4", "tcp"}
	}
	var lastErr error
	for _, network := range networks {
		var conn net.Conn
		var err error
		if implicitTLS {
			conn, err = tls.DialWithDialer(dialer, network, addr, &tls.Config{ServerName: serverName})
		} else {
			conn, err = dialer.Dial(network, addr)
		}
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// NormalizeGmailSMTPPort prefers submission port 587 (STARTTLS). Port 465 often fails on
// hosts with broken IPv6 or filtered SMTPS, while Free shared SMTP on 587 still works.
func NormalizeGmailSMTPPort(host, port string) string {
	port = strings.TrimSpace(port)
	if !isGmailHost(host) {
		if port == "" {
			return "587"
		}
		return port
	}
	if port == "" || port == "465" {
		return "587"
	}
	return port
}
