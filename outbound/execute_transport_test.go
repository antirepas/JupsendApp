package outbound

import (
	"testing"

	"emailtracker.com/model"
)

func TestSendTransportGoogleOAuth(t *testing.T) {
	acc := model.SMTPAccount{AuthType: model.AuthTypeGoogleOAuth, OAuthRefreshToken: "enc"}
	if got := sendTransport(acc); got != "smtp-xoauth2" {
		t.Fatalf("got %q want smtp-xoauth2", got)
	}
}

func TestSendTransportPlainSMTP(t *testing.T) {
	acc := model.SMTPAccount{AuthType: "password"}
	if got := sendTransport(acc); got != "smtp" {
		t.Fatalf("got %q want smtp", got)
	}
}
