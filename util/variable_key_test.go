package util

import "testing"

func TestNormalizeVariableKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"fullname", "fullname"},
		{"  FullName  ", "fullname"},
		{"\ufefffullname", "fullname"},
		{"\ufeffFULLNAME", "fullname"},
		{"companyName", "companyname"},
		{"", ""},
		{"   ", ""},
		{"\ufeff", ""},
	}
	for _, tc := range cases {
		if got := NormalizeVariableKey(tc.in); got != tc.want {
			t.Fatalf("NormalizeVariableKey(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestVariableKeysMatch(t *testing.T) {
	if !VariableKeysMatch("\ufefffullname", "fullname") {
		t.Fatal("BOM fullname should match fullname")
	}
	if !VariableKeysMatch("companyName", "companyname") {
		t.Fatal("camelCase should match lower")
	}
	if VariableKeysMatch("fullname", "companyname") {
		t.Fatal("different keys must not match")
	}
}
