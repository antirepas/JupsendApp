package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

	DB.SetMaxOpenConns(10)
	DB.SetMaxIdleConns(5)
	DB.SetConnMaxLifetime(30 * time.Minute)

	if err := DB.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	fmt.Println("Connected to PostgreSQL")
	CreateTables()
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

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schemaAdvisoryLockKey); err != nil {
		log.Fatalf("schema lock: %v", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaAdvisoryLockKey); err != nil {
			log.Printf("schema unlock: %v", err)
		}
	}()

	runSchema()
}
