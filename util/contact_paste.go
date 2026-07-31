package util

import (
	"strings"
)

// ParseContactPaste imports email-only rows (one email per line).
func ParseContactPaste(text string) []ContactImportRow {
	lines := strings.Split(text, "\n")
	var rows []ContactImportRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" { //
			continue
		}
		email, ok := ResolveImportEmail(line)
		if !ok {
			parts := splitCSVLine(line)
			if len(parts) > 0 {
				email, ok = ResolveImportEmail(parts[0])
			}
		}
		if !ok {
			continue
		}
		rows = append(rows, ContactImportRow{Email: email, Variables: map[string]string{}})
	}
	return rows
}

func pasteNormalizeHeader(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// DetectPasteVariableKeys reads a header row like email,name,company.
func DetectPasteVariableKeys(paste string) []string {
	lines := strings.Split(paste, "\n")
	if len(lines) == 0 {
		return nil
	}
	parts := splitCSVLine(strings.TrimSpace(lines[0]))
	if len(parts) < 2 || pasteNormalizeHeader(parts[0]) != "email" {
		return nil
	}
	var keys []string
	for i := 1; i < len(parts); i++ {
		k := pasteNormalizeHeader(parts[i])
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// ParseContactPasteAuto detects headers or falls back to email-only rows.
func ParseContactPasteAuto(paste string) ([]ContactImportRow, []string) {
	keys := DetectPasteVariableKeys(paste)
	if len(keys) == 0 {
		return ParseContactPaste(paste), nil
	}
	lines := strings.Split(paste, "\n")
	if len(lines) <= 1 {
		return nil, keys
	}
	body := strings.Join(lines[1:], "\n")
	return ParseContactPasteWithHeaders(body, keys), keys
}

func ParseContactPasteWithHeaders(text string, variableKeys []string) []ContactImportRow {
	lines := strings.Split(text, "\n")
	var rows []ContactImportRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := splitCSVLine(line)
		if len(parts) == 0 {
			continue
		}
		email, ok := ResolveImportEmail(parts[0])
		if !ok {
			continue
		}
		row := ContactImportRow{
			Email:     email,
			Variables: make(map[string]string),
		}
		for i, key := range variableKeys {
			idx := i + 1
			if idx < len(parts) {
				row.Variables[key] = strings.TrimSpace(parts[idx])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// splitCSVLine splits a paste row on commas outside quoted strings.
func splitCSVLine(line string) []string {
	var parts []string
	var b strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if inQuotes {
				b.WriteByte(ch)
			} else {
				parts = append(parts, strings.TrimSpace(b.String()))
				b.Reset()
			}
		default:
			b.WriteByte(ch)
		}
	}
	parts = append(parts, strings.TrimSpace(b.String()))
	return parts
}
