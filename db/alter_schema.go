package db

import "log"

func runAlterSchema() {
	alters := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'none'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS wizard_dismissed_at TIMESTAMPTZ`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS plan_tier TEXT NOT NULL DEFAULT 'free'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS whop_membership_id TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS whop_member_id TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_ends_at TIMESTAMPTZ`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_credits_used_today INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_credits_reset_at DATE`,
		`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS auth_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS oauth_refresh_token TEXT DEFAULT ''`,
		`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS oauth_access_token TEXT DEFAULT ''`,
		`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS oauth_expiry TIMESTAMPTZ`,
		`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS google_email TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS send_cooldown_days INTEGER NOT NULL DEFAULT 30`,
		`ALTER TABLE contact ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP`,
		`CREATE TABLE IF NOT EXISTS contact_lists (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS contact_list_members (
			list_id BIGINT NOT NULL REFERENCES contact_lists(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES contact(id) ON DELETE CASCADE,
			PRIMARY KEY (list_id, contact_id)
		)`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS contact_list_id BIGINT REFERENCES contact_lists(id) ON DELETE SET NULL`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS include_unsubscribe_link BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE contact ADD COLUMN IF NOT EXISTS email_status TEXT DEFAULT 'unknown'`,
		`ALTER TABLE contact ADD COLUMN IF NOT EXISTS email_status_reason TEXT DEFAULT ''`,
		`ALTER TABLE contact ADD COLUMN IF NOT EXISTS replied_at TIMESTAMPTZ`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS experiment_variable TEXT DEFAULT ''`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS experiment_hypothesis TEXT DEFAULT ''`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS success_metric TEXT DEFAULT 'reply'`,
		`ALTER TABLE workflow_instances ADD COLUMN IF NOT EXISTS fork_root_id BIGINT`,
		`ALTER TABLE workflow_instances ADD COLUMN IF NOT EXISTS branch_priority INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS campaign_workflow_step_templates (
			campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			node_key TEXT NOT NULL,
			template_id BIGINT NOT NULL REFERENCES template(id),
			PRIMARY KEY (campaign_id, node_key)
		)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS goal_meetings_per_month INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS goal_reply_to_meeting_pct INTEGER NOT NULL DEFAULT 50`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS goal_daily_send_cap INTEGER NOT NULL DEFAULT 0`,
		// Prefer 587 STARTTLS for Gmail — 465/SMTPS often times out on VPS IPv6 routes.
		`UPDATE smtp_accounts SET smtp_port = '587' WHERE smtp_host = 'smtp.gmail.com' AND smtp_port = '465'`,
		`UPDATE smtp_accounts SET warmup_daily_cap = 20, warmup_increment_per_day = 20 WHERE warmup_enabled = 1 AND warmup_daily_cap = 5 AND warmup_increment_per_day = 5`,
		`ALTER TABLE contact_lists ADD COLUMN IF NOT EXISTS variable_schema TEXT DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS gmail_processed_messages (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			message_key TEXT NOT NULL,
			processed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, message_key)
		)`,
		`CREATE TABLE IF NOT EXISTS outreach_domains (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			domain TEXT NOT NULL,
			inboxkit_order_id TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			included BOOLEAN NOT NULL DEFAULT TRUE,
			redirect_url TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (user_id, domain)
		)`,
		`CREATE TABLE IF NOT EXISTS outreach_mailboxes (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			domain_id BIGINT REFERENCES outreach_domains(id) ON DELETE SET NULL,
			smtp_account_id BIGINT REFERENCES smtp_accounts(id) ON DELETE SET NULL,
			inboxkit_mailbox_id TEXT DEFAULT '',
			email TEXT NOT NULL,
			first_name TEXT DEFAULT '',
			last_name TEXT DEFAULT '',
			platform TEXT NOT NULL DEFAULT 'GOOGLE',
			status TEXT NOT NULL DEFAULT 'pending',
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			health_json TEXT DEFAULT '{}',
			analytics_json TEXT DEFAULT '{}',
			included BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS mailbox_purchases (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			domain_id BIGINT REFERENCES outreach_domains(id) ON DELETE SET NULL,
			quantity INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'pending_payment',
			whop_checkout_id TEXT DEFAULT '',
			whop_membership_id TEXT DEFAULT '',
			inboxkit_order_id TEXT DEFAULT '',
			payload_json TEXT DEFAULT '{}',
			error_message TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_outreach_mailboxes_user ON outreach_mailboxes(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_outreach_domains_user ON outreach_domains(user_id)`,
		// Allow campaigns to be stopped mid-flight (cancel queued sends).
		`ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_status_check`,
		`ALTER TABLE campaigns ADD CONSTRAINT campaigns_status_check CHECK (status IN ('draft', 'sent', 'stopped'))`,
		`CREATE TABLE IF NOT EXISTS contact_interested_dismissed (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES contact(id) ON DELETE CASCADE,
			dismissed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, contact_id)
		)`,
		`CREATE TABLE IF NOT EXISTS import_jobs (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			list_id BIGINT DEFAULT 0,
			campaign_id BIGINT DEFAULT 0,
			payload_json TEXT NOT NULL DEFAULT '{}',
			total_rows INTEGER NOT NULL DEFAULT 0,
			processed_rows INTEGER NOT NULL DEFAULT 0,
			created_count INTEGER NOT NULL DEFAULT 0,
			updated_count INTEGER NOT NULL DEFAULT 0,
			skipped_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			message TEXT DEFAULT '',
			error_message TEXT DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_user_status ON import_jobs(user_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_pending ON import_jobs(status, id) WHERE status IN ('pending', 'processing')`,
		`ALTER TABLE outreach_domains ADD COLUMN IF NOT EXISTS nameservers_json TEXT DEFAULT ''`,
		`ALTER TABLE email_sends ADD COLUMN IF NOT EXISTS rendered_subject TEXT DEFAULT ''`,
		`ALTER TABLE email_sends ADD COLUMN IF NOT EXISTS rendered_html TEXT DEFAULT ''`,
		`ALTER TABLE email_sends ADD COLUMN IF NOT EXISTS rendered_text TEXT DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS conversation_messages (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES contact(id) ON DELETE CASCADE,
			smtp_account_id BIGINT DEFAULT 0,
			email_send_id BIGINT DEFAULT 0,
			direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
			from_email TEXT NOT NULL DEFAULT '',
			to_email TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			body_text TEXT NOT NULL DEFAULT '',
			body_html TEXT NOT NULL DEFAULT '',
			message_id TEXT DEFAULT '',
			in_reply_to TEXT DEFAULT '',
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_messages_user_msgid
			ON conversation_messages(user_id, message_id) WHERE message_id IS NOT NULL AND message_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_messages_contact ON conversation_messages(user_id, contact_id, occurred_at)`,
		`ALTER TABLE outreach_domains ADD COLUMN IF NOT EXISTS last_error TEXT DEFAULT ''`,
		`ALTER TABLE outreach_domains ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ`,
		`ALTER TABLE outreach_mailboxes ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE outreach_mailboxes ADD COLUMN IF NOT EXISTS role TEXT DEFAULT ''`,
		`ALTER TABLE outreach_mailboxes ADD COLUMN IF NOT EXISTS forwarding_email TEXT DEFAULT ''`,
		`ALTER TABLE outreach_mailboxes ADD COLUMN IF NOT EXISTS last_error TEXT DEFAULT ''`,
		`ALTER TABLE outreach_mailboxes ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ`,
		`ALTER TABLE email_events ADD COLUMN IF NOT EXISTS is_bot SMALLINT NOT NULL DEFAULT 0`,
		`ALTER TABLE email_events ADD COLUMN IF NOT EXISTS bot_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS open_tracking_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS temperature_rules_json TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_email_events_open_human ON email_events(email_send_id) WHERE event_type = 'open' AND COALESCE(is_bot, 0) = 0`,
	}
	for _, stmt := range alters {
		if _, err := DB.Exec(stmt); err != nil {
			log.Printf("alter schema note: %v", err)
		}
	}

	// Allow multiple smtp_accounts per user (InboxKit mailboxes).
	_, _ = DB.Exec(`DROP INDEX IF EXISTS idx_smtp_accounts_user`)
	_, _ = DB.Exec(`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS inboxkit_mailbox_id TEXT DEFAULT ''`)
	_, _ = DB.Exec(`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS is_default SMALLINT NOT NULL DEFAULT 0`)
	_, _ = DB.Exec(`ALTER TABLE smtp_accounts ADD COLUMN IF NOT EXISTS mailbox_source TEXT NOT NULL DEFAULT ''`)
	_, _ = DB.Exec(`
		DELETE FROM outreach_mailboxes a USING outreach_mailboxes b
		WHERE a.user_id = b.user_id AND lower(a.email) = lower(b.email) AND a.id > b.id
	`)
	_, _ = DB.Exec(`UPDATE outreach_mailboxes SET email = lower(email) WHERE email <> lower(email)`)
	_, _ = DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_outreach_mailboxes_user_email ON outreach_mailboxes(user_id, email)`)
}
