package util

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequestClientIP returns the best-effort public client IP for tracking requests.
// Prefer Gin ClientIP when trusted proxies are configured; also honor Cloudflare / common proxy headers.
func RequestClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if v := strings.TrimSpace(c.GetHeader("CF-Connecting-IP")); v != "" && isPlausibleIP(v) {
		return v
	}
	if v := strings.TrimSpace(c.GetHeader("True-Client-IP")); v != "" && isPlausibleIP(v) {
		return v
	}
	if xff := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); xff != "" {
		// Left-most is original client when proxies append.
		parts := strings.Split(xff, ",")
		for _, p := range parts {
			ip := strings.TrimSpace(p)
			if isPlausibleIP(ip) && !isLoopbackOrUnspecified(ip) {
				return ip
			}
		}
	}
	if v := strings.TrimSpace(c.GetHeader("X-Real-IP")); v != "" && isPlausibleIP(v) {
		return v
	}
	ip := strings.TrimSpace(c.ClientIP())
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	return ip
}

func isPlausibleIP(s string) bool {
	return net.ParseIP(s) != nil
}

func isLoopbackOrUnspecified(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsUnspecified()
}
