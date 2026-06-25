package db

import "log"

func runAlterSchema() {
	alters := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'none'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS whop_membership_id TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS whop_member_id TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_ends_at TIMESTAMPTZ`,
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
	}
	for _, stmt := range alters {
		if _, err := DB.Exec(stmt); err != nil {
			log.Printf("alter schema note: %v", err)
		}
	}
}
