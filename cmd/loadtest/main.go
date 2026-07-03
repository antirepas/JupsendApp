package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/internal/fakesmtp"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
)

func main() {
	users := flag.Int("users", 10, "number of simulated sender accounts")
	jobsPerUser := flag.Int("jobs", 20, "send jobs to enqueue per user")
	smtpDelay := flag.Duration("smtp-delay", 50*time.Millisecond, "simulated SMTP round-trip per message")
	timeout := flag.Duration("timeout", 10*time.Minute, "max time to wait for queue drain")
	cleanup := flag.Bool("cleanup", true, "remove loadtest users and data when finished")
	smtpAddr := flag.String("smtp-addr", "127.0.0.1:0", "fake SMTP listen address")
	flag.Parse()

	config.Load()
	if config.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	// Speed up worker for load testing (must be set before outbound.LoadConfig).
	_ = os.Setenv("OUTBOUND_WORKER_INTERVAL", "1")
	if v := os.Getenv("OUTBOUND_MAX_CONCURRENT"); v == "" {
		_ = os.Setenv("OUTBOUND_MAX_CONCURRENT", "32")
	}
	if v := os.Getenv("OUTBOUND_BATCH_SIZE"); v == "" {
		_ = os.Setenv("OUTBOUND_BATCH_SIZE", "50")
	}

	db.Prepare()
	defer db.Close()

	host, port := splitHostPort(*smtpAddr)
	smtp, err := fakesmtp.Start(netJoin(host, port), *smtpDelay)
	if err != nil {
		log.Fatal(err)
	}
	defer smtp.Close()
	smtpHost, smtpPort := splitHostPort(smtp.Addr)
	log.Printf("fake SMTP listening on %s (delay %s)", smtp.Addr, smtpDelay)

	runID := time.Now().Unix()
	prefix := fmt.Sprintf("loadtest-%d", runID)
	totalJobs := *users * *jobsPerUser

	log.Printf("seeding %d users x %d jobs = %d total...", *users, *jobsPerUser, totalJobs)
	seedStart := time.Now()
	userIDs, err := seedLoadTest(prefix, smtpHost, smtpPort, *users, *jobsPerUser)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("seeded in %s", time.Since(seedStart).Round(time.Millisecond))

	outbound.LoadConfig()
	outbound.StartWorker()

	log.Printf("worker running (max_concurrent=%d, batch=%d)...", outbound.MaxConcurrent, outbound.BatchSize)
	waitStart := time.Now()
	deadline := time.Now().Add(*timeout)
	var last model.GlobalSendJobStats
	lastLog := time.Now()

	for time.Now().Before(deadline) {
		stats, err := model.GetSendJobStatsForUsers(userIDs)
		if err != nil {
			log.Fatal(err)
		}
		last = stats
		ready, _ := countReadyLoadTestJobs(userIDs)
		if ready == 0 && stats.Processing == 0 {
			break
		}
		if time.Since(lastLog) >= 5*time.Second {
			log.Printf("queue: ready=%d pending=%d processing=%d dead=%d", ready, stats.Pending, stats.Processing, stats.Dead)
			lastLog = time.Now()
		}
		time.Sleep(500 * time.Millisecond)
	}

	elapsed := time.Since(waitStart)
	sent, _ := countLoadTestOutcomes(prefix)
	smtpAccepted := smtp.Accepted.Load()

	fmt.Println()
	fmt.Println("=== jupsend load test results ===")
	fmt.Printf("Users:              %d\n", *users)
	fmt.Printf("Jobs enqueued:      %d\n", totalJobs)
	fmt.Printf("SMTP delay:         %s\n", smtpDelay)
	fmt.Printf("Max concurrent:     %d\n", outbound.MaxConcurrent)
	fmt.Printf("Drain time:         %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Jobs sent (DB):     %d\n", sent)
	fmt.Printf("Jobs failed/dead:   %d (dead=%d failed=%d)\n", last.Dead+last.Failed, last.Dead, last.Failed)
	fmt.Printf("SMTP accepted:      %d\n", smtpAccepted)
	if elapsed > 0 && sent > 0 {
		fmt.Printf("Throughput:         %.1f sends/sec\n", float64(sent)/elapsed.Seconds())
		fmt.Printf("Throughput:         %.0f sends/min\n", float64(sent)/elapsed.Minutes())
	}
	if last.Pending+last.Processing > 0 {
		fmt.Printf("WARNING:            %d jobs still queued after timeout\n", last.Pending+last.Processing)
	}
	fmt.Println()
	fmt.Println("Notes:")
	fmt.Println("- Uses a local fake SMTP server (not Gmail).")
	fmt.Println("- Rate limits are disabled on loadtest accounts.")
	fmt.Println("- Real Gmail throughput will be lower (per-account limits + network).")
	fmt.Println("- Run one app instance; multi-instance needs P1 DB rate limits.")

	if *cleanup {
		log.Printf("cleaning up %d loadtest users...", len(userIDs))
		if err := cleanupLoadTest(userIDs); err != nil {
			log.Printf("cleanup warning: %v", err)
		}
	} else {
		log.Printf("loadtest users kept (prefix %s)", prefix)
	}
}

func seedLoadTest(prefix, smtpHost, smtpPort string, users, jobsPerUser int) ([]int64, error) {
	var userIDs []int64
	for i := 0; i < users; i++ {
		email := fmt.Sprintf("%s-user%d@loadtest.local", prefix, i)
		userID, err := model.CreateUser(email, "loadtest", "http://localhost:8080")
		if err != nil {
			return nil, fmt.Errorf("create user %s: %w", email, err)
		}
		userIDs = append(userIDs, userID)

		_, err = model.CreateSMTPAccountForUser(userID, model.SMTPAccount{
			Name:                   "LoadTest",
			SMTPHost:               smtpHost,
			SMTPPort:               smtpPort,
			SMTPUser:               email,
			SMTPPassword:           "loadtest",
			FromEmail:              email,
			FromName:               "Load Test",
			Status:                 "active",
			DailyLimit:             1_000_000,
			PerMinuteLimit:         1_000_000,
			MinSecondsBetweenSends: 0,
			WarmupEnabled:          false,
		})
		if err != nil {
			return nil, err
		}
		_, err = db.Exec(`
			UPDATE smtp_accounts SET
				per_minute_limit = 1000000,
				min_seconds_between_sends = 0,
				daily_limit = 1000000,
				warmup_enabled = 0,
				status = 'active'
			WHERE user_id = ?
		`, userID)
		if err != nil {
			return nil, err
		}

		var templateID int64
		err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('Load','Hi','Hello', ?) RETURNING id`, userID).Scan(&templateID)
		if err != nil {
			return nil, err
		}

		for j := 0; j < jobsPerUser; j++ {
			var contactID int64
			contactEmail := fmt.Sprintf("%s-contact%d-%d@example.com", prefix, i, j)
			err = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES (?, ?) RETURNING id`, contactEmail, userID).Scan(&contactID)
			if err != nil {
				return nil, err
			}
			_, _, err = outbound.EnqueueSend(outbound.EnqueueInput{
				UserID:     userID,
				ContactID:  contactID,
				TemplateID: templateID,
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return userIDs, nil
}

func countLoadTestOutcomes(prefix string) (sent, failed int) {
	like := prefix + "%"
	_ = db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN es.delivery_status = 'sent' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN es.delivery_status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		INNER JOIN users u ON u.id = es.user_id
		WHERE u.email LIKE ?
	`, like).Scan(&sent, &failed)
	return sent, failed
}

func countReadyLoadTestJobs(userIDs []int64) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(userIDs))
	args := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM send_jobs
		WHERE user_id IN (`+strings.Join(placeholders, ",")+`)
			AND status IN ('pending', 'processing')
			AND scheduled_at <= CURRENT_TIMESTAMP
	`, args...).Scan(&n)
	return n, err
}

func cleanupLoadTest(userIDs []int64) error {
	for _, userID := range userIDs {
		_, _ = db.Exec(`DELETE FROM send_jobs WHERE user_id = ?`, userID)
		_, _ = db.Exec(`DELETE FROM email_sends WHERE user_id = ?`, userID)
		_, _ = db.Exec(`DELETE FROM contact WHERE user_id = ?`, userID)
		_, _ = db.Exec(`DELETE FROM template WHERE user_id = ?`, userID)
		_, _ = db.Exec(`DELETE FROM smtp_accounts WHERE user_id = ?`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	}
	return nil
}

func splitHostPort(addr string) (string, string) {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i], addr[i+1:]
	}
	return addr, "0"
}

func netJoin(host, port string) string {
	if port == "0" || port == "" {
		return host + ":0"
	}
	return host + ":" + port
}
