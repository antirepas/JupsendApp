package googleoauth

import "testing"

func TestXOAuth2String(t *testing.T) {
	s := XOAuth2String("user@gmail.com", "access-token")
	if s == "" {
		t.Fatal("expected encoded string")
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
