package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"emailtracker.com/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var DB *sql.DB

func Prepare() {
	if config.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required (PostgreSQL connection string)")
	}

	var err error
	DB, err = sql.Open("pgx", config.DatabaseURL)
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

func CreateTables() {
	runSchema()
}
