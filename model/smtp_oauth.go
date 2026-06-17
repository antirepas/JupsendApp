package model

import (
	"fmt"
	"time"

	"emailtracker.com/googleoauth"
)

const AuthTypeGoogleOAuth = "google_oauth"

func GmailAccessToken(acc SMTPAccount) (string, error) {
	if !acc.IsGoogleOAuth() {
		return "", fmt.Errorf("gmail not connected")
	}
	if acc.OAuthAccessToken != "" && acc.OAuthExpiry.After(time.Now().Add(30*time.Second)) {
		plain, err := googleoauth.Decrypt(acc.OAuthAccessToken)
		if err == nil && plain != "" {
			return plain, nil
		}
	}
	tok, err := googleoauth.TokenSource(acc.OAuthRefreshToken).Token()
	if err != nil {
		return "", err
	}
	encAccess, _ := googleoauth.Encrypt(tok.AccessToken)
	_ = UpdateOAuthTokens(acc.ID, encAccess, tok.Expiry)
	return tok.AccessToken, nil
}
