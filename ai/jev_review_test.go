package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildTemplateReviewStateOmitsContact(t *testing.T) {
	state := BuildTemplateReviewState(TemplateReviewInput{
		Subject:      "Quick question for {{company}}",
		BodyPlain:    "Hi {{name}},\n\nWould a short walkthrough help?",
		TemplateName: "Staffing outreach",
		SenderEmail:  "antanas@tryinfradone.com",
		SenderName:   "Antanas",
		Variables:    []string{"name", "company"},
	})
	if _, ok := state["contact"]; ok {
		t.Fatal("contact must not be in template review state")
	}
	email, _ := state["email"].(map[string]interface{})
	if email["subject"] != "Quick question for {{company}}" {
		t.Fatalf("subject=%v", email["subject"])
	}
	if !strings.Contains(email["body"].(string), "walkthrough") {
		t.Fatalf("body=%v", email["body"])
	}
	if email["cta"] == "" {
		t.Fatal("expected CTA extraction")
	}
	tpl, _ := state["template"].(map[string]interface{})
	if tpl["has_placeholders"] != true {
		t.Fatal("expected placeholders flag")
	}
	syntax, _ := state["template_syntax"].(map[string]interface{})
	if syntax["engine"] != "jupsend" {
		t.Fatalf("syntax=%v", syntax)
	}
	forms, _ := syntax["forms"].([]string)
	if len(forms) < 3 {
		t.Fatalf("expected syntax forms, got %v", forms)
	}
}

func TestExtractCTA(t *testing.T) {
	cta := extractCTA("Hi there.\n\nWould it be useful if I sent a walkthrough?\n\nBest,\nAntanas")
	if !strings.Contains(cta, "walkthrough") {
		t.Fatalf("cta=%q", cta)
	}
}

func TestJevClientEvaluateMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "jev-1.13.0",
			"answers": map[string]interface{}{
				"decision": map[string]interface{}{
					"type": "choice", "choice": "revise", "confidence": 0.72,
					"probabilities": map[string]float64{"revise": 0.72, "send": 0.2, "do_not_send": 0.05, "manual_review": 0.03},
				},
				"cta_clear": map[string]interface{}{"type": "noul", "noul": 0.91},
				"ai_slop":   map[string]interface{}{"type": "noul", "noul": 0.22},
				"primary_issue": map[string]interface{}{
					"type": "choice", "choice": "weak_value_proposition", "confidence": 0.61,
					"probabilities": map[string]float64{"weak_value_proposition": 0.61, "none": 0.2},
				},
				"problem_area": map[string]interface{}{
					"type": "choice", "choice": "value_proposition", "confidence": 0.55,
					"probabilities": map[string]float64{"value_proposition": 0.55, "none": 0.2},
				},
				"recommended_fix": map[string]interface{}{
					"type": "choice", "choice": "clarify_value", "confidence": 0.58,
					"probabilities": map[string]float64{"clarify_value": 0.58, "no_change": 0.2},
				},
			},
			"usage": map[string]int{"input_tokens": 100, "output_tokens": 20},
		})
	}))
	defer srv.Close()

	c := &JevClient{
		APIKey:  "test-key",
		Model:   "jev-latest",
		BaseURL: srv.URL,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
	res, err := ReviewTemplate(context.Background(), c, TemplateReviewInput{
		Subject:   "Hello",
		BodyPlain: "Would this help?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Choice != "revise" || res.Decision.Confidence < 0.7 {
		t.Fatalf("decision=%+v", res.Decision)
	}
	if len(res.Checks) < 2 {
		t.Fatalf("checks=%+v", res.Checks)
	}
}
