package util

import "testing"

func TestHTMLToPlainPreview(t *testing.T) {
	in := `<p>Hey there,</p><p>Noticed you&#39;re building something.</p><p>Quick question?</p>`
	got := HTMLToPlainPreview(in)
	want := "Hey there,\nNoticed you're building something.\nQuick question?"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestHTMLToPlainPreviewLeavesPlain(t *testing.T) {
	in := "Just plain text with {{name}}"
	if got := HTMLToPlainPreview(in); got != in {
		t.Fatalf("got %q", got)
	}
}
