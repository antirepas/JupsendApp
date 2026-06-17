package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

var (
	Port                  string
	BaseURL               string
	DatabaseURL           string
	SMTPHost              string
	SMTPPort              string
	SMTPUser              string
	SMTPPass              string
	SMTPFrom              string
	SessionSecret         string
	TokenEncryptionKey    string
	GoogleClientID        string
	GoogleClientSecret    string
	GoogleOAuthRedirectURI string
	WhopAPIKey            string
	WhopWebhookSecret     string
	WhopCompanyID         string
	WhopPlanID            string
	WhopProductID         string
)

func Load() {
	_ = godotenv.Load()
	reloadFromEnv()
}

func Reload() {
	_ = godotenv.Load()
	reloadFromEnv()
}

func reloadFromEnv() {
	Port = envOr("PORT", "8080")
	BaseURL = strings.TrimRight(strings.TrimSpace(envOr("BASE_URL", "http://localhost:8080")), "/")
	DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	SMTPHost = envOr("SMTP_HOST", "smtp.gmail.com")
	SMTPPort = envOr("SMTP_PORT", "587")
	SMTPUser = strings.TrimSpace(os.Getenv("SMTP_USER"))
	SMTPPass = normalizeAppPassword(os.Getenv("APP_PASSWORD"))
	SMTPFrom = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if SMTPFrom == "" {
		SMTPFrom = SMTPUser
	}
	SessionSecret = envOr("SESSION_SECRET", "")
	TokenEncryptionKey = strings.TrimSpace(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	GoogleClientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	GoogleClientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	GoogleOAuthRedirectURI = strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_REDIRECT_URI"))
	WhopAPIKey = strings.TrimSpace(os.Getenv("WHOP_API_KEY"))
	WhopWebhookSecret = strings.TrimSpace(os.Getenv("WHOP_WEBHOOK_SECRET"))
	WhopCompanyID = strings.TrimSpace(os.Getenv("WHOP_COMPANY_ID"))
	WhopPlanID = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID"))
	WhopProductID = strings.TrimSpace(os.Getenv("WHOP_PRODUCT_ID"))
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
