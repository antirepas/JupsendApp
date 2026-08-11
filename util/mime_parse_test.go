package util

import (
	"strings"
	"testing"
)

func TestParseMIMEBodyPlain(t *testing.T) {
	got := ParseMIMEBody("Hello there")
	if got.Text != "Hello there" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseMIMEBodyHTML(t *testing.T) {
	raw := "Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hi <b>there</b></p>"
	got := ParseMIMEBody(raw)
	if !strings.Contains(got.HTML, "Hi") {
		t.Fatalf("html=%q", got.HTML)
	}
	if got.Text == "" {
		t.Fatal("expected stripped text")
	}
}

func TestParseMIMEBodyMultipart(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"bnd\"\r\n\r\n" +
		"--bnd\r\nContent-Type: text/plain\r\n\r\nPlain part\r\n" +
		"--bnd\r\nContent-Type: text/html\r\n\r\n<p>HTML part</p>\r\n" +
		"--bnd--\r\n"
	got := ParseMIMEBody(raw)
	if got.Text != "Plain part" {
		t.Fatalf("text=%q", got.Text)
	}
	if !strings.Contains(got.HTML, "HTML part") {
		t.Fatalf("html=%q", got.HTML)
	}
}

func TestParseMIMEBodyQuotedPrintableHTML(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"Thanks for=20contacting us.<br>Our team will get back to you."
	got := ParseMIMEBody(raw)
	if !strings.Contains(got.HTML, "Thanks for contacting us") {
		t.Fatalf("html not decoded: %q", got.HTML)
	}
	if strings.Contains(got.HTML, "=20") {
		t.Fatalf("qp artifact remained: %q", got.HTML)
	}
}

func TestRepairEmailBodyDecodesStoredQP(t *testing.T) {
	raw := "Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"<p>Thanks for=20contacting us.</p>"
	text, html := RepairEmailBody(raw, "")
	if !strings.Contains(html, "Thanks for contacting us") {
		t.Fatalf("html=%q text=%q", html, text)
	}
}

func TestSanitizeHTMLForDisplayStripsScript(t *testing.T) {
	in := `<p>ok</p><script>alert(1)</script><a href="javascript:alert(1)">x</a>`
	out := SanitizeHTMLForDisplay(in)
	if strings.Contains(strings.ToLower(out), "<script") {
		t.Fatalf("script remained: %q", out)
	}
	if strings.Contains(out, "javascript:") {
		t.Fatalf("javascript uri remained: %q", out)
	}
}

func TestBuildMultipartEmailThreadingHeaders(t *testing.T) {
	raw := string(BuildMultipartEmail(
		"me@test.com", "Me", "lead@example.com", "Re: Hello", "body", "<p>body</p>",
		SendMeta{MessageID: "<out@test>", InReplyTo: "<in@client>", References: "<in@client>"},
	))
	if !strings.Contains(raw, "In-Reply-To: <in@client>") {
		t.Fatalf("missing In-Reply-To: %s", raw)
	}
	if !strings.Contains(raw, "References: <in@client>") {
		t.Fatalf("missing References: %s", raw)
	}
	if !strings.Contains(raw, "Message-ID: <out@test>") {
		t.Fatalf("missing Message-ID: %s", raw)
	}
}
