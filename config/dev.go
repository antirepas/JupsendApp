package config

import (
	"os"
	"strings"
)

const defaultDevLoginEmail = "dev@localhost.local"

// IsLocalDev is true when BASE_URL points at loopback (localhost / 127.0.0.1).
// Set ALLOW_DEV_LOGIN=false to disable even on loopback.
func IsLocalDev() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_DEV_LOGIN")), "false") {
		return false
	}
	return isLoopbackHost(urlHost(BaseURL))
}

func DevLoginEmail() string {
	if e := strings.TrimSpace(os.Getenv("DEV_LOGIN_EMAIL")); e != "" {
		return strings.ToLower(e)
	}
	return defaultDevLoginEmail
}
