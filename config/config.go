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
	GoogleAppOAuthRedirectURI string
	WhopAPIKey            string
	WhopAPIBase           string
	WhopWebhookSecret     string
	WhopCompanyID         string
	WhopPlanID            string
	WhopPlanIDStandard   string
	WhopPlanIDPro        string
	WhopProductID         string
	AdminEmails           map[string]struct{}
	OpenAIAPIKey          string
	OpenAIModel           string
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
	GoogleAppOAuthRedirectURI = strings.TrimSpace(os.Getenv("GOOGLE_APP_OAUTH_REDIRECT_URI"))
	if GoogleAppOAuthRedirectURI == "" {
		GoogleAppOAuthRedirectURI = GoogleOAuthRedirectURI
	}
	WhopAPIKey = strings.TrimSpace(os.Getenv("WHOP_API_KEY"))
	WhopAPIBase = strings.TrimRight(strings.TrimSpace(envOr("WHOP_API_BASE", "https://api.whop.com/api/v1")), "/")
	WhopWebhookSecret = strings.TrimSpace(os.Getenv("WHOP_WEBHOOK_SECRET"))
	WhopCompanyID = strings.TrimSpace(os.Getenv("WHOP_COMPANY_ID"))
	WhopPlanID = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID"))
	WhopPlanIDStandard = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID_STANDARD"))
	WhopPlanIDPro = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID_PRO"))
	WhopProductID = strings.TrimSpace(os.Getenv("WHOP_PRODUCT_ID"))
	AdminEmails = parseEmailList(os.Getenv("ADMIN_EMAILS"))
	OpenAIAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	OpenAIModel = envOr("OPENAI_MODEL", "gpt-5-nano")
}

func AIEnabled() bool {
	return OpenAIAPIKey != ""
}

func parseEmailList(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		e := strings.TrimSpace(strings.ToLower(part))
		if e != "" {
			out[e] = struct{}{}
		}
	}
	return out
}

func IsAdminEmail(email string) bool {
	if len(AdminEmails) == 0 {
		return false
	}
	_, ok := AdminEmails[strings.TrimSpace(strings.ToLower(email))]
	return ok
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

// OAuthTokenKey returns the secret used to encrypt Gmail OAuth tokens at rest.
// Set TOKEN_ENCRYPTION_KEY to the same value on every app instance (local + production).
func OAuthTokenKey() string {
	if TokenEncryptionKey != "" {
		return TokenEncryptionKey
	}
	if SessionSecret != "" {
		return SessionSecret
	}
	return "dev-insecure-token-key"
}
