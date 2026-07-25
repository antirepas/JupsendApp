package model

import "emailtracker.com/db"

type AccountBenchmark struct {
	PeriodDays             int
	TotalSends             int
	OpenRate               float64
	ClickRate              float64
	OpenRateDelta          float64
	PersonalBestOpenRate   float64
	PersonalBestCampaignID int64
	// Legacy reply fields kept zeroed so older templates/tests don't panic if referenced.
	UniqueReplies         int
	ReplyRate             float64
	PersonalBestReplyRate float64
	PriorPeriodReplyRate  float64
	ReplyRateDelta        float64
}

type RecentExperiment struct {
	CampaignID     int64
	Name           string
	Hypothesis     string
	Variable       string
	OpenRate       float64
	ABWinner       string
	ABWinnerMethod string
	TemplateAName  string
	TemplateBName  string
	// Legacy
	ReplyRate float64
}

func GetAccountBenchmark(userID int64, periodDays int) AccountBenchmark {
	if periodDays < 1 {
		periodDays = 30
	}
	b := AccountBenchmark{PeriodDays: periodDays}

	var priorSends int
	var uniqueOpens, uniqueClicks, priorOpens int

	_ = db.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')),
			COUNT(*) FILTER (WHERE sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
				AND sent_at < CURRENT_TIMESTAMP - (? * INTERVAL '1 day'))
		FROM email_sends
		WHERE user_id = ?
	`, periodDays, periodDays*2, periodDays, userID).Scan(&b.TotalSends, &priorSends)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE es.user_id = ? AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, periodDays).Scan(&uniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE es.user_id = ? AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
			AND es.sent_at < CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, periodDays*2, periodDays).Scan(&priorOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'click'
		WHERE es.user_id = ? AND es.sent_at >= CURRENT_TIMESTAMP - (? * INTERVAL '1 day')
	`, userID, periodDays).Scan(&uniqueClicks)

	if b.TotalSends > 0 {
		b.OpenRate = float64(uniqueOpens) / float64(b.TotalSends) * 100
		b.ClickRate = float64(uniqueClicks) / float64(b.TotalSends) * 100
	}
	var priorOpenRate float64
	if priorSends > 0 {
		priorOpenRate = float64(priorOpens) / float64(priorSends) * 100
	}
	b.OpenRateDelta = b.OpenRate - priorOpenRate

	b.PersonalBestOpenRate, b.PersonalBestCampaignID = findPersonalBestCampaignByOpen(userID)
	return b
}

func findPersonalBestCampaignByOpen(userID int64) (float64, int64) {
	const minSends = 20
	rows, err := db.Query(`
		SELECT c.id,
			COUNT(es.id) AS sends,
			COUNT(DISTINCT CASE WHEN ee.event_type = 'open' THEN es.contact_id END) AS opens
		FROM campaigns c
		INNER JOIN email_sends es ON es.campaign_id = c.id
		LEFT JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE c.user_id = ? AND COALESCE(c.execution_mode, 'bulk') = 'bulk'
		GROUP BY c.id
		HAVING COUNT(es.id) >= ?
	`, userID, minSends)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()

	var bestRate float64
	var bestID int64
	for rows.Next() {
		var id int64
		var sends, opens int
		if rows.Scan(&id, &sends, &opens) != nil || sends == 0 {
			continue
		}
		rate := float64(opens) / float64(sends) * 100
		if rate > bestRate {
			bestRate = rate
			bestID = id
		}
	}
	return bestRate, bestID
}

func ListRecentExperiments(userID int64, limit int) ([]RecentExperiment, error) {
	if limit < 1 {
		limit = 5
	}
	rows, err := db.Query(`
		SELECT c.id, c.name, COALESCE(c.experiment_hypothesis, ''), COALESCE(c.experiment_variable, ''),
			COALESCE(ta.name, ''), COALESCE(tb.name, '')
		FROM campaigns c
		LEFT JOIN template ta ON ta.id = c.template_a_id
		LEFT JOIN template tb ON tb.id = c.template_b_id
		WHERE c.user_id = ? AND c.template_b_id IS NOT NULL
			AND COALESCE(c.execution_mode, 'bulk') = 'bulk'
		ORDER BY c.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecentExperiment
	for rows.Next() {
		var e RecentExperiment
		if err := rows.Scan(&e.CampaignID, &e.Name, &e.Hypothesis, &e.Variable, &e.TemplateAName, &e.TemplateBName); err != nil {
			return nil, err
		}
		sent, opens := campaignOverviewOpenCounts(e.CampaignID)
		if sent > 0 {
			e.OpenRate = float64(opens) / float64(sent) * 100
		}
		e.ABWinner, e.ABWinnerMethod = experimentABSummary(e.CampaignID)
		out = append(out, e)
	}
	return out, nil
}

func CountRepliesThisMonth(userID int64) int {
	return 0
}
