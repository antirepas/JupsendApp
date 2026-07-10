package util

import "testing"

func TestFilterFirst(t *testing.T) {
	got, _, _ := ApplyFilters("John Smith", []Filter{{Name: "first"}}, false)
	if got != "John" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterTitle(t *testing.T) {
	got, _, _ := ApplyFilters("acme corp", []Filter{{Name: "title"}}, false)
	if got != "Acme Corp" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterDefault(t *testing.T) {
	got, _, _ := ApplyFilters("", []Filter{{Name: "default", Arg: "there"}}, false)
	if got != "there" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterTruncate(t *testing.T) {
	raw := "one two three four five six"
	got, _, _ := ApplyFilters(raw, []Filter{{Name: "truncate", Arg: "3"}}, false)
	if got != "one two three…" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterURLBody(t *testing.T) {
	got, _, _ := ApplyFilters("example.com/page", []Filter{{Name: "url"}}, true)
	if got == "" || got == "example.com/page" {
		t.Fatalf("expected anchor tag, got %q", got)
	}
}

func TestFilterRequiredMarker(t *testing.T) {
	_, required, _ := ApplyFilters("", []Filter{{Name: "required"}}, false)
	if !required {
		t.Fatal("expected required")
	}
}
