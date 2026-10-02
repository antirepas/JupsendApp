package model

import (
	"strings"
	"testing"
)

func TestSOPFilename(t *testing.T) {
	c := Campaign{ID: 42, Name: "Bar Staff / Amsterdam!!"}
	got := sopFilename(c)
	want := "campaign-42-bar-staff-amsterdam-sop.md"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSOPPlainBodyStripsHTML(t *testing.T) {
	html := `<p>Hi <strong>there</strong>,</p><p>You can find my CV here: <a href="https://example.com">CV</a>.</p>`
	got := sopPlainBody(html)
	for _, part := range []string{"Hi there", "You can find my CV here", "CV"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
	if strings.Contains(got, "<p>") || strings.Contains(got, "<a href") {
		t.Fatalf("html tags left in: %q", got)
	}
}

func TestSOPExecutionModeLabel(t *testing.T) {
	if sopExecutionModeLabel("workflow_ab") != "Hybrid workflow + A/B first email" {
		t.Fatal(sopExecutionModeLabel("workflow_ab"))
	}
	if sopExecutionModeLabel("") != "One-time / bulk send" {
		t.Fatal(sopExecutionModeLabel(""))
	}
}
