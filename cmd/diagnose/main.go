// One-off send diagnostics. Usage: go run ./cmd/diagnose [user-email]
package main

import (
	"fmt"
	"log"
	"os"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func main() {
	email := "akupstas9@gmail.com"
	if len(os.Args) > 1 {
		email = os.Args[1]
	}
	config.Load()
	if config.DatabaseURL == "" {
		log.Fatal("DATABASE_URL not set")
	}
	db.Prepare()
	defer db.Close()

	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID); err != nil {
		log.Fatalf("user %q: %v", email, err)
	}
	fmt.Printf("User %s (id=%d)\n\n", email, userID)

	fmt.Println("=== SMTP account ===")
	var authType, status, googleEmail, fromEmail string
	var warmupEnabled int
	var sendsToday, dailyLimit, perMin int
	err := db.QueryRow(`
		SELECT auth_type, status, COALESCE(google_email,''), COALESCE(from_email,''),
			warmup_enabled, sends_today, daily_limit, per_minute_limit
		FROM smtp_accounts WHERE user_id = ?
	`, userID).Scan(&authType, &status, &googleEmail, &fromEmail, &warmupEnabled, &sendsToday, &dailyLimit, &perMin)
	if err != nil {
		fmt.Println("  (no smtp account)", err)
	} else {
		fmt.Printf("  auth_type=%s status=%s google=%s from=%s warmup=%d sends_today=%d daily_limit=%d per_min=%d\n",
			authType, status, googleEmail, fromEmail, warmupEnabled, sendsToday, dailyLimit, perMin)
	}

	fmt.Println("\n=== Last 10 sends ===")
	rows, err := db.Query(`
		SELECT es.id, COALESCE(c.email,''), COALESCE(es.delivery_status,''),
			COALESCE(sj.status,''), COALESCE(sj.last_error,''), es.sent_at
		FROM email_sends es
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ?
		ORDER BY es.id DESC LIMIT 10
	`, userID)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var contactEmail, delivery, jobStatus, lastErr string
		var sentAt interface{}
		if err := rows.Scan(&id, &contactEmail, &delivery, &jobStatus, &lastErr, &sentAt); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  send #%d → %s | delivery=%s job=%s sent_at=%v\n", id, contactEmail, delivery, jobStatus, sentAt)
		if lastErr != "" {
			fmt.Printf("    last_error: %s\n", lastErr)
		}
	}

	fmt.Println("\n=== Pending/failed jobs ===")
	rows2, err := db.Query(`
		SELECT sj.id, sj.status, sj.last_error, sj.scheduled_at, COALESCE(c.email,'')
		FROM send_jobs sj
		LEFT JOIN contact c ON c.id = sj.contact_id
		WHERE sj.user_id = ? AND sj.status IN ('pending','processing','failed','dead')
		ORDER BY sj.id DESC LIMIT 10
	`, userID)
	if err != nil {
		log.Fatal(err)
	}
	defer rows2.Close()
	found := false
	for rows2.Next() {
		found = true
		var id int64
		var st, errMsg, contactEmail string
		var sched interface{}
		if err := rows2.Scan(&id, &st, &errMsg, &sched, &contactEmail); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  job #%d → %s status=%s scheduled=%v\n", id, contactEmail, st, sched)
		if errMsg != "" {
			fmt.Printf("    last_error: %s\n", errMsg)
		}
	}
	if !found {
		fmt.Println("  (none)")
	}
}
