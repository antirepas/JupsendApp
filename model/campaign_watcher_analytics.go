package model

import (
	"encoding/json"
	"sort"
	"strings"

	"emailtracker.com/db"
)

// WatcherIntentStat is one reply-intent bucket in campaign analytics.
type WatcherIntentStat struct {
	Intent          string
	Label           string
	Sentiment       string // positive | negative | neutral
	Replies         int    // unique contacts classified with this intent
	WatcherAttached bool
	Started         int // watcher instances started for this intent
	Waiting         int
	Sent            int // watcher auto-replies delivered
	Failed          int
	Skipped         int
}

// CampaignWatcherAnalytics is the reply → intent → watcher funnel for a campaign.
type CampaignWatcherAnalytics struct {
	HasWatchers          bool
	AttachedWorkflows    int
	AttachedIntents      []string
	Sent                 int
	Replied              int
	Classified           int
	Unclassified         int
	WatcherStarted       int
	WatcherWaiting       int
	WatcherSent          int
	WatcherFailed        int
	WatcherSkipped       int
	Intents              []WatcherIntentStat
	PositiveFromIntent   int
	NegativeFromIntent   int
	NeutralFromIntent    int
	PositivePctOfReplies float64
	NegativePctOfReplies float64
}

// GetCampaignWatcherAnalytics builds the watcher funnel + intent breakdown.
func GetCampaignWatcherAnalytics(campaignID int64) CampaignWatcherAnalytics {
	out := CampaignWatcherAnalytics{}
	if campaignID <= 0 {
		return out
	}

	attachedIntents := attachedWatcherIntentSet(campaignID)
	out.AttachedWorkflows = countAttachedWatcherWorkflows(campaignID)
	out.HasWatchers = out.AttachedWorkflows > 0
	for intent := range attachedIntents {
		out.AttachedIntents = append(out.AttachedIntents, intent)
	}
	sort.Strings(out.AttachedIntents)

	out.Sent, out.Replied = campaignOverviewCounts(campaignID)

	intentReplies := map[string]int{}
	classified := 0
	rows, err := db.Query(`
		SELECT c.id,
			COALESCE(NULLIF(TRIM(c.last_reply_intent), ''), ''),
			COALESCE(NULLIF(TRIM(c.last_reply_sentiment), ''), '')
		FROM contact c
		WHERE c.id IN (
			SELECT DISTINCT es.contact_id
			FROM contact_events ce
			INNER JOIN email_sends es ON es.id = ce.email_send_id
			WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
		)
	`, campaignID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int64
			var intentRaw, sentimentRaw string
			if rows.Scan(&cid, &intentRaw, &sentimentRaw) != nil {
				continue
			}
			intent := NormalizeReplyIntent(intentRaw)
			if intent == "" {
				// Fall back: derive intent bucket from coarse sentiment when AI intent missing.
				switch NormalizeReplySentiment(sentimentRaw) {
				case ReplySentimentPositive:
					intent = "interested"
				case ReplySentimentNegative:
					intent = "not_now"
				case ReplySentimentNeutral:
					intent = "ooo"
				}
			}
			if intent == "" {
				out.Unclassified++
				continue
			}
			classified++
			intentReplies[intent]++
			switch IntentToSentiment(intent) {
			case ReplySentimentPositive:
				out.PositiveFromIntent++
			case ReplySentimentNegative:
				out.NegativeFromIntent++
			default:
				out.NeutralFromIntent++
			}
		}
	}
	out.Classified = classified

	startedByIntent, totalStarted := countWatcherStartsByIntent(campaignID)
	waitingByIntent, totalWaiting := countWatcherInstancesByIntent(campaignID, []string{"active", "waiting"})
	sentByIntent, totalSent := countWatcherSendsByIntent(campaignID)
	failedByIntent, totalFailed := countWatcherInstancesByIntent(campaignID, []string{"failed", "cancelled"})
	skippedByIntent, totalSkipped := countWatcherSkipsByIntent(campaignID)

	out.WatcherStarted = totalStarted
	out.WatcherWaiting = totalWaiting
	out.WatcherSent = totalSent
	out.WatcherFailed = totalFailed
	out.WatcherSkipped = totalSkipped
	if totalStarted > 0 || totalSent > 0 || totalWaiting > 0 {
		out.HasWatchers = true
	}

	intentKeys := map[string]bool{}
	for _, k := range ReplyIntents {
		intentKeys[k] = true
	}
	for k := range intentReplies {
		intentKeys[k] = true
	}
	for k := range attachedIntents {
		intentKeys[k] = true
	}
	for k := range startedByIntent {
		intentKeys[k] = true
	}

	var intents []WatcherIntentStat
	for intent := range intentKeys {
		st := WatcherIntentStat{
			Intent:          intent,
			Label:           FormatReplyIntentLabel(intent),
			Sentiment:       IntentToSentiment(intent),
			Replies:         intentReplies[intent],
			WatcherAttached: attachedIntents[intent],
			Started:         startedByIntent[intent],
			Waiting:         waitingByIntent[intent],
			Sent:            sentByIntent[intent],
			Failed:          failedByIntent[intent],
			Skipped:         skippedByIntent[intent],
		}
		if st.Replies == 0 && !st.WatcherAttached && st.Started == 0 && st.Sent == 0 {
			continue
		}
		intents = append(intents, st)
	}
	sort.Slice(intents, func(i, j int) bool {
		if intents[i].Replies != intents[j].Replies {
			return intents[i].Replies > intents[j].Replies
		}
		return intents[i].Label < intents[j].Label
	})
	out.Intents = intents

	if out.Replied > 0 {
		out.PositivePctOfReplies = float64(out.PositiveFromIntent) / float64(out.Replied) * 100
		out.NegativePctOfReplies = float64(out.NegativeFromIntent) / float64(out.Replied) * 100
	}
	return out
}

func countAttachedWatcherWorkflows(campaignID int64) int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM campaign_watcher_workflows WHERE campaign_id = ?`, campaignID).Scan(&n)
	return n
}

func attachedWatcherIntentSet(campaignID int64) map[string]bool {
	out := map[string]bool{}
	ids, err := ListCampaignWatcherWorkflowIDs(campaignID)
	if err != nil {
		return out
	}
	for _, wid := range ids {
		pub, err := LatestPublishedVersionID(wid)
		if err != nil || pub <= 0 {
			continue
		}
		intents, err := ListReplyTriggerIntents(pub)
		if err != nil {
			continue
		}
		for _, intent := range intents {
			out[intent] = true
		}
	}
	return out
}

func countWatcherStartsByIntent(campaignID int64) (map[string]int, int) {
	out := map[string]int{}
	total := 0
	rows, err := db.Query(`
		SELECT COALESCE(wi.context_json, '{}'), COUNT(DISTINCT ce.contact_id)
		FROM contact_events ce
		LEFT JOIN workflow_instances wi ON wi.id = ce.workflow_instance_id
		WHERE ce.campaign_id = ? AND ce.event_type = 'REPLY_WATCHER_STARTED'
		GROUP BY COALESCE(wi.context_json, '{}')
	`, campaignID)
	if err != nil {
		return out, 0
	}
	defer rows.Close()
	for rows.Next() {
		var ctxRaw string
		var n int
		if rows.Scan(&ctxRaw, &n) != nil {
			continue
		}
		intent := intentFromInstanceContext(ctxRaw)
		if intent == "" {
			intent = "unknown"
		}
		out[intent] += n
		total += n
	}
	return out, total
}

func countWatcherInstancesByIntent(campaignID int64, statuses []string) (map[string]int, int) {
	out := map[string]int{}
	total := 0
	if len(statuses) == 0 {
		return out, 0
	}
	placeholders := make([]string, len(statuses))
	args := []interface{}{campaignID}
	for i, s := range statuses {
		placeholders[i] = "?"
		args = append(args, s)
	}
	q := `
		SELECT COALESCE(context_json, '{}'), COUNT(*)
		FROM workflow_instances
		WHERE campaign_id = ?
		  AND status IN (` + strings.Join(placeholders, ",") + `)
		  AND COALESCE(context_json::json->>'watcher', '') IN ('true', '1')
		GROUP BY COALESCE(context_json, '{}')
	`
	rows, err := db.Query(q, args...)
	if err != nil {
		return out, 0
	}
	defer rows.Close()
	for rows.Next() {
		var ctxRaw string
		var n int
		if rows.Scan(&ctxRaw, &n) != nil {
			continue
		}
		intent := intentFromInstanceContext(ctxRaw)
		if intent == "" {
			intent = "unknown"
		}
		out[intent] += n
		total += n
	}
	return out, total
}

func countWatcherSendsByIntent(campaignID int64) (map[string]int, int) {
	out := map[string]int{}
	total := 0
	rows, err := db.Query(`
		SELECT COALESCE(wi.context_json, '{}'), COUNT(DISTINCT es.contact_id)
		FROM email_sends es
		INNER JOIN workflow_instances wi ON wi.id = es.workflow_instance_id
		WHERE es.campaign_id = ?
		  AND LOWER(COALESCE(es.delivery_status, '')) = 'sent'
		  AND COALESCE(wi.context_json::json->>'watcher', '') IN ('true', '1')
		GROUP BY COALESCE(wi.context_json, '{}')
	`, campaignID)
	if err != nil {
		return out, 0
	}
	defer rows.Close()
	for rows.Next() {
		var ctxRaw string
		var n int
		if rows.Scan(&ctxRaw, &n) != nil {
			continue
		}
		intent := intentFromInstanceContext(ctxRaw)
		if intent == "" {
			intent = "unknown"
		}
		out[intent] += n
		total += n
	}
	return out, total
}

func countWatcherSkipsByIntent(campaignID int64) (map[string]int, int) {
	out := map[string]int{}
	total := 0
	// Legacy system notes from the former AI verify/rewrite gate (kept for historical analytics).
	rows, err := db.Query(`
		SELECT COALESCE(wi.context_json, '{}'), COUNT(DISTINCT cm.contact_id)
		FROM conversation_messages cm
		INNER JOIN workflow_instances wi
			ON wi.contact_id = cm.contact_id AND wi.campaign_id = ?
		WHERE cm.direction = 'system'
		  AND cm.body_text ILIKE '[Auto-reply skipped]%'
		  AND COALESCE(wi.context_json::json->>'watcher', '') IN ('true', '1')
		  AND COALESCE(wi.context_json::json->>'reply_message_id', '') <> ''
		GROUP BY COALESCE(wi.context_json, '{}')
	`, campaignID)
	if err != nil {
		return out, 0
	}
	defer rows.Close()
	for rows.Next() {
		var ctxRaw string
		var n int
		if rows.Scan(&ctxRaw, &n) != nil {
			continue
		}
		intent := intentFromInstanceContext(ctxRaw)
		if intent == "" {
			intent = "unknown"
		}
		out[intent] += n
		total += n
	}
	return out, total
}

func intentFromInstanceContext(ctxRaw string) string {
	meta := map[string]interface{}{}
	_ = json.Unmarshal([]byte(ctxRaw), &meta)
	if v, ok := meta["reply_intent"].(string); ok {
		return NormalizeReplyIntent(v)
	}
	return ""
}

// SentimentFromReplyIntent prefers watcher intent taxonomy over coarse sentiment labels.
func SentimentFromReplyIntent(intent, sentiment string) string {
	if n := NormalizeReplyIntent(intent); n != "" {
		return IntentToSentiment(n)
	}
	return NormalizeReplySentiment(sentiment)
}
