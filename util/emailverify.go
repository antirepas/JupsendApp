package util

import (
	"context"
	"net"
	"regexp"
	"strings"
	"time"
)

var emailSyntaxRe = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

func ValidateEmail(email string) (ok bool, reason string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return false, "empty email"
	}
	if len(email) > 254 {
		return false, "email too long"
	}
	if !emailSyntaxRe.MatchString(email) {
		return false, "invalid syntax"
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false, "invalid syntax"
	}
	domain := strings.ToLower(strings.TrimSpace(parts[1]))
	if domain == "" {
		return false, "missing domain"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resolver := net.Resolver{}
	mx, err := resolver.LookupMX(ctx, domain)
	if err == nil && len(mx) > 0 {
		return true, ""
	}
	addrs, aErr := resolver.LookupHost(ctx, domain)
	if aErr == nil && len(addrs) > 0 {
		return true, ""
	}
	if err != nil {
		return false, "domain has no mail server"
	}
	return false, "domain has no mail server"
}
