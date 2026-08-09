package model

import (
	"database/sql"
	"math"
	"strings"
	"time"

	"emailtracker.com/db"
)

var errNotFound = sql.ErrNoRows

type Contact struct {
	ID     int64
	UserID int64
	Email  string
}

type ContactVariables struct {
	ContactID int64
	Key       string
	Value     string
}

type ContactListItem struct {
	ID               int64
	Email            string
	Variables        []ContactVariables
	CreatedAt        time.Time
	Suppressed       bool
	RepliedAt        *time.Time
	ListNames        []string
	VarPreview       string
	ExtraVarCount    int
	LastCampaignID   int64
	LastCampaignName string
	LastSignal       string
	LastActivity     *time.Time
}

type ContactListFilter struct {
	Query             string
	ListID            int64
	CampaignID        int64
	ExcludeCampaignID int64 // omit contacts already on this campaign (picker)
	Engagement        string // "", opened_no_reply, clicked_no_reply, replied, interested
	Sort              string // newest, email
	Page              int
	PageSize          int
	RepliedOnly       bool
	Lite              bool // id+email only (no enrichment / variables)
}

type ContactListPage struct {
	Items      []ContactListItem
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

type ContactSummary struct {
	Contact     Contact
	Variables   []ContactVariables
	Lists       []ContactList
	Suppressed  bool
	RepliedAt   *time.Time
	RecentSends []EmailSendListItem
	Campaigns   []ContactCampaignRef
	Timeline    []ContactTimelineItem
}

func (c *Contact) SaveContact(userID int64, variables []ContactVariables) (int64, error) {
	query := `INSERT INTO contact (email, user_id) VALUES (?, ?) RETURNING id`

	row := db.QueryRow(query, c.Email, userID)
	var contactID int64
	err := row.Scan(&contactID)
	if err != nil {
		return 0, err
	}
	query = `INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`
	for _, v := range variables {
		_, err = db.Exec(query, v.Key, v.Value, contactID)
		if err != nil {
			return 0, err
		}
	}

	return contactID, err
}

func GetContact(contactId int64) (Contact, []ContactVariables, error) {
	query := `SELECT id, COALESCE(user_id, 0), email FROM contact WHERE id = ?`
	row := db.QueryRow(query, contactId)
	var c Contact
	err := row.Scan(&c.ID, &c.UserID, &c.Email)
	if err != nil {
		return Contact{}, nil, err
	}
	query = `SELECT key, value FROM contact_variables WHERE contact_id = ?`
	rows, err := db.Query(query, contactId)
	if err != nil {
		return Contact{}, nil, err
	}
	defer rows.Close()
	var cVars []ContactVariables
	for rows.Next() {
		var cVar ContactVariables
		err = rows.Scan(&cVar.Key, &cVar.Value)
		if err != nil {
			return Contact{}, nil, err
		}
		cVar.ContactID = contactId
		cVars = append(cVars, cVar)
	}
	return c, cVars, nil
}

func GetContactForUser(contactId, userID int64) (Contact, []ContactVariables, error) {
	c, vars, err := GetContact(contactId)
	if err != nil {
		return Contact{}, nil, err
	}
	if userID > 0 && c.UserID > 0 && c.UserID != userID {
		return Contact{}, nil, errNotFound
	}
	return c, vars, nil
}

func ListContacts(userID int64) ([]ContactListItem, error) {
	return ListContactPickerItems(userID, 2000)
}

// ListContactPickerItems returns id+email only for campaign contact pickers (fast).
func ListContactPickerItems(userID int64, limit int) ([]ContactListItem, error) {
	if limit <= 0 {
		limit = 2000
	}
	if limit > 5000 {
		limit = 5000
	}
	rows, err := db.Query(`
		SELECT id, email FROM contact WHERE user_id = ? ORDER BY email ASC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ContactListItem
	for rows.Next() {
		var item ContactListItem
		if err := rows.Scan(&item.ID, &item.Email); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func ListContactsFiltered(userID int64, f ContactListFilter) (ContactListPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}

	where := []string{"c.user_id = ?"}
	args := []interface{}{userID}

	if f.ListID > 0 {
		where = append(where, `c.id IN (SELECT contact_id FROM contact_list_members WHERE list_id = ?)`)
		args = append(args, f.ListID)
	}
	engagement := strings.TrimSpace(f.Engagement)
	if f.RepliedOnly {
		engagement = "replied"
	}
	// Campaign alone = membership. Campaign + engagement = engagement on that campaign's sends.
	if f.CampaignID > 0 && engagement == "" {
		where = append(where, campaignMembershipFilterSQL())
		args = append(args, f.CampaignID, f.CampaignID)
	}
	if f.ExcludeCampaignID > 0 {
		where = append(where, `c.id NOT IN (SELECT contact_id FROM campaign_contacts WHERE campaign_id = ?)`)
		args = append(args, f.ExcludeCampaignID)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "LOWER(c.email) LIKE ?")
		args = append(args, "%"+strings.ToLower(q)+"%")
	}
	if clause, extra := engagementFilterSQL(engagement, f.CampaignID); clause != "" {
		where = append(where, "("+clause+")")
		args = append(args, extra...)
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countQ := `SELECT COUNT(*) FROM contact c WHERE ` + whereSQL
	if err := db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return ContactListPage{}, err
	}

	order := "c.id DESC"
	if f.Sort == "email" {
		order = "LOWER(c.email) ASC"
	}

	offset := (f.Page - 1) * f.PageSize
	listQ := `
		SELECT c.id, c.email, COALESCE(c.created_at, CURRENT_TIMESTAMP),
			EXISTS(SELECT 1 FROM contact_suppressions cs WHERE cs.contact_id = c.id),
			c.replied_at
		FROM contact c
		WHERE ` + whereSQL + `
		ORDER BY ` + order + `
		LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), f.PageSize, offset)

	rows, err := db.Query(listQ, listArgs...)
	if err != nil {
		return ContactListPage{}, err
	}
	defer rows.Close()

	var items []ContactListItem
	var contactIDs []int64
	for rows.Next() {
		var item ContactListItem
		var replied sql.NullTime
		if err := rows.Scan(&item.ID, &item.Email, &item.CreatedAt, &item.Suppressed, &replied); err != nil {
			return ContactListPage{}, err
		}
		if replied.Valid {
			t := replied.Time
			item.RepliedAt = &t
		}
		contactIDs = append(contactIDs, item.ID)
		items = append(items, item)
	}

	listMap := map[int64][]ContactList{}
	engMap := map[int64]ContactEngagement{}
	if len(contactIDs) > 0 {
		engMap, _ = EnrichContactsEngagement(userID, contactIDs)
		for i := range items {
			if eng, ok := engMap[items[i].ID]; ok {
				items[i].LastCampaignID = eng.LastCampaignID
				items[i].LastCampaignName = eng.LastCampaignName
				items[i].LastSignal = eng.LastSignal
				items[i].LastActivity = eng.LastActivity
			}
		}
	}
	if !f.Lite {
		listMap, _ = GetListIDsForContacts(userID, contactIDs)
		for i := range items {
			for _, l := range listMap[items[i].ID] {
				items[i].ListNames = append(items[i].ListNames, l.Name)
			}
			varRows, err := db.Query(`SELECT key, value FROM contact_variables WHERE contact_id = ?`, items[i].ID)
			if err != nil {
				continue
			}
			for varRows.Next() {
				var cv ContactVariables
				if err := varRows.Scan(&cv.Key, &cv.Value); err != nil {
					varRows.Close()
					break
				}
				cv.ContactID = items[i].ID
				items[i].Variables = append(items[i].Variables, cv)
			}
			varRows.Close()
			if len(items[i].Variables) > 0 {
				parts := []string{}
				for j, cv := range items[i].Variables {
					if j >= 2 {
						items[i].ExtraVarCount = len(items[i].Variables) - 2
						break
					}
					parts = append(parts, cv.Key+"="+cv.Value)
				}
				items[i].VarPreview = strings.Join(parts, ", ")
			}
		}
	}

	totalPages := int(math.Ceil(float64(total) / float64(f.PageSize)))
	if totalPages < 1 {
		totalPages = 1
	}

	return ContactListPage{
		Items:      items,
		Total:      total,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalPages: totalPages,
	}, nil
}

// ListContactIDsMatching returns all contact IDs matching the filter (capped).
func ListContactIDsMatching(userID int64, f ContactListFilter, max int) ([]int64, error) {
	if max < 1 {
		max = 2000
	}
	if max > 10000 {
		max = 10000
	}
	f.Lite = true
	f.PageSize = 200
	ids := make([]int64, 0, 256)
	for page := 1; len(ids) < max; page++ {
		f.Page = page
		result, err := ListContactsFiltered(userID, f)
		if err != nil {
			return nil, err
		}
		for _, it := range result.Items {
			ids = append(ids, it.ID)
			if len(ids) >= max {
				return ids, nil
			}
		}
		if page >= result.TotalPages || len(result.Items) == 0 {
			break
		}
	}
	return ids, nil
}

func BulkDeleteContacts(userID int64, ids []int64) (int, error) {
	deleted := 0
	for _, id := range ids {
		if err := DeleteContact(id, userID); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func GetContactSummary(userID, contactID int64) (ContactSummary, error) {
	c, vars, err := GetContactForUser(contactID, userID)
	if err != nil {
		return ContactSummary{}, err
	}
	lists, _ := GetListsForContact(userID, contactID)
	suppressed, _ := IsContactSuppressed(contactID)
	var replied sql.NullTime
	_ = db.QueryRow(`SELECT replied_at FROM contact WHERE id = ?`, contactID).Scan(&replied)
	var repliedAt *time.Time
	if replied.Valid {
		t := replied.Time
		repliedAt = &t
	}

	sends, _ := ListEmailSendsForContact(userID, contactID, 10)
	timeline, _ := ListContactTimeline(userID, contactID, 150)

	campaignRows, _ := db.Query(`
		SELECT DISTINCT camp.id, camp.name FROM campaign_contacts cc
		INNER JOIN campaigns camp ON camp.id = cc.campaign_id
		WHERE cc.contact_id = ? AND camp.user_id = ?
		ORDER BY camp.name
	`, contactID, userID)
	var campaigns []ContactCampaignRef
	if campaignRows != nil {
		for campaignRows.Next() {
			var ref ContactCampaignRef
			if err := campaignRows.Scan(&ref.ID, &ref.Name); err == nil {
				campaigns = append(campaigns, ref)
			}
		}
		campaignRows.Close()
	}

	return ContactSummary{
		Contact:     c,
		Variables:   vars,
		Lists:       lists,
		Suppressed:  suppressed,
		RepliedAt:   repliedAt,
		RecentSends: sends,
		Campaigns:   campaigns,
		Timeline:    timeline,
	}, nil
}

func DeleteContact(id, userID int64) error {
	if _, _, err := GetContactForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM contact WHERE id = ?`, id)
	return err
}

func UpdateContact(id, userID int64, email string, variables []ContactVariables) error {
	if _, _, err := GetContactForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE contact SET email = ? WHERE id = ?`, email, id)
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM contact_variables WHERE contact_id = ?`, id)
	if err != nil {
		return err
	}
	for _, v := range variables {
		if v.Key == "" {
			continue
		}
		_, err = db.Exec(
			`INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`,
			v.Key, v.Value, id,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func FindOrCreateContact(userID int64, email string, variables []ContactVariables) (int64, error) {
	row := db.QueryRow(`SELECT id FROM contact WHERE email = ? AND user_id = ?`, email, userID)
	var id int64
	err := row.Scan(&id)
	if err == nil {
		return id, nil
	}

	c := Contact{Email: email}
	return c.SaveContact(userID, variables)
}

func FindContactByEmail(userID int64, email string) (Contact, error) {
	row := db.QueryRow(`SELECT id, COALESCE(user_id, 0), email FROM contact WHERE email = ? AND user_id = ?`, email, userID)
	var c Contact
	err := row.Scan(&c.ID, &c.UserID, &c.Email)
	return c, err
}
