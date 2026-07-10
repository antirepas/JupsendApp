package util

import (
	"context"
	"strings"
	"testing"

	"emailtracker.com/ai"
	"emailtracker.com/config"
)

func TestFitGrammarHintHelps(t *testing.T) {
	hint := fitGrammarHint("I noticed Acme Corp helps ___.")
	if hint == "" || !strings.Contains(hint, "helps") || !strings.Contains(hint, "therapists") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

func TestFitGrammarHintEmpty(t *testing.T) {
	if fitGrammarHint("Hello ___ world") != "" {
		t.Fatal("expected no hint for unknown pattern")
	}
}

func TestBuildFitUserMessageIncludesHint(t *testing.T) {
	msg := buildFitUserMessage("I noticed Acme helps ___.", "B2B SaaS for therapists.")
	if !strings.Contains(msg, "Grammar hint:") || !strings.Contains(msg, "B2B SaaS for therapists") {
		t.Fatalf("message missing expected parts: %q", msg)
	}
}

func TestRenderTemplateAIFitHelpsPattern(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	config.Reload()

	ai.SetDefaultCompleter(&mockAI{out: "therapists and service businesses manage bookings and payments"})
	oldConsume := DefaultAICreditConsumer
	DefaultAICreditConsumer = func(userID int64) bool { return true }
	defer func() {
		ai.SetDefaultCompleter(ai.DefaultCompleter)
		DefaultAICreditConsumer = oldConsume
	}()

	desc := "B2B SaaS helping therapists, psychologists, and service businesses manage bookings, payments, and client relationships."
	tpl := `<p>I noticed {{CompanyName}} helps {{~Description}}.</p>`
	res, err := RenderTemplate(tpl, vars(map[string]string{
		"CompanyName": "Acme Corp",
		"Description": desc,
	}), RenderOptions{
		UserID:         1,
		BodyMode:       true,
		Ctx:            context.Background(),
		AICreditsCheck: func(userID int64) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "I noticed Acme Corp helps therapists and service businesses manage bookings and payments."
	plain := StripHTML(res.Text)
	if !strings.Contains(plain, want) {
		t.Fatalf("got %q", plain)
	}
	if strings.Contains(strings.ToLower(plain), "b2b saas") {
		t.Fatalf("fitted phrase should not include B2B SaaS label: %q", plain)
	}
}
