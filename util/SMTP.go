package util

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"

	"emailtracker.com/googleoauth"
)

// DefaultSMTPSendTimeout caps how long a send may block the worker.
var DefaultSMTPSendTimeout = 30 * time.Second

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

func sendMailWithTimeout(host, port string, auth smtp.Auth, from string, to []string, msg []byte) error {
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: DefaultSMTPSendTimeout}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(DefaultSMTPSendTimeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
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
