package db

import "log"

func runAlterSchema() {
	alters := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'none'`,
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
		`UPDATE smtp_accounts SET smtp_port = '465' WHERE smtp_host = 'smtp.gmail.com' AND smtp_port = '587'`,
		`UPDATE smtp_accounts SET warmup_daily_cap = 20, warmup_increment_per_day = 20 WHERE warmup_enabled = 1 AND warmup_daily_cap = 5 AND warmup_increment_per_day = 5`,
		`ALTER TABLE contact_lists ADD COLUMN IF NOT EXISTS variable_schema TEXT DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS gmail_processed_messages (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			message_key TEXT NOT NULL,
			processed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, message_key)
		)`,
	}
	for _, stmt := range alters {
		if _, err := DB.Exec(stmt); err != nil {
			log.Printf("alter schema note: %v", err)
		}
	}
}
