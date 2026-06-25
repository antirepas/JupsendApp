package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

type Campaign struct {
	ID                 int64
	UserID             int64
	Name               string
	TemplateAID        int64
	TemplateBID        int64
	Status             string
	IsSending          bool
	CreatedAt          time.Time
	ScheduledAt        *time.Time
	ExecutionMode      string
	WorkflowVersionID  int64
	ContactListID      int64
}

type CampaignListItem struct {
	ID                int64
	Name              string
	TemplateAName     string
	TemplateBName     string
	ExecutionMode     string
	WorkflowName      string
	WorkflowVersionID int64
	Status            string
	DisplayStatus     string
	IsSending         bool
	ContactCount      int
	CreatedAt         time.Time
	ScheduledAt       *time.Time
	SendJobCounts     SendJobCounts
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

func ComputeDisplayStatus(status string, scheduledAt *time.Time, isSending bool) string {
	if status == "sent" {
		return "sent"
	}
	if isSending {
		return "sending"
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

func CreateCampaign(userID int64, name string, templateAID, templateBID int64, executionMode string, workflowVersionID int64) (int64, error) {
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
	row := db.QueryRow(
		`INSERT INTO campaigns (name, template_a_id, template_b_id, execution_mode, workflow_version_id, user_id) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		name, templateAID, bID, executionMode, wfVer, userID,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func ListCampaigns(userID int64) ([]CampaignListItem, error) {
	query := `
		SELECT c.id, c.name, c.status, c.created_at, c.scheduled_at,
			COALESCE(c.execution_mode, 'bulk'), COALESCE(c.workflow_version_id, 0),
			COALESCE(ta.name, ''), COALESCE(tb.name, ''),
			COALESCE(w.name, ''),
			COALESCE(cc.cnt, 0), COALESCE(c.is_sending, 0)
		FROM campaigns c
		LEFT JOIN template ta ON ta.id = c.template_a_id
		LEFT JOIN template tb ON tb.id = c.template_b_id
		LEFT JOIN workflow_versions wv ON wv.id = c.workflow_version_id
		LEFT JOIN workflows w ON w.id = wv.workflow_id
		LEFT JOIN (
			SELECT campaign_id, COUNT(*) as cnt FROM campaign_contacts GROUP BY campaign_id
		) cc ON cc.campaign_id = c.id
		WHERE c.user_id = ?
		ORDER BY c.created_at DESC
	`
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CampaignListItem
	for rows.Next() {
		var item CampaignListItem
		var scheduled sql.NullTime
		var isSending int
		err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt, &scheduled,
			&item.ExecutionMode, &item.WorkflowVersionID,
			&item.TemplateAName, &item.TemplateBName, &item.WorkflowName,
			&item.ContactCount, &isSending)
		if err != nil {
			return nil, err
		}
		if item.ExecutionMode == "" {
			item.ExecutionMode = "bulk"
		}
		item.ScheduledAt = scanScheduledAt(scheduled)
		item.IsSending = isSending == 1
		item.DisplayStatus = ComputeDisplayStatus(item.Status, item.ScheduledAt, item.IsSending)
		items = append(items, item)
	}
	return items, nil
}

func GetCampaign(id int64) (Campaign, error) {
	row := db.QueryRow(`
		SELECT id, COALESCE(user_id, 0), name, template_a_id, template_b_id, status, created_at, scheduled_at,
			COALESCE(execution_mode, 'bulk'), COALESCE(workflow_version_id, 0), COALESCE(is_sending, 0),
			COALESCE(contact_list_id, 0)
		FROM campaigns WHERE id = ?
	`, id)
	return scanCampaignRow(row)
}

func GetCampaignForUser(id, userID int64) (Campaign, error) {
	row := db.QueryRow(`
		SELECT id, COALESCE(user_id, 0), name, template_a_id, template_b_id, status, created_at, scheduled_at,
			COALESCE(execution_mode, 'bulk'), COALESCE(workflow_version_id, 0), COALESCE(is_sending, 0),
			COALESCE(contact_list_id, 0)
		FROM campaigns WHERE id = ? AND user_id = ?
	`, id, userID)
	return scanCampaignRow(row)
}

func scanCampaignRow(row interface{ Scan(...interface{}) error }) (Campaign, error) {
	var c Campaign
	var bID sql.NullInt64
	var scheduled sql.NullTime
	var isSending int
	var listID sql.NullInt64
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.TemplateAID, &bID, &c.Status, &c.CreatedAt, &scheduled,
		&c.ExecutionMode, &c.WorkflowVersionID, &isSending, &listID)
	if err != nil {
		return Campaign{}, err
	}
	if bID.Valid {
		c.TemplateBID = bID.Int64
	}
	if listID.Valid {
		c.ContactListID = listID.Int64
	}
	c.ScheduledAt = scanScheduledAt(scheduled)
	c.IsSending = isSending == 1
	return c, nil
}

func GetCampaignDetail(id, userID int64) (CampaignDetail, error) {
	c, err := GetCampaignForUser(id, userID)
	if err != nil {
		return CampaignDetail{}, err
	}

	list, err := ListCampaigns(userID)
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
	detail.IsSending = c.IsSending
	detail.DisplayStatus = ComputeDisplayStatus(detail.Status, detail.ScheduledAt, detail.IsSending)
	if c.IsSending || detail.Status != "sent" {
		detail.SendJobCounts, _ = CountSendJobsByCampaign(id)
	}

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
	row := db.QueryRow(query, campaignID, variant)
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
	rows, err := db.Query(query, campaignID)
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
		varRows, err := db.Query(`SELECT key, value FROM contact_variables WHERE contact_id = ?`, item.ID)
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
	rows, err := db.Query(`SELECT contact_id FROM campaign_contacts WHERE campaign_id = ? ORDER BY contact_id ASC`, campaignID)
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
		_, err := db.Exec(
			`INSERT INTO campaign_contacts (campaign_id, contact_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			campaignID, cid,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func MarkCampaignSent(id int64) error {
	_, err := db.Exec(`UPDATE campaigns SET status = 'sent', scheduled_at = NULL, is_sending = 0 WHERE id = ?`, id)
	return err
}

func MarkCampaignSending(id int64) error {
	_, err := db.Exec(`UPDATE campaigns SET is_sending = 1 WHERE id = ?`, id)
	return err
}

func ScheduleCampaign(id int64, at time.Time) error {
	result, err := db.Exec(
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
	_, err := db.Exec(
		`UPDATE campaigns SET scheduled_at = NULL WHERE id = ? AND status = 'draft'`,
		id,
	)
	return err
}

func GetDueScheduledCampaignIDs() ([]int64, error) {
	rows, err := db.Query(
		`SELECT id, scheduled_at FROM campaigns WHERE status = 'draft' AND scheduled_at IS NOT NULL AND COALESCE(is_sending, 0) = 0`,
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

func MergeTemplateVariables(userID int64, templateIDs []int64) ([]string, error) {
	seen := make(map[string]bool)
	var vars []string
	for _, tid := range templateIDs {
		if tid == 0 {
			continue
		}
		_, v, err := GetTemplateByID(tid, userID)
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

func DeleteCampaign(id, userID int64) error {
	if _, err := GetCampaignForUser(id, userID); err != nil {
		return err
	}
	tx, err := db.Begin()
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
