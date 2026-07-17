package googleoauth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeGmailBodyData(t *testing.T) {
	raw := base64.URLEncoding.EncodeToString([]byte("hello"))
	if got := decodeGmailBodyData(raw); got != "hello" {
		t.Fatalf("got %q want hello", got)
	}
}

func TestExtractEmailAddress(t *testing.T) {
	tests := map[string]string{
		`Jane Doe <jane@acme.com>`: "jane@acme.com",
		`<bob@test.com>`:           "bob@test.com",
		`plain@test.com`:           "plain@test.com",
		``:                         "",
	}
	for in, want := range tests {
		if got := ExtractEmailAddress(in); got != want {
			t.Fatalf("ExtractEmailAddress(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseGmailAPIMessage(t *testing.T) {
	body := "X-Failed-Recipients: bad@example.com"
	raw := gmailAPIMessage{
		ID:       "abc",
		ThreadID: "t1",
		Payload: &gmailPayload{
			MimeType: "text/plain",
			Headers: []gmailHeader{
				{Name: "From", Value: "Mail Delivery Subsystem <mailer-daemon@google.com>"},
				{Name: "Subject", Value: "Delivery Status Notification (Failure)"},
				{Name: "Message-ID", Value: "<bounce123@google.com>"},
			},
			Body: gmailBody{Data: base64.URLEncoding.EncodeToString([]byte(body))},
		},
	}
	msg := parseGmailAPIMessage(raw)
	if msg.ID != "abc" {
		t.Fatalf("id=%q", msg.ID)
	}
	if ExtractEmailAddress(msg.From) != "mailer-daemon@google.com" {
		t.Fatalf("from=%q", msg.From)
	}
	if !strings.Contains(msg.Body, "bad@example.com") {
		t.Fatalf("body=%q", msg.Body)
	}
}
