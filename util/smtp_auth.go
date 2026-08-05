package util

import (
	"errors"
	"net/smtp"
	"strings"
)

// SMTPPasswordAuth returns an Auth usable for Gmail/Google Workspace password login.
// Unlike smtp.PlainAuth, it works after tls.Dial (port 465) where Go's Client.tls stays false,
// and it falls back to LOGIN when PLAIN is unavailable.
func SMTPPasswordAuth(host, username, password string, connectionIsTLS bool) smtp.Auth {
	return &smtpPasswordAuth{
		host:             strings.TrimSpace(host),
		username:         strings.TrimSpace(username),
		password:         password,
		connectionIsTLS:  connectionIsTLS,
	}
}

type smtpPasswordAuth struct {
	host, username, password string
	connectionIsTLS          bool
	loginStep                int
	useLogin                 bool
}

func (a *smtpPasswordAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server == nil {
		return "", nil, errors.New("smtp auth: missing server info")
	}
	encrypted := server.TLS || a.connectionIsTLS
	if !encrypted && !isSMTPLocalhost(server.Name) {
		return "", nil, errors.New("unencrypted connection")
	}
	// Prefer LOGIN when advertised — more reliable with some Google Workspace configs.
	for _, m := range server.Auth {
		if strings.EqualFold(m, "LOGIN") {
			a.useLogin = true
			a.loginStep = 0
			return "LOGIN", nil, nil
		}
	}
	resp := []byte("\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

func (a *smtpPasswordAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !a.useLogin {
		// PLAIN is one-shot.
		return nil, nil
	}
	if !more {
		return nil, nil
	}
	switch a.loginStep {
	case 0:
		a.loginStep++
		return []byte(a.username), nil
	case 1:
		a.loginStep++
		return []byte(a.password), nil
	default:
		return nil, errors.New("unexpected LOGIN challenge")
	}
}

func isSMTPLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
