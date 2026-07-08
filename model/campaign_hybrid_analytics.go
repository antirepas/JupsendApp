package model

import (
	"context"
	"database/sql"
	"time"

	"emailtracker.com/db"
	"golang.org/x/sync/errgroup"
)

// CampaignHybridAnalytics combines workflow funnel metrics with starter-email A/B results.
type CampaignHybridAnalytics struct {
	CampaignID   int64
	CampaignName string
	WorkflowName string
	Status       string
	CreatedAt    time.Time
	Workflow     CampaignWorkflowAnalytics
	StarterAB    CampaignStarterABAnalytics
}

// CampaignStarterABAnalytics tracks the A/B test on the workflow's first send step only.
type CampaignStarterABAnalytics struct {
	CampaignID           int64
	TemplateAName        string
	TemplateBName        string
	HasVariantB          bool
	ExperimentVariable   string
	ExperimentHypothesis string
	FirstSendStepLabel   string
	VariantA             VariantAnalytics
	VariantB             VariantAnalytics
	Contacts             []ContactEngagementRow
	Funnel               EngagementFunnel
	ABWinner             string
	ABWinnerMethod       string
}

const starterSendJoin = `
	INNER JOIN workflow_executions we ON es.id = NULLIF(we.output_json::json->>'email_send_id', '')::bigint
	INNER JOIN workflow_instances wi ON wi.id = we.instance_id
`

const starterSendWhere = `
	wi.campaign_id = ? AND we.node_key = ? AND we.status = 'succeeded'
`

// GetCampaignHybridAnalyticsFor loads workflow + starter A/B analytics in parallel.
func GetCampaignHybridAnalyticsFor(c Campaign, userID int64) (CampaignHybridAnalytics, error) {
	if c.ExecutionMode != "workflow_ab" || c.WorkflowVersionID == 0 {
		return CampaignHybridAnalytics{}, errNotWorkflowCampaign
	}

	var result CampaignHybridAnalytics
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		var err error
		result.Workflow, err = GetCampaignWorkflowAnalyticsFor(c, userID)
		return err
	})
	g.Go(func() error {
		var err error
		result.StarterAB, err = GetStarterABAnalyticsFor(c, userID)
		return err
	})
	if err := g.Wait(); err != nil {
		return CampaignHybridAnalytics{}, err
	}

	wfInfo, _ := GetWorkflowForVersion(c.WorkflowVersionID)
	result.CampaignID = c.ID
	result.CampaignName = c.Name
	result.WorkflowName = wfInfo.WorkflowName
	result.Status = ComputeDisplayStatus(c.Status, c.ScheduledAt, c.IsSending)
	result.CreatedAt = c.CreatedAt
	return result, nil
}

// GetStarterABAnalyticsFor returns A/B stats scoped to the workflow's first send node.
func GetStarterABAnalyticsFor(c Campaign, userID int64) (CampaignStarterABAnalytics, error) {
	firstSendKey, err := GetFirstSendNodeKey(c.WorkflowVersionID)
	if err != nil {
		return CampaignStarterABAnalytics{}, err
	}

	aName, bName, err := templateNamesForCampaign(c.ID, userID)
	if err != nil {
		return CampaignStarterABAnalytics{}, err
	}

	contactIDs, err := GetCampaignContactIDs(c.ID)
	if err != nil {
		return CampaignStarterABAnalytics{}, err
	}

	hasB := c.TemplateBID > 0
	labels := NodeLabelMapForVersion(c.WorkflowVersionID)

	analytics := CampaignStarterABAnalytics{
		CampaignID:           c.ID,
		HasVariantB:          hasB,
		TemplateAName:        aName,
		TemplateBName:        bName,
		ExperimentVariable:   c.ExperimentVariable,
		ExperimentHypothesis: c.ExperimentHypothesis,
		FirstSendStepLabel:   LabelFromMap(labels, firstSendKey),
	}

	analytics.VariantA = buildStarterVariantAnalytics("A", c.TemplateAID, aName, contactIDs, hasB, c.ID, firstSendKey)
	if hasB {
		analytics.VariantB = buildStarterVariantAnalytics("B", c.TemplateBID, bName, contactIDs, hasB, c.ID, firstSendKey)
	}
	analytics.Contacts = getStarterContactEngagement(c.ID, firstSendKey, contactIDs, hasB, aName, bName)
	analytics.Funnel = starterEngagementFunnel(analytics.VariantA, analytics.VariantB, hasB)
	analytics.ABWinner, analytics.ABWinnerMethod = pickABWinner(analytics.VariantA, analytics.VariantB, hasB)

	return analytics, nil
}

// CampaignStarterABWinner returns the A/B winner for the first send step only.
func CampaignStarterABWinner(campaignID int64, firstSendNodeKey string) (winner, method string) {
	var hasB bool
	_ = db.QueryRow(`
		SELECT template_b_id IS NOT NULL AND template_b_id > 0
		FROM campaigns WHERE id = ?
	`, campaignID).Scan(&hasB)
	if !hasB {
		return "", ""
	}
	var a, b VariantAnalytics
	a.Variant = "A"
	b.Variant = "B"
	loadStarterVariantMetrics(campaignID, firstSendNodeKey, "A", &a)
	loadStarterVariantMetrics(campaignID, firstSendNodeKey, "B", &b)
	return pickABWinner(a, b, true)
}

// CampaignABWinnerFor picks the winner using starter-email scope for hybrid campaigns.
func CampaignABWinnerFor(c Campaign) (winner, method string) {
	if c.ExecutionMode == "workflow_ab" && c.WorkflowVersionID > 0 {
		firstKey, err := GetFirstSendNodeKey(c.WorkflowVersionID)
		if err != nil {
			return "", ""
		}
		return CampaignStarterABWinner(c.ID, firstKey)
	}
	return CampaignABWinner(c.ID)
}

func buildStarterVariantAnalytics(variant string, templateID int64, templateName string, contactIDs []int64, hasB bool, campaignID int64, firstSendNodeKey string) VariantAnalytics {
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
	loadStarterVariantMetrics(campaignID, firstSendNodeKey, variant, &va)
	va.Pending = va.AssignedContacts - va.Sent
	return va
}

func loadStarterVariantMetrics(campaignID int64, firstSendNodeKey, variant string, va *VariantAnalytics) {
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM email_sends es
		`+starterSendJoin+`
		WHERE `+starterSendWhere+` AND es.variant = ?
	`, campaignID, firstSendNodeKey, variant).Scan(&va.Sent)

	_ = db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_events ee
		INNER JOIN email_sends es ON es.id = ee.email_send_id OR ee.tracking_id = es.tracking_id
		`+starterSendJoin+`
		WHERE `+starterSendWhere+` AND es.variant = ?
	`, campaignID, firstSendNodeKey, variant).Scan(&va.Opens, &va.Clicks)

	if va.Sent > 0 {
		va.OpenRate = float64(va.Opens) / float64(va.Sent) * 100
		va.ClickRate = float64(va.Clicks) / float64(va.Sent) * 100
	}
	if va.Opens > 0 {
		va.ClickToOpenRate = float64(va.Clicks) / float64(va.Opens) * 100
	}

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		`+starterSendJoin+`
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE `+starterSendWhere+` AND es.variant = ?
	`, campaignID, firstSendNodeKey, variant).Scan(&va.UniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		`+starterSendJoin+`
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'click'
		WHERE `+starterSendWhere+` AND es.variant = ?
	`, campaignID, firstSendNodeKey, variant).Scan(&va.UniqueClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		`+starterSendJoin+`
		WHERE `+starterSendWhere+` AND es.variant = ? AND ce.event_type = 'REPLY'
	`, campaignID, firstSendNodeKey, variant).Scan(&va.UniqueReplies)

	if va.Sent > 0 {
		va.ReplyRate = float64(va.UniqueReplies) / float64(va.Sent) * 100
	}
}

func getStarterContactEngagement(campaignID int64, firstSendNodeKey string, contactIDs []int64, hasB bool, aName, bName string) []ContactEngagementRow {
	sendMap := map[int64]ContactEngagementRow{}
	rows, err := db.Query(`
		SELECT
			es.id, es.contact_id, es.variant, es.sent_at,
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			MIN(CASE WHEN ee.event_type = 'open' THEN ee.created_at END),
			MAX(ee.created_at)
		FROM email_sends es
		`+starterSendJoin+`
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE `+starterSendWhere+`
		GROUP BY es.id, es.contact_id, es.variant, es.sent_at
	`, campaignID, firstSendNodeKey)
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

func starterEngagementFunnel(a, b VariantAnalytics, hasB bool) EngagementFunnel {
	f := EngagementFunnel{
		Sent:    a.Sent,
		Opened:  a.UniqueOpens,
		Clicked: a.UniqueClicks,
		Replied: a.UniqueReplies,
	}
	if hasB {
		f.Sent += b.Sent
		f.Opened += b.UniqueOpens
		f.Clicked += b.UniqueClicks
		f.Replied += b.UniqueReplies
	}
	if f.Sent > 0 {
		f.OpenPctOfSent = float64(f.Opened) / float64(f.Sent) * 100
	}
	if f.Opened > 0 {
		f.ClickPctOfOpens = float64(f.Clicked) / float64(f.Opened) * 100
	}
	return f
}
