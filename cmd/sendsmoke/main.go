// Send a smoke-test email via the user's connected Gmail account (SMTP XOAUTH2).
// Usage: go run ./cmd/sendsmoke [user-email] [recipient]
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/util"
)

func main() {
	userEmail := "akupstas9@gmail.com"
	recipient := userEmail
	if len(os.Args) > 1 {
		userEmail = os.Args[1]
	}
	if len(os.Args) > 2 {
		recipient = os.Args[2]
	}

	config.Load()
	db.Prepare()
	defer db.Close()

	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, userEmail).Scan(&userID); err != nil {
		log.Fatalf("user: %v", err)
	}
	acc, err := model.GetSendReadyAccountForUser(userID)
	if err != nil {
		log.Fatalf("account: %v", err)
	}
	from := acc.SenderEmail()
	if from == "" {
		log.Fatal("no sender email on account")
	}

	subject := fmt.Sprintf("jupsend smoke test %d", time.Now().Unix())
	plain := "If you receive this, SMTP XOAUTH2 delivery is working from jupsend."
	html := "<p>If you receive this, SMTP XOAUTH2 delivery is working from <strong>jupsend</strong>.</p>"

	meta := util.SendMeta{
		MessageID: fmt.Sprintf("<smoke-%d@%s>", time.Now().UnixNano(), domain(from)),
		FromName:  acc.FromName,
	}

	sender := util.NewEmailSender(acc.SMTPHost, acc.SMTPPort, acc.SMTPUser, acc.SMTPPassword, from)
	fmt.Printf("Sending from %s to %s (auth=%s host=%s:%s)...\n", from, recipient, acc.AuthType, acc.SMTPHost, acc.SMTPPort)

	var sendErr error
	if acc.IsGoogleOAuth() {
		token, err := model.GmailAccessToken(acc)
		if err != nil {
			log.Fatalf("oauth token: %v", err)
		}
		fmt.Println("OAuth access token refreshed OK")
		sendErr = sender.SendWithMetaOAuth(recipient, subject, plain, html, meta, token)
	} else {
		sendErr = sender.SendWithMeta(recipient, subject, plain, html, meta)
	}
	if sendErr != nil {
		log.Fatalf("send failed: %v", sendErr)
	}
	fmt.Println("SMTP accepted the message.")
	fmt.Printf("Check inbox and Gmail Sent for: %s\n", recipient)
	fmt.Printf("Subject: %s\n", subject)
}

func domain(email string) string {
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			return email[i+1:]
		}
	}
	return "localhost"
}
