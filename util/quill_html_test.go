package util

import (
	"strings"
	"testing"
)

func TestNormalizeQuillHTML_Bullets(t *testing.T) {
	in := `<p>Hi</p><ol><li data-list="bullet"><span class="ql-ui" contenteditable="false"></span>One</li><li data-list="bullet"><span class="ql-ui" contenteditable="false"></span>Two</li></ol>`
	out := NormalizeQuillHTML(in)
	if strings.Contains(out, "data-list") {
		t.Fatalf("still has data-list: %s", out)
	}
	if strings.Contains(out, "ql-ui") {
		t.Fatalf("still has ql-ui: %s", out)
	}
	if !strings.Contains(out, "<ul>") || !strings.Contains(out, "</ul>") {
		t.Fatalf("expected <ul>, got: %s", out)
	}
	if !strings.Contains(out, "<li>One</li>") || !strings.Contains(out, "<li>Two</li>") {
		t.Fatalf("missing items: %s", out)
	}
}

func TestNormalizeQuillHTML_Ordered(t *testing.T) {
	in := `<ol><li data-list="ordered"><span class="ql-ui" contenteditable="false"></span>A</li><li data-list="ordered"><span class="ql-ui" contenteditable="false"></span>B</li></ol>`
	out := NormalizeQuillHTML(in)
	if !strings.Contains(out, "<ol>") {
		t.Fatalf("expected <ol>, got: %s", out)
	}
	if strings.Contains(out, "<ul>") {
		t.Fatalf("unexpected <ul>: %s", out)
	}
	if strings.Contains(out, "data-list") || strings.Contains(out, "ql-ui") {
		t.Fatalf("quill chrome left: %s", out)
	}
}

func TestNormalizeQuillHTML_Mixed(t *testing.T) {
	in := `<ol><li data-list="bullet"><span class="ql-ui" contenteditable="false"></span>B1</li><li data-list="ordered"><span class="ql-ui" contenteditable="false"></span>O1</li></ol>`
	out := NormalizeQuillHTML(in)
	if !strings.Contains(out, "<ul>") || !strings.Contains(out, "<ol>") {
		t.Fatalf("expected both list types: %s", out)
	}
}

func TestNormalizeQuillHTML_Passthrough(t *testing.T) {
	in := `<p>Hello</p><ul><li>Already good</li></ul>`
	out := NormalizeQuillHTML(in)
	if out != in {
		t.Fatalf("changed clean html: %q -> %q", in, out)
	}
}

func TestWrapHTMLBody_NormalizesLists(t *testing.T) {
	in := `<ol><li data-list="bullet"><span class="ql-ui" contenteditable="false"></span>X</li></ol>`
	out := WrapHTMLBody(in)
	if strings.Contains(out, "data-list") || strings.Contains(out, "ql-ui") {
		t.Fatalf("wrap left quill chrome: %s", out)
	}
	if !strings.Contains(out, "<ul>") {
		t.Fatalf("expected ul in wrapped body: %s", out)
	}
}
