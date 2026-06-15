package util

import (
	"strings"
)

func ParseContactPaste(text string) []ContactImportRow {
	lines := strings.Split(text, "\n")
	var rows []ContactImportRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		email := strings.TrimSpace(parts[0])
		if !strings.Contains(email, "@") {
			continue
		}
		row := ContactImportRow{
			Email:     email,
			Variables: make(map[string]string),
		}
		if len(parts) > 1 {
			for i := 1; i < len(parts); i++ {
				val := strings.TrimSpace(parts[i])
				row.Variables["col"+string(rune('0'+i))] = val
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func ParseContactPasteWithHeaders(text string, variableKeys []string) []ContactImportRow {
	lines := strings.Split(text, "\n")
	var rows []ContactImportRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) == 0 {
			continue
		}
		email := strings.TrimSpace(parts[0])
		if !strings.Contains(email, "@") {
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
