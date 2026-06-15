package db

import (
	"log"
)

func runMigrations() {
	legacy := []string{
		`ALTER TABLE email_sends ADD COLUMN campaign_id INTEGER REFERENCES campaigns(id)`,
		`ALTER TABLE email_sends ADD COLUMN variant TEXT DEFAULT ''`,
		`ALTER TABLE campaigns ADD COLUMN scheduled_at DATETIME`,
	}
	for _, m := range legacy {
		if _, err := DB.Exec(m); err != nil {
			log.Printf("Migration note: %v", err)
		}
	}

	workflowMigrations := []string{
		`ALTER TABLE email_sends ADD COLUMN workflow_instance_id INTEGER`,
		`ALTER TABLE campaigns ADD COLUMN workflow_version_id INTEGER`,
		`ALTER TABLE campaigns ADD COLUMN execution_mode TEXT DEFAULT 'bulk'`,

		`CREATE TABLE IF NOT EXISTS workflows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			current_version_id INTEGER,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workflow_id INTEGER NOT NULL,
			version INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
			published_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE,
			UNIQUE(workflow_id, version)
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL,
			node_key TEXT NOT NULL,
			node_type TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			config_json TEXT NOT NULL DEFAULT '{}',
			position_x REAL NOT NULL DEFAULT 0,
			position_y REAL NOT NULL DEFAULT 0,
			ui_meta_json TEXT DEFAULT '{}',
			FOREIGN KEY (version_id) REFERENCES workflow_versions(id) ON DELETE CASCADE,
			UNIQUE(version_id, node_key)
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL,
			source_node_key TEXT NOT NULL,
			target_node_key TEXT NOT NULL,
			edge_type TEXT NOT NULL DEFAULT 'default',
			priority INTEGER NOT NULL DEFAULT 0,
			condition_json TEXT DEFAULT '{}',
			FOREIGN KEY (version_id) REFERENCES workflow_versions(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER,
			workflow_version_id INTEGER NOT NULL,
			contact_id INTEGER NOT NULL,
			campaign_id INTEGER,
			current_node_key TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			next_wake_at DATETIME,
			waiting_for_event TEXT,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			lock_token TEXT,
			lock_expires_at DATETIME,
			context_json TEXT DEFAULT '{}',
			FOREIGN KEY (workflow_version_id) REFERENCES workflow_versions(id),
			FOREIGN KEY (contact_id) REFERENCES contact(id),
			FOREIGN KEY (campaign_id) REFERENCES campaigns(id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_workflow_instances_wake ON workflow_instances(status, next_wake_at)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_instances_campaign ON workflow_instances(campaign_id, status)`,

		`CREATE TABLE IF NOT EXISTS workflow_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL,
			node_key TEXT NOT NULL,
			execution_key TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending',
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			error_message TEXT,
			output_json TEXT DEFAULT '{}',
			FOREIGN KEY (instance_id) REFERENCES workflow_instances(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS contact_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER,
			contact_id INTEGER NOT NULL,
			campaign_id INTEGER,
			workflow_id INTEGER,
			workflow_instance_id INTEGER,
			email_send_id INTEGER,
			event_type TEXT NOT NULL,
			metadata_json TEXT DEFAULT '{}',
			occurred_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			dedupe_key TEXT UNIQUE,
			FOREIGN KEY (contact_id) REFERENCES contact(id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_contact_events_contact ON contact_events(contact_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_events_instance ON contact_events(workflow_instance_id)`,
	}

	for _, m := range workflowMigrations {
		if _, err := DB.Exec(m); err != nil {
			log.Printf("Migration note: %v", err)
		}
	}

	outboundMigrations := []string{
		`ALTER TABLE campaigns ADD COLUMN is_sending INTEGER DEFAULT 0`,
		`ALTER TABLE email_sends ADD COLUMN smtp_account_id INTEGER`,
		`ALTER TABLE email_sends ADD COLUMN send_job_id INTEGER`,
		`ALTER TABLE email_sends ADD COLUMN delivery_status TEXT DEFAULT 'sent'`,

		`CREATE TABLE IF NOT EXISTS smtp_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			smtp_host TEXT NOT NULL,
			smtp_port TEXT NOT NULL DEFAULT '587',
			smtp_user TEXT NOT NULL,
			smtp_password TEXT NOT NULL,
			from_email TEXT NOT NULL,
			from_name TEXT DEFAULT '',
			imap_host TEXT DEFAULT '',
			imap_port TEXT DEFAULT '993',
			imap_user TEXT DEFAULT '',
			imap_password TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			daily_limit INTEGER NOT NULL DEFAULT 50,
			per_minute_limit INTEGER NOT NULL DEFAULT 2,
			min_seconds_between_sends INTEGER NOT NULL DEFAULT 30,
			warmup_enabled INTEGER NOT NULL DEFAULT 1,
			warmup_daily_cap INTEGER NOT NULL DEFAULT 5,
			warmup_target_daily_cap INTEGER NOT NULL DEFAULT 50,
			warmup_increment_per_day INTEGER NOT NULL DEFAULT 5,
			warmup_started_at DATETIME,
			sends_today INTEGER NOT NULL DEFAULT 0,
			sends_today_reset_at DATE,
			last_send_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS send_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			smtp_account_id INTEGER,
			contact_id INTEGER NOT NULL,
			template_id INTEGER NOT NULL,
			campaign_id INTEGER,
			variant TEXT DEFAULT '',
			workflow_instance_id INTEGER,
			email_send_id INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER NOT NULL DEFAULT 0,
			scheduled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			claimed_at DATETIME,
			lock_token TEXT,
			lock_expires_at DATETIME,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			last_error TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (contact_id) REFERENCES contact(id),
			FOREIGN KEY (template_id) REFERENCES template(id),
			FOREIGN KEY (email_send_id) REFERENCES email_sends(id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_send_jobs_status_sched ON send_jobs(status, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_send_jobs_campaign ON send_jobs(campaign_id, status)`,

		`CREATE TABLE IF NOT EXISTS contact_suppressions (
			contact_id INTEGER PRIMARY KEY,
			reason TEXT NOT NULL,
			source_message TEXT DEFAULT '',
			smtp_account_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (contact_id) REFERENCES contact(id) ON DELETE CASCADE
		)`,
	}

	for _, m := range outboundMigrations {
		if _, err := DB.Exec(m); err != nil {
			log.Printf("Migration note: %v", err)
		}
	}

	authMigrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			base_url TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE template ADD COLUMN user_id INTEGER`,
		`ALTER TABLE contact ADD COLUMN user_id INTEGER`,
		`ALTER TABLE campaigns ADD COLUMN user_id INTEGER`,
		`ALTER TABLE email_sends ADD COLUMN user_id INTEGER`,
		`ALTER TABLE smtp_accounts ADD COLUMN user_id INTEGER`,
		`ALTER TABLE send_jobs ADD COLUMN user_id INTEGER`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_accounts_user ON smtp_accounts(user_id) WHERE user_id IS NOT NULL`,
	}

	for _, m := range authMigrations {
		if _, err := DB.Exec(m); err != nil {
			log.Printf("Migration note: %v", err)
		}
	}
}
