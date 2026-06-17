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
	}
	for _, stmt := range alters {
		if _, err := DB.Exec(stmt); err != nil {
			log.Printf("alter schema note: %v", err)
		}
	}
}
