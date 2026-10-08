package model

import "strings"

const utf8BOM = "\ufeff"

// NormalizeVariableKey trims whitespace, strips a leading UTF-8 BOM, and lowercases.
func NormalizeVariableKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, utf8BOM)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToLower(s)
}
