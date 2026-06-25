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
			Message: "No {{variables}} detected",
			Source:  "rule",
		})
	}

	hasName := containsVar(vars, "name")
	hasCompany := containsVar(vars, "company")
	if hasName && !hasCompany {
		issues = append(issues, TemplateLintIssue{
			Level:   "info",
			Code:    "partial_personalization",
			Message: "You're using {{name}} but not {{company}} — consider referencing their company",
			Source:  "rule",
		})
	} else if hasCompany && !hasName {
		issues = append(issues, TemplateLintIssue{
			Level:   "info",
			Code:    "partial_personalization",
			Message: "You're using {{company}} but not {{name}} — a name often feels more personal",
			Source:  "rule",
		})
	}

	bodyLower := strings.ToLower(bodyHTML)
	if !strings.Contains(bodyLower, "<a ") && !strings.Contains(bodyLower, "href=") {
		issues = append(issues, TemplateLintIssue{
			Level:   "info",
			Code:    "no_cta",
			Message: "No clear CTA / link detected",
			Source:  "rule",
		})
	}

	plain := StripHTML(bodyHTML)
	if plain != "" {
		firstLine := firstNonEmptyLine(plain)
		if len([]rune(firstLine)) > 80 {
			issues = append(issues, TemplateLintIssue{
				Level:   "info",
				Code:    "opener_long",
				Message: "First line is long — consider a shorter opener",
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

	return issues
}

func containsVar(vars []string, key string) bool {
	for _, v := range vars {
		if v == key {
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
	for _, m := range templateVarRe.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 && m[1] == key {
			count++
		}
	}
	return count
}
