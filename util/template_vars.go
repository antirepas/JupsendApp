package util

import (
	"regexp"
	"sort"
)

var templateVarRe = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

func ExtractTemplateVariables(parts ...string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, part := range parts {
		for _, m := range templateVarRe.FindAllStringSubmatch(part, -1) {
			if len(m) < 2 {
				continue
			}
			key := m[1]
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
