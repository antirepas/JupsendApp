package model

import "testing"

func TestUnsubscribeTokenRoundTrip(t *testing.T) {
	token := UnsubscribeToken(42, 99)
	uid, cid, err := VerifyUnsubscribeToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if uid != 42 || cid != 99 {
		t.Fatalf("got %d %d", uid, cid)
	}
}

func TestUnsubscribeTokenInvalid(t *testing.T) {
	_, _, err := VerifyUnsubscribeToken("bad-token")
	if err == nil {
		t.Fatal("expected error")
	}
}
