package util

import (
	"regexp"
	"strings"
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
	// For this app we only validate email syntax here. DNS (MX/A) checks are
	// unreliable in some environments and are better handled by delivery/bounce
	// signals later.
	return true, ""
}
