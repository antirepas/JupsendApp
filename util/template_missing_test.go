package util

import (
	"testing"

	"emailtracker.com/model"
)

func TestMissingContactVarsForTemplates(t *testing.T) {
	vars := []model.ContactVariables{
		{Key: "Name", Value: "Alex"},
		{Key: "company", Value: ""},
	}
	missing := MissingContactVarsForTemplates(vars, "Hi {{name}} at {{company}}")
	if len(missing) != 1 || missing[0] != "company" {
		t.Fatalf("got %v", missing)
	}
	// Case-insensitive match for Name
	missing = MissingContactVarsForTemplates(vars, "Hi {{NAME}}")
	if len(missing) != 0 {
		t.Fatalf("expected name present, got %v", missing)
	}
	// Default covers empty
	missing = MissingContactVarsForTemplates(vars, "Hi {{company|default:there}}")
	if len(missing) != 0 {
		t.Fatalf("expected default to cover, got %v", missing)
	}
}
