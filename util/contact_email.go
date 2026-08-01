package util

import (
	"strings"
)

// Preferred local-parts when a cell contains multiple emails separated by ';'.
// Lower score wins.
var preferredEmailLocalParts = map[string]int{
	"hello":   1,
	"contact": 2,
	"support": 3,
}

// SplitEmailCandidates splits a raw email cell on ';' or ',' into candidate addresses.
func SplitEmailCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Normalize commas to semicolons so one splitter handles both separators.
	raw = strings.ReplaceAll(raw, ",", ";")
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NormalizeEmail trims whitespace and lowercases the full address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// DedupeEmails removes duplicates while preserving first-seen order.
func DedupeEmails(emails []string) []string {
	if len(emails) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(emails))
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// RankPreferredEmail picks the best email from normalized, deduped candidates.
func RankPreferredEmail(emails []string) string {
	if len(emails) == 0 {
		return ""
	}
	best := emails[0]
	bestScore := emailPreferenceScore(best)
	for _, e := range emails[1:] {
		score := emailPreferenceScore(e)
		if score < bestScore {
			best = e
			bestScore = score
		}
	}
	return best
}

func emailPreferenceScore(email string) int {
	local := email
	if i := strings.Index(email, "@"); i >= 0 {
		local = email[:i]
	}
	if score, ok := preferredEmailLocalParts[local]; ok {
		return score
	}
	return 100
}

// ResolveImportEmail splits on ';' or ',', normalizes, dedupes, validates, and ranks emails.
func ResolveImportEmail(raw string) (string, bool) {
	candidates := SplitEmailCandidates(raw)
	if len(candidates) == 0 {
		return "", false
	}

	var valid []string
	for _, c := range candidates {
		n := NormalizeEmail(c)
		if n == "" {
			continue
		}
		if ok, _ := ValidateEmail(n); ok {
			valid = append(valid, n)
		}
	}
	valid = DedupeEmails(valid)
	if len(valid) == 0 {
		return "", false
	}
	return RankPreferredEmail(valid), true
}
