package routes

import (
	"strings"
	"testing"

	"emailtracker.com/util"
)

func TestHighlightPlaceholdersRendersMarks(t *testing.T) {
	got := string(highlightPlaceholders("hello {{company name}} there", []string{"company name"}))
	if !strings.Contains(got, `<mark class="bg-amber-100 text-amber-900 px-1 rounded">{{company name}}</mark>`) {
		t.Fatalf("missing mark: %q", got)
	}
	if strings.Contains(got, "&lt;mark") {
		t.Fatalf("mark should not be escaped: %q", got)
	}
}

func TestTemplatePreviewBodyStripsHTML(t *testing.T) {
	plain := util.HTMLToPlainPreview(`<p>Hey {{founder}},</p><p>Saw {{company name}}.</p>`)
	got := string(highlightPlaceholders(plain, []string{"founder", "company name"}))
	if strings.Contains(got, "&lt;p&gt;") || strings.Contains(got, "<p>") {
		t.Fatalf("html tags leaked: %q", got)
	}
	if !strings.Contains(got, "Hey ") || !strings.Contains(got, "Saw ") {
		t.Fatalf("plain text missing: %q", got)
	}
	if !strings.Contains(got, "<mark") {
		t.Fatalf("expected variable highlights: %q", got)
	}
}
