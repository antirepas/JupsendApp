package util

import (
	"regexp"
	"sort"
	"strings"
)

// Filter describes one pipe modifier on a variable reference.
type Filter struct {
	Name string
	Arg  string
}

// VarRef is a parsed {{variable|filter}} or {{~variable}} token.
type VarRef struct {
	Raw      string // exact text as it appears in template
	Name     string
	Filters  []Filter
	AIFit    bool // {{~name}} or |fit
	TokenPos int  // byte offset in source text
}

// IfBlock is a parsed {% if var %}...{% endif %} region.
type IfBlock struct {
	Raw      string
	VarName  string
	Inner    string
	TokenPos int
}

var (
	varRefRe = regexp.MustCompile(`\{\{\s*~?\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(\|[^}]*)?\s*\}\}`)
	ifBlockRe = regexp.MustCompile(`(?s)\{%\s*if\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*%\}(.*?)\{%\s*endif\s*%\}`)
)

// ParseVarRefs returns all variable references in text in document order.
func ParseVarRefs(text string) []VarRef {
	var refs []VarRef
	for _, loc := range varRefRe.FindAllStringSubmatchIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		name := text[loc[2]:loc[3]]
		ref := VarRef{
			Raw:      raw,
			Name:     name,
			TokenPos: loc[0],
		}
		if strings.Contains(raw, "~") {
			ref.AIFit = true
		}
		if loc[4] >= 0 && loc[5] > loc[4] {
			filterPart := strings.TrimPrefix(text[loc[4]:loc[5]], "|")
			ref.Filters = parseFilters(filterPart)
			for _, f := range ref.Filters {
				if f.Name == "fit" {
					ref.AIFit = true
				}
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

func parseFilters(s string) []Filter {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	var filters []Filter
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i := strings.Index(p, ":"); i >= 0 {
			filters = append(filters, Filter{Name: strings.TrimSpace(p[:i]), Arg: strings.TrimSpace(p[i+1:])})
		} else {
			filters = append(filters, Filter{Name: p})
		}
	}
	return filters
}

// ParseIfBlocks returns all liquid-style if blocks in text.
func ParseIfBlocks(text string) []IfBlock {
	var blocks []IfBlock
	for _, m := range ifBlockRe.FindAllStringSubmatchIndex(text, -1) {
		blocks = append(blocks, IfBlock{
			Raw:      text[m[0]:m[1]],
			VarName:  text[m[2]:m[3]],
			Inner:    text[m[4]:m[5]],
			TokenPos: m[0],
		})
	}
	return blocks
}

// ExtractTemplateVariables returns sorted unique base variable keys from text parts.
func ExtractTemplateVariables(parts ...string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, part := range parts {
		for _, ref := range ParseVarRefs(part) {
			if ref.Name != "" && !seen[ref.Name] {
				seen[ref.Name] = true
				keys = append(keys, ref.Name)
			}
		}
		for _, block := range ParseIfBlocks(part) {
			if block.VarName != "" && !seen[block.VarName] {
				seen[block.VarName] = true
				keys = append(keys, block.VarName)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// KnownFilters is the set of supported filter names for linting.
var KnownFilters = map[string]bool{
	"first": true, "title": true, "default": true, "required": true,
	"truncate": true, "url": true, "raw": true, "fit": true, "summarize": true,
}
