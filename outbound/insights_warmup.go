package outbound

import (
	"encoding/json"
	"strconv"
	"strings"

	"emailtracker.com/model"
)

// InsightsWarmupHint extracts an optional daily-cap hint from InboxKit insights JSON.
type InsightsWarmupHint struct {
	RecommendedDaily int
	HealthScore      float64 // 0-100 if present
	Adjusted         bool
	Reason           string
}

// ParseInsightsWarmupHint reads common InboxKit / analytics field names.
func ParseInsightsWarmupHint(raw string) InsightsWarmupHint {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return InsightsWarmupHint{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return InsightsWarmupHint{}
	}
	h := InsightsWarmupHint{}
	h.RecommendedDaily = intFromAny(
		m["recommended_daily"],
		m["recommended_daily_limit"],
		m["daily_send_limit"],
		m["suggested_daily_volume"],
		nested(m, "warmup", "recommended_daily"),
		nested(m, "health", "recommended_daily"),
	)
	h.HealthScore = floatFromAny(
		m["health_score"],
		m["score"],
		nested(m, "health", "score"),
		nested(m, "infraguard", "score"),
	)
	if h.RecommendedDaily > 0 {
		h.Adjusted = true
		h.Reason = "InboxKit recommended daily"
		return h
	}
	if h.HealthScore > 0 && h.HealthScore < 50 {
		h.Adjusted = true
		h.Reason = "InboxKit health score low"
		h.RecommendedDaily = -1 // signal: soften schedule
	}
	return h
}

func nested(m map[string]any, keys ...string) any {
	cur := any(m)
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[k]
	}
	return cur
}

func intFromAny(vals ...any) int {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			if int(t) > 0 {
				return int(t)
			}
		case int:
			if t > 0 {
				return t
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func floatFromAny(vals ...any) float64 {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			if t > 0 {
				return t
			}
		case int:
			if t > 0 {
				return float64(t)
			}
		}
	}
	return 0
}

// ApplyInsightsToCap clamps a schedule cap using InboxKit hints; never exceeds account.DailyLimit.
func ApplyInsightsToCap(scheduleCap int, account model.SMTPAccount, analyticsJSON string) (int, InsightsWarmupHint) {
	cap := scheduleCap
	hint := ParseInsightsWarmupHint(analyticsJSON)
	if !hint.Adjusted {
		return cap, hint
	}
	if hint.RecommendedDaily > 0 {
		if hint.RecommendedDaily < cap {
			cap = hint.RecommendedDaily
		}
	} else if hint.RecommendedDaily < 0 {
		cap = scheduleCap / 2
		if cap < model.DefaultWarmupDailyCap {
			cap = model.DefaultWarmupDailyCap
		}
	}
	if account.DailyLimit > 0 && cap > account.DailyLimit {
		cap = account.DailyLimit
	}
	if cap < 1 {
		cap = 1
	}
	return cap, hint
}
