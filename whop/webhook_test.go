package whop

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"

	"emailtracker.com/config"
)

func TestVerifyWebhookSignature(t *testing.T) {
	config.WhopWebhookSecret = "whsec_testsecret"
	body := []byte(`{"type":"membership.activated"}`)
	ts := "1234567890"
	msgID := "msg_123"
	payload := msgID + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, []byte("testsecret"))
	mac.Write([]byte(payload))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil)) //

	h := http.Header{}
	h.Set("webhook-id", msgID)
	h.Set("webhook-timestamp", ts)
	h.Set("webhook-signature", sig)
	if !VerifyWebhookSignature(body, h) {
		t.Fatal("expected valid signature")
	}
}

func TestUserIDFromMetadata(t *testing.T) {
	id := UserIDFromMetadata(map[string]interface{}{"user_id": "42"})
	if id != 42 {
		t.Fatalf("expected 42 got %d", id)
	}
}
