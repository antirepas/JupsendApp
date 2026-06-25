package util

import (
	"strings"
	"testing"
)

func TestInjectUnsubscribeFooter(t *testing.T) {
	html, plain := InjectUnsubscribeFooter("<html><body><p>Hi</p></body></html>", "Hi", "https://example.com/u/token")
	if !strings.Contains(html, "unsubscribe") || !strings.Contains(plain, "unsubscribe") {
		t.Fatal("footer missing")
	}
}
