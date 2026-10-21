package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildCampaignReviewStateNoPII(t *testing.T) {
	state := BuildCampaignReviewState(CampaignReviewInput{
		Name:                "Staffing pilot",
		Mode:                "workflow_ab",
		Status:              "draft",
		ContactCount:        100,
		ReadyContactCount:   82,
		MissingVarsContacts: 18,
		ExperimentVariable:  "subject_line",
		ExperimentHypothesis: "Curiosity subjects outperform benefit subjects",
		OpenTracking:        true,
		StopOnReply:         true,
		MailboxCount:        2,
		WorkflowName:        "Recommended outreach",
		MergedVars:          []string{"name", "company"},
		VarCoverage: []CampaignVarStat{
			{Key: "name", Filled: 95, Total: 100, Missing: 5, Pct: 95},
			{Key: "company", Filled: 60, Total: 100, Missing: 40, Pct: 60},
		},
		Emails: []CampaignEmailStep{
			{Role: "first_a", Label: "First (A)", TemplateName: "A", Subject: "Quick question", BodyPlain: "Hi {{name}}, interested?"},
			{Role: "first_b", Label: "First (B)", TemplateName: "B", Subject: "Hiring help", BodyPlain: "Hi {{name}}, we help {{company}}."},
			{Role: "follow_up", Label: "Follow-up: nudge", TemplateName: "Nudge", Subject: "Re: hiring", BodyPlain: "Circling back — still useful?"},
		},
	})
	if _, ok := state["contact"]; ok {
		t.Fatal("must not include per-contact records")
	}
	aud, _ := state["audience"].(map[string]interface{})
	if aud["contact_count"] != 100 {
		t.Fatalf("audience=%v", aud)
	}
	weak, _ := aud["weak_coverage_vars"].([]string)
	if len(weak) != 1 || weak[0] != "company" {
		t.Fatalf("weak=%v", weak)
	}
	camp, _ := state["campaign"].(map[string]interface{})
	if camp["has_ab_test"] != true {
		t.Fatal("expected AB flag")
	}
	if camp["follow_up_count"] != 1 {
		t.Fatalf("follow_ups=%v", camp["follow_up_count"])
	}
	emails, _ := state["emails"].([]map[string]interface{})
	if len(emails) != 3 {
		t.Fatalf("emails=%d", len(emails))
	}
}

func TestReviewCampaignMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "jev-1.13.0",
			"answers": map[string]interface{}{
				"decision": map[string]interface{}{
					"type": "choice", "choice": "revise", "confidence": 0.7,
					"probabilities": map[string]float64{"revise": 0.7, "send": 0.2, "do_not_send": 0.05, "manual_review": 0.05},
				},
				"sequence_coherent":     map[string]interface{}{"type": "noul", "noul": 0.8},
				"ab_design_sound":       map[string]interface{}{"type": "noul", "noul": 0.4},
				"hypothesis_clear":      map[string]interface{}{"type": "noul", "noul": 0.75},
				"followups_add_value":   map[string]interface{}{"type": "noul", "noul": 0.7},
				"audience_ready":        map[string]interface{}{"type": "noul", "noul": 0.55},
				"personalization_honest": map[string]interface{}{"type": "noul", "noul": 0.9},
				"value_clear_across_steps": map[string]interface{}{"type": "noul", "noul": 0.65},
				"cta_staging_ok":        map[string]interface{}{"type": "noul", "noul": 0.8},
				"commitment_appropriate": map[string]interface{}{"type": "noul", "noul": 0.85},
				"credibility_ok":        map[string]interface{}{"type": "noul", "noul": 0.88},
				"deliverability_risk_low": map[string]interface{}{"type": "noul", "noul": 0.7},
				"launch_blocker": map[string]interface{}{
					"type": "choice", "choice": "weak_ab_design", "confidence": 0.6,
					"probabilities": map[string]float64{"weak_ab_design": 0.6, "none": 0.2},
				},
				"focus_area": map[string]interface{}{
					"type": "choice", "choice": "variant_b", "confidence": 0.5,
					"probabilities": map[string]float64{"variant_b": 0.5, "none": 0.2},
				},
				"recommended_action": map[string]interface{}{
					"type": "choice", "choice": "differentiate_ab", "confidence": 0.62,
					"probabilities": map[string]float64{"differentiate_ab": 0.62, "no_change": 0.2},
				},
			},
			"usage": map[string]int{"input_tokens": 200, "output_tokens": 40},
		})
	}))
	defer srv.Close()

	c := &JevClient{
		APIKey:  "test-key",
		Model:   "jev-latest",
		BaseURL: srv.URL,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
	res, err := ReviewCampaign(context.Background(), c, CampaignReviewInput{
		Name: "Test",
		Mode: "bulk_ab",
		Emails: []CampaignEmailStep{
			{Role: "first_a", Subject: "A", BodyPlain: "Hello?"},
			{Role: "first_b", Subject: "B", BodyPlain: "Hi?"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Choice != "revise" {
		t.Fatalf("decision=%+v", res.Decision)
	}
	if res.PrimaryIssue.Choice != "weak_ab_design" {
		t.Fatalf("blocker=%+v", res.PrimaryIssue)
	}
	if len(res.Checks) < 5 {
		t.Fatalf("checks=%d", len(res.Checks))
	}
}
