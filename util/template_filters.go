package util

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

func isEmptyValue(s string) bool {
	return strings.TrimSpace(s) == ""
}

// ApplyFilters runs deterministic filters (not AI) on a raw value.
// Returns processed value, whether required marker is set, and whether raw (no escape) is set.
func ApplyFilters(raw string, filters []Filter, bodyMode bool) (value string, required bool, rawHTML bool) {
	value = strings.TrimSpace(raw)
	for _, f := range filters {
		switch f.Name {
		case "first":
			value = filterFirst(value)
		case "title":
			value = filterTitle(value)
		case "default":
			if isEmptyValue(value) {
				value = f.Arg
			}
		case "truncate":
			value = filterTruncate(value, f.Arg)
		case "url":
			value = filterURL(value, bodyMode)
		case "raw":
			rawHTML = true
		case "required":
			required = true
		case "fit", "summarize":
			// AI filters applied later
		default:
			// unknown — pass through; lint catches these
		}
	}
	return value, required, rawHTML
}

func filterFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func filterTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		runes := []rune(strings.ToLower(w))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func filterTruncate(s, arg string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	maxWords := 0
	if _, err := fmt.Sscanf(arg, "%d", &maxWords); err != nil || maxWords <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) <= maxWords {
		return s
	}
	return strings.Join(words[:maxWords], " ") + "…"
}

func filterURL(s string, bodyMode bool) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	u := s
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return s
	}
	normalized := parsed.String()
	if !bodyMode {
		return normalized
	}
	display := parsed.Host
	if parsed.Path != "" && parsed.Path != "/" {
		display = normalized
	}
	return fmt.Sprintf(`<a href="%s">%s</a>`, normalized, display)
}

// ValueForIfBlock resolves whether an if-block should show (after default filter only).
func ValueForIfBlock(varName string, varMap map[string]string, refs []VarRef) bool {
	raw := varMap[varName]
	// Apply only default filters for if-block visibility
	for _, ref := range refs {
		if ref.Name != varName {
			continue
		}
		for _, f := range ref.Filters {
			if f.Name == "default" && isEmptyValue(raw) {
				raw = f.Arg
			}
		}
	}
	// Also check if any ref for this var has default when raw from map is empty
	val := strings.TrimSpace(raw)
	if val != "" {
		return true
	}
	// Global default from any occurrence
	for _, ref := range refs {
		if ref.Name != varName {
			continue
		}
		for _, f := range ref.Filters {
			if f.Name == "default" && strings.TrimSpace(f.Arg) != "" {
				return true
			}
		}
	}
	return false
}
