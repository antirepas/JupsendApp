package googleoauth

import (
	"fmt"
	"strings"

	"github.com/emersion/go-sasl"
)

type imapXOAuth2Client struct {
	email string
	token string
}

func (c *imapXOAuth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", XOAuth2Payload(c.email, c.token), nil
}

func (c *imapXOAuth2Client) Next(challenge []byte) ([]byte, error) {
	if len(challenge) > 0 {
		return nil, fmt.Errorf("oauth2 auth rejected: %s", strings.TrimSpace(string(challenge)))
	}
	return nil, nil
}

func IMAPAuth(email, accessToken string) sasl.Client {
	return &imapXOAuth2Client{email: email, token: accessToken}
}
