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

// VarRef is a parsed {{variable|filter}}, {{@mailbox}}, or {{~variable}} token.
type VarRef struct {
	Raw      string // exact text as it appears in template
	Name     string
	Filters  []Filter
	AIFit    bool // {{~name}} or |fit
	Mailbox  bool // {{@name}} — filled from the sending mailbox, not the contact
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
	// Optional ~ (AI) and/or @ (mailbox), then variable name, then optional filters.
	varRefRe  = regexp.MustCompile(`\{\{\s*(~)?\s*(@)?\s*([a-zA-Z_][a-zA-Z0-9_ ]*)\s*(\|[^}]*)?\s*\}\}`)
	ifBlockRe = regexp.MustCompile(`(?s)\{%\s*if\s+([a-zA-Z_][a-zA-Z0-9_ ]*)\s*%\}(.*?)\{%\s*endif\s*%\}`)
)

// ParseVarRefs returns all variable references in text in document order.
func ParseVarRefs(text string) []VarRef {
	var refs []VarRef
	for _, loc := range varRefRe.FindAllStringSubmatchIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		name := strings.TrimSpace(text[loc[6]:loc[7]])
		ref := VarRef{
			Raw:      raw,
			Name:     name,
			TokenPos: loc[0],
			AIFit:    loc[2] >= 0 && loc[3] > loc[2],
			Mailbox:  loc[4] >= 0 && loc[5] > loc[4],
		}
		if loc[8] >= 0 && loc[9] > loc[8] {
			filterPart := strings.TrimPrefix(text[loc[8]:loc[9]], "|")
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
			VarName:  strings.TrimSpace(text[m[2]:m[3]]),
			Inner:    text[m[4]:m[5]],
			TokenPos: m[0],
		})
	}
	return blocks
}

// ExtractTemplateVariables returns sorted unique contact variable keys (excludes {{@…}} mailbox vars).
func ExtractTemplateVariables(parts ...string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, part := range parts {
		for _, ref := range ParseVarRefs(part) {
			if ref.Mailbox || ref.Name == "" || seen[ref.Name] {
				continue
			}
			seen[ref.Name] = true
			keys = append(keys, ref.Name)
		}
		for _, block := range ParseIfBlocks(part) {
			if block.VarName == "" || seen[block.VarName] {
				continue
			}
			seen[block.VarName] = true
			keys = append(keys, block.VarName)
		}
	}
	sort.Strings(keys)
	return keys
}

// ExtractMailboxVariables returns sorted unique mailbox variable keys used as {{@key}}.
func ExtractMailboxVariables(parts ...string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, part := range parts {
		for _, ref := range ParseVarRefs(part) {
			if !ref.Mailbox || ref.Name == "" || seen[ref.Name] {
				continue
			}
			seen[ref.Name] = true
			keys = append(keys, ref.Name)
		}
	}
	sort.Strings(keys)
	return keys
}

// KnownMailboxVariableKeys are filled from the sending mailbox at send time.
var KnownMailboxVariableKeys = map[string]bool{
	"name":       true,
	"first_name": true,
	"email":      true,
}

// KnownFilters is the set of supported filter names for linting.
var KnownFilters = map[string]bool{
	"first": true, "title": true, "default": true, "required": true,
	"truncate": true, "url": true, "raw": true, "fit": true, "summarize": true,
}
