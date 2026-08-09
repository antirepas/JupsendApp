package util

import (
	"fmt"
	"strings"
	"unicode"
)

type TemplateLintIssue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

func LintTemplate(subject, bodyHTML string) []TemplateLintIssue {
	var issues []TemplateLintIssue

	subjectRunes := len([]rune(subject))
	if subjectRunes > 60 {
		issues = append(issues, TemplateLintIssue{
			Level:   "warn",
			Code:    "subject_long",
			Message: fmt.Sprintf("Subject is %d chars — may truncate on mobile", subjectRunes),
			Source:  "rule",
		})
	}

	if isMostlyUppercase(subject) {
		issues = append(issues, TemplateLintIssue{
			Level:   "warn",
			Code:    "subject_shouting",
			Message: "Subject is mostly uppercase",
			Source:  "rule",
		})
	}

	vars := ExtractTemplateVariables(subject, bodyHTML)
	if len(vars) == 0 {
		issues = append(issues, TemplateLintIssue{
			Level:   "info",
			Code:    "no_personalization",
			Message: "Tip: a {{name}} placeholder can make cold emails feel more personal",
			Source:  "rule",
		})
	}

	bodyLower := strings.ToLower(bodyHTML)
	plain := StripHTML(bodyHTML)
	hasLink := strings.Contains(bodyLower, "<a ") || strings.Contains(bodyLower, "href=")
	if !hasLink && len([]rune(plain)) >= 80 && !hasClearAsk(plain) {
		issues = append(issues, TemplateLintIssue{
			Level:   "info",
			Code:    "no_cta",
			Message: "Tip: a question or link can make your ask clearer — optional for short notes",
			Source:  "rule",
		})
	}

	if plain != "" {
		firstLine := firstNonEmptyLine(plain)
		if len([]rune(firstLine)) > 80 {
			issues = append(issues, TemplateLintIssue{
				Level:   "info",
				Code:    "opener_long",
				Message: "Tip: a shorter first line can be easier to scan on mobile",
				Source:  "rule",
			})
		}
	}

	combined := subject + "\n" + bodyHTML
	for _, v := range vars {
		count := countVarOccurrences(combined, v)
		if count >= 4 {
			issues = append(issues, TemplateLintIssue{
				Level:   "warn",
				Code:    "var_repeated",
				Message: fmt.Sprintf("You used {{%s}} %d times — might feel templated", v, count),
				Source:  "rule",
			})
		}
	}

	lintFilters(combined, &issues)
	if strings.Contains(subject, "~") || strings.Contains(subject, "|fit") || strings.Contains(subject, "|summarize") {
		issues = append(issues, TemplateLintIssue{
			Level:   "warn",
			Code:    "ai_in_subject",
			Message: "AI filters in the subject add latency and use credits — consider using them in the body only",
			Source:  "rule",
		})
	}

	return issues
}

func lintFilters(text string, issues *[]TemplateLintIssue) {
	seenRequired := map[string]bool{}
	for _, ref := range ParseVarRefs(text) {
		displayName := ref.Name
		if ref.Mailbox {
			displayName = "@" + ref.Name
			if !KnownMailboxVariableKeys[ref.Name] {
				*issues = append(*issues, TemplateLintIssue{
					Level:   "warn",
					Code:    "unknown_mailbox_var",
					Message: fmt.Sprintf("Unknown mailbox variable {{@%s}} — use @name, @first_name, or @email", ref.Name),
					Source:  "rule",
				})
			}
		}
		for _, f := range ref.Filters {
			if !KnownFilters[f.Name] {
				*issues = append(*issues, TemplateLintIssue{
					Level:   "warn",
					Code:    "unknown_filter",
					Message: fmt.Sprintf("Unknown filter %q on {{%s}}", f.Name, displayName),
					Source:  "rule",
				})
			}
			if f.Name == "required" && !ref.Mailbox && !seenRequired[ref.Name] {
				seenRequired[ref.Name] = true
				*issues = append(*issues, TemplateLintIssue{
					Level:   "info",
					Code:    "required_without_default",
					Message: fmt.Sprintf("{{%s|required}} — contacts missing this value will be skipped at send", ref.Name),
					Source:  "rule",
				})
			}
			if f.Name == "raw" {
				*issues = append(*issues, TemplateLintIssue{
					Level:   "warn",
					Code:    "raw_html",
					Message: fmt.Sprintf("{{%s|raw}} inserts unescaped HTML — only use with trusted content", displayName),
					Source:  "rule",
				})
			}
		}
	}
}

func hasClearAsk(plain string) bool {
	if strings.Contains(plain, "?") {
		return true
	}
	lower := strings.ToLower(plain)
	for _, phrase := range []string{"let me know", "open to", "would you", "could we", "schedule", "quick call", "quick chat"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(text)
}

func isMostlyUppercase(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	letters := 0
	upper := 0
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			upper++
		}
	}
	return letters >= 8 && upper*100/letters >= 70
}

func countVarOccurrences(text, key string) int {
	count := 0
	for _, ref := range ParseVarRefs(text) {
		if ref.Name == key {
			count++
		}
	}
	return count
}
