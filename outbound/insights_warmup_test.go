package outbound

import (
	"testing"

	"emailtracker.com/model"
)

func TestParseInsightsRecommendedDaily(t *testing.T) {
	h := ParseInsightsWarmupHint(`{"recommended_daily": 40}`)
	if !h.Adjusted || h.RecommendedDaily != 40 {
		t.Fatalf("%+v", h)
	}
}

func TestParseInsightsHealthSoftens(t *testing.T) {
	h := ParseInsightsWarmupHint(`{"health_score": 30}`)
	if !h.Adjusted || h.RecommendedDaily != -1 {
		t.Fatalf("%+v", h)
	}
}

func TestApplyInsightsToCap(t *testing.T) {
	acc := model.SMTPAccount{DailyLimit: 250}
	cap, hint := ApplyInsightsToCap(80, acc, `{"recommended_daily": 50}`)
	if cap != 50 || !hint.Adjusted {
		t.Fatalf("cap=%d hint=%+v", cap, hint)
	}
	cap2, _ := ApplyInsightsToCap(80, acc, `{}`)
	if cap2 != 80 {
		t.Fatalf("want 80 got %d", cap2)
	}
}
