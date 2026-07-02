package model

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"emailtracker.com/db"
)

type CampaignOverview struct {
	ContactCount    int
	SentCount       int
	PendingCount    int
	TotalOpens      int
	UniqueOpens     int
	TotalClicks     int
	UniqueClicks    int
	ReplyCount      int
	OpenRate        float64
	ClickRate       float64
	ClickToOpenRate float64
	AvgMinutesToOpen float64
}

type VariantAnalytics struct {
	Variant            string
	TemplateID         int64
	TemplateName       string
	AssignedContacts   int
	Sent               int
	Pending            int
	Opens              int
	UniqueOpens        int
	Clicks             int
	UniqueClicks       int
	UniqueReplies      int
	OpenRate           float64
	ClickRate          float64
	ClickToOpenRate    float64
	ReplyRate          float64
}

type ContactEngagementRow struct {
	ContactID        int64
	Email            string
	Variant          string
	TemplateName     string
	SendID           int64
	SentAt           *time.Time
	OpenCount        int
	ClickCount       int
	FirstOpenAt      *time.Time
	LastActivityAt   *time.Time
	Engaged          bool
	MinutesToFirstOpen float64
}

type CampaignDailyStat struct {
	Date   string
	Sends  int
	Opens  int
	Clicks int
}

type HourlyStat struct {
	Hour  int
	Count int
}

type LinkClickStat struct {
	OriginalURL    string
	Clicks         int
	UniqueContacts int
}

type VariableCoverageStat struct {
	Variable           string
	Templates          string
	ContactsWithValue  int
	ContactsMissing    int
	CoveragePercent    float64
}

type EngagementFunnel struct {
	Sent            int
	Opened          int
	Clicked         int
	Replied         int
	OpenPctOfSent   float64
	ClickPctOfOpens float64
}

type CampaignAnalytics struct {
	CampaignID   int64
	Name         string
	Status       string
	CreatedAt    time.Time
	HasVariantB  bool
	TemplateAName string
	TemplateBName string
	Overview     CampaignOverview
	VariantA     VariantAnalytics
	VariantB     VariantAnalytics
	Contacts     []ContactEngagementRow
	DailyStats   []CampaignDailyStat
	HourlyOpens  []HourlyStat
	HourlyClicks []HourlyStat
	LinkClicks   []LinkClickStat
	Funnel       EngagementFunnel
	VariableCoverage []VariableCoverageStat
	ABWinner         string
	ABWinnerMethod   string
	ExperimentVariable  string
	ExperimentHypothesis string
	CampaignReplyRate   float64
	ReplyRateDelta      float64
	IsPersonalBest      bool
	AccountBenchmark    AccountBenchmark
}

func GetCampaignAnalytics(campaignID, userID int64) (CampaignAnalytics, error) {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return CampaignAnalytics{}, err
	}

	list, _ := ListCampaigns(userID)
	var aName, bName string
	for _, item := range list {
		if item.ID == campaignID {
			aName = item.TemplateAName
			bName = item.TemplateBName
			break
		}
	}

	contactIDs, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return CampaignAnalytics{}, err
	}

	hasB := c.TemplateBID > 0
	analytics := CampaignAnalytics{
		CampaignID:           campaignID,
		Name:                 c.Name,
		Status:               ComputeDisplayStatus(c.Status, c.ScheduledAt, c.IsSending),
		CreatedAt:            c.CreatedAt,
		HasVariantB:          hasB,
		TemplateAName:          aName,
		TemplateBName:          bName,
		ExperimentVariable:     c.ExperimentVariable,
		ExperimentHypothesis:   c.ExperimentHypothesis,
	}

	analytics.Overview.ContactCount = len(contactIDs)
	analytics.VariantA = buildVariantAnalytics("A", c.TemplateAID, aName, contactIDs, hasB, campaignID)
	if hasB {
		analytics.VariantB = buildVariantAnalytics("B", c.TemplateBID, bName, contactIDs, hasB, campaignID)
	}

	analytics.Contacts = getContactEngagement(campaignID, contactIDs, hasB, c.TemplateAID, c.TemplateBID, aName, bName)
	analytics.DailyStats = getCampaignDailyStats(campaignID)
	analytics.HourlyOpens = getCampaignHourlyStats(campaignID, "open")
	analytics.HourlyClicks = getCampaignHourlyStats(campaignID, "click")
	analytics.LinkClicks = getCampaignLinkClicks(campaignID)
	analytics.VariableCoverage = getVariableCoverage(campaignID, c.TemplateAID, c.TemplateBID, contactIDs)

	fillOverview(&analytics)
	analytics.Funnel = EngagementFunnel{
		Sent:    analytics.Overview.SentCount,
		Opened:  analytics.Overview.UniqueOpens,
		Clicked: analytics.Overview.UniqueClicks,
		Replied: analytics.Overview.ReplyCount,
	}
	if analytics.Funnel.Sent > 0 {
		analytics.Funnel.OpenPctOfSent = float64(analytics.Funnel.Opened) / float64(analytics.Funnel.Sent) * 100
	}
	if analytics.Funnel.Opened > 0 {
		analytics.Funnel.ClickPctOfOpens = float64(analytics.Funnel.Clicked) / float64(analytics.Funnel.Opened) * 100
	}
	analytics.ABWinner, analytics.ABWinnerMethod = pickABWinner(analytics.VariantA, analytics.VariantB, hasB)

	bench := GetAccountBenchmark(userID, 30)
	analytics.AccountBenchmark = bench
	if analytics.Overview.SentCount > 0 {
		analytics.CampaignReplyRate = float64(analytics.Overview.ReplyCount) / float64(analytics.Overview.SentCount) * 100
		analytics.ReplyRateDelta = analytics.CampaignReplyRate - bench.ReplyRate
	}
	if bench.PersonalBestCampaignID == campaignID || (analytics.CampaignReplyRate > 0 && analytics.CampaignReplyRate >= bench.PersonalBestReplyRate && analytics.Overview.SentCount >= 20) {
		analytics.IsPersonalBest = true
	}

	sort.Slice(analytics.DailyStats, func(i, j int) bool {
		return analytics.DailyStats[i].Date < analytics.DailyStats[j].Date
	})
	sort.Slice(analytics.VariableCoverage, func(i, j int) bool {
		return analytics.VariableCoverage[i].Variable < analytics.VariableCoverage[j].Variable
	})

	return analytics, nil
}

func buildVariantAnalytics(variant string, templateID int64, templateName string, contactIDs []int64, hasB bool, campaignID int64) VariantAnalytics {
	va := VariantAnalytics{
		Variant:      variant,
		TemplateID:   templateID,
		TemplateName: templateName,
	}
	for i := range contactIDs {
		assigned := "A"
		if hasB && i%2 == 1 {
			assigned = "B"
		}
		if assigned == variant {
			va.AssignedContacts++
		}
	}

	loadVariantMetrics(campaignID, variant, &va)
	va.Pending = va.AssignedContacts - va.Sent
	return va
}

func loadVariantMetrics(campaignID int64, variant string, va *VariantAnalytics) {
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM email_sends WHERE campaign_id = ? AND variant = ?
	`, campaignID, variant).Scan(&va.Sent)

	_ = db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id OR ee.tracking_id = es.tracking_id
		WHERE es.campaign_id = ? AND es.variant = ?
	`, campaignID, variant).Scan(&va.Opens, &va.Clicks)

	if va.Sent > 0 {
		va.OpenRate = float64(va.Opens) / float64(va.Sent) * 100
		va.ClickRate = float64(va.Clicks) / float64(va.Sent) * 100
	}
	if va.Opens > 0 {
		va.ClickToOpenRate = float64(va.Clicks) / float64(va.Opens) * 100
	}

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE es.campaign_id = ? AND es.variant = ?
	`, campaignID, variant).Scan(&va.UniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'click'
		WHERE es.campaign_id = ? AND es.variant = ?
	`, campaignID, variant).Scan(&va.UniqueClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND es.variant = ? AND ce.event_type = 'REPLY'
	`, campaignID, variant).Scan(&va.UniqueReplies)

	if va.Sent > 0 {
		va.ReplyRate = float64(va.UniqueReplies) / float64(va.Sent) * 100
	}
}

func campaignOverviewCounts(campaignID int64) (sent, replies int) {
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE campaign_id = ?`, campaignID).Scan(&sent)
	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
	`, campaignID).Scan(&replies)
	return sent, replies
}

func experimentABSummary(campaignID int64) (winner, method string) {
	var a, b VariantAnalytics
	a.Variant = "A"
	b.Variant = "B"
	loadVariantMetrics(campaignID, "A", &a)
	loadVariantMetrics(campaignID, "B", &b)
	return pickABWinner(a, b, true)
}

func getContactEngagement(campaignID int64, contactIDs []int64, hasB bool, templateAID, templateBID int64, aName, bName string) []ContactEngagementRow {
	sendMap := map[int64]ContactEngagementRow{}
	rows, err := db.Query(`
		SELECT
			es.id, es.contact_id, es.variant, es.sent_at,
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			MIN(CASE WHEN ee.event_type = 'open' THEN ee.created_at END),
			MAX(ee.created_at)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.campaign_id = ?
		GROUP BY es.id, es.contact_id, es.variant, es.sent_at
	`, campaignID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row ContactEngagementRow
			var sentAt time.Time
			var firstOpen, lastAct sql.NullTime
			if err := rows.Scan(&row.SendID, &row.ContactID, &row.Variant, &sentAt,
				&row.OpenCount, &row.ClickCount, &firstOpen, &lastAct); err != nil {
				continue
			}
			row.SentAt = &sentAt
			if firstOpen.Valid {
				row.FirstOpenAt = &firstOpen.Time
				mins := firstOpen.Time.Sub(sentAt).Minutes()
				if mins >= 0 {
					row.MinutesToFirstOpen = mins
				}
			}
			if lastAct.Valid {
				row.LastActivityAt = &lastAct.Time
			}
			row.Engaged = row.OpenCount > 0 || row.ClickCount > 0
			sendMap[row.ContactID] = row
		}
	}

	var result []ContactEngagementRow
	for i, cid := range contactIDs {
		variant := "A"
		templateName := aName
		if hasB && i%2 == 1 {
			variant = "B"
			templateName = bName
		}
		_, email, _ := getContactEmail(cid)
		row := ContactEngagementRow{
			ContactID:    cid,
			Email:        email,
			Variant:      variant,
			TemplateName: templateName,
		}
		if sent, ok := sendMap[cid]; ok {
			row.SendID = sent.SendID
			row.SentAt = sent.SentAt
			row.OpenCount = sent.OpenCount
			row.ClickCount = sent.ClickCount
			row.FirstOpenAt = sent.FirstOpenAt
			row.LastActivityAt = sent.LastActivityAt
			row.Engaged = sent.Engaged
			row.MinutesToFirstOpen = sent.MinutesToFirstOpen
		}
		result = append(result, row)
	}
	return result
}

func getContactEmail(contactID int64) (int64, string, error) {
	var email string
	err := db.QueryRow(`SELECT email FROM contact WHERE id = ?`, contactID).Scan(&email)
	return contactID, email, err
}

func getCampaignDailyStats(campaignID int64) []CampaignDailyStat {
	sendMap := map[string]int{}
	rows, _ := db.Query(`
		SELECT (sent_at)::date, COUNT(*) FROM email_sends
		WHERE campaign_id = ? GROUP BY (sent_at)::date
	`, campaignID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var day string
			var n int
			if rows.Scan(&day, &n) == nil {
				sendMap[day] = n
			}
		}
	}

	openMap := map[string]int{}
	openRows, _ := db.Query(`
		SELECT (ee.created_at)::date, COUNT(*) FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.campaign_id = ? AND ee.event_type = 'open'
		GROUP BY (ee.created_at)::date
	`, campaignID)
	if openRows != nil {
		defer openRows.Close()
		for openRows.Next() {
			var day string
			var n int
			if openRows.Scan(&day, &n) == nil {
				openMap[day] = n
			}
		}
	}

	clickMap := map[string]int{}
	clickRows, _ := db.Query(`
		SELECT (ee.created_at)::date, COUNT(*) FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.campaign_id = ? AND ee.event_type = 'click'
		GROUP BY (ee.created_at)::date
	`, campaignID)
	if clickRows != nil {
		defer clickRows.Close()
		for clickRows.Next() {
			var day string
			var n int
			if clickRows.Scan(&day, &n) == nil {
				clickMap[day] = n
			}
		}
	}

	seen := map[string]bool{}
	for d := range sendMap {
		seen[d] = true
	}
	for d := range openMap {
		seen[d] = true
	}
	for d := range clickMap {
		seen[d] = true
	}

	var stats []CampaignDailyStat
	for day := range seen {
		stats = append(stats, CampaignDailyStat{
			Date:   day,
			Sends:  sendMap[day],
			Opens:  openMap[day],
			Clicks: clickMap[day],
		})
	}
	return stats
}

func getCampaignHourlyStats(campaignID int64, eventType string) []HourlyStat {
	rows, err := db.Query(`
		SELECT CAST(EXTRACT(HOUR FROM ee.created_at) AS INTEGER), COUNT(*)
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.campaign_id = ? AND ee.event_type = ?
		GROUP BY EXTRACT(HOUR FROM ee.created_at)
	`, campaignID, eventType)
	if err != nil {
		return nil
	}
	defer rows.Close()

	counts := make([]int, 24)
	for rows.Next() {
		var hour, n int
		if rows.Scan(&hour, &n) == nil && hour >= 0 && hour < 24 {
			counts[hour] = n
		}
	}
	var stats []HourlyStat
	for h := 0; h < 24; h++ {
		stats = append(stats, HourlyStat{Hour: h, Count: counts[h]})
	}
	return stats
}

func getCampaignLinkClicks(campaignID int64) []LinkClickStat {
	rows, err := db.Query(`
		SELECT tl.original_url, COUNT(*), COUNT(DISTINCT es.contact_id)
		FROM email_events ee
		INNER JOIN tracked_links tl ON tl.tracking_id = ee.tracking_id
		INNER JOIN email_sends es ON es.id = tl.email_send_id
		WHERE es.campaign_id = ? AND ee.event_type = 'click'
		GROUP BY tl.original_url
		ORDER BY COUNT(*) DESC
		LIMIT 20
	`, campaignID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var stats []LinkClickStat
	for rows.Next() {
		var s LinkClickStat
		if rows.Scan(&s.OriginalURL, &s.Clicks, &s.UniqueContacts) == nil {
			stats = append(stats, s)
		}
	}
	return stats
}

func getVariableCoverage(campaignID int64, templateAID, templateBID int64, contactIDs []int64) []VariableCoverageStat {
	contacts, _ := GetCampaignContacts(campaignID)
	contactMap := make(map[int64]CampaignContactItem)
	for _, c := range contacts {
		contactMap[c.ID] = c
	}

	type varInfo struct {
		templates string
	}
	varMap := map[string]varInfo{}

	_, aVars, _ := GetTemplateByID(templateAID, 0)
	for _, k := range aVars {
		info := varMap[k]
		if info.templates == "" {
			info.templates = "A"
		} else if !strings.Contains(info.templates, "A") {
			info.templates += ", A"
		}
		varMap[k] = info
	}
	if templateBID > 0 {
		_, bVars, _ := GetTemplateByID(templateBID, 0)
		for _, k := range bVars {
			info := varMap[k]
			if info.templates == "" {
				info.templates = "B"
			} else if !strings.Contains(info.templates, "B") {
				info.templates += ", B"
			}
			varMap[k] = info
		}
	}

	var stats []VariableCoverageStat
	for key, info := range varMap {
		withValue := 0
		for _, cid := range contactIDs {
			c := contactMap[cid]
			found := false
			for _, v := range c.Variables {
				if v.Key == key && strings.TrimSpace(v.Value) != "" {
					found = true
					break
				}
			}
			if found {
				withValue++
			}
		}
		missing := len(contactIDs) - withValue
		pct := 0.0
		if len(contactIDs) > 0 {
			pct = float64(withValue) / float64(len(contactIDs)) * 100
		}
		stats = append(stats, VariableCoverageStat{
			Variable:          key,
			Templates:         info.templates,
			ContactsWithValue: withValue,
			ContactsMissing:   missing,
			CoveragePercent:   pct,
		})
	}
	return stats
}

func fillOverview(a *CampaignAnalytics) {
	o := &a.Overview
	for _, c := range a.Contacts {
		if c.SendID > 0 {
			o.SentCount++
		}
	}
	o.PendingCount = o.ContactCount - o.SentCount

	_ = db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id
		WHERE es.campaign_id = ?
	`, a.CampaignID).Scan(&o.TotalOpens, &o.TotalClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE es.campaign_id = ?
	`, a.CampaignID).Scan(&o.UniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'click'
		WHERE es.campaign_id = ?
	`, a.CampaignID).Scan(&o.UniqueClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
	`, a.CampaignID).Scan(&o.ReplyCount)

	if o.SentCount > 0 {
		o.OpenRate = float64(o.UniqueOpens) / float64(o.SentCount) * 100
		o.ClickRate = float64(o.UniqueClicks) / float64(o.SentCount) * 100
	}
	if o.UniqueOpens > 0 {
		o.ClickToOpenRate = float64(o.UniqueClicks) / float64(o.UniqueOpens) * 100
	}

	var totalMins float64
	var openCount int
	for _, c := range a.Contacts {
		if c.MinutesToFirstOpen > 0 {
			totalMins += c.MinutesToFirstOpen
			openCount++
		}
	}
	if openCount > 0 {
		o.AvgMinutesToOpen = totalMins / float64(openCount)
	}
}

func pickABWinner(a, b VariantAnalytics, hasB bool) (string, string) {
	if !hasB {
		return "", ""
	}
	if a.Sent == 0 && b.Sent == 0 {
		return "", ""
	}

	const minSends = 10
	if a.Sent >= minSends && b.Sent >= minSends && (a.UniqueReplies > 0 || b.UniqueReplies > 0) {
		if a.ReplyRate > b.ReplyRate+0.01 {
			return "A", "reply"
		}
		if b.ReplyRate > a.ReplyRate+0.01 {
			return "B", "reply"
		}
		if a.UniqueReplies == b.UniqueReplies && a.UniqueReplies > 0 {
			return "Tie", "reply"
		}
	}

	if a.Sent == 0 || b.Sent == 0 {
		if a.OpenRate > b.OpenRate {
			return "A", "opens"
		}
		if b.OpenRate > a.OpenRate {
			return "B", "opens"
		}
		return "Tie", "opens"
	}

	scoreA := a.OpenRate*0.6 + a.ClickRate*0.4
	scoreB := b.OpenRate*0.6 + b.ClickRate*0.4
	if scoreA > scoreB+0.5 {
		return "A", "opens"
	}
	if scoreB > scoreA+0.5 {
		return "B", "opens"
	}
	return "Tie", "opens"
}
