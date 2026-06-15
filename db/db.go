package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/glebarez/go-sqlite"
)

var DB *sql.DB

func Prepare() {
	var err error

	DB, err = sql.Open("sqlite", "./my.db")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Connected to sqlite database")

	CreateTables()
}

func CreateTables() {
	_, err := DB.Exec(`PRAGMA foreign_keys = ON`)
	if err != nil {
		log.Fatalf("Failed to enable foreign keys: %v", err)
	}

	createEventsTable := `
		CREATE TABLE IF NOT EXISTS email_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email_send_id INTEGER,
			tracking_id TEXT,
			event_type TEXT NOT NULL CHECK (
				event_type IN ('open', 'click', 'reply', 'bounce')
			),
			user_agent TEXT,
			ip_address TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (email_send_id)
				REFERENCES email_sends(id)
				ON DELETE CASCADE
		);
	`
	_, err = DB.Exec(createEventsTable)
	if err != nil {
		log.Fatalf("Failed to create events table: %v", err)
	}

	createContactsTable := `
		CREATE TABLE IF NOT EXISTS contact (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT
		);
	`
	_, err = DB.Exec(createContactsTable)
	if err != nil {
		log.Fatalf("Failure to create contacts table: %v", err)
	}

	createContactsVariablesTable := `
		CREATE TABLE IF NOT EXISTS contact_variables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT,
			value TEXT,
			contact_id INTEGER,
			FOREIGN KEY (contact_id)
				REFERENCES contact(id)
				ON DELETE CASCADE
		);
	`
	_, err = DB.Exec(createContactsVariablesTable)
	if err != nil {
		log.Fatalf("Failure to create contacts variable table: %v", err)
	}

	createTrackedLinksTable := `
		CREATE TABLE IF NOT EXISTS tracked_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email_send_id INTEGER REFERENCES email_sends(id),
			tracking_id TEXT UNIQUE,
			original_url TEXT
		);
	`
	_, err = DB.Exec(createTrackedLinksTable)
	if err != nil {
		log.Fatalf("Failed to create tracked links table: %v", err)
	}

	createTemplateTable := `
		CREATE TABLE IF NOT EXISTS template (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			subject TEXT,
			body TEXT
		);
	`
	_, err = DB.Exec(createTemplateTable)
	if err != nil {
		log.Fatalf("Failure to create template table: %v", err)
	}

	createTemplateVariablesTable := `
		CREATE TABLE IF NOT EXISTS template_variables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			template_id INTEGER,
			key TEXT,
			FOREIGN KEY (template_id)
				REFERENCES template(id)
				ON DELETE CASCADE
		);
	`
	_, err = DB.Exec(createTemplateVariablesTable)
	if err != nil {
		log.Fatalf("Failure to create template variables: %v", err)
	}

	createEmailSendsTable := `
		CREATE TABLE IF NOT EXISTS email_sends (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			contact_id INTEGER,
			template_id INTEGER,
			tracking_id TEXT UNIQUE,
			sent_at DATETIME
		);
	`
	_, err = DB.Exec(createEmailSendsTable)
	if err != nil {
		log.Fatalf("Failed to create email sends table: %v", err)
	}

	createCampaignsTable := `
		CREATE TABLE IF NOT EXISTS campaigns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			template_a_id INTEGER NOT NULL,
			template_b_id INTEGER,
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (template_a_id) REFERENCES template(id),
			FOREIGN KEY (template_b_id) REFERENCES template(id)
		);
	`
	_, err = DB.Exec(createCampaignsTable)
	if err != nil {
		log.Fatalf("Failed to create campaigns table: %v", err)
	}

	createCampaignContactsTable := `
		CREATE TABLE IF NOT EXISTS campaign_contacts (
			campaign_id INTEGER NOT NULL,
			contact_id INTEGER NOT NULL,
			PRIMARY KEY (campaign_id, contact_id),
			FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE,
			FOREIGN KEY (contact_id) REFERENCES contact(id) ON DELETE CASCADE
		);
	`
	_, err = DB.Exec(createCampaignContactsTable)
	if err != nil {
		log.Fatalf("Failed to create campaign_contacts table: %v", err)
	}

	runMigrations()
}

