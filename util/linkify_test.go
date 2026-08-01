package util

import (
	"strings"
	"testing"
)

func TestAutolinkBareURLs(t *testing.T) {
	in := `<p>Book here https://calendly.com/demo?x=1 and reply.</p>`
	got := AutolinkBareURLs(in)
	if !strings.Contains(got, `<a href="https://calendly.com/demo?x=1">https://calendly.com/demo?x=1</a>`) {
		t.Fatalf("expected autolinked URL, got %q", got)
	}

	already := `<p><a href="https://example.com">go</a></p>`
	if AutolinkBareURLs(already) != already {
		t.Fatalf("should not modify existing anchors: %q", AutolinkBareURLs(already))
	}

	img := `<img src="https://cdn.example.com/pixel.gif">`
	if AutolinkBareURLs(img) != img {
		t.Fatalf("should not linkify attribute URLs: %q", AutolinkBareURLs(img))
	}
}

func TestAutolinkBareURLsTrailingPunct(t *testing.T) {
	got := AutolinkBareURLs(`See https://example.com/path.`)
	if !strings.Contains(got, `<a href="https://example.com/path">https://example.com/path</a>.`) {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestNormalizeTrackableURL(t *testing.T) {
	got, ok := normalizeTrackableURL("www.example.com/x")
	if !ok || got != "https://www.example.com/x" {
		t.Fatalf("got %q %v", got, ok)
	}
	got, ok = normalizeTrackableURL("https://ex.com?a=1&amp;b=2")
	if !ok || !strings.Contains(got, "a=1") || !strings.Contains(got, "b=2") {
		t.Fatalf("unescape failed: %q %v", got, ok)
	}
}

func TestHrefAttrSingleQuotes(t *testing.T) {
	match := hrefAttrRe.FindStringSubmatch(`href='https://example.com'`)
	if match == nil || match[3] != "https://example.com" {
		t.Fatalf("single-quote href not matched: %#v", match)
	}
	match = hrefAttrRe.FindStringSubmatch(`HREF = "https://example.com"`)
	if match == nil || match[2] != "https://example.com" {
		t.Fatalf("case/spacing href not matched: %#v", match)
	}
}
