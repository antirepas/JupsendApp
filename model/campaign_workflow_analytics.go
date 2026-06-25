package model

import (
	"fmt"
	"sort"
	"time"

	"emailtracker.com/db"
)

type CampaignWorkflowStepAnalytics struct {
	CampaignWorkflowStepStat
	Opens       int
	Clicks      int
	OpenRate    float64
	PassedPct   float64
	StoppedHere int
}

type CampaignWorkflowEngagement struct {
	EmailsSent      int
	UniqueOpens     int
	UniqueClicks    int
	Replies         int
	OpenRate        float64
	ClickRate       float64
	ClickToOpenRate float64
	Funnel          EngagementFunnel
	ClickPctOfSent  float64
	ReplyPctOfSent  float64
}

type CampaignWorkflowContactAnalytics struct {
	ContactID      int64
	Email          string
	InstanceStatus string
	CurrentStep    string
	NodeKey        string
	EmailsSent     int
	OpenCount      int
	ClickCount     int
	HasReplied     bool
}

type CampaignWorkflowAnalytics struct {
	CampaignID   int64
	CampaignName string
	WorkflowName string
	Status       string
	CreatedAt    time.Time
	Overview     CampaignWorkflowOverview
	Engagement   CampaignWorkflowEngagement
	Steps        []CampaignWorkflowStepAnalytics
	Contacts     []CampaignWorkflowContactAnalytics
	DailyStats   []CampaignDailyStat
	HourlyOpens  []HourlyStat
	HourlyClicks []HourlyStat
}

func GetCampaignWorkflowAnalytics(campaignID, userID int64) (CampaignWorkflowAnalytics, error) {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return CampaignWorkflowAnalytics{}, err
	}
	if c.ExecutionMode != "workflow" || c.WorkflowVersionID == 0 {
		return CampaignWorkflowAnalytics{}, fmt.Errorf("not a workflow campaign")
	}

	wfInfo, _ := GetWorkflowForVersion(c.WorkflowVersionID)
	contactIDs, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return CampaignWorkflowAnalytics{}, err
	}

	overview, err := GetCampaignWorkflowOverview(campaignID, c.WorkflowVersionID, len(contactIDs))
	if err != nil {
		return CampaignWorkflowAnalytics{}, err
	}

	engagement := getCampaignWorkflowEngagement(campaignID, len(contactIDs))
	stepEng := getCampaignStepEngagement(campaignID)
	stoppedAt := getCampaignStoppedAtNode(campaignID)

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
		Contacts:     buildCampaignWorkflowContactAnalytics(campaignID, c.WorkflowVersionID, contactIDs),
		DailyStats:   getCampaignDailyStats(campaignID),
		HourlyOpens:  getCampaignHourlyStats(campaignID, "open"),
		HourlyClicks: getCampaignHourlyStats(campaignID, "click"),
	}

	sort.Slice(result.DailyStats, func(i, j int) bool {
		return result.DailyStats[i].Date < result.DailyStats[j].Date
	})
	return result, nil
}

type stepEngagement struct {
	Opens  int
	Clicks int
}

func getCampaignWorkflowEngagement(campaignID int64, contactCount int) CampaignWorkflowEngagement {
	eng := CampaignWorkflowEngagement{}
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE campaign_id = ?`, campaignID).Scan(&eng.EmailsSent)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'open'
		WHERE es.campaign_id = ?
	`, campaignID).Scan(&eng.UniqueOpens)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT es.contact_id) FROM email_sends es
		INNER JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id) AND ee.event_type = 'click'
		WHERE es.campaign_id = ?
	`, campaignID).Scan(&eng.UniqueClicks)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT ce.contact_id) FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
	`, campaignID).Scan(&eng.Replies)

	if eng.EmailsSent > 0 {
		eng.OpenRate = float64(eng.UniqueOpens) / float64(eng.EmailsSent) * 100
		eng.ClickRate = float64(eng.UniqueClicks) / float64(eng.EmailsSent) * 100
		eng.ClickPctOfSent = eng.ClickRate
		eng.ReplyPctOfSent = float64(eng.Replies) / float64(eng.EmailsSent) * 100
	}
	if eng.UniqueOpens > 0 {
		eng.ClickToOpenRate = float64(eng.UniqueClicks) / float64(eng.UniqueOpens) * 100
	}

	eng.Funnel = EngagementFunnel{
		Sent:    eng.EmailsSent,
		Opened:  eng.UniqueOpens,
		Clicked: eng.UniqueClicks,
		Replied: eng.Replies,
	}
	if eng.Funnel.Sent > 0 {
		eng.Funnel.OpenPctOfSent = float64(eng.Funnel.Opened) / float64(eng.Funnel.Sent) * 100
	}
	if eng.Funnel.Opened > 0 {
		eng.Funnel.ClickPctOfOpens = float64(eng.Funnel.Clicked) / float64(eng.Funnel.Opened) * 100
	}
	_ = contactCount
	return eng
}

func getCampaignStepEngagement(campaignID int64) map[string]stepEngagement {
	result := map[string]stepEngagement{}
	rows, err := db.Query(`
		SELECT we.node_key,
			COUNT(DISTINCT CASE WHEN ee.event_type = 'open' THEN es.contact_id END),
			COUNT(DISTINCT CASE WHEN ee.event_type = 'click' THEN es.contact_id END)
		FROM workflow_executions we
		INNER JOIN workflow_instances wi ON wi.id = we.instance_id
		LEFT JOIN email_sends es ON es.id = NULLIF(we.output_json::json->>'email_send_id', '')::bigint
		LEFT JOIN email_events ee ON (ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id)
		WHERE wi.campaign_id = ? AND we.status = 'succeeded'
		GROUP BY we.node_key
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var opens, clicks int
		if rows.Scan(&key, &opens, &clicks) == nil {
			result[key] = stepEngagement{Opens: opens, Clicks: clicks}
		}
	}
	return result
}

func getCampaignStoppedAtNode(campaignID int64) map[string]int {
	result := map[string]int{}
	rows, err := db.Query(`
		SELECT current_node_key, COUNT(*)
		FROM workflow_instances
		WHERE campaign_id = ? AND status = 'cancelled'
		GROUP BY current_node_key
	`, campaignID)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if rows.Scan(&key, &n) == nil {
			result[key] = n
		}
	}
	return result
}

func buildCampaignWorkflowContactAnalytics(campaignID, versionID int64, contactIDs []int64) []CampaignWorkflowContactAnalytics {
	instanceMap, _ := GetCampaignInstanceMap(campaignID)
	sendMap := map[int64]struct{ sent, opens, clicks int }{}
	rows, err := db.Query(`
		SELECT es.contact_id, COUNT(*),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE es.campaign_id = ?
		GROUP BY es.contact_id
	`, campaignID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int64
			var sent, opens, clicks int
			if rows.Scan(&cid, &sent, &opens, &clicks) == nil {
				sendMap[cid] = struct{ sent, opens, clicks int }{sent, opens, clicks}
			}
		}
	}

	replied := map[int64]bool{}
	rRows, _ := db.Query(`
		SELECT DISTINCT es.contact_id FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY'
	`, campaignID)
	if rRows != nil {
		defer rRows.Close()
		for rRows.Next() {
			var cid int64
			if rRows.Scan(&cid) == nil {
				replied[cid] = true
			}
		}
	}

	var result []CampaignWorkflowContactAnalytics
	for _, cid := range contactIDs {
		_, email, err := getContactEmail(cid)
		if err != nil {
			continue
		}
		row := CampaignWorkflowContactAnalytics{
			ContactID: cid,
			Email:     email,
		}
		if inst, ok := instanceMap[cid]; ok {
			row.InstanceStatus = inst.Status
			row.NodeKey = inst.CurrentNodeKey
			row.CurrentStep = NodeLabelForKey(versionID, inst.CurrentNodeKey)
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
