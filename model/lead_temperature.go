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

// LeadTemperatureRules are campaign-level cold/warm/hot definitions (reply-centric).
// Evaluation order: hot (positive) → warm (non-negative reply) → cold.
type LeadTemperatureRules struct {
	Hot struct {
		MinPositiveReplies int `json:"min_positive_replies"`
	} `json:"hot"`
	Warm struct {
		AnyNonNegativeReply bool `json:"any_non_negative_reply"`
	} `json:"warm"`
	NegativeStops bool `json:"negative_stops"`

	// Legacy open/click fields retained for JSON migration only.
	LegacyWarm *struct {
		MinOpens  int `json:"min_opens"`
		MinClicks int `json:"min_clicks"`
	} `json:"-"`
}

// DefaultLeadTemperatureRules: hot = ≥1 positive reply; warm = any non-negative reply.
func DefaultLeadTemperatureRules() LeadTemperatureRules {
	var r LeadTemperatureRules
	r.Hot.MinPositiveReplies = 1
	r.Warm.AnyNonNegativeReply = true
	r.NegativeStops = true
	return r
}

func NormalizeLeadTemperatureRules(r LeadTemperatureRules) LeadTemperatureRules {
	def := DefaultLeadTemperatureRules()
	if r.Hot.MinPositiveReplies < 0 {
		r.Hot.MinPositiveReplies = 0
	}
	// Empty / zeroed → defaults.
	if r.Hot.MinPositiveReplies == 0 && !r.Warm.AnyNonNegativeReply && !r.NegativeStops {
		return def
	}
	if r.Hot.MinPositiveReplies == 0 {
		r.Hot.MinPositiveReplies = def.Hot.MinPositiveReplies
	}
	if !r.Warm.AnyNonNegativeReply && r.Hot.MinPositiveReplies > 0 {
		// Keep warm enabled by default when hot is set.
		r.Warm.AnyNonNegativeReply = true
	}
	return r
}

func ParseLeadTemperatureRulesJSON(raw string) LeadTemperatureRules {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return DefaultLeadTemperatureRules()
	}
	// Detect legacy open/click shape and map to reply-centric defaults.
	if strings.Contains(raw, "min_opens") || strings.Contains(raw, "reply_is_hot") {
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
	hot := fmt.Sprintf("Hot = ≥%d positive reply", r.Hot.MinPositiveReplies)
	if r.Hot.MinPositiveReplies != 1 {
		hot = fmt.Sprintf("Hot = ≥%d positive replies", r.Hot.MinPositiveReplies)
	}
	warm := "Warm = any non-negative reply (pending/neutral/positive)"
	if !r.Warm.AnyNonNegativeReply {
		warm = "Warm disabled"
	}
	neg := "Negative replies stop outreach"
	if !r.NegativeStops {
		neg = "Negative replies do not auto-stop"
	}
	return warm + ". " + hot + ". " + neg + ". Otherwise cold."
}

// CampaignContactEngagementCounts is lifetime engagement within one campaign.
type CampaignContactEngagementCounts struct {
	Opens              int
	Clicks             int
	Replies            int
	PositiveReplies    int
	NegativeReplies    int
	NonNegativeReplies int
}

// CountCampaignContactEngagement sums opens/clicks/replies and reply sentiments.
func CountCampaignContactEngagement(campaignID, contactID int64) (CampaignContactEngagementCounts, error) {
	var out CampaignContactEngagementCounts
	if campaignID <= 0 || contactID <= 0 {
		return out, nil
	}
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
	pos, neg, nonNeg, total, err := CountCampaignContactReplySentiments(campaignID, contactID)
	if err != nil {
		return out, err
	}
	out.PositiveReplies = pos
	out.NegativeReplies = neg
	out.NonNegativeReplies = nonNeg
	out.Replies = total
	return out, nil
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
	if counts.PositiveReplies >= rules.Hot.MinPositiveReplies && rules.Hot.MinPositiveReplies > 0 {
		return LeadTemperatureHot
	}
	if rules.Warm.AnyNonNegativeReply && counts.NonNegativeReplies > 0 {
		return LeadTemperatureWarm
	}
	// Fallback: any reply without sentiment still counts as warm when pending.
	if rules.Warm.AnyNonNegativeReply && counts.Replies > 0 && counts.NegativeReplies < counts.Replies {
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

// LeadTemperatureRulesFromForm builds reply-centric rules from campaign form fields.
func LeadTemperatureRulesFromForm(minPositive int, anyNonNegative, negativeStops bool) LeadTemperatureRules {
	var r LeadTemperatureRules
	r.Hot.MinPositiveReplies = minPositive
	r.Warm.AnyNonNegativeReply = anyNonNegative
	r.NegativeStops = negativeStops
	return NormalizeLeadTemperatureRules(r)
}
