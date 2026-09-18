package util

import (
	"net"
	"testing"
)

func TestCheckDNSAuthPass(t *testing.T) {
	origMX, origTXT := lookupMX, lookupTXT
	defer func() { lookupMX, lookupTXT = origMX, origTXT }()

	lookupMX = func(name string) ([]*net.MX, error) {
		return []*net.MX{{Host: "aspmx.l.google.com.", Pref: 1}, {Host: "alt1.aspmx.l.google.com.", Pref: 5}}, nil
	}
	lookupTXT = func(name string) ([]string, error) {
		switch name {
		case "example.com":
			return []string{"v=spf1 include:_spf.google.com ~all"}, nil
		case "_dmarc.example.com":
			return []string{"v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com"}, nil
		case "google._domainkey.example.com":
			return []string{"v=DKIM1; k=rsa; p=MIGf"}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		}
	}

	checks := CheckDNSAuth("example.com")
	byName := map[string]DNSAuthCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["MX"].Status != "pass" {
		t.Fatalf("MX: %+v", byName["MX"])
	}
	if byName["SPF"].Status != "pass" {
		t.Fatalf("SPF: %+v", byName["SPF"])
	}
	if byName["DKIM"].Status != "pass" {
		t.Fatalf("DKIM: %+v", byName["DKIM"])
	}
	if byName["DMARC"].Status != "pass" {
		t.Fatalf("DMARC: %+v", byName["DMARC"])
	}
}

func TestCheckDNSAuthFailAndUnknown(t *testing.T) {
	origMX, origTXT := lookupMX, lookupTXT
	defer func() { lookupMX, lookupTXT = origMX, origTXT }()

	lookupMX = func(name string) ([]*net.MX, error) {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	lookupTXT = func(name string) ([]string, error) {
		if name == "_dmarc.bad.test" {
			return []string{"v=DMARC1; p=none"}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}

	checks := CheckDNSAuth("bad.test")
	byName := map[string]DNSAuthCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["MX"].Status != "fail" {
		t.Fatalf("MX want fail got %+v", byName["MX"])
	}
	if byName["SPF"].Status != "fail" {
		t.Fatalf("SPF want fail got %+v", byName["SPF"])
	}
	if byName["DKIM"].Status != "unknown" {
		t.Fatalf("DKIM want unknown got %+v", byName["DKIM"])
	}
	if byName["DMARC"].Status != "warn" {
		t.Fatalf("DMARC p=none should warn got %+v", byName["DMARC"])
	}
}

func TestCheckDNSAuthEmptyDomain(t *testing.T) {
	checks := CheckDNSAuth("  ")
	if len(checks) != 1 || checks[0].Status != "fail" {
		t.Fatalf("%+v", checks)
	}
}
