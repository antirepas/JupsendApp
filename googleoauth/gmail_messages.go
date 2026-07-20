package googleoauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

const gmailListTimeout = 45 * time.Second

// GmailMessage is a parsed inbox message used for bounce/reply detection.
type GmailMessage struct {
	ID         string
	ThreadID   string
	From       string
	Subject    string
	MessageID  string // RFC822 Message-ID header
	InReplyTo  string
	References string
	Body       string
	LabelIDs   []string
}

type gmailListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

type gmailAPIMessage struct {
	ID       string        `json:"id"`
	ThreadID string        `json:"threadId"`
	LabelIDs []string      `json:"labelIds"`
	Payload  *gmailPayload `json:"payload"`
	Raw      string        `json:"raw"`
}

type gmailPayload struct {
	Headers  []gmailHeader  `json:"headers"`
	Body     gmailBody      `json:"body"`
	Parts    []gmailPayload `json:"parts"`
	MimeType string         `json:"mimeType"`
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	Data string `json:"data"`
	Size int    `json:"size"`
}

// ListRecentMessageIDsForPolling returns recent message IDs for bounce/reply scanning.
// Includes inbox + spam and explicitly targets delivery-failure notifications.
func ListRecentMessageIDsForPolling(accessToken string, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 100 {
		maxResults = 100
	}
	seen := map[string]struct{}{}
	var ids []string
	queries := []string{
		// Replies and normal mail
		"(in:inbox OR in:spam) newer_than:3d",
		// Bounce / DSN notifications (often not categorized as normal inbox mail)
		`(from:mailer-daemon OR from:postmaster OR subject:("delivery status notification" OR undeliverable OR "delivery failure" OR "mail delivery failed" OR "returned mail")) newer_than:7d`,
	}
	for _, query := range queries {
		batch, err := listMessageIDs(accessToken, query, maxResults)
		if err != nil {
			return nil, err
		}
		for _, id := range batch {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			if len(ids) >= maxResults {
				return ids, nil
			}
		}
	}
	return ids, nil
}

// ListRecentInboxMessageIDs is kept for callers that only want inbox.
func ListRecentInboxMessageIDs(accessToken string, maxResults int) ([]string, error) {
	return listMessageIDs(accessToken, "in:inbox newer_than:3d", maxResults)
}

func listMessageIDs(accessToken, query string, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("maxResults", fmt.Sprintf("%d", maxResults))
	q.Set("q", query)
	req, err := http.NewRequest(http.MethodGet, gmailAPIBase+"/messages?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := gmailHTTPClient(gmailListTimeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("gmail api list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gmailAPIError(resp)
	}
	var out gmailListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Messages))
	for _, m := range out.Messages {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// GetMessageFull fetches a message with headers and decoded body parts.
func GetMessageFull(accessToken, messageID string) (GmailMessage, error) {
	return getMessage(accessToken, messageID, "full")
}

// GetMessageRaw fetches the full RFC822 message. Prefer this for bounce detection
// so delivery-status / attached original headers are included.
func GetMessageRaw(accessToken, messageID string) (GmailMessage, error) {
	return getMessage(accessToken, messageID, "raw")
}

func getMessage(accessToken, messageID, format string) (GmailMessage, error) {
	u := gmailAPIBase + "/messages/" + url.PathEscape(messageID) + "?format=" + url.QueryEscape(format)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return GmailMessage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := gmailHTTPClient(gmailListTimeout).Do(req)
	if err != nil {
		return GmailMessage{}, fmt.Errorf("gmail api get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GmailMessage{}, gmailAPIError(resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return GmailMessage{}, err
	}
	var raw gmailAPIMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return GmailMessage{}, err
	}
	if format == "raw" {
		return parseGmailRawMessage(raw)
	}
	return parseGmailAPIMessage(raw), nil
}

func parseGmailRawMessage(raw gmailAPIMessage) (GmailMessage, error) {
	msg := GmailMessage{
		ID:       raw.ID,
		ThreadID: raw.ThreadID,
		LabelIDs: raw.LabelIDs,
	}
	decoded := decodeGmailBodyData(raw.Raw)
	if decoded == "" {
		return msg, fmt.Errorf("gmail api raw message empty")
	}
	msg.Body = decoded
	r := strings.NewReader(decoded)
	m, err := mail.ReadMessage(r)
	if err == nil {
		msg.From = m.Header.Get("From")
		msg.Subject = m.Header.Get("Subject")
		msg.MessageID = m.Header.Get("Message-Id")
		if msg.MessageID == "" {
			msg.MessageID = m.Header.Get("Message-ID")
		}
		msg.InReplyTo = m.Header.Get("In-Reply-To")
		msg.References = m.Header.Get("References")
	} else {
		// Fall back to lightweight header scan if MIME parse fails on multipart roots.
		msg.From = rawHeaderValue(decoded, "From")
		msg.Subject = rawHeaderValue(decoded, "Subject")
		msg.MessageID = rawHeaderValue(decoded, "Message-ID")
		if msg.MessageID == "" {
			msg.MessageID = rawHeaderValue(decoded, "Message-Id")
		}
		msg.InReplyTo = rawHeaderValue(decoded, "In-Reply-To")
		msg.References = rawHeaderValue(decoded, "References")
	}
	return msg, nil
}

func rawHeaderValue(raw, name string) string {
	prefix := strings.ToLower(name) + ":"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(name)+1:])
		}
		if line == "" {
			break
		}
	}
	return ""
}

func parseGmailAPIMessage(raw gmailAPIMessage) GmailMessage {
	msg := GmailMessage{
		ID:       raw.ID,
		ThreadID: raw.ThreadID,
		LabelIDs: raw.LabelIDs,
	}
	if raw.Payload != nil {
		msg.From = headerValue(raw.Payload.Headers, "From")
		msg.Subject = headerValue(raw.Payload.Headers, "Subject")
		msg.MessageID = headerValue(raw.Payload.Headers, "Message-ID")
		if msg.MessageID == "" {
			msg.MessageID = headerValue(raw.Payload.Headers, "Message-Id")
		}
		msg.InReplyTo = headerValue(raw.Payload.Headers, "In-Reply-To")
		msg.References = headerValue(raw.Payload.Headers, "References")
		msg.Body = extractTextBody(raw.Payload)
	}
	return msg
}

func headerValue(headers []gmailHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func extractTextBody(p *gmailPayload) string {
	if p == nil {
		return ""
	}
	var parts []string
	var walk func(*gmailPayload)
	walk = func(part *gmailPayload) {
		if part == nil {
			return
		}
		mt := strings.ToLower(part.MimeType)
		data := decodeGmailBodyData(part.Body.Data)
		if data != "" {
			switch {
			case mt == "text/plain",
				mt == "text/html",
				mt == "message/delivery-status",
				mt == "message/rfc822",
				mt == "text/rfc822-headers",
				strings.HasPrefix(mt, "multipart/"):
				// multipart containers usually have empty body; children walked below
				if mt != "" && !strings.HasPrefix(mt, "multipart/") {
					parts = append(parts, data)
				} else if !strings.HasPrefix(mt, "multipart/") && data != "" {
					parts = append(parts, data)
				}
			default:
				// Include unknown text-ish parts that may carry bounce metadata.
				if strings.HasPrefix(mt, "text/") || mt == "" {
					parts = append(parts, data)
				}
			}
		}
		for i := range part.Parts {
			walk(&part.Parts[i])
		}
		if len(part.Parts) == 0 && data != "" && len(parts) == 0 {
			parts = append(parts, data)
		}
	}
	walk(p)
	return strings.Join(parts, "\n")
}

func decodeGmailBodyData(data string) string {
	if data == "" {
		return ""
	}
	// Gmail uses URL-safe base64 without padding.
	b, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		b, err = base64.RawURLEncoding.DecodeString(data)
	}
	if err != nil {
		return ""
	}
	return string(b)
}

// ExtractEmailAddress pulls the address from a From header like `Name <a@b.com>`.
func ExtractEmailAddress(fromHeader string) string {
	fromHeader = strings.TrimSpace(fromHeader)
	if fromHeader == "" {
		return ""
	}
	if i := strings.Index(fromHeader, "<"); i >= 0 {
		if j := strings.Index(fromHeader[i:], ">"); j > 0 {
			return strings.TrimSpace(fromHeader[i+1 : i+j])
		}
	}
	return strings.Trim(fromHeader, "<> ")
}
