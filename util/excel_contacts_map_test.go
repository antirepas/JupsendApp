package util

import (
	"strings"
	"testing"
)

func TestSuggestEmailHeader(t *testing.T) {
	got := SuggestEmailHeader([]string{"Name", "Email Address", "Company"})
	if got != "Email Address" {
		t.Fatalf("got %q", got)
	}
	got = SuggestEmailHeader([]string{"Name", "e-mail", "Company"})
	if got != "e-mail" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyContactColumnMap(t *testing.T) {
	headers := []string{"Email Address", "Name", "Company", "Notes"}
	rows := [][]string{
		{"hello@acme.com, other@acme.com", "Ada", "Acme", "skip me"},
		{"bad", "Bob", "Beta", ""},
		{"person@x.com", "Cara", "Co", ""},
	}
	colMap := map[string]string{
		"Email Address": "email",
		"Name":          "name",
		"Company":       "company",
		"Notes":         "skip",
	}
	out, err := ApplyContactColumnMap(headers, rows, colMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].Email != "hello@acme.com" {
		t.Fatalf("email=%q", out[0].Email)
	}
	if out[0].Variables["name"] != "Ada" || out[0].Variables["company"] != "Acme" {
		t.Fatalf("vars=%v", out[0].Variables)
	}
	if _, ok := out[0].Variables["notes"]; ok {
		t.Fatal("notes should be skipped")
	}
}

func TestApplyContactColumnMapSegment(t *testing.T) {
	headers := []string{"Email", "Segment", "Company"}
	rows := [][]string{
		{"a@x.com", "Interested", "Acme"},
		{"b@x.com", "Not interested", "Beta"},
	}
	colMap := map[string]string{
		"Email":   "email",
		"Segment": "segment",
		"Company": "company",
	}
	out, err := ApplyContactColumnMap(headers, rows, colMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Segment != "Interested" || out[0].Variables["segment"] != "Interested" {
		t.Fatalf("row0=%+v", out[0])
	}
	if out[1].Segment != "Not interested" {
		t.Fatalf("row1 segment=%q", out[1].Segment)
	}
	if out[0].Variables["company"] != "Acme" {
		t.Fatalf("company=%v", out[0].Variables)
	}
}

func TestSuggestContactColumnMapSegmentHeader(t *testing.T) {
	m := SuggestContactColumnMap([]string{"Email", "Segment", "Name"}, nil)
	if m["Segment"] != MapTargetSegment {
		t.Fatalf("got %v", m)
	}
}

func TestApplyContactColumnMapRejectsDuplicateEmail(t *testing.T) {
	_, err := ApplyContactColumnMap(
		[]string{"A", "B"},
		[][]string{{"a@x.com", "b@x.com"}},
		map[string]string{"A": "email", "B": "email"},
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPeekContactsUploadRaw(t *testing.T) {
	csv := "Email Address,Name\nhello@x.com,Ada\n"
	peek, err := PeekContactsUploadRaw(strings.NewReader(csv), "contacts.csv", nil)
	if err != nil {
		t.Fatal(err)
	}
	if peek.RowCount != 1 {
		t.Fatalf("row_count=%d", peek.RowCount)
	}
	if peek.SuggestedMap["Email Address"] != MapTargetEmail {
		t.Fatalf("suggested=%v", peek.SuggestedMap)
	}
	if len(peek.SampleRows) != 1 || peek.SampleRows[0][0] != "hello@x.com" {
		t.Fatalf("sample=%v", peek.SampleRows)
	}
}

func TestFormatResolvedEmailSample(t *testing.T) {
	got := FormatResolvedEmailSample("a@x.com, hello@x.com")
	if got != "hello@x.com (from 2 in cell)" {
		t.Fatalf("got %q", got)
	}
}
