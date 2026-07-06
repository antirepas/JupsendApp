package googleoauth

import "testing"

func TestXOAuth2String(t *testing.T) {
	s := XOAuth2String("user@gmail.com", "access-token")
	if s == "" {
		t.Fatal("expected encoded string")
	}
}

func TestSMTPAuthInitialResponse(t *testing.T) {
	auth := SMTPAuth("user@gmail.com", "access-token")
	proto, ir, err := auth.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	if proto != "XOAUTH2" {
		t.Fatalf("proto=%q", proto)
	}
	if len(ir) == 0 {
		t.Fatal("expected initial XOAUTH2 response in Start()")
	}
	if string(ir) != "user=user@gmail.com\x01auth=Bearer access-token\x01\x01" {
		t.Fatalf("unexpected payload: %q", ir)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	enc, err := Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(enc)
	if err != nil || plain != "refresh-secret" {
		t.Fatalf("round trip failed: %q err=%v", plain, err)
	}
}
