package model

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"

	"emailtracker.com/db"
	"golang.org/x/sync/errgroup"
)

var errNotWorkflowCampaign = errors.New("not a workflow campaign")

func templateNamesForCampaign(campaignID, userID int64) (aName, bName string, err error) {
	err = db.QueryRow(`
		SELECT COALESCE(ta.name, ''), COALESCE(tb.name, '')
		FROM campaigns c
		LEFT JOIN template ta ON ta.id = c.template_a_id
		LEFT JOIN template tb ON tb.id = c.template_b_id
		WHERE c.id = ? AND c.user_id = ?
	`, campaignID, userID).Scan(&aName, &bName)
	return aName, bName, err
}

// GetCampaignAnalytics loads campaign analytics with parallel independent queries.
func GetCampaignAnalyticsParallel(campaignID, userID int64) (CampaignAnalytics, error) {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return CampaignAnalytics{}, err
	}
	return GetCampaignAnalyticsFor(c, userID)
}

// GetCampaignAnalyticsFor loads analytics for an already-fetched campaign (avoids duplicate fetch).
func GetCampaignAnalyticsFor(c Campaign, userID int64) (CampaignAnalytics, error) {
	campaignID := c.ID

	aName, bName, err := templateNamesForCampaign(campaignID, userID)
	if err != nil {
		return CampaignAnalytics{}, err
	}

	contactIDs, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return CampaignAnalytics{}, err
	}

	hasB := c.TemplateBID > 0
	analytics := CampaignAnalytics{
		CampaignID:             campaignID,
		Name:                   c.Name,
		Status:                 ComputeDisplayStatus(c.Status, c.ScheduledAt, c.IsSending),
		CreatedAt:              c.CreatedAt,
		HasVariantB:            hasB,
		TemplateAName:          aName,
		TemplateBName:          bName,
		ExperimentVariable:     c.ExperimentVariable,
		ExperimentHypothesis:   c.ExperimentHypothesis,
		Overview:               CampaignOverview{ContactCount: len(contactIDs)},
	}

	ctx := context.Background()
	g, _ := errgroup.WithContext(ctx)

	g.Go(func() error {
		analytics.VariantA = buildVariantAnalytics("A", c.TemplateAID, aName, contactIDs, hasB, campaignID)
		return nil
	})
	if hasB {
		g.Go(func() error {
			analytics.VariantB = buildVariantAnalytics("B", c.TemplateBID, bName, contactIDs, hasB, campaignID)
			return nil
		})
	}
	g.Go(func() error {
		analytics.Contacts = getContactEngagementFast(campaignID, contactIDs, hasB, aName, bName)
		return nil
	})
	g.Go(func() error {
		analytics.DailyStats = getCampaignDailyStats(campaignID)
		return nil
	})
	g.Go(func() error {
		analytics.HourlyOpens = getCampaignHourlyStats(campaignID, "open")
		return nil
	})
	g.Go(func() error {
		analytics.HourlyClicks = getCampaignHourlyStats(campaignID, "click")
		return nil
	})
	g.Go(func() error {
		analytics.LinkClicks = getCampaignLinkClicks(campaignID)
		return nil
	})
	g.Go(func() error {
		analytics.VariableCoverage = getVariableCoverageFast(campaignID, c.TemplateAID, c.TemplateBID, contactIDs)
		return nil
	})
	g.Go(func() error {
		analytics.AccountBenchmark = GetAccountBenchmark(userID, 30)
		return nil
	})

	if err := g.Wait(); err != nil {
		return CampaignAnalytics{}, err
	}

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

	bench := analytics.AccountBenchmark
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

func getContactEngagementFast(campaignID int64, contactIDs []int64, hasB bool, aName, bName string) []ContactEngagementRow {
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
			var sentAt sql.NullTime
			var firstOpen, lastAct sql.NullTime
			if err := rows.Scan(&row.SendID, &row.ContactID, &row.Variant, &sentAt,
				&row.OpenCount, &row.ClickCount, &firstOpen, &lastAct); err != nil {
				continue
			}
			if sentAt.Valid {
				row.SentAt = &sentAt.Time
			}
			if firstOpen.Valid && sentAt.Valid {
				row.FirstOpenAt = &firstOpen.Time
				mins := firstOpen.Time.Sub(sentAt.Time).Minutes()
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

	emailMap, _ := GetCampaignContactEmailMap(campaignID)

	var result []ContactEngagementRow
	for i, cid := range contactIDs {
		variant := "A"
		templateName := aName
		if hasB && i%2 == 1 {
			variant = "B"
			templateName = bName
		}
		row := ContactEngagementRow{
			ContactID:    cid,
			Email:        emailMap[cid],
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

func getVariableCoverageFast(campaignID int64, templateAID, templateBID int64, contactIDs []int64) []VariableCoverageStat {
	contactData, _ := GetCampaignContactDataMap(campaignID)

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
			if contactHasVariable(contactData[cid].Variables, key) {
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

// GetCampaignWorkflowAnalyticsParallel loads workflow analytics with concurrent queries.
func GetCampaignWorkflowAnalyticsParallel(campaignID, userID int64) (CampaignWorkflowAnalytics, error) {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return CampaignWorkflowAnalytics{}, err
	}
	return GetCampaignWorkflowAnalyticsFor(c, userID)
}

// GetCampaignWorkflowAnalyticsFor loads workflow analytics for an already-fetched campaign.
func GetCampaignWorkflowAnalyticsFor(c Campaign, userID int64) (CampaignWorkflowAnalytics, error) {
	campaignID := c.ID
	if (c.ExecutionMode != "workflow" && c.ExecutionMode != "workflow_ab") || c.WorkflowVersionID == 0 {
		return CampaignWorkflowAnalytics{}, errNotWorkflowCampaign
	}

	contactIDs, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return CampaignWorkflowAnalytics{}, err
	}

	var (
		wfInfo     WorkflowVersionInfo
		overview   CampaignWorkflowOverview
		engagement CampaignWorkflowEngagement
		stepEng    map[string]stepEngagement
		stoppedAt  map[string]int
		contacts   []CampaignWorkflowContactAnalytics
		daily      []CampaignDailyStat
		hourlyOpen []HourlyStat
		hourlyClk  []HourlyStat
	)

	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		var e error
		wfInfo, e = GetWorkflowForVersion(c.WorkflowVersionID)
		return e
	})
	g.Go(func() error {
		var e error
		overview, e = GetCampaignWorkflowOverview(campaignID, c.WorkflowVersionID, len(contactIDs))
		return e
	})
	g.Go(func() error {
		engagement = getCampaignWorkflowEngagement(campaignID, len(contactIDs))
		return nil
	})
	g.Go(func() error {
		stepEng = getCampaignStepEngagement(campaignID)
		return nil
	})
	g.Go(func() error {
		stoppedAt = getCampaignStoppedAtNode(campaignID)
		return nil
	})
	g.Go(func() error {
		contacts = buildCampaignWorkflowContactAnalyticsFast(campaignID, c.WorkflowVersionID, contactIDs)
		return nil
	})
	g.Go(func() error {
		daily = getCampaignDailyStats(campaignID)
		return nil
	})
	g.Go(func() error {
		hourlyOpen = getCampaignHourlyStats(campaignID, "open")
		return nil
	})
	g.Go(func() error {
		hourlyClk = getCampaignHourlyStats(campaignID, "click")
		return nil
	})
	if err := g.Wait(); err != nil {
		return CampaignWorkflowAnalytics{}, err
	}

	denom := overview.TotalContacts
	if denom < 1 {
		denom = 1
	}
	var steps []CampaignWorkflowStepAnalytics
	for _, s := range overview.Steps {
		e := stepEng[s.NodeKey]
		sa := CampaignWorkflowStepAnalytics{
			CampaignWorkflowStepStat: s,
			Opens:                    e.Opens,
			Clicks:                   e.Clicks,
			StoppedHere:              stoppedAt[s.NodeKey],
			PassedPct:                float64(s.PassedThrough) / float64(denom) * 100,
		}
		if s.PassedThrough > 0 {
			sa.OpenRate = float64(e.Opens) / float64(s.PassedThrough) * 100
		}
		steps = append(steps, sa)
	}

	result := CampaignWorkflowAnalytics{
		CampaignID:   campaignID,
		CampaignName: c.Name,
		WorkflowName: wfInfo.WorkflowName,
		Status:       ComputeDisplayStatus(c.Status, c.ScheduledAt, c.IsSending),
		CreatedAt:    c.CreatedAt,
		Overview:     overview,
		Engagement:   engagement,
		Steps:        steps,
		Contacts:     contacts,
		DailyStats:   daily,
		HourlyOpens:  hourlyOpen,
		HourlyClicks: hourlyClk,
	}

	sort.Slice(result.DailyStats, func(i, j int) bool {
		return result.DailyStats[i].Date < result.DailyStats[j].Date
	})
	return result, nil
}

func buildCampaignWorkflowContactAnalyticsFast(campaignID, versionID int64, contactIDs []int64) []CampaignWorkflowContactAnalytics {
	var (
		instances []WorkflowInstance
		emailMap  map[int64]string
		sendMap   map[int64]struct{ sent, opens, clicks int }
		replied   map[int64]bool
		labels    map[string]string
	)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		instances, _ = ListInstancesForCampaign(campaignID)
	}()
	go func() {
		defer wg.Done()
		emailMap, _ = GetCampaignContactEmailMap(campaignID)
	}()
	go func() {
		defer wg.Done()
		sendMap = loadCampaignContactSendStats(campaignID)
	}()
	go func() {
		defer wg.Done()
		replied = loadCampaignRepliedContacts(campaignID)
	}()
	go func() {
		defer wg.Done()
		labels = NodeLabelMapForVersion(versionID)
	}()
	wg.Wait()

	instanceMap := make(map[int64]WorkflowInstance, len(instances))
	for _, inst := range instances {
		instanceMap[inst.ContactID] = inst
	}

	var result []CampaignWorkflowContactAnalytics
	for _, cid := range contactIDs {
		row := CampaignWorkflowContactAnalytics{
			ContactID: cid,
			Email:     emailMap[cid],
		}
		if inst, ok := instanceMap[cid]; ok {
			row.InstanceStatus = inst.Status
			row.NodeKey = inst.CurrentNodeKey
			row.CurrentStep = LabelFromMap(labels, inst.CurrentNodeKey)
		} else {
			row.InstanceStatus = "not started"
			row.CurrentStep = "—"
		}
		if s, ok := sendMap[cid]; ok {
			row.EmailsSent = s.sent
			row.OpenCount = s.opens
			row.ClickCount = s.clicks
		}
		row.HasReplied = replied[cid]
		result = append(result, row)
	}
	return result
}

func loadCampaignContactSendStats(campaignID int64) map[int64]struct{ sent, opens, clicks int } {
	result := map[int64]struct{ sent, opens, clicks int }{}
	rows, err := db.Query(`
		SELECT es.contact_id, COUNT(*),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.campaign_id = ?
		GROUP BY es.contact_id
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var sent, opens, clicks int
		if rows.Scan(&cid, &sent, &opens, &clicks) == nil {
			result[cid] = struct{ sent, opens, clicks int }{sent, opens, clicks}
		}
	}
	return result
}

func loadCampaignRepliedContacts(campaignID int64) map[int64]bool {
	return CampaignRepliedContactSet(campaignID)
}

// CampaignRepliedContactSet returns contact IDs that replied to a send in this campaign.
func CampaignRepliedContactSet(campaignID int64) map[int64]bool {
	result := map[int64]bool{}
	rows, err := db.Query(`
		SELECT DISTINCT es.contact_id FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		if rows.Scan(&cid) == nil {
			result[cid] = true
		}
	}
	// Also treat contact.replied_at as replied when they are on this campaign and have a send.
	rows2, err := db.Query(`
		SELECT DISTINCT es.contact_id FROM email_sends es
		INNER JOIN contact c ON c.id = es.contact_id
		WHERE es.campaign_id = ? AND c.replied_at IS NOT NULL
	`, campaignID)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var cid int64
			if rows2.Scan(&cid) == nil {
				result[cid] = true
			}
		}
	}
	return result
}
