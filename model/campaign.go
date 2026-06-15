package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

type Campaign struct {
	ID                 int64
	Name               string
	TemplateAID        int64
	TemplateBID        int64
	Status             string
	CreatedAt          time.Time
	ScheduledAt        *time.Time
	ExecutionMode      string
	WorkflowVersionID  int64
}

type CampaignListItem struct {
	ID              int64
	Name            string
	TemplateAName   string
	TemplateBName   string
	Status          string
	DisplayStatus   string
	ContactCount    int
	CreatedAt       time.Time
	ScheduledAt     *time.Time
}

type VariantStats struct {
	Variant    string
	TemplateID int64
	Sends      int
	Opens      int
	Clicks     int
	OpenRate   float64
	ClickRate  float64
}

type CampaignDetail struct {
	CampaignListItem
	TemplateAID int64
	TemplateBID int64
	Contacts    []CampaignContactItem
	VariantA    VariantStats
	VariantB    VariantStats
}

type CampaignContactItem struct {
	ID        int64
	Email     string
	Variables []ContactVariables
}

func ComputeDisplayStatus(status string, scheduledAt *time.Time) string {
	if status == "sent" {
		return "sent"
	}
	if scheduledAt != nil {
		return "scheduled"
	}
	return "draft"
}

func scanScheduledAt(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

func CreateCampaign(name string, templateAID, templateBID int64, executionMode string, workflowVersionID int64) (int64, error) {
	var bID interface{}
	if templateBID > 0 {
		bID = templateBID
	}
	if executionMode == "" {
		executionMode = "bulk"
	}
	var wfVer interface{}
	if workflowVersionID > 0 {
		wfVer = workflowVersionID
	}
	row := db.DB.QueryRow(
		`INSERT INTO campaigns (name, template_a_id, template_b_id, execution_mode, workflow_version_id) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		name, templateAID, bID, executionMode, wfVer,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func ListCampaigns() ([]CampaignListItem, error) {
	query := `
		SELECT c.id, c.name, c.status, c.created_at, c.scheduled_at,
			COALESCE(ta.name, ''), COALESCE(tb.name, ''),
			COALESCE(cc.cnt, 0)
		FROM campaigns c
		LEFT JOIN template ta ON ta.id = c.template_a_id
		LEFT JOIN template tb ON tb.id = c.template_b_id
		LEFT JOIN (
			SELECT campaign_id, COUNT(*) as cnt FROM campaign_contacts GROUP BY campaign_id
		) cc ON cc.campaign_id = c.id
		ORDER BY c.created_at DESC
	`
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CampaignListItem
	for rows.Next() {
		var item CampaignListItem
		var scheduled sql.NullTime
		err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt, &scheduled,
			&item.TemplateAName, &item.TemplateBName, &item.ContactCount)
		if err != nil {
			return nil, err
		}
		item.ScheduledAt = scanScheduledAt(scheduled)
		item.DisplayStatus = ComputeDisplayStatus(item.Status, item.ScheduledAt)
		items = append(items, item)
	}
	return items, nil
}

func GetCampaign(id int64) (Campaign, error) {
	row := db.DB.QueryRow(`
		SELECT id, name, template_a_id, template_b_id, status, created_at, scheduled_at,
			COALESCE(execution_mode, 'bulk'), COALESCE(workflow_version_id, 0)
		FROM campaigns WHERE id = ?
	`, id)
	var c Campaign
	var bID sql.NullInt64
	var scheduled sql.NullTime
	err := row.Scan(&c.ID, &c.Name, &c.TemplateAID, &bID, &c.Status, &c.CreatedAt, &scheduled,
		&c.ExecutionMode, &c.WorkflowVersionID)
	if err != nil {
		return Campaign{}, err
	}
	if bID.Valid {
		c.TemplateBID = bID.Int64
	}
	c.ScheduledAt = scanScheduledAt(scheduled)
	return c, nil
}

func GetCampaignDetail(id int64) (CampaignDetail, error) {
	c, err := GetCampaign(id)
	if err != nil {
		return CampaignDetail{}, err
	}

	list, err := ListCampaigns()
	if err != nil {
		return CampaignDetail{}, err
	}

	var detail CampaignDetail
	for _, item := range list {
		if item.ID == id {
			detail.CampaignListItem = item
			break
		}
	}
	detail.TemplateAID = c.TemplateAID
	detail.TemplateBID = c.TemplateBID

	contacts, err := GetCampaignContacts(id)
	if err != nil {
		return CampaignDetail{}, err
	}
	detail.Contacts = contacts

	detail.VariantA, _ = getVariantStats(id, "A", c.TemplateAID)
	if c.TemplateBID > 0 {
		detail.VariantB, _ = getVariantStats(id, "B", c.TemplateBID)
	}

	return detail, nil
}

func getVariantStats(campaignID int64, variant string, templateID int64) (VariantStats, error) {
	query := `
		SELECT
			COUNT(es.id),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id
		WHERE es.campaign_id = ? AND es.variant = ?
	`
	row := db.DB.QueryRow(query, campaignID, variant)
	var stats VariantStats
	stats.Variant = variant
	stats.TemplateID = templateID
	err := row.Scan(&stats.Sends, &stats.Opens, &stats.Clicks)
	if err != nil {
		return stats, err
	}
	if stats.Sends > 0 {
		stats.OpenRate = float64(stats.Opens) / float64(stats.Sends) * 100
		stats.ClickRate = float64(stats.Clicks) / float64(stats.Sends) * 100
	}
	return stats, nil
}

func GetCampaignContacts(campaignID int64) ([]CampaignContactItem, error) {
	query := `
		SELECT c.id, c.email FROM contact c
		INNER JOIN campaign_contacts cc ON cc.contact_id = c.id
		WHERE cc.campaign_id = ?
		ORDER BY cc.contact_id ASC
	`
	rows, err := db.DB.Query(query, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CampaignContactItem
	for rows.Next() {
		var item CampaignContactItem
		if err := rows.Scan(&item.ID, &item.Email); err != nil {
			return nil, err
		}
		varRows, err := db.DB.Query(`SELECT key, value FROM contact_variables WHERE contact_id = ?`, item.ID)
		if err != nil {
			return nil, err
		}
		for varRows.Next() {
			var cv ContactVariables
			if err := varRows.Scan(&cv.Key, &cv.Value); err != nil {
				varRows.Close()
				return nil, err
			}
			cv.ContactID = item.ID
			item.Variables = append(item.Variables, cv)
		}
		varRows.Close()
		items = append(items, item)
	}
	return items, nil
}

func GetCampaignContactIDs(campaignID int64) ([]int64, error) {
	rows, err := db.DB.Query(`SELECT contact_id FROM campaign_contacts WHERE campaign_id = ? ORDER BY contact_id ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func AddContactsToCampaign(campaignID int64, contactIDs []int64) error {
	for _, cid := range contactIDs {
		_, err := db.DB.Exec(
			`INSERT OR IGNORE INTO campaign_contacts (campaign_id, contact_id) VALUES (?, ?)`,
			campaignID, cid,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func MarkCampaignSent(id int64) error {
	_, err := db.DB.Exec(`UPDATE campaigns SET status = 'sent', scheduled_at = NULL WHERE id = ?`, id)
	return err
}

func ScheduleCampaign(id int64, at time.Time) error {
	result, err := db.DB.Exec(
		`UPDATE campaigns SET scheduled_at = ? WHERE id = ? AND status = 'draft'`,
		at, id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func ClearCampaignSchedule(id int64) error {
	_, err := db.DB.Exec(
		`UPDATE campaigns SET scheduled_at = NULL WHERE id = ? AND status = 'draft'`,
		id,
	)
	return err
}

func GetDueScheduledCampaignIDs() ([]int64, error) {
	rows, err := db.DB.Query(
		`SELECT id, scheduled_at FROM campaigns WHERE status = 'draft' AND scheduled_at IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var ids []int64
	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if !at.After(now) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func MergeTemplateVariables(templateIDs []int64) ([]string, error) {
	seen := make(map[string]bool)
	var vars []string
	for _, tid := range templateIDs {
		if tid == 0 {
			continue
		}
		_, v, err := GetTemplateByID(tid)
		if err != nil {
			return nil, err
		}
		for _, key := range v {
			if !seen[key] {
				seen[key] = true
				vars = append(vars, key)
			}
		}
	}
	return vars, nil
}

func DeleteCampaign(id int64) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// email_sends.campaign_id references campaigns; unlink so sends stay in history
	_, err = tx.Exec(`UPDATE email_sends SET campaign_id = NULL, variant = '' WHERE campaign_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM campaigns WHERE id = ?`, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}
