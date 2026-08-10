package model

import (
	"testing"
)

func TestParseAdminOutreachMailboxSpecs(t *testing.T) {
	specs := ParseAdminOutreachMailboxSpecs("hello, Alex:Smith:sales , bob@tryjupsend.com, wrong@other.com", "tryjupsend.com")
	if len(specs) != 3 {
		t.Fatalf("got %d specs: %+v", len(specs), specs)
	}
	if specs[0].LocalPart != "hello" || specs[0].FirstName != "Hello" {
		t.Fatalf("hello: %+v", specs[0])
	}
	if specs[1].LocalPart != "sales" || specs[1].FirstName != "Alex" || specs[1].LastName != "Smith" {
		t.Fatalf("sales: %+v", specs[1])
	}
	if specs[2].LocalPart != "bob" {
		t.Fatalf("bob: %+v", specs[2])
	}
}
