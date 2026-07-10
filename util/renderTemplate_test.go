package util

import (
	"context"
	"strings"
	"testing"

	"emailtracker.com/ai"
	"emailtracker.com/config"
	"emailtracker.com/model"
)

func vars(m map[string]string) []model.ContactVariables {
	var out []model.ContactVariables
	for k, v := range m {
		out = append(out, model.ContactVariables{Key: k, Value: v})
	}
	return out
}

func TestRenderTemplateMultiWordVariable(t *testing.T) {
	tpl := `Hi {{company name}}`
	res, err := RenderTemplate(tpl, vars(map[string]string{"company name": "Acme Corp"}), RenderOptions{BodyMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hi Acme Corp" {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateDefaultFallback(t *testing.T) {
	tpl := "Hi {{name|first|default:there}}"
	res, err := RenderTemplate(tpl, vars(map[string]string{"name": "Jane Doe"}), RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hi Jane" {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateDefaultWhenEmpty(t *testing.T) {
	tpl := "Hi {{name|default:there}}"
	res, err := RenderTemplate(tpl, vars(map[string]string{}), RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hi there" {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateRequiredMissing(t *testing.T) {
	tpl := "Hi {{company|required}}"
	res, err := RenderTemplate(tpl, vars(map[string]string{}), RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.MissingRequired) != 1 || res.MissingRequired[0] != "company" {
		t.Fatalf("missing: %v", res.MissingRequired)
	}
}

func TestRenderTemplateIfBlockHidden(t *testing.T) {
	tpl := `Before{% if description %} MIDDLE {% endif %}After`
	res, err := RenderTemplate(tpl, vars(map[string]string{}), RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "MIDDLE") {
		t.Fatalf("block should be hidden: %q", res.Text)
	}
	if res.Text != "BeforeAfter" {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateIfBlockShown(t *testing.T) {
	tpl := `{% if description %}I saw {{description}}.{% endif %}`
	res, err := RenderTemplate(tpl, vars(map[string]string{"description": "your launch"}), RenderOptions{BodyMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "your launch") {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateHTMLEscape(t *testing.T) {
	tpl := `<p>{{note}}</p>`
	res, err := RenderTemplate(tpl, vars(map[string]string{"note": `<script>alert(1)</script>`}), RenderOptions{BodyMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "<script>") {
		t.Fatalf("should escape: %q", res.Text)
	}
}

func TestRenderTemplateTitleCase(t *testing.T) {
	tpl := `{{company|title}}`
	res, err := RenderTemplate(tpl, vars(map[string]string{"company": "acme inc"}), RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Acme Inc" {
		t.Fatalf("got %q", res.Text)
	}
}

func TestRenderTemplateAIFitWithMock(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	config.Reload()

	ai.SetDefaultCompleter(&mockAI{out: "probate lead sales to investors and agents"})
	oldConsume := DefaultAICreditConsumer
	DefaultAICreditConsumer = func(userID int64) bool { return true }
	defer func() {
		ai.SetDefaultCompleter(ai.DefaultCompleter)
		DefaultAICreditConsumer = oldConsume
	}()

	tpl := `I noticed {{CompanyName}} helps {{~Description}}.`
	res, err := RenderTemplate(tpl, vars(map[string]string{
		"companyname": "Probate Leads Co",
		"Description": "We sell probate leads to real estate investors and agents.",
	}), RenderOptions{
		UserID:         1,
		BodyMode:       true,
		Ctx:            context.Background(),
		AICreditsCheck: func(userID int64) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "probate lead sales") {
		t.Fatalf("got %q", res.Text)
	}
	if strings.Contains(res.Text, "We sell probate leads") {
		t.Fatalf("expected fitted phrase, got raw value: %q", res.Text)
	}
}

func TestRenderTemplateAIFitWithMockLegacy(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	config.Reload()

	ai.SetDefaultCompleter(&mockAI{out: "that you are hiring"})
	oldConsume := DefaultAICreditConsumer
	DefaultAICreditConsumer = func(userID int64) bool { return true }
	defer func() {
		ai.SetDefaultCompleter(ai.DefaultCompleter)
		DefaultAICreditConsumer = oldConsume
	}()

	tpl := `I noticed {{~description}} and wanted to reach out.`
	res, err := RenderTemplate(tpl, vars(map[string]string{"description": "you are hiring"}), RenderOptions{
		UserID:         1,
		BodyMode:       true,
		Ctx:            context.Background(),
		AICreditsCheck: func(userID int64) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "that you are hiring") {
		t.Fatalf("got %q", res.Text)
	}
}

type mockAI struct{ out string }

func (m *mockAI) Complete(ctx context.Context, system, user string) (string, error) {
	return m.out, nil
}
