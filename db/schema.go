package db

import "log"

func runSchema() {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			base_url TEXT DEFAULT '',
			subscription_status TEXT NOT NULL DEFAULT 'none',
			whop_membership_id TEXT DEFAULT '',
			whop_member_id TEXT DEFAULT '',
			subscription_ends_at TIMESTAMPTZ,
			is_admin BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS template (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
			name TEXT,
			subject TEXT,
			body TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS template_variables (
			id BIGSERIAL PRIMARY KEY,
			template_id BIGINT REFERENCES template(id) ON DELETE CASCADE,
			key TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS contact (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
			email TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS contact_variables (
			id BIGSERIAL PRIMARY KEY,
			key TEXT,
			value TEXT,
			contact_id BIGINT REFERENCES contact(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS campaigns (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
			name TEXT NOT NULL,
			template_a_id BIGINT NOT NULL REFERENCES template(id),
			template_b_id BIGINT REFERENCES template(id),
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent')),
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			scheduled_at TIMESTAMPTZ,
			workflow_version_id BIGINT,
			execution_mode TEXT DEFAULT 'bulk',
			is_sending SMALLINT DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS campaign_contacts (
			campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES contact(id) ON DELETE CASCADE,
			PRIMARY KEY (campaign_id, contact_id)
		)`,

		`CREATE TABLE IF NOT EXISTS email_sends (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
			contact_id BIGINT,
			template_id BIGINT,
			tracking_id TEXT UNIQUE,
			sent_at TIMESTAMPTZ,
			campaign_id BIGINT REFERENCES campaigns(id),
			variant TEXT DEFAULT '',
			workflow_instance_id BIGINT,
			smtp_account_id BIGINT,
			send_job_id BIGINT,
			delivery_status TEXT DEFAULT 'sent'
		)`,

		`CREATE TABLE IF NOT EXISTS email_events (
			id BIGSERIAL PRIMARY KEY,
			email_send_id BIGINT REFERENCES email_sends(id) ON DELETE CASCADE,
			tracking_id TEXT,
			event_type TEXT NOT NULL CHECK (event_type IN ('open', 'click', 'reply', 'bounce')),
			user_agent TEXT,
			ip_address TEXT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS tracked_links (
			id BIGSERIAL PRIMARY KEY,
			email_send_id BIGINT REFERENCES email_sends(id),
			tracking_id TEXT UNIQUE,
			original_url TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS workflows (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			current_version_id BIGINT,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_versions (
			id BIGSERIAL PRIMARY KEY,
			workflow_id BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
			version INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
			published_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(workflow_id, version)
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_nodes (
			id BIGSERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,
			node_key TEXT NOT NULL,
			node_type TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			config_json TEXT NOT NULL DEFAULT '{}',
			position_x DOUBLE PRECISION NOT NULL DEFAULT 0,
			position_y DOUBLE PRECISION NOT NULL DEFAULT 0,
			ui_meta_json TEXT DEFAULT '{}',
			UNIQUE(version_id, node_key)
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_edges (
			id BIGSERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,
			source_node_key TEXT NOT NULL,
			target_node_key TEXT NOT NULL,
			edge_type TEXT NOT NULL DEFAULT 'default',
			priority INTEGER NOT NULL DEFAULT 0,
			condition_json TEXT DEFAULT '{}'
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_instances (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			workflow_version_id BIGINT NOT NULL REFERENCES workflow_versions(id),
			contact_id BIGINT NOT NULL REFERENCES contact(id),
			campaign_id BIGINT REFERENCES campaigns(id),
			current_node_key TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			next_wake_at TIMESTAMPTZ,
			waiting_for_event TEXT,
			started_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMPTZ,
			lock_token TEXT,
			lock_expires_at TIMESTAMPTZ,
			context_json TEXT DEFAULT '{}'
		)`,

		`CREATE TABLE IF NOT EXISTS workflow_executions (
			id BIGSERIAL PRIMARY KEY,
			instance_id BIGINT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
			node_key TEXT NOT NULL,
			execution_key TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending',
			started_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			finished_at TIMESTAMPTZ,
			error_message TEXT,
			output_json TEXT DEFAULT '{}'
		)`,

		`CREATE TABLE IF NOT EXISTS contact_events (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			contact_id BIGINT NOT NULL REFERENCES contact(id),
			campaign_id BIGINT,
			workflow_id BIGINT,
			workflow_instance_id BIGINT,
			email_send_id BIGINT,
			event_type TEXT NOT NULL,
			metadata_json TEXT DEFAULT '{}',
			occurred_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			dedupe_key TEXT UNIQUE
		)`,

		`CREATE TABLE IF NOT EXISTS smtp_accounts (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
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
			warmup_enabled SMALLINT NOT NULL DEFAULT 1,
			warmup_daily_cap INTEGER NOT NULL DEFAULT 5,
			warmup_target_daily_cap INTEGER NOT NULL DEFAULT 50,
			warmup_increment_per_day INTEGER NOT NULL DEFAULT 5,
			warmup_started_at TIMESTAMPTZ,
			sends_today INTEGER NOT NULL DEFAULT 0,
			sends_today_reset_at DATE,
			last_send_at TIMESTAMPTZ,
			auth_type TEXT NOT NULL DEFAULT '',
			oauth_refresh_token TEXT DEFAULT '',
			oauth_access_token TEXT DEFAULT '',
			oauth_expiry TIMESTAMPTZ,
			google_email TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS send_jobs (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id),
			smtp_account_id BIGINT,
			contact_id BIGINT NOT NULL REFERENCES contact(id),
			template_id BIGINT NOT NULL REFERENCES template(id),
			campaign_id BIGINT,
			variant TEXT DEFAULT '',
			workflow_instance_id BIGINT,
			email_send_id BIGINT REFERENCES email_sends(id),
			status TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER NOT NULL DEFAULT 0,
			scheduled_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			claimed_at TIMESTAMPTZ,
			lock_token TEXT,
			lock_expires_at TIMESTAMPTZ,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			last_error TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS contact_suppressions (
			contact_id BIGINT PRIMARY KEY REFERENCES contact(id) ON DELETE CASCADE,
			reason TEXT NOT NULL,
			source_message TEXT DEFAULT '',
			smtp_account_id BIGINT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE INDEX IF NOT EXISTS idx_workflow_instances_wake ON workflow_instances(status, next_wake_at)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_instances_campaign ON workflow_instances(campaign_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_events_contact ON contact_events(contact_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_events_instance ON contact_events(workflow_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_send_jobs_status_sched ON send_jobs(status, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_send_jobs_campaign ON send_jobs(campaign_id, status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_accounts_user ON smtp_accounts(user_id) WHERE user_id IS NOT NULL`,
	}

	for _, stmt := range stmts {
		if _, err := DB.Exec(stmt); err != nil {
			log.Fatalf("schema: %v\nstmt: %s", err, stmt)
		}
	}
	runAlterSchema()
}
