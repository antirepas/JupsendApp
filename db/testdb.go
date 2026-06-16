package db

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultTestDatabaseURL = "postgres://emailtracker:emailtracker@localhost:5432/emailtracker_test?sslmode=disable"

func TestDatabaseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return defaultTestDatabaseURL
}

func OpenTestDB(t *testing.T) {
	t.Helper()
	conn, err := sql.Open("pgx", TestDatabaseURL())
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	conn.SetConnMaxLifetime(2 * time.Minute)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		t.Skipf("postgres ping failed: %v", err)
	}
	DB = conn
	CreateTables()
	t.Cleanup(func() { _ = conn.Close() })
}

func SetDB(database *sql.DB) {
	DB = database
}
