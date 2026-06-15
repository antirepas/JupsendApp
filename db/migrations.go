package db

import "log"

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
}
