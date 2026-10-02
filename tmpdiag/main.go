package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"emailtracker.com/config"
	"emailtracker.com/db"
)

func main() {
	config.Load()
	db.Prepare()
	defer db.Close()

	var bodyText, bodyHTML string
	err := db.QueryRow(`
		SELECT COALESCE(body_text,''), COALESCE(body_html,'')
		FROM conversation_messages WHERE id = 1686
	`).Scan(&bodyText, &bodyHTML)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("=== TEXT ===")
	fmt.Println(bodyText)
	fmt.Println("\n=== HTML ===")
	fmt.Println(bodyHTML)
	fmt.Println("\n=== HREFS ===")
	re := regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
	for _, m := range re.FindAllStringSubmatch(bodyHTML, -1) {
		fmt.Println(m[1])
	}
	fmt.Println("\n=== URL-like in text ===")
	re2 := regexp.MustCompile(`https?://[^\s<>"']+`)
	for _, m := range re2.FindAllString(bodyText+"\n"+bodyHTML, -1) {
		fmt.Println(m)
	}
	// also check what 4509 is
	fmt.Println("\n=== id 4509 probes ===")
	probes := []string{
		`SELECT 'contact' AS t, id::text, email FROM contact WHERE id=4509`,
		`SELECT 'email_sends' AS t, id::text, contact_id::text FROM email_sends WHERE id=4509`,
		`SELECT 'send_jobs' AS t, id::text, contact_id::text FROM send_jobs WHERE id=4509`,
		`SELECT 'conversation_messages' AS t, id::text, contact_id::text FROM conversation_messages WHERE id=4509`,
		`SELECT 'contact_events' AS t, id::text, contact_id::text FROM contact_events WHERE id=4509`,
	}
	for _, q := range probes {
		var t, a, b string
		if err := db.QueryRow(q).Scan(&t, &a, &b); err != nil {
			fmt.Printf("%s: %v\n", strings.Split(q, " ")[1], err)
		} else {
			fmt.Printf("%s: %s %s\n", t, a, b)
		}
	}
}
