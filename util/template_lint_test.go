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

func TestLintTemplate_partialPersonalization(t *testing.T) {
	issues := LintTemplate("Hi {{name}}", "<p>Question for you</p>")
	found := false
	for _, i := range issues {
		if i.Code == "partial_personalization" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected partial_personalization issue")
	}
}

func TestLintTemplate_noCTA(t *testing.T) {
	issues := LintTemplate("Hi {{name}}", "<p>Just checking in.</p>")
	found := false
	for _, i := range issues {
		if i.Code == "no_cta" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected no_cta issue")
	}
}
