package model

import (
	"fmt"
	"strings"

	"emailtracker.com/db"
)

// CampaignMemberFilter pages/filter the campaign manage-page contact table.
type CampaignMemberFilter struct {
	Query      string // email substring
	Engagement string // opened|clicked|replied|sent|not_sent|missing_vars|""
	Page       int
	PageSize   int
	// Missing-vars filter helpers (bulk campaigns).
	HasB          bool
	TemplateAVars []string
	TemplateBVars []string
}

// CampaignMemberPage is one page of campaign contact IDs.
type CampaignMemberPage struct {
	ContactIDs []int64
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// ListCampaignMemberPage returns a page of campaign contact IDs with optional email/engagement filters.
func ListCampaignMemberPage(campaignID int64, f CampaignMemberFilter) (CampaignMemberPage, error) {
	out := CampaignMemberPage{Page: f.Page, PageSize: f.PageSize}
	if out.Page < 1 {
		out.Page = 1
	}
	if out.PageSize < 1 {
		out.PageSize = 50
	}
	if out.PageSize > 200 {
		out.PageSize = 200
	}

	q := strings.ToLower(strings.TrimSpace(f.Query))
	engagement := strings.TrimSpace(f.Engagement)

	where := []string{"cc.campaign_id = ?"}
	args := []interface{}{campaignID}
	if q != "" {
		where = append(where, "POSITION(? IN LOWER(c.email)) > 0")
		args = append(args, q)
	}

	join := `
		FROM campaign_contacts cc
		INNER JOIN contact c ON c.id = cc.contact_id
	`
	switch engagement {
	case "sent":
		where = append(where, `EXISTS (
			SELECT 1 FROM email_sends es
			WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
			  AND es.delivery_status = 'sent'
		)`)
	case "not_sent":
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM email_sends es
			WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
			  AND es.delivery_status = 'sent'
		)`)
	case "opened":
		where = append(where, `EXISTS (
			SELECT 1 FROM email_sends es
			INNER JOIN email_events ee ON ee.email_send_id = es.id
			WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
			  AND ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0
		)`)
	case "clicked":
		where = append(where, `EXISTS (
			SELECT 1 FROM email_sends es
			INNER JOIN email_events ee ON ee.email_send_id = es.id
			WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
			  AND ee.event_type = 'click'
		)`)
	case "replied":
		where = append(where, `(
			EXISTS (
				SELECT 1 FROM email_sends es
				INNER JOIN contact_events ce ON ce.email_send_id = es.id
				WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
				  AND ce.event_type = 'REPLY'
			)
			OR (
				c.replied_at IS NOT NULL
				AND EXISTS (
					SELECT 1 FROM email_sends es
					WHERE es.campaign_id = cc.campaign_id AND es.contact_id = cc.contact_id
				)
			)
		)`)
	case "missing_vars":
		return listCampaignMembersMissingVarsPage(campaignID, q, f)
	}

	whereSQL := strings.Join(where, " AND ")
	countQ := `SELECT COUNT(*) ` + join + ` WHERE ` + whereSQL
	if err := db.QueryRow(countQ, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if out.Total == 0 {
		out.TotalPages = 0
		return out, nil
	}
	out.TotalPages = (out.Total + out.PageSize - 1) / out.PageSize
	if out.Page > out.TotalPages {
		out.Page = out.TotalPages
	}
	offset := (out.Page - 1) * out.PageSize

	listArgs := append(append([]interface{}{}, args...), out.PageSize, offset)
	listQ := `SELECT cc.contact_id ` + join + ` WHERE ` + whereSQL + ` ORDER BY cc.contact_id ASC LIMIT ? OFFSET ?`
	rows, err := db.Query(listQ, listArgs...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.ContactIDs = append(out.ContactIDs, id)
	}
	return out, nil
}

func listCampaignMembersMissingVarsPage(campaignID int64, emailQ string, f CampaignMemberFilter) (CampaignMemberPage, error) {
	out := CampaignMemberPage{Page: f.Page, PageSize: f.PageSize}
	if out.Page < 1 {
		out.Page = 1
	}
	if out.PageSize < 1 {
		out.PageSize = 50
	}
	ids, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return out, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	dataMap, err := GetCampaignContactDataMap(campaignID)
	if err != nil {
		return out, err
	}
	aVars := f.TemplateAVars
	bVars := f.TemplateBVars
	var missing []int64
	for i, id := range ids {
		data := dataMap[id]
		if emailQ != "" && !strings.Contains(strings.ToLower(data.Email), emailQ) {
			continue
		}
		keys := aVars
		if f.HasB && i%2 == 1 && len(bVars) > 0 {
			keys = bVars
		}
		if contactMissingAnyVar(data.Variables, keys) {
			missing = append(missing, id)
		}
	}
	out.Total = len(missing)
	if out.Total == 0 {
		return out, nil
	}
	out.TotalPages = (out.Total + out.PageSize - 1) / out.PageSize
	if out.Page > out.TotalPages {
		out.Page = out.TotalPages
	}
	start := (out.Page - 1) * out.PageSize
	end := start + out.PageSize
	if start >= len(missing) {
		return out, nil
	}
	if end > len(missing) {
		end = len(missing)
	}
	out.ContactIDs = missing[start:end]
	return out, nil
}

func contactMissingAnyVar(vars []ContactVariables, keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	m := contactVariableMap(vars)
	for _, k := range keys {
		if strings.TrimSpace(m[k]) == "" {
			return true
		}
	}
	return false
}

// CountCampaignContactsMissingVars counts bulk-campaign contacts missing required template variables
// without rendering email previews.
func CountCampaignContactsMissingVars(campaignID int64, templateAVars, templateBVars []string, hasB bool) (int, error) {
	ids, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 || (len(templateAVars) == 0 && (!hasB || len(templateBVars) == 0)) {
		return 0, nil
	}
	dataMap, err := GetCampaignContactDataMap(campaignID)
	if err != nil {
		return 0, err
	}
	n := 0
	for i, id := range ids {
		keys := templateAVars
		if hasB && i%2 == 1 && len(templateBVars) > 0 {
			keys = templateBVars
		}
		if contactMissingAnyVar(dataMap[id].Variables, keys) {
			n++
		}
	}
	return n, nil
}

// GetCampaignContactEngagementLiteForContacts is like GetCampaignContactEngagementLite but scoped to IDs.
func GetCampaignContactEngagementLiteForContacts(campaignID int64, contactIDs []int64) (map[int64]ContactEngagementLite, error) {
	out := make(map[int64]ContactEngagementLite)
	if len(contactIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+1)
	args = append(args, campaignID)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(`
		SELECT es.contact_id, es.id,
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' AND COALESCE(ee.is_bot, 0) = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0)
		FROM email_sends es
		LEFT JOIN email_events ee ON ee.email_send_id = es.id
		WHERE es.campaign_id = ? AND es.contact_id IN (`+joinPlaceholders(placeholders)+`)
		GROUP BY es.contact_id, es.id
		ORDER BY es.contact_id, es.id DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var contactID, sendID int64
		var opens, clicks int
		if err := rows.Scan(&contactID, &sendID, &opens, &clicks); err != nil {
			return nil, err
		}
		if _, exists := out[contactID]; exists {
			continue
		}
		out[contactID] = ContactEngagementLite{OpenCount: opens, ClickCount: clicks, SendID: sendID}
	}
	return out, nil
}

// GetCampaignContactDataMapForIDs loads email+variables for a subset of contacts.
func GetCampaignContactDataMapForIDs(contactIDs []int64) (map[int64]CampaignContactData, error) {
	out := make(map[int64]CampaignContactData)
	if len(contactIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := db.Query(`SELECT id, email FROM contact WHERE id IN (`+joinPlaceholders(placeholders)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = CampaignContactData{Email: email}
	}
	varRows, err := db.Query(
		`SELECT contact_id, key, value FROM contact_variables WHERE contact_id IN (`+joinPlaceholders(placeholders)+`)`,
		args...,
	)
	if err != nil {
		return out, err
	}
	defer varRows.Close()
	for varRows.Next() {
		var cid int64
		var key, val string
		if err := varRows.Scan(&cid, &key, &val); err != nil {
			return out, err
		}
		d := out[cid]
		d.Variables = append(d.Variables, ContactVariables{ContactID: cid, Key: key, Value: val})
		out[cid] = d
	}
	return out, nil
}

// ListInstancesForContactIDs returns workflow instances for a campaign scoped to contacts.
func ListInstancesForContactIDs(campaignID int64, contactIDs []int64) ([]WorkflowInstance, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+1)
	args = append(args, campaignID)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(`
		SELECT id, workflow_version_id, contact_id, campaign_id, fork_root_id, branch_priority,
			current_node_key, status, next_wake_at, waiting_for_event, started_at, completed_at, context_json
		FROM workflow_instances
		WHERE campaign_id = ? AND contact_id IN (`+joinPlaceholders(placeholders)+`)
		ORDER BY contact_id ASC, id ASC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()
	return scanInstances(rows)
}

// GetCampaignContactIndexMap returns 0-based positions in the campaign's contact_id order.
func GetCampaignContactIndexMap(campaignID int64, contactIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(contactIDs))
	if len(contactIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+1)
	args = append(args, campaignID)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(`
		WITH ranked AS (
			SELECT contact_id, (ROW_NUMBER() OVER (ORDER BY contact_id) - 1)::int AS idx
			FROM campaign_contacts
			WHERE campaign_id = ?
		)
		SELECT contact_id, idx FROM ranked WHERE contact_id IN (`+joinPlaceholders(placeholders)+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var idx int
		if err := rows.Scan(&id, &idx); err != nil {
			return nil, err
		}
		out[id] = idx
	}
	return out, nil
}

// CampaignRepliedContactSetForIDs is a scoped replied set.
func CampaignRepliedContactSetForIDs(campaignID int64, contactIDs []int64) map[int64]bool {
	result := map[int64]bool{}
	if len(contactIDs) == 0 {
		return result
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+1)
	args = append(args, campaignID)
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	in := joinPlaceholders(placeholders)
	rows, err := db.Query(`
		SELECT DISTINCT es.contact_id FROM contact_events ce
		INNER JOIN email_sends es ON es.id = ce.email_send_id
		WHERE es.campaign_id = ? AND ce.event_type = 'REPLY' AND es.contact_id IN (`+in+`)
	`, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int64
			if rows.Scan(&cid) == nil {
				result[cid] = true
			}
		}
	}
	rows2, err := db.Query(`
		SELECT DISTINCT es.contact_id FROM email_sends es
		INNER JOIN contact c ON c.id = es.contact_id
		WHERE es.campaign_id = ? AND c.replied_at IS NOT NULL AND es.contact_id IN (`+in+`)
	`, args...)
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
