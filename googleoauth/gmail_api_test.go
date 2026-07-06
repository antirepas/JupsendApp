package googleoauth

import (
	"encoding/base64"
	"testing"
)

func TestEncodeGmailRaw(t *testing.T) {
	raw := []byte("From: a@b.com\r\nTo: c@d.com\r\n\r\nHello")
	enc := encodeGmailRaw(raw)
	if enc == "" {
		t.Fatal("expected encoded raw")
	}
	dec, err := base64.URLEncoding.DecodeString(enc)
	if err != nil || string(dec) != string(raw) {
		t.Fatalf("round trip failed: %q err=%v", dec, err)
	}
}
