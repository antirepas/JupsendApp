package util

import "net/smtp"

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

func (s *EmailSender) Send(to, subject, plainBody, htmlBody string) error {
	auth := smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)

	boundary := "my-boundary-123"

	msg := []byte(
		"From: " + s.Config.From + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
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

	return smtp.SendMail(s.Config.Host+":"+s.Config.Port, auth, s.Config.From, []string{to}, msg)
}
