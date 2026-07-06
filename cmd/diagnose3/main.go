// Recent send outcomes with job errors. Usage: go run ./cmd/diagnose3 [user-email]
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
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
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, userEmail).Scan(&userID); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Recent sends for %s (id=%d)\n\n", userEmail, userID)
	rows, err := db.Query(`
		SELECT es.id, COALESCE(c.email,''), es.delivery_status, es.sent_at,
			COALESCE(sj.status,''), COALESCE(sj.last_error,'')
		FROM email_sends es
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ?
		ORDER BY es.id DESC LIMIT 15
	`, userID)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var to, delivery, jobStatus, lastErr string
		var sentAt interface{}
		rows.Scan(&id, &to, &delivery, &sentAt, &jobStatus, &lastErr)
		fmt.Printf("#%d → %s | delivery=%s job=%s sent=%v\n", id, to, delivery, jobStatus, sentAt)
		if lastErr != "" {
			fmt.Printf("  job error: %s\n", lastErr)
		}
	}

	fmt.Println("\nRecent failed/dead jobs:")
	frows, _ := db.Query(`
		SELECT sj.id, COALESCE(c.email,''), sj.status, sj.last_error, sj.updated_at
		FROM send_jobs sj
		LEFT JOIN contact c ON c.id = sj.contact_id
		WHERE sj.user_id = ? AND sj.status IN ('failed','dead','pending')
		ORDER BY sj.updated_at DESC LIMIT 10
	`, userID)
	defer frows.Close()
	for frows.Next() {
		var id int64
		var to, status, errMsg string
		var updated interface{}
		frows.Scan(&id, &to, &status, &errMsg, &updated)
		fmt.Printf("  job #%d → %s status=%s updated=%v\n", id, to, status, updated)
		if errMsg != "" {
			fmt.Printf("    %s\n", errMsg)
		}
	}

	fmt.Println("\nGlobal queue stats (last hour updates):")
	var pending, processing, dead int
	_ = db.QueryRow(`SELECT COUNT(*) FROM send_jobs WHERE status='pending'`).Scan(&pending)
	_ = db.QueryRow(`SELECT COUNT(*) FROM send_jobs WHERE status='processing'`).Scan(&processing)
	_ = db.QueryRow(`SELECT COUNT(*) FROM send_jobs WHERE status='dead'`).Scan(&dead)
	fmt.Printf("  pending=%d processing=%d dead=%d (at %s)\n", pending, processing, dead, time.Now().Format(time.RFC3339))
}
