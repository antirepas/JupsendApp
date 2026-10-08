package util

import "strings"

const utf8BOM = "\ufeff"

// NormalizeVariableKey trims whitespace, strips a leading UTF-8 BOM (common in
// Excel/CSV first-column headers), and lowercases. Empty after trim returns "".
func NormalizeVariableKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, utf8BOM)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToLower(s)
}

// VariableKeysMatch reports whether two variable names refer to the same key.
func VariableKeysMatch(a, b string) bool {
	na, nb := NormalizeVariableKey(a), NormalizeVariableKey(b)
	return na != "" && na == nb
}
