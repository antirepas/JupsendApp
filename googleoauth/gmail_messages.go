package googleoauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const gmailListTimeout = 45 * time.Second

// GmailMessage is a parsed inbox message used for bounce/reply detection.
type GmailMessage struct {
	ID        string
	ThreadID  string
	From      string
	Subject   string
	MessageID string // RFC822 Message-ID header
	InReplyTo string
	References string
	Body      string
	LabelIDs  []string
}

type gmailListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

type gmailAPIMessage struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	LabelIDs []string `json:"labelIds"`
	Payload  *gmailPayload `json:"payload"`
	Raw      string   `json:"raw"`
}

type gmailPayload struct {
	Headers []gmailHeader `json:"headers"`
	Body    gmailBody     `json:"body"`
	Parts   []gmailPayload `json:"parts"`
	MimeType string       `json:"mimeType"`
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	Data string `json:"data"`
	Size int    `json:"size"`
}

// ListRecentInboxMessageIDs returns recent INBOX message IDs (newest first).
func ListRecentInboxMessageIDs(accessToken string, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 100 {
		maxResults = 100
	}
	q := url.Values{}
	q.Set("maxResults", fmt.Sprintf("%d", maxResults))
	q.Set("q", "in:inbox newer_than:3d")
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

// GetMessageFull fetches a message with headers and decoded text body.
func GetMessageFull(accessToken, messageID string) (GmailMessage, error) {
	u := gmailAPIBase + "/messages/" + url.PathEscape(messageID) + "?format=full"
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return GmailMessage{}, err
	}
	var raw gmailAPIMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return GmailMessage{}, err
	}
	return parseGmailAPIMessage(raw), nil
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
	var plain, html string
	var walk func(*gmailPayload)
	walk = func(part *gmailPayload) {
		if part == nil {
			return
		}
		mt := strings.ToLower(part.MimeType)
		data := decodeGmailBodyData(part.Body.Data)
		switch {
		case mt == "text/plain" && data != "":
			if plain == "" {
				plain = data
			}
		case mt == "text/html" && data != "":
			if html == "" {
				html = data
			}
		}
		for i := range part.Parts {
			walk(&part.Parts[i])
		}
		// Single-part message with body on the root payload.
		if len(part.Parts) == 0 && data != "" && plain == "" && html == "" {
			if strings.Contains(mt, "html") {
				html = data
			} else {
				plain = data
			}
		}
	}
	walk(p)
	if plain != "" {
		return plain
	}
	return html
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
