package db

import (
	"net/url"
	"strings"
)

// pgxConnectURL ensures pooler-safe settings for Supabase/PgBouncer (transaction mode).
// Prepared statement caching causes SQLSTATE 42P05 on pooled connections.
func pgxConnectURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	// pgx stdlib expects a URL; normalize postgresql:// to postgres:// for parsing.
	normalized := strings.Replace(raw, "postgresql://", "postgres://", 1)
	u, err := url.Parse(normalized)
	if err != nil {
		return raw
	}

	q := u.Query()
	if q.Get("default_query_exec_mode") == "" {
		q.Set("default_query_exec_mode", "simple_protocol")
	}
	u.RawQuery = q.Encode()

	out := u.String()
	if strings.HasPrefix(raw, "postgresql://") {
		out = strings.Replace(out, "postgres://", "postgresql://", 1)
	}
	return out
}
