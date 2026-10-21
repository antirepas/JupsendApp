package ai

import (
	"context"
	"fmt"
	"strings"
)

// CampaignEmailStep is one send in the sequence (first email or follow-up).
type CampaignEmailStep struct {
	Role         string // first_a | first_b | follow_up
	Label        string // human label
	NodeKey      string // workflow node key when applicable
	TemplateName string
	Subject      string
	BodyPlain    string
	Variables    []string
	CTA          string
}

// CampaignVarStat summarizes list↔template variable coverage.
type CampaignVarStat struct {
	Key     string
	Filled  int
	Total   int
	Missing int
	Pct     int
}

// CampaignReviewInput is assembled by the route from campaign assets.
type CampaignReviewInput struct {
	Name                 string
	Mode                 string // bulk | bulk_ab | workflow | workflow_ab
	Status               string
	ContactCount         int
	ReadyContactCount    int
	MissingVarsContacts  int
	ExperimentVariable   string
	ExperimentHypothesis string
	OpenTracking         bool
	StopOnReply          bool
	StopOnHot            bool
	MailboxCount         int
	WorkflowName         string
	Emails               []CampaignEmailStep
	VarCoverage          []CampaignVarStat
	MergedVars           []string
}

// BuildCampaignReviewState builds a Jev state for whole-campaign review.
// No raw contact emails/PII — aggregates + email copy only.
func BuildCampaignReviewState(in CampaignReviewInput) map[string]interface{} {
	emails := make([]map[string]interface{}, 0, len(in.Emails))
	for _, e := range in.Emails {
		cta := strings.TrimSpace(e.CTA)
		if cta == "" {
			cta = extractCTA(e.BodyPlain)
		}
		emails = append(emails, map[string]interface{}{
			"role":          e.Role,
			"label":         e.Label,
			"node_key":      e.NodeKey,
			"template_name": e.TemplateName,
			"subject":       truncateRunes(strings.TrimSpace(e.Subject), 300),
			"body":          truncateRunes(strings.TrimSpace(e.BodyPlain), 1800),
			"cta":           cta,
			"variables":     e.Variables,
		})
	}
	coverage := make([]map[string]interface{}, 0, len(in.VarCoverage))
	weakVars := []string{}
	for _, v := range in.VarCoverage {
		coverage = append(coverage, map[string]interface{}{
			"key": v.Key, "filled": v.Filled, "total": v.Total, "missing": v.Missing, "pct": v.Pct,
		})
		if v.Total > 0 && v.Pct < 70 {
			weakVars = append(weakVars, v.Key)
		}
	}
	ab := strings.Contains(in.Mode, "ab") || hasRole(in.Emails, "first_b")
	followUps := 0
	for _, e := range in.Emails {
		if e.Role == "follow_up" {
			followUps++
		}
	}
	readyPct := 0
	if in.ContactCount > 0 {
		readyPct = (in.ReadyContactCount * 100) / in.ContactCount
	}

	return map[string]interface{}{
		"campaign": map[string]interface{}{
			"name":                  in.Name,
			"mode":                  in.Mode,
			"status":                in.Status,
			"has_ab_test":           ab,
			"follow_up_count":       followUps,
			"email_step_count":      len(in.Emails),
			"experiment_variable":   strings.TrimSpace(in.ExperimentVariable),
			"experiment_hypothesis": strings.TrimSpace(in.ExperimentHypothesis),
			"open_tracking":         in.OpenTracking,
			"stop_on_reply":         in.StopOnReply,
			"stop_on_hot":           in.StopOnHot,
			"mailbox_count":         in.MailboxCount,
			"workflow_name":         strings.TrimSpace(in.WorkflowName),
		},
		"audience": map[string]interface{}{
			"contact_count":         in.ContactCount,
			"ready_contact_count":   in.ReadyContactCount,
			"ready_pct":             readyPct,
			"contacts_missing_vars": in.MissingVarsContacts,
			"merged_template_vars":  in.MergedVars,
			"variable_coverage":     coverage,
			"weak_coverage_vars":    weakVars,
			"note":                  "No individual contact records are included. Judge fit from coverage aggregates and email copy.",
		},
		"emails": emails,
		"evaluation_policy": map[string]interface{}{
			"goal": "Decide whether this campaign is ready to launch as a coherent cold outreach system — not whether a single email is well written.",
			"rules": []string{
				"Judge sequence design: first email, A/B variants, and follow-ups must fit together.",
				"A/B tests need a clear hypothesis and meaningfully different variants — not duplicate copy.",
				"Follow-ups should advance the conversation, not spam the same pitch.",
				"Audience fit matters: weak variable coverage or many missing-vars contacts is a launch risk.",
				"Cold outreach commitment rules still apply across every step.",
				"Prefer revise when issues are fixable; do_not_send for deceptive or fundamentally broken campaigns.",
				"manual_review when essential campaign context is missing (no contacts, unmapped workflow steps, empty templates).",
			},
		},
	}
}

func CampaignReviewQuestions() map[string]interface{} {
	return map[string]interface{}{
		"decision": map[string]interface{}{
			"type": "choice",
			"instructions": `Choose the single best action for launching this campaign.
Evaluate campaign mode, A/B design, follow-up sequence, audience variable coverage,
and overall cold-outreach appropriateness together.

A polished first email is not enough if follow-ups are redundant, A/B variants are identical,
or most contacts are missing required personalization variables.`,
			"criteria": map[string]string{
				"send":          "The campaign is coherent, audience-ready, and appropriate to launch. No material issues remain.",
				"revise":        "The campaign has potential but needs fixable changes to sequence, A/B design, copy, CTA staging, or audience readiness.",
				"do_not_send":   "The campaign is deceptive, materially misleading, or fundamentally inappropriate to run.",
				"manual_review": "Essential campaign context is missing (contacts, templates, mappings) so a reliable launch judgment cannot be made.",
			},
		},
		"sequence_coherent": map[string]interface{}{
			"type": "noul",
			"instructions": `Do the email steps form a coherent sequence (first touch → follow-ups)
rather than disconnected or contradictory messages?`,
		},
		"ab_design_sound": map[string]interface{}{
			"type": "noul",
			"instructions": `If this is an A/B campaign, are variants meaningfully different and aligned
with the stated hypothesis? If there is no A/B test, answer yes.`,
		},
		"hypothesis_clear": map[string]interface{}{
			"type": "noul",
			"instructions": `If A/B is enabled, is the experiment hypothesis clear enough to learn from?
If there is no A/B test, answer yes.`,
		},
		"followups_add_value": map[string]interface{}{
			"type": "noul",
			"instructions": `If follow-ups exist, do they add new value or a progressive ask rather than
repeating the same pitch? If there are no follow-ups, answer yes.`,
		},
		"audience_ready": map[string]interface{}{
			"type": "noul",
			"instructions": `Based on contact counts and variable coverage, is the audience ready enough
to personalize these templates without widespread empty merge fields?`,
		},
		"personalization_honest": map[string]interface{}{
			"type": "noul",
			"instructions": `Across the emails, is personalization structured honestly (placeholders / evidence-friendly)
rather than fabricating prospect-specific facts?`,
		},
		"value_clear_across_steps": map[string]interface{}{
			"type":         "noul",
			"instructions": `Is the value proposition clear across the campaign, especially in the first email?`,
		},
		"cta_staging_ok": map[string]interface{}{
			"type": "noul",
			"instructions": `Are CTAs appropriately staged for cold outreach across steps
(interest/permission early; no premature payment asks)?`,
		},
		"commitment_appropriate": map[string]interface{}{
			"type":         "noul",
			"instructions": `Is the requested commitment proportionate to cold outreach across all steps?`,
		},
		"credibility_ok": map[string]interface{}{
			"type":         "noul",
			"instructions": `Are claims across the campaign credible and appropriately qualified?`,
		},
		"deliverability_risk_low": map[string]interface{}{
			"type": "noul",
			"instructions": `From copy and sequence design alone, does this look reasonably low-risk for spammy
or reputation-damaging cold email patterns (excessive repetition, aggressive CTAs, misleading subjects)?
Yes = risk looks low.`,
		},
		"launch_blocker": map[string]interface{}{
			"type":         "choice",
			"instructions": `What is the single most important reason not to launch this campaign as-is?`,
			"criteria": map[string]string{
				"none":                 "No material launch blocker.",
				"weak_ab_design":       "A/B variants are too similar, missing, or lack a useful hypothesis.",
				"weak_first_email":     "The first email is unclear, weak, or inappropriate.",
				"weak_followups":       "Follow-ups are redundant, missing value, or poorly staged.",
				"audience_not_ready":   "Too many contacts lack required variables or the list looks unfit.",
				"premature_commitment": "CTAs or pricing ask for too much too soon.",
				"credibility_issue":    "Claims or positioning undermine trust.",
				"incoherent_sequence":  "Steps do not fit together as one campaign.",
				"missing_assets":       "Templates, mappings, or other essentials are missing.",
				"other":                "Another material issue dominates.",
			},
		},
		"focus_area": map[string]interface{}{
			"type":         "choice",
			"instructions": `Where should the user focus first to improve launch readiness?`,
			"criteria": map[string]string{
				"first_email":   "First email / variant A.",
				"variant_b":     "A/B variant B.",
				"follow_ups":    "Follow-up emails or workflow mappings.",
				"hypothesis":    "Experiment hypothesis / A/B setup.",
				"audience_list": "Contact list quality or variable coverage.",
				"cta_staging":   "Calls to action across the sequence.",
				"offer":         "Underlying offer framing.",
				"multiple":      "Several areas need work.",
				"none":          "No focused change needed.",
			},
		},
		"recommended_action": map[string]interface{}{
			"type":         "choice",
			"instructions": `What single corrective action would most improve this campaign before launch?`,
			"criteria": map[string]string{
				"rewrite_first_email":    "Improve the first email copy and CTA.",
				"differentiate_ab":       "Make A/B variants meaningfully different and hypothesis-aligned.",
				"rewrite_followups":      "Improve or re-stage follow-up emails.",
				"fix_audience":           "Clean the list or fill missing template variables.",
				"lower_commitment":       "Reduce premature asks across steps.",
				"clarify_value":          "Clarify the value proposition.",
				"map_workflow_templates": "Finish mapping templates to workflow send steps.",
				"gather_information":     "Gather missing campaign context before deciding.",
				"no_change":              "No material correction is needed.",
			},
		},
	}
}

var campaignNoulMeta = []struct {
	ID    string
	Label string
}{
	{"sequence_coherent", "Sequence is coherent"},
	{"ab_design_sound", "A/B design is sound"},
	{"hypothesis_clear", "Hypothesis is clear"},
	{"followups_add_value", "Follow-ups add value"},
	{"audience_ready", "Audience looks ready"},
	{"personalization_honest", "Personalization is honest"},
	{"value_clear_across_steps", "Value is clear"},
	{"cta_staging_ok", "CTA staging is OK"},
	{"commitment_appropriate", "Commitment fits cold outreach"},
	{"credibility_ok", "Claims are credible"},
	{"deliverability_risk_low", "Deliverability risk looks low"},
}

// ReviewCampaign runs Jev on a full campaign and shapes the UI payload.
func ReviewCampaign(ctx context.Context, client *JevClient, in CampaignReviewInput) (*TemplateReviewResult, error) {
	if client == nil {
		client = NewJevClient()
	}
	if len(in.Emails) == 0 {
		return nil, fmt.Errorf("campaign has no email templates to review")
	}
	resp, err := client.Evaluate(ctx, BuildCampaignReviewState(in), CampaignReviewQuestions())
	if err != nil {
		return nil, err
	}
	out := &TemplateReviewResult{
		Model: resp.Model,
		Usage: map[string]int{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}
	if raw, ok := resp.Answers["decision"]; ok {
		if c, err := DecodeJevChoice(raw); err == nil {
			out.Decision = TemplateReviewChoice{
				ID: "decision", Label: "Decision", Choice: c.Choice,
				Confidence: c.Confidence, Probabilities: c.Probabilities,
			}
		}
	}
	for _, meta := range campaignNoulMeta {
		raw, ok := resp.Answers[meta.ID]
		if !ok {
			continue
		}
		n, err := DecodeJevNoul(raw)
		if err != nil {
			continue
		}
		out.Checks = append(out.Checks, TemplateReviewCheck{ID: meta.ID, Label: meta.Label, Yes: n.Noul})
	}
	out.PrimaryIssue = decodeChoiceLabeled(resp.Answers, "launch_blocker", "Launch blocker")
	out.ProblemArea = decodeChoiceLabeled(resp.Answers, "focus_area", "Focus area")
	out.RecommendedFix = decodeChoiceLabeled(resp.Answers, "recommended_action", "Recommended action")
	if out.Decision.Choice == "" {
		return nil, fmt.Errorf("jev returned no decision")
	}
	return out, nil
}

func hasRole(emails []CampaignEmailStep, role string) bool {
	for _, e := range emails {
		if e.Role == role {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
