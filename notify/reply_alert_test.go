package notify

import "testing"

func TestCompactSnippet(t *testing.T) {
	if got := compactSnippet("  hello  ", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	long := stringsRepeat("a", 50)
	got := compactSnippet(long, 20)
	if len([]rune(got)) != 21 { // 20 + ellipsis
		t.Fatalf("len=%d got=%q", len([]rune(got)), got)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
