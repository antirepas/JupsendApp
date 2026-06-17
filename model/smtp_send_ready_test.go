package model

import "testing"

func TestSMTPAccountIsSendReady(t *testing.T) {
	oauth := SMTPAccount{
		Status:            "active",
		AuthType:          AuthTypeGoogleOAuth,
		OAuthRefreshToken: "enc",
		GoogleEmail:       "user@gmail.com",
		SMTPHost:          "smtp.gmail.com",
	}
	if !oauth.IsSendReady() {
		t.Fatal("oauth account should be send ready")
	}
	inactive := oauth
	inactive.Status = "inactive"
	if inactive.IsSendReady() {
		t.Fatal("inactive account should not be send ready")
	}
	noGmail := SMTPAccount{Status: "active", SMTPHost: "smtp.gmail.com"}
	if noGmail.IsSendReady() {
		t.Fatal("account without gmail should not be send ready")
	}
}
