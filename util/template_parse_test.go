package util

import (
	"reflect"
	"testing"
)

func TestParseVarRefsMultiWord(t *testing.T) {
	text := `Hi {{company name|title}} and {{~company name}}`
	refs := ParseVarRefs(text)
	if len(refs) != 2 {
		t.Fatalf("got %d refs", len(refs))
	}
	if refs[0].Name != "company name" {
		t.Fatalf("first ref name %q", refs[0].Name)
	}
	if refs[1].Name != "company name" || !refs[1].AIFit {
		t.Fatalf("second ref: %+v", refs[1])
	}
}

func TestParseIfBlocksMultiWord(t *testing.T) {
	text := `{% if company name %}Hello {{company name}}.{% endif %}`
	blocks := ParseIfBlocks(text)
	if len(blocks) != 1 || blocks[0].VarName != "company name" {
		t.Fatalf("got %+v", blocks)
	}
}

func TestExtractTemplateVariablesMultiWord(t *testing.T) {
	got := ExtractTemplateVariables("Hello {{company name}}")
	want := []string{"company name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseVarRefsWithFilters(t *testing.T) {
	text := `Hi {{name|first|default:there}} and {{~description|summarize:20}}`
	refs := ParseVarRefs(text)
	if len(refs) != 2 {
		t.Fatalf("got %d refs", len(refs))
	}
	if refs[0].Name != "name" || len(refs[0].Filters) < 2 {
		t.Fatalf("name ref: %+v", refs[0])
	}
	if refs[1].Name != "description" || !refs[1].AIFit {
		t.Fatalf("description ref: %+v", refs[1])
	}
}

func TestParseIfBlocks(t *testing.T) {
	text := `{% if description %}Hello {{description}}.{% endif %}`
	blocks := ParseIfBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if blocks[0].VarName != "description" {
		t.Fatalf("var %q", blocks[0].VarName)
	}
	if blocks[0].Inner != "Hello {{description}}." {
		t.Fatalf("inner %q", blocks[0].Inner)
	}
}

func TestExtractTemplateVariablesWithFiltersAndIf(t *testing.T) {
	got := ExtractTemplateVariables(
		"{{name|first}}",
		"{% if company %}at {{company|title}}{% endif %}",
	)
	want := []string{"company", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
