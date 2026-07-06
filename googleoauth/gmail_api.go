package googleoauth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	gmailAPIBase       = "https://gmail.googleapis.com/gmail/v1/users/me"
	gmailSendTimeout   = 30 * time.Second
	gmailProbeTimeout  = 10 * time.Second
)

// SendRawMessage sends a RFC 2822 message via Gmail API (HTTPS, port 443).
func SendRawMessage(accessToken string, raw []byte) error {
	payload, err := json.Marshal(map[string]string{
		"raw": encodeGmailRaw(raw),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, gmailAPIBase+"/messages/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := gmailHTTPClient(gmailSendTimeout).Do(req)
	if err != nil {
		return fmt.Errorf("gmail api send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return gmailAPIError(resp)
}

// ProbeGmailAPI verifies Gmail API access without sending mail (works when SMTP ports are blocked).
func ProbeGmailAPI(accessToken string) error {
	req, err := http.NewRequest(http.MethodGet, gmailAPIBase+"/profile", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := gmailHTTPClient(gmailProbeTimeout).Do(req)
	if err != nil {
		return fmt.Errorf("gmail api probe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return gmailAPIError(resp)
}

func encodeGmailRaw(raw []byte) string {
	return base64.URLEncoding.EncodeToString(raw)
}

func gmailHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func gmailAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("gmail api unauthorized — reconnect Gmail in Settings")
	case http.StatusForbidden:
		return fmt.Errorf("gmail api forbidden — check OAuth scopes (%s)", msg)
	default:
		return fmt.Errorf("gmail api %s: %s", resp.Status, msg)
	}
}
