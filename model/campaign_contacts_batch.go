package model

import (
	"strings"

	"emailtracker.com/db"
)

// GetCampaignContactEmailMap returns contact_id → email for all contacts on a campaign.
func GetCampaignContactEmailMap(campaignID int64) (map[int64]string, error) {
	rows, err := db.Query(`
		SELECT c.id, c.email
		FROM contact c
		INNER JOIN campaign_contacts cc ON cc.contact_id = c.id
		WHERE cc.campaign_id = ?
		ORDER BY cc.contact_id ASC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]string)
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, nil
}

// CampaignContactData holds email and variables for campaign contact rows.
type CampaignContactData struct {
	Email     string
	Variables []ContactVariables
}

// GetCampaignContactDataMap loads emails + variables for campaign contacts in two queries.
func GetCampaignContactDataMap(campaignID int64) (map[int64]CampaignContactData, error) {
	rows, err := db.Query(`
		SELECT c.id, c.email
		FROM contact c
		INNER JOIN campaign_contacts cc ON cc.contact_id = c.id
		WHERE cc.campaign_id = ?
		ORDER BY cc.contact_id ASC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]CampaignContactData)
	var contactIDs []int64
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = CampaignContactData{Email: email}
		contactIDs = append(contactIDs, id)
	}
	if len(contactIDs) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(contactIDs))
	args := make([]interface{}, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	varRows, err := db.Query(
		`SELECT contact_id, key, value FROM contact_variables WHERE contact_id IN (`+joinPlaceholders(placeholders)+`)`,
		args...,
	)
	if err != nil {
		return out, nil
	}
	defer varRows.Close()
	for varRows.Next() {
		var cid int64
		var key, value string
		if err := varRows.Scan(&cid, &key, &value); err != nil {
			continue
		}
		entry := out[cid]
		entry.Variables = append(entry.Variables, ContactVariables{ContactID: cid, Key: key, Value: value})
		out[cid] = entry
	}
	return out, nil
}

// GetCampaignContactsBatched is a faster replacement for GetCampaignContacts (no per-contact queries).
func GetCampaignContactsBatched(campaignID int64) ([]CampaignContactItem, error) {
	dataMap, err := GetCampaignContactDataMap(campaignID)
	if err != nil {
		return nil, err
	}
	ids, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return nil, err
	}
	items := make([]CampaignContactItem, 0, len(ids))
	for _, id := range ids {
		d := dataMap[id]
		items = append(items, CampaignContactItem{
			ID:        id,
			Email:     d.Email,
			Variables: d.Variables,
		})
	}
	return items, nil
}

func contactVariableMap(vars []ContactVariables) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Key] = v.Value
	}
	return m
}

func contactHasVariable(vars []ContactVariables, key string) bool {
	for _, v := range vars {
		if v.Key == key && strings.TrimSpace(v.Value) != "" {
			return true
		}
	}
	return false
}
