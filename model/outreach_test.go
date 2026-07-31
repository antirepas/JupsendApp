package model

import "testing"

func TestSanitizeLocalPart(t *testing.T) {
	if got := sanitizeLocalPart(" Alex.Out! "); got != "alex.out" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeLocalPart("..."); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParseMailboxSpecsJSON(t *testing.T) {
	specs, err := ParseMailboxSpecsJSON(`[{"FirstName":"A","LastName":"B","LocalPart":"a"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].LocalPart != "a" {
		t.Fatalf("%+v", specs)
	}
}
