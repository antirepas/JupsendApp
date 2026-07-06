// Extended send diagnostics. Usage: go run ./cmd/diagnose2
package main

import (
	"fmt"
	"log"
	"os"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
)

func main() {
	config.Load()
	db.Prepare()
	defer db.Close()

	userEmail := "akupstas9@gmail.com"
	if len(os.Args) > 1 {
		userEmail = os.Args[1]
	}

	var userID int64
	if err := db.QueryRow(`SELECT id, COALESCE(send_cooldown_days, 30) FROM users WHERE email = ?`, userEmail).Scan(&userID, new(int)); err != nil {
		log.Fatal(err)
	}
	var cooldown int
	_ = db.QueryRow(`SELECT COALESCE(send_cooldown_days, 30) FROM users WHERE id = ?`, userID).Scan(&cooldown)
	fmt.Printf("User %s id=%d cooldown=%d days\n\n", userEmail, userID, cooldown)

	fmt.Println("=== All SMTP accounts ===")
	rows, _ := db.Query(`
		SELECT id, user_id, status, auth_type, COALESCE(google_email,''), sends_today, warmup_enabled, daily_limit
		FROM smtp_accounts ORDER BY id
	`)
	for rows.Next() {
		var id, uid, sendsToday, warmup, daily int
		var status, auth, google string
		rows.Scan(&id, &uid, &status, &auth, &google, &sendsToday, &warmup, &daily)
		fmt.Printf("  account %d user=%d status=%s auth=%s google=%s sends_today=%d warmup=%d\n", id, uid, status, auth, google, sendsToday, warmup)
		if acc, err := model.GetSMTPAccount(int64(id)); err == nil && acc.IsGoogleOAuth() {
			if _, err := model.GmailAccessToken(acc); err != nil {
				fmt.Printf("    OAuth token: FAILED — %v\n", err)
			} else {
				fmt.Printf("    OAuth token: OK\n")
			}
		}
	}
	rows.Close()

	fmt.Println("\n=== Campaign 5 ===")
	var campName, campStatus string
	var isSending int
	var listID interface{}
	err := db.QueryRow(`
		SELECT name, status, COALESCE(is_sending,0), contact_list_id FROM campaigns WHERE id = 5
	`).Scan(&campName, &campStatus, &isSending, &listID)
	if err != nil {
		fmt.Println("  not found:", err)
	} else {
		fmt.Printf("  name=%q status=%s is_sending=%d\n", campName, campStatus, isSending)
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM campaign_contacts WHERE campaign_id = 5`).Scan(&n)
		fmt.Printf("  contacts=%d\n", n)
	}

	fmt.Println("\n=== Queued email_sends (not sent) ===")
	qrows, _ := db.Query(`
		SELECT es.id, COALESCE(c.email,''), es.delivery_status, COALESCE(sj.status,''), COALESCE(sj.last_error,'')
		FROM email_sends es
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ? AND es.delivery_status IN ('queued','sending')
		ORDER BY es.id DESC LIMIT 10
	`, userID)
	found := false
	for qrows.Next() {
		found = true
		var id int64
		var email, delivery, jobStatus, lastErr string
		qrows.Scan(&id, &email, &delivery, &jobStatus, &lastErr)
		fmt.Printf("  send #%d → %s delivery=%s job=%s\n", id, email, delivery, jobStatus)
		if lastErr != "" {
			fmt.Printf("    error: %s\n", lastErr)
		}
	}
	if !found {
		fmt.Println("  (none)")
	}
	qrows.Close()

	fmt.Println("\n=== Pending send_jobs ===")
	prows, _ := db.Query(`
		SELECT sj.id, COALESCE(c.email,''), sj.status, sj.scheduled_at, COALESCE(sj.last_error,'')
		FROM send_jobs sj
		LEFT JOIN contact c ON c.id = sj.contact_id
		WHERE sj.user_id = ? AND sj.status IN ('pending','processing')
		ORDER BY sj.id DESC LIMIT 10
	`, userID)
	found = false
	for prows.Next() {
		found = true
		var id int64
		var email, status, lastErr string
		var sched interface{}
		prows.Scan(&id, &email, &status, &sched, &lastErr)
		fmt.Printf("  job #%d → %s status=%s scheduled=%v\n", id, email, status, sched)
		if lastErr != "" {
			fmt.Printf("    error: %s\n", lastErr)
		}
	}
	if !found {
		fmt.Println("  (none)")
	}
	prows.Close()
}
