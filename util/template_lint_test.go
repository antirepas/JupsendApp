package util

import (
	"strings"
	"testing"
)

func TestLintTemplate_subjectLong(t *testing.T) {
	subject := strings.Repeat("a", 65)
	issues := LintTemplate(subject, "<p>Hi</p>")
	found := false
	for _, i := range issues {
		if i.Code == "subject_long" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected subject_long issue")
	}
}

func TestLintTemplate_noPersonalization(t *testing.T) {
	issues := LintTemplate("Hello there", "<p>Plain email</p>")
	found := false
	for _, i := range issues {
		if i.Code == "no_personalization" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected no_personalization issue")
	}
}

func TestLintTemplate_varRepeated(t *testing.T) {
	body := "<p>{{company}} and {{company}} and {{company}} and {{company}}</p>"
	issues := LintTemplate("Hi {{company}}", body)
	found := false
	for _, i := range issues {
		if i.Code == "var_repeated" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected var_repeated issue")
	}
}

func TestLintTemplate_nameOnlyNoCompanyNag(t *testing.T) {
	issues := LintTemplate("Hi {{name}}", "<p>Just checking in — would you be open to a quick chat?</p>")
	for _, i := range issues {
		if i.Code == "partial_personalization" {
			t.Fatal("should not nag about missing company when name is used")
		}
	}
}

func TestLintTemplate_noCTA(t *testing.T) {
	body := "<p>" + strings.Repeat("Just checking in with a longer note that has no ask. ", 6) + "</p>"
	issues := LintTemplate("Hi {{name}}", body)
	found := false
	for _, i := range issues {
		if i.Code == "no_cta" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected no_cta issue for long body without ask")
	}
}

func TestLintTemplate_shortNoteSkipsCTA(t *testing.T) {
	issues := LintTemplate("Hi {{name}}", "<p>Just checking in.</p>")
	for _, i := range issues {
		if i.Code == "no_cta" {
			t.Fatal("short notes should not get CTA nag")
		}
	}
}
