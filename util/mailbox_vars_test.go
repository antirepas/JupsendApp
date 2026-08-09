package util

import (
	"reflect"
	"strings"
	"testing"

	"emailtracker.com/model"
)

func TestParseMailboxVarRefs(t *testing.T) {
	text := `Best regards, {{@name}} — {{@first_name}} <{{@email}}>`
	refs := ParseVarRefs(text)
	if len(refs) != 3 {
		t.Fatalf("got %d refs", len(refs))
	}
	for _, ref := range refs {
		if !ref.Mailbox {
			t.Fatalf("expected mailbox ref: %+v", ref)
		}
	}
	if refs[0].Name != "name" || refs[1].Name != "first_name" || refs[2].Name != "email" {
		t.Fatalf("names: %+v", refs)
	}
}

func TestExtractTemplateVariablesExcludesMailbox(t *testing.T) {
	got := ExtractTemplateVariables("Hi {{name}}, from {{@name}} at {{company}}")
	want := []string{"company", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	mbox := ExtractMailboxVariables("Hi {{name}}, from {{@name}} at {{@email}}")
	wantM := []string{"email", "name"}
	if !reflect.DeepEqual(mbox, wantM) {
		t.Fatalf("mailbox got %v want %v", mbox, wantM)
	}
}

func TestRenderMailboxVariables(t *testing.T) {
	body := "Best regards,\n{{@name}}\n{{@first_name}} · {{@email}}"
	res, err := RenderTemplate(body, []model.ContactVariables{{Key: "name", Value: "Lead Name"}}, RenderOptions{
		MailboxVars: MailboxVarsFromSender("Antanas Kupstas", "antanas@acme.com"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Antanas Kupstas") {
		t.Fatalf("missing full name: %q", res.Text)
	}
	if !strings.Contains(res.Text, "Antanas · antanas@acme.com") {
		t.Fatalf("missing first/email: %q", res.Text)
	}
	if strings.Contains(res.Text, "Lead Name") {
		t.Fatalf("contact name should not replace @name: %q", res.Text)
	}
}

func TestMailboxVarsFromSender(t *testing.T) {
	got := MailboxVarsFromSender("  Ada Lovelace  ", "ada@example.com")
	if got["name"] != "Ada Lovelace" || got["first_name"] != "Ada" || got["email"] != "ada@example.com" {
		t.Fatalf("%v", got)
	}
}
