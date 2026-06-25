package util

import (
	"reflect"
	"testing"
)

func TestExtractTemplateVariables(t *testing.T) {
	got := ExtractTemplateVariables(
		"Hello {{name}} at {{company}}",
		"<p>Hi {{ name }}, welcome to {{company}} and {{role}}</p>",
	)
	want := []string{"company", "name", "role"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExtractTemplateVariablesEmpty(t *testing.T) {
	if got := ExtractTemplateVariables("", "no placeholders"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestExtractTemplateVariablesInvalidKeys(t *testing.T) {
	got := ExtractTemplateVariables("{{123bad}} {{_ok}} {{also-valid}}")
	want := []string{"_ok"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
