package googleoauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"

	"emailtracker.com/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const AuthTypeGoogle = "google_oauth"

// Login scopes only — used for signup/sign-in. No Gmail send access (avoids sensitive-scope verification).
var loginScopes = []string{
	"openid",
	"email",
	"profile",
}

// Legacy Gmail-send scopes (kept for token refresh of old connected accounts).
var gmailSendScopes = []string{
	"https://www.googleapis.com/auth/gmail.send",
	"openid",
	"email",
	"profile",
}

func Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		RedirectURL:  config.GoogleOAuthRedirectURI,
		Scopes:       gmailSendScopes,
		Endpoint:     google.Endpoint,
	}
}

// AppConfig is used for app sign-in / signup (identity only).
func AppConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		RedirectURL:  config.GoogleAppOAuthRedirectURI,
		Scopes:       loginScopes,
		Endpoint:     google.Endpoint,
	}
}

func AuthURL(state string) string {
	return Config().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func AppAuthURL(state string) string {
	return AppConfig().AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func ExchangeCode(ctx context.Context, code string) (*oauth2.Token, googleProfile, error) {
	tok, err := Config().Exchange(ctx, code)
	if err != nil {
		return nil, googleProfile{}, err
	}
	profile, err := fetchProfile(ctx, tok)
	return tok, profile, err
}

// AppExchangeCode is the same as ExchangeCode, but uses the app-specific redirect URI.
func AppExchangeCode(ctx context.Context, code string) (*oauth2.Token, googleProfile, error) {
	tok, err := AppConfig().Exchange(ctx, code)
	if err != nil {
		return nil, googleProfile{}, err
	}
	profile, err := fetchProfile(ctx, tok)
	return tok, profile, err
}

type googleProfile struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func fetchProfile(ctx context.Context, tok *oauth2.Token) (googleProfile, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(tok))
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return googleProfile{}, err
	}
	defer resp.Body.Close()
	var p googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return googleProfile{}, err
	}
	return p, nil
}

func TokenSource(refreshToken string) oauth2.TokenSource {
	plain, err := DecryptRefreshToken(refreshToken)
	if err != nil {
		return oauth2.ReuseTokenSource(nil, &brokenTokenSource{err: err})
	}
	return Config().TokenSource(context.Background(), &oauth2.Token{RefreshToken: plain})
}

type brokenTokenSource struct {
	err error
}

func (b *brokenTokenSource) Token() (*oauth2.Token, error) {
	return nil, b.err
}

// DecryptRefreshToken decrypts a stored Gmail refresh token or returns a clear error.
func DecryptRefreshToken(stored string) (string, error) {
	if stored == "" {
		return "", fmt.Errorf("gmail refresh token missing")
	}
	plain, err := Decrypt(stored)
	if err != nil {
		return "", fmt.Errorf("gmail token unreadable on this server — set TOKEN_ENCRYPTION_KEY and reconnect Gmail in Settings")
	}
	if plain == "" {
		return "", fmt.Errorf("gmail refresh token missing")
	}
	return plain, nil
}

func XOAuth2Payload(email, accessToken string) []byte {
	return []byte("user=" + email + "\x01auth=Bearer " + accessToken + "\x01\x01")
}

func XOAuth2String(email, accessToken string) string {
	return base64.StdEncoding.EncodeToString(XOAuth2Payload(email, accessToken))
}

type xoauth2Auth struct {
	email string
	token string
}

func (a *xoauth2Auth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", XOAuth2Payload(a.email, a.token), nil
}

func (a *xoauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("oauth2 auth rejected: %s", strings.TrimSpace(string(fromServer)))
	}
	return nil, nil
}

func SMTPAuth(email, accessToken string) smtp.Auth {
	return &xoauth2Auth{email: email, token: accessToken}
}

func IsConfigured() bool {
	return config.GoogleClientID != "" && config.GoogleClientSecret != "" && config.GoogleAppOAuthRedirectURI != ""
}

func EncodeState(userID int64, nonce string) string {
	payload := fmt.Sprintf("%d:%s", userID, nonce)
	return base64.URLEncoding.EncodeToString([]byte(payload))
}

func DecodeState(state string) (int64, string, error) {
	raw, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return 0, "", err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid state")
	}
	var userID int64
	_, err = fmt.Sscanf(parts[0], "%d", &userID)
	return userID, parts[1], err
}
