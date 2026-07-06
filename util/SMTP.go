package util

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

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

	boundary := "my-boundary-123"

	fromHeader := s.Config.From
	if meta.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", meta.FromName, s.Config.From)
	}

	headers := "From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n"

	if meta.MessageID != "" {
		headers += "Message-ID: " + meta.MessageID + "\r\n"
	}
	if meta.EmailTrackerSendID != "" {
		headers += "X-EmailTracker-Send-ID: " + meta.EmailTrackerSendID + "\r\n"
	}

	msg := []byte(headers +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		plainBody + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		htmlBody + "\r\n" +
		"--" + boundary + "--\r\n",
	)

	return sendMailWithTimeout(s.Config.Host, s.Config.Port, auth, s.Config.From, []string{to}, msg)
}

// ProbeSMTPAuth verifies OAuth SMTP login without sending a message.
func ProbeSMTPAuth(host, port, from, accessToken string) error {
	auth := googleoauth.SMTPAuth(from, accessToken)
	return sendMailAttempt(host, port, auth, from, nil, nil, ProbeSMTPSendTimeout)
}

func sendMailWithTimeout(host, port string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return sendMailAttempt(host, port, auth, from, to, msg, DefaultSMTPSendTimeout)
}

func sendMailAttempt(host, port string, auth smtp.Auth, from string, to []string, msg []byte, timeout time.Duration) error {
	if isGmailHost(host) {
		if err := sendMail(host, "465", true, auth, from, to, msg, timeout); err == nil {
			return nil
		}
	}
	err := sendMail(host, port, false, auth, from, to, msg, timeout)
	if err == nil {
		return nil
	}
	if (port == "" || port == "587") && shouldTrySMTP465(err) {
		if err465 := sendMail(host, "465", true, auth, from, to, msg, timeout); err465 == nil {
			return nil
		}
	}
	return err
}

func isGmailHost(host string) bool {
	return strings.EqualFold(host, "smtp.gmail.com")
}

func shouldTrySMTP465(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable")
}

func sendMail(host, port string, implicitTLS bool, auth smtp.Auth, from string, to []string, msg []byte, timeout time.Duration) error {
	if port == "" {
		port = "587"
	}
	if timeout <= 0 {
		timeout = DefaultSMTPSendTimeout
	}
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: timeout}

	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
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
