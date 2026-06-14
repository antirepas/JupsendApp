package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

var (
	Port       string
	BaseURL    string
	SMTPHost   string
	SMTPPort   string
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
)

func Load() {
	_ = godotenv.Load()

	Port = envOr("PORT", "8080")
	BaseURL = envOr("BASE_URL", "http://localhost:8080")
	SMTPHost = envOr("SMTP_HOST", "smtp.gmail.com")
	SMTPPort = envOr("SMTP_PORT", "587")
	SMTPUser = strings.TrimSpace(os.Getenv("SMTP_USER"))
	SMTPPass = normalizeAppPassword(os.Getenv("APP_PASSWORD"))
	SMTPFrom = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if SMTPFrom == "" {
		SMTPFrom = SMTPUser
	}
}

func normalizeAppPassword(password string) string {
	password = strings.TrimSpace(password)
	password = strings.Trim(password, "\"'")
	// Gmail app passwords are often shown in groups; SMTP accepts without spaces.
	return strings.ReplaceAll(password, " ", "")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
