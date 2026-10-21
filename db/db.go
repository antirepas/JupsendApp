package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"emailtracker.com/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var DB *sql.DB

// schemaAdvisoryLockKey serializes CREATE TABLE across processes that share a DB
// (e.g. go test ./... running packages in parallel against emailtracker_test).
const schemaAdvisoryLockKey int64 = 0x6a757073656e64 // "jupsend"

func Prepare() {
	if config.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required (PostgreSQL connection string)")
	}

	var err error
	DB, err = sql.Open("pgx", pgxConnectURL(config.DatabaseURL))
	if err != nil {
		log.Fatal(err)
	}

	// Campaign launch enqueue + outbound workers + page loads share this pool.
	// 10 was too low: large sends starved HTTP and proxies returned 502/503.
	DB.SetMaxOpenConns(40)
	DB.SetMaxIdleConns(20)
	DB.SetConnMaxLifetime(30 * time.Minute)

	if err := DB.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	fmt.Println("Connected to PostgreSQL")
	ensurePublicSearchPath()
	CreateTables()
}

// ensurePublicSearchPath repairs empty search_path left by Supabase pg_restore
// and makes CREATE TABLE IF NOT EXISTS work on a plain Postgres host.
func ensurePublicSearchPath() {
	if DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, _ = DB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS public`)
	_, _ = DB.ExecContext(ctx, `GRANT USAGE, CREATE ON SCHEMA public TO CURRENT_USER`)
	_, _ = DB.ExecContext(ctx, `SELECT set_config('search_path', 'public', false)`)
	// Persist for new pool connections / app restarts (ignore errors if not allowed).
	// RESET first: Supabase dumps often leave an explicit empty search_path on the role.
	var role, dbname string
	_ = DB.QueryRowContext(ctx, `SELECT CURRENT_USER, current_database()`).Scan(&role, &dbname)
	if role != "" {
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(`ALTER ROLE %s RESET search_path`, pqQuoteIdent(role)))
	}
	if dbname != "" {
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(`ALTER DATABASE %s RESET search_path`, pqQuoteIdent(dbname)))
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(`ALTER DATABASE %s SET search_path TO public`, pqQuoteIdent(dbname)))
	}
	if role != "" && dbname != "" {
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(
			`ALTER ROLE %s IN DATABASE %s RESET search_path`,
			pqQuoteIdent(role), pqQuoteIdent(dbname),
		))
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(
			`ALTER ROLE %s IN DATABASE %s SET search_path TO public`,
			pqQuoteIdent(role), pqQuoteIdent(dbname),
		))
		_, _ = DB.ExecContext(ctx, fmt.Sprintf(
			`ALTER ROLE %s SET search_path TO public`,
			pqQuoteIdent(role),
		))
	}
}

func pqQuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

func Ping() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	return DB.Ping()
}

func CreateTables() {
	if DB == nil {
		log.Fatal("database not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Hold the lock on one dedicated connection for the whole schema run.
	conn, err := DB.Conn(ctx)
	if err != nil {
		log.Fatalf("schema conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	locked := false
	for attempt := 0; attempt < 60; attempt++ {
		var got bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schemaAdvisoryLockKey).Scan(&got); err != nil {
			log.Fatalf("schema lock: %v", err)
		}
		if got {
			locked = true
			break
		}
		if attempt == 0 || attempt%5 == 4 {
			log.Printf("waiting for schema advisory lock (another instance migrating?) attempt=%d", attempt+1)
		}
		select {
		case <-ctx.Done():
			log.Fatalf("schema lock: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	if !locked {
		log.Fatal("schema lock: could not acquire advisory lock")
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaAdvisoryLockKey); err != nil {
			log.Printf("schema unlock: %v", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, `SET search_path TO public`); err != nil {
		log.Printf("schema search_path: %v", err)
	}

	runSchema()
}
