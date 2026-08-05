package util

import (
	"net/smtp"
	"strings"
	"testing"
)

func TestSMTPPasswordAuthPrefersLogin(t *testing.T) {
	auth := SMTPPasswordAuth("smtp.gmail.com", "a@b.com", "secret", true).(*smtpPasswordAuth)
	mech, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: true, Auth: []string{"PLAIN", "LOGIN"}})
	if err != nil {
		t.Fatal(err)
	}
	if mech != "LOGIN" {
		t.Fatalf("mech=%s", mech)
	}
	if resp != nil {
		t.Fatalf("LOGIN start should have empty resp")
	}
	u, err := auth.Next([]byte("Username:"), true)
	if err != nil || string(u) != "a@b.com" {
		t.Fatalf("user=%q err=%v", u, err)
	}
	p, err := auth.Next([]byte("Password:"), true)
	if err != nil || string(p) != "secret" {
		t.Fatalf("pass=%q err=%v", p, err)
	}
}

func TestSMTPPasswordAuthPlainOverImplicitTLS(t *testing.T) {
	auth := SMTPPasswordAuth("smtp.gmail.com", "a@b.com", "secret", true).(*smtpPasswordAuth)
	mech, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: false, Auth: []string{"PLAIN"}})
	if err != nil {
		t.Fatal(err)
	}
	if mech != "PLAIN" {
		t.Fatalf("mech=%s", mech)
	}
	if !strings.Contains(string(resp), "a@b.com") || !strings.Contains(string(resp), "secret") {
		t.Fatalf("resp=%q", resp)
	}
}

func TestSMTPPasswordAuthRejectsCleartext(t *testing.T) {
	auth := SMTPPasswordAuth("smtp.gmail.com", "a@b.com", "secret", false)
	_, _, err := auth.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: false, Auth: []string{"PLAIN"}})
	if err == nil {
		t.Fatal("expected unencrypted error")
	}
}
