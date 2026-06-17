package googleoauth

import "github.com/emersion/go-sasl"

type imapXOAuth2Client struct {
	email string
	token string
}

func (c *imapXOAuth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", nil, nil
}

func (c *imapXOAuth2Client) Next(_ []byte) ([]byte, error) {
	return []byte(XOAuth2String(c.email, c.token)), nil
}

func IMAPAuth(email, accessToken string) sasl.Client {
	return &imapXOAuth2Client{email: email, token: accessToken}
}
