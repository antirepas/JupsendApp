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

var gmailScopes = []string{
	"https://mail.google.com/",
	"openid",
	"email",
	"profile",
}

func Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		RedirectURL:  config.GoogleOAuthRedirectURI,
		Scopes:       gmailScopes,
		Endpoint:     google.Endpoint,
	}
}

func AuthURL(state string) string {
	return Config().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

type googleProfile struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func ExchangeCode(ctx context.Context, code string) (*oauth2.Token, googleProfile, error) {
	tok, err := Config().Exchange(ctx, code)
	if err != nil {
		return nil, googleProfile{}, err
	}
	profile, err := fetchProfile(ctx, tok)
	return tok, profile, err
}

func fetchProfile(ctx context.Context, tok *oauth2.Token) (googleProfile, error) {
	client := Config().Client(ctx, tok)
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
	plain, err := Decrypt(refreshToken)
	if err != nil {
		plain = refreshToken
	}
	return Config().TokenSource(context.Background(), &oauth2.Token{RefreshToken: plain})
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
	return config.GoogleClientID != "" && config.GoogleClientSecret != "" && config.GoogleOAuthRedirectURI != ""
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
