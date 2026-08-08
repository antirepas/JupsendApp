package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"emailtracker.com/db"
)

const (
	LeadTemperatureCold = "cold"
	LeadTemperatureWarm = "warm"
	LeadTemperatureHot  = "hot"
)

// LeadTemperatureTierThresholds is the AND rule for a single tier.
type LeadTemperatureTierThresholds struct {
	MinOpens   int  `json:"min_opens"`
	MinClicks  int  `json:"min_clicks"`
	ReplyIsHot bool `json:"reply_is_hot,omitempty"` // only meaningful on hot
}

// LeadTemperatureRules are campaign-level cold/warm/hot definitions.
// Evaluation order: hot first, then warm, else cold.
type LeadTemperatureRules struct {
	Warm LeadTemperatureTierThresholds `json:"warm"`
	Hot  LeadTemperatureTierThresholds `json:"hot"`
}

// DefaultLeadTemperatureRules matches the product defaults from the plan.
func DefaultLeadTemperatureRules() LeadTemperatureRules {
	return LeadTemperatureRules{
		Warm: LeadTemperatureTierThresholds{MinOpens: 2, MinClicks: 1},
		Hot:  LeadTemperatureTierThresholds{MinOpens: 3, MinClicks: 2, ReplyIsHot: true},
	}
}

func NormalizeLeadTemperatureRules(r LeadTemperatureRules) LeadTemperatureRules {
	def := DefaultLeadTemperatureRules()
	if r.Warm.MinOpens < 0 {
		r.Warm.MinOpens = 0
	}
	if r.Warm.MinClicks < 0 {
		r.Warm.MinClicks = 0
	}
	if r.Hot.MinOpens < 0 {
		r.Hot.MinOpens = 0
	}
	if r.Hot.MinClicks < 0 {
		r.Hot.MinClicks = 0
	}
	// Empty/zeroed hot+warm from missing JSON → defaults.
	if r.Warm.MinOpens == 0 && r.Warm.MinClicks == 0 && r.Hot.MinOpens == 0 && r.Hot.MinClicks == 0 && !r.Hot.ReplyIsHot {
		return def
	}
	return r
}

func ParseLeadTemperatureRulesJSON(raw string) LeadTemperatureRules {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return DefaultLeadTemperatureRules()
	}
	var r LeadTemperatureRules
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return DefaultLeadTemperatureRules()
	}
	return NormalizeLeadTemperatureRules(r)
}

func (r LeadTemperatureRules) ToJSON() string {
	r = NormalizeLeadTemperatureRules(r)
	b, err := json.Marshal(r)
	if err != nil {
		b, _ = json.Marshal(DefaultLeadTemperatureRules())
	}
	return string(b)
}

// PreviewLeadTemperatureRules returns a short human summary for the UI.
func PreviewLeadTemperatureRules(r LeadTemperatureRules) string {
	r = NormalizeLeadTemperatureRules(r)
	hot := fmt.Sprintf("Hot = ≥%d opens and ≥%d clicks", r.Hot.MinOpens, r.Hot.MinClicks)
	if r.Hot.ReplyIsHot {
		hot += " (or any reply)"
	}
	warm := fmt.Sprintf("Warm = ≥%d opens and ≥%d clicks", r.Warm.MinOpens, r.Warm.MinClicks)
	return warm + ". " + hot + ". Otherwise cold."
}

// CampaignContactEngagementCounts is lifetime engagement within one campaign.
type CampaignContactEngagementCounts struct {
	Opens   int
	Clicks  int
	Replies int
}

// CountCampaignContactEngagement sums human opens, clicks, and replies for a contact in a campaign.
func CountCampaignContactEngagement(campaignID, contactID int64) (CampaignContactEngagementCounts, error) {
	var out CampaignContactEngagementCounts
	if campaignID <= 0 || contactID <= 0 {
		return out, nil
	}
	// Opens: human-only via email_events.is_bot. Clicks from email_events.
	err := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id
		WHERE es.campaign_id = ? AND es.contact_id = ?
	`, campaignID, contactID).Scan(&out.Opens, &out.Clicks)
	if err != nil {
		return out, err
	}
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND es.contact_id = ? AND ce.event_type = 'REPLY'
	`, campaignID, contactID).Scan(&out.Replies)
	return out, err
}

// ResolveLeadTemperature applies campaign rules to engagement counts.
func ResolveLeadTemperature(campaignID, contactID int64) (string, error) {
	rules, err := GetCampaignTemperatureRules(campaignID)
	if err != nil {
		return LeadTemperatureCold, err
	}
	counts, err := CountCampaignContactEngagement(campaignID, contactID)
	if err != nil {
		return LeadTemperatureCold, err
	}
	return ClassifyLeadTemperature(rules, counts), nil
}

// ClassifyLeadTemperature is pure rule evaluation (hot → warm → cold).
func ClassifyLeadTemperature(rules LeadTemperatureRules, counts CampaignContactEngagementCounts) string {
	rules = NormalizeLeadTemperatureRules(rules)
	if rules.Hot.ReplyIsHot && counts.Replies > 0 {
		return LeadTemperatureHot
	}
	if counts.Opens >= rules.Hot.MinOpens && counts.Clicks >= rules.Hot.MinClicks {
		return LeadTemperatureHot
	}
	if counts.Opens >= rules.Warm.MinOpens && counts.Clicks >= rules.Warm.MinClicks {
		return LeadTemperatureWarm
	}
	return LeadTemperatureCold
}

// GetCampaignTemperatureRules loads rules for a campaign (defaults if empty/missing).
func GetCampaignTemperatureRules(campaignID int64) (LeadTemperatureRules, error) {
	if campaignID <= 0 {
		return DefaultLeadTemperatureRules(), nil
	}
	var raw string
	err := db.QueryRow(`SELECT COALESCE(temperature_rules_json, '') FROM campaigns WHERE id = ?`, campaignID).Scan(&raw)
	if err != nil {
		return DefaultLeadTemperatureRules(), err
	}
	return ParseLeadTemperatureRulesJSON(raw), nil
}

// SetCampaignTemperatureRules persists rules for a campaign owned by userID.
func SetCampaignTemperatureRules(campaignID, userID int64, rules LeadTemperatureRules) error {
	raw := rules.ToJSON()
	res, err := db.Exec(`
		UPDATE campaigns SET temperature_rules_json = ?
		WHERE id = ? AND user_id = ?
	`, raw, campaignID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("campaign not found")
	}
	return nil
}

// LeadTemperatureRulesFromForm parses warm/hot thresholds from form fields.
func LeadTemperatureRulesFromForm(
	warmOpens, warmClicks, hotOpens, hotClicks int,
	replyIsHot bool,
) LeadTemperatureRules {
	return NormalizeLeadTemperatureRules(LeadTemperatureRules{
		Warm: LeadTemperatureTierThresholds{MinOpens: warmOpens, MinClicks: warmClicks},
		Hot:  LeadTemperatureTierThresholds{MinOpens: hotOpens, MinClicks: hotClicks, ReplyIsHot: replyIsHot},
	})
}
