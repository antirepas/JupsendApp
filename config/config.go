package config

import (
	"os"
	"strconv"
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
	IMAPHost              string
	IMAPPort              string
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
	WhopMailboxAddonID    string
	WhopDomainAddonID     string
	InboxKitAPIKey        string
	InboxKitWorkspaceID   string
	InboxKitBaseURL       string
	InboxKitRedirectURL   string
	InboxKitPlatform      string
	InboxKitIncludedMBs   string
	InboxKitRegistrantEmail string
	InboxKitRegistrantName  string
	InboxKitRegistrantOrg   string
	AdminEmails           map[string]struct{}
	TrustedProxies        []string
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIFallbackModel   string
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
	SMTPPass = NormalizeAppPassword(os.Getenv("APP_PASSWORD"))
	SMTPFrom = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if SMTPFrom == "" {
		SMTPFrom = SMTPUser
	}
	IMAPHost = strings.TrimSpace(os.Getenv("IMAP_HOST"))
	IMAPPort = envOr("IMAP_PORT", "993")
	SessionSecret = envOr("SESSION_SECRET", "")
	TokenEncryptionKey = strings.TrimSpace(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	GoogleClientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	GoogleClientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	applyGoogleOAuthRedirectURIs()
	WhopAPIKey = strings.TrimSpace(os.Getenv("WHOP_API_KEY"))
	WhopAPIBase = strings.TrimRight(strings.TrimSpace(envOr("WHOP_API_BASE", "https://api.whop.com/api/v1")), "/")
	WhopWebhookSecret = strings.TrimSpace(os.Getenv("WHOP_WEBHOOK_SECRET"))
	WhopCompanyID = strings.TrimSpace(os.Getenv("WHOP_COMPANY_ID"))
	WhopPlanID = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID"))
	WhopPlanIDStandard = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID_STANDARD"))
	WhopPlanIDPro = strings.TrimSpace(os.Getenv("WHOP_PLAN_ID_PRO"))
	WhopProductID = strings.TrimSpace(os.Getenv("WHOP_PRODUCT_ID"))
	WhopMailboxAddonID = strings.TrimSpace(os.Getenv("WHOP_MAILBOX_ADDON_ID"))
	WhopDomainAddonID = strings.TrimSpace(os.Getenv("WHOP_DOMAIN_ADDON_ID"))
	InboxKitAPIKey = strings.TrimSpace(os.Getenv("INBOXKIT_API_KEY"))
	InboxKitWorkspaceID = strings.TrimSpace(os.Getenv("INBOXKIT_WORKSPACE_ID"))
	InboxKitBaseURL = strings.TrimRight(strings.TrimSpace(envOr("INBOXKIT_BASE_URL", "https://api.inboxkit.com/v1")), "/")
	InboxKitRedirectURL = strings.TrimSpace(envOr("INBOXKIT_REDIRECT_URL", BaseURL))
	InboxKitPlatform = strings.ToUpper(strings.TrimSpace(envOr("INBOXKIT_DEFAULT_PLATFORM", "GOOGLE")))
	InboxKitIncludedMBs = envOr("INBOXKIT_INCLUDED_MAILBOXES", "1")
	InboxKitRegistrantEmail = strings.TrimSpace(os.Getenv("INBOXKIT_REGISTRANT_EMAIL"))
	InboxKitRegistrantName = strings.TrimSpace(os.Getenv("INBOXKIT_REGISTRANT_NAME"))
	InboxKitRegistrantOrg = strings.TrimSpace(os.Getenv("INBOXKIT_REGISTRANT_ORG"))
	AdminEmails = parseEmailList(os.Getenv("ADMIN_EMAILS"))
	TrustedProxies = parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	OpenAIAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	OpenAIModel = envOr("OPENAI_MODEL", "gpt-5-nano")
	OpenAIFallbackModel = strings.TrimSpace(os.Getenv("OPENAI_FALLBACK_MODEL"))
}

func parseTrustedProxies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Default: trust local reverse proxies (nginx/caddy/docker) so ClientIP uses X-Forwarded-For.
		return []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	if raw == "none" || raw == "false" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func AIEnabled() bool {
	return OpenAIAPIKey != ""
}

func InboxKitIncludedMailboxCount() int {
	n, err := strconv.Atoi(strings.TrimSpace(InboxKitIncludedMBs))
	if err != nil || n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
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

// SharedIMAPHost returns IMAP host for the Free shared mailbox (env or derived from SMTP).
func SharedIMAPHost() string {
	if IMAPHost != "" {
		return IMAPHost
	}
	h := strings.ToLower(strings.TrimSpace(SMTPHost))
	if strings.Contains(h, "gmail") || strings.Contains(h, "google") {
		return "imap.gmail.com"
	}
	if strings.HasPrefix(h, "smtp.") {
		return "imap." + strings.TrimPrefix(h, "smtp.")
	}
	return SMTPHost
}

// SharedIMAPPort returns IMAP port for the Free shared mailbox.
func SharedIMAPPort() string {
	if strings.TrimSpace(IMAPPort) != "" {
		return IMAPPort
	}
	return "993"
}

// NormalizeAppPassword strips quotes/spaces so Gmail app passwords pasted as
// "xxxx xxxx xxxx xxxx" match what Free shared SMTP uses from APP_PASSWORD.
func NormalizeAppPassword(password string) string {
	password = strings.TrimSpace(password)
	password = strings.Trim(password, "\"'")
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
