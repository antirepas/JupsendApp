// Report recent user product activity. Usage: go run ./cmd/useractivity [limit]
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

type userRow struct {
	ID        int64
	Email     string
	PlanTier  string
	IsAdmin   bool
	CreatedAt time.Time
}

func main() {
	limit := 25
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil && n > 0 {
			limit = n
		}
	}
	config.Load()
	if config.DatabaseURL == "" {
		log.Fatal("DATABASE_URL not set")
	}
	db.Prepare()
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, email, COALESCE(plan_tier, 'free'), COALESCE(is_admin, false), created_at
		FROM users
		WHERE email NOT LIKE '%@loadtest.local'
		  AND email NOT LIKE '%@localhost.local'
		  AND email NOT LIKE 'loadtest-%'
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var users []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Email, &u.PlanTier, &u.IsAdmin, &u.CreatedAt); err != nil {
			log.Fatal(err)
		}
		users = append(users, u)
	}

	fmt.Printf("Recent users (newest %d)\n", len(users))
	fmt.Printf("Generated at %s\n\n", time.Now().Format(time.RFC3339))

	for _, u := range users {
		printUser(u)
	}
}

func printUser(u userRow) {
	admin := ""
	if u.IsAdmin {
		admin = " [admin]"
	}
	fmt.Printf("=== %s%s | plan=%s | id=%d | joined=%s ===\n",
		u.Email, admin, u.PlanTier, u.ID, u.CreatedAt.Format("2006-01-02 15:04"))

	contacts := count(u.ID, `SELECT COUNT(*) FROM contact WHERE user_id = ?`)
	lists := count(u.ID, `SELECT COUNT(*) FROM contact_lists WHERE user_id = ?`)
	templates := count(u.ID, `SELECT COUNT(*) FROM template WHERE user_id = ?`)
	campaigns := count(u.ID, `SELECT COUNT(*) FROM campaigns WHERE user_id = ?`)
	smtpReady := count(u.ID, `SELECT COUNT(*) FROM smtp_accounts WHERE user_id = ? AND status = 'active'`)
	smtpAll := count(u.ID, `SELECT COUNT(*) FROM smtp_accounts WHERE user_id = ?`)
	sends := count(u.ID, `SELECT COUNT(*) FROM email_sends WHERE user_id = ?`)
	sendsSent := count(u.ID, `SELECT COUNT(*) FROM email_sends WHERE user_id = ? AND delivery_status = 'sent'`)
	jobs := count(u.ID, `SELECT COUNT(*) FROM send_jobs WHERE user_id = ?`)
	workflows := count(u.ID, `SELECT COUNT(*) FROM workflows WHERE user_id = ?`)
	outreachDomains := count(u.ID, `SELECT COUNT(*) FROM outreach_domains WHERE user_id = ?`)
	outreachMboxes := count(u.ID, `SELECT COUNT(*) FROM outreach_mailboxes WHERE user_id = ?`)

	printSMTPDetails(u.ID)

	lastSend := nullTime(u.ID, `
		SELECT MAX(sent_at) FROM email_sends WHERE user_id = ? AND delivery_status = 'sent'`)
	lastJob := nullTime(u.ID, `
		SELECT MAX(COALESCE(updated_at, created_at)) FROM send_jobs WHERE user_id = ?`)
	lastCampaign := nullTime(u.ID, `
		SELECT MAX(created_at) FROM campaigns WHERE user_id = ?`)
	lastContact := nullTime(u.ID, `
		SELECT MAX(created_at) FROM contact WHERE user_id = ?`)
	lastSMTP := nullTime(u.ID, `
		SELECT MAX(updated_at) FROM smtp_accounts WHERE user_id = ?`)

	level := classify(contacts, lists, templates, campaigns, smtpAll, sendsSent, jobs, workflows, outreachDomains)

	fmt.Printf("  usage_level: %s\n", level)
	fmt.Printf("  contacts=%d lists=%d templates=%d campaigns=%d workflows=%d\n",
		contacts, lists, templates, campaigns, workflows)
	fmt.Printf("  smtp_accounts=%d (active=%d) outreach_domains=%d outreach_mailboxes=%d\n",
		smtpAll, smtpReady, outreachDomains, outreachMboxes)
	fmt.Printf("  email_sends=%d (sent=%d) send_jobs=%d\n", sends, sendsSent, jobs)
	fmt.Printf("  last_contact=%s last_campaign=%s last_smtp=%s last_send=%s last_job=%s\n",
		fmtTime(lastContact), fmtTime(lastCampaign), fmtTime(lastSMTP), fmtTime(lastSend), fmtTime(lastJob))

	// Campaign snapshot
	cRows, err := db.Query(`
		SELECT id, COALESCE(name,''), COALESCE(status,''), COALESCE(execution_mode,''), created_at,
			(SELECT COUNT(*) FROM campaign_contacts cc WHERE cc.campaign_id = c.id)
		FROM campaigns c
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT 5
	`, u.ID)
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var id, nContacts int64
			var name, status, mode string
			var created time.Time
			if cRows.Scan(&id, &name, &status, &mode, &created, &nContacts) == nil {
				fmt.Printf("  campaign #%d %q status=%s mode=%s contacts=%d created=%s\n",
					id, name, status, mode, nContacts, created.Format("2006-01-02"))
			}
		}
	}

	fmt.Println()
}

func printSMTPDetails(userID int64) {
	rows, err := db.Query(`
		SELECT id, COALESCE(from_email,''), COALESCE(mailbox_source,''), COALESCE(auth_type,''),
			COALESCE(inboxkit_mailbox_id,''), status, is_default
		FROM smtp_accounts WHERE user_id = ?
		ORDER BY id ASC
	`, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var from, source, auth, ik, status string
		var isDefault int
		if rows.Scan(&id, &from, &source, &auth, &ik, &status, &isDefault) != nil {
			continue
		}
		fmt.Printf("  smtp #%d from=%s source=%s auth=%s default=%d status=%s ik=%s\n",
			id, from, source, auth, isDefault, status, truncate(ik, 24))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func classify(contacts, lists, templates, campaigns, smtp, sendsSent, jobs, workflows, domains int) string {
	if sendsSent > 0 || jobs > 0 {
		return "active_sender"
	}
	if campaigns > 0 || workflows > 0 {
		return "setup_campaigns_no_send"
	}
	if contacts > 0 || lists > 0 || templates > 0 {
		return "setup_content_no_campaign"
	}
	if smtp > 0 || domains > 0 {
		return "setup_mailbox_only"
	}
	return "login_only"
}

func count(userID int64, q string) int {
	var n int
	_ = db.QueryRow(q, userID).Scan(&n)
	return n
}

func nullTime(userID int64, q string) *time.Time {
	var t *time.Time
	_ = db.QueryRow(q, userID).Scan(&t)
	return t
}

func fmtTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}
