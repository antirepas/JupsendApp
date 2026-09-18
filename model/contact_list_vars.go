package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"emailtracker.com/db"
)

// ListContactRow is a contact row for list detail views.
type ListContactRow struct {
	ID               int64
	Email            string
	Variables        map[string]string
	LastCampaignID   int64
	LastCampaignName string
	LastSignal       string
	LastActivity     *time.Time
	Suppressed       bool
	RepliedAt        *time.Time
}

// ListMembersFilter controls pagination and filtering for list detail.
type ListMembersFilter struct {
	Query      string
	Engagement string // "", opened_no_reply, clicked_no_reply, replied, interested, not_interested, positive_reply, negative_reply
	VarKey     string
	VarOp      string // equals, not_equals, exists, empty
	VarValue   string
	Sort       string // email, newest
	Page       int
	PageSize   int
}

// ListMembersPage is a paginated list-members result.
type ListMembersPage struct {
	List       ContactList
	Items      []ListContactRow
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     ListMembersFilter
}

func parseVariableSchema(raw sql.NullString) []string {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw.String), &keys); err != nil {
		return nil
	}
	return keys
}

func encodeVariableSchema(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sortedUniqueKeys(keys []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysFromVariables(vars []ContactVariables) []string {
	keys := make([]string, 0, len(vars))
	for _, v := range vars {
		if strings.TrimSpace(v.Key) != "" {
			keys = append(keys, v.Key)
		}
	}
	return sortedUniqueKeys(keys)
}

func keysFromMap(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	return sortedUniqueKeys(keys)
}

// GetListVariableSchema returns the canonical variable keys for a list.
func GetListVariableSchema(listID, userID int64) ([]string, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return nil, err
	}
	var raw sql.NullString
	err := db.QueryRow(`SELECT variable_schema FROM contact_lists WHERE id = ? AND user_id = ?`, listID, userID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return parseVariableSchema(raw), nil
}

func setListVariableSchema(listID, userID int64, keys []string) error {
	encoded, err := encodeVariableSchema(sortedUniqueKeys(keys))
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE contact_lists SET variable_schema = ? WHERE id = ? AND user_id = ?`, encoded, listID, userID)
	return err
}

// SetListVariableSchema updates the list's required variable keys (exported for routes).
func SetListVariableSchema(listID, userID int64, keys []string) error {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return err
	}
	return setListVariableSchema(listID, userID, keys)
}

// ParseSchemaKeysInput splits comma/newline/space-separated variable key names.
func ParseSchemaKeysInput(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, ";", ",")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	return sortedUniqueKeys(parts)
}

// ValidateVariableKeysForList checks contact keys against the list schema.
// Missing keys on the contact are allowed; unknown keys are rejected.
func ValidateVariableKeysForList(listID, userID int64, contactKeys []string) error {
	schema, err := GetListVariableSchema(listID, userID)
	if err != nil {
		return err
	}
	if len(schema) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	for _, k := range schema {
		allowed[k] = true
	}
	var unknown []string
	for _, k := range sortedUniqueKeys(contactKeys) {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("contact variables not allowed in this list: %s (list expects: %s)", strings.Join(unknown, ", "), strings.Join(schema, ", "))
	}
	return nil
}

// ValidateImportKeysForList ensures imported columns fit the list schema.
func ValidateImportKeysForList(listID, userID int64, importKeys []string) error {
	schema, err := GetListVariableSchema(listID, userID)
	if err != nil {
		return err
	}
	if len(schema) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	for _, k := range schema {
		allowed[k] = true
	}
	var unknown []string
	for _, k := range sortedUniqueKeys(importKeys) {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("spreadsheet columns not allowed in this list: %s (list expects: %s)", strings.Join(unknown, ", "), strings.Join(schema, ", "))
	}
	return nil
}

// EnsureListVariableSchema sets schema from the first batch when empty.
func EnsureListVariableSchema(listID, userID int64, keys []string) error {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return err
	}
	existing, err := GetListVariableSchema(listID, userID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ValidateImportKeysForList(listID, userID, keys)
	}
	return setListVariableSchema(listID, userID, keys)
}

func addContactToListValidated(listID, userID, contactID int64) error {
	if _, _, err := GetContactForUser(contactID, userID); err != nil {
		return err
	}
	_, vars, err := GetContact(contactID)
	if err != nil {
		return err
	}
	contactKeys := keysFromVariables(vars)

	schema, err := GetListVariableSchema(listID, userID)
	if err != nil {
		return err
	}
	if len(schema) == 0 {
		if err := setListVariableSchema(listID, userID, contactKeys); err != nil {
			return err
		}
	} else if err := ValidateVariableKeysForList(listID, userID, contactKeys); err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO contact_list_members (list_id, contact_id) VALUES (?, ?)
		ON CONFLICT DO NOTHING
	`, listID, contactID)
	return err
}

// ListContactsInList returns members (no variable columns; use schema helpers for import keys).
func ListContactsInList(listID, userID int64) (list ContactList, rows []ListContactRow, columns []string, err error) {
	page, err := ListContactsInListPage(listID, userID, ListMembersFilter{Page: 1, PageSize: 5000})
	if err != nil {
		return ContactList{}, nil, nil, err
	}
	columns, _ = GetListVariableSchema(listID, userID)
	return page.List, page.Items, columns, nil
}

// ListContactsInListFiltered returns members optionally filtered by email search (unpaginated, legacy).
func ListContactsInListFiltered(listID, userID int64, query string) (list ContactList, rows []ListContactRow, columns []string, err error) {
	page, err := ListContactsInListPage(listID, userID, ListMembersFilter{Query: query, Page: 1, PageSize: 5000})
	if err != nil {
		return ContactList{}, nil, nil, err
	}
	columns, _ = GetListVariableSchema(listID, userID)
	return page.List, page.Items, columns, nil
}

// ListContactsInListPage returns paginated list members. Variable columns from the list
// schema are loaded for the current page when present.
func ListContactsInListPage(listID, userID int64, f ListMembersFilter) (ListMembersPage, error) {
	out := ListMembersPage{Filter: f}
	list, err := GetContactListForUser(listID, userID)
	if err != nil {
		return out, err
	}
	out.List = list

	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	out.Filter = f
	out.Page = f.Page
	out.PageSize = f.PageSize

	where, args, err := listMembersWhere(listID, userID, f)
	if err != nil {
		return out, err
	}
	whereSQL := strings.Join(where, " AND ")

	countQ := `
		SELECT COUNT(*)
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE ` + whereSQL
	if err := db.QueryRow(countQ, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	out.TotalPages = out.Total / f.PageSize
	if out.Total%f.PageSize != 0 || out.TotalPages == 0 && out.Total > 0 {
		out.TotalPages++
	}
	if out.Total == 0 {
		out.TotalPages = 0
	}
	if f.Page > out.TotalPages && out.TotalPages > 0 {
		f.Page = out.TotalPages
		out.Page = f.Page
		out.Filter.Page = f.Page
	}

	order := "LOWER(c.email) ASC"
	if f.Sort == "newest" {
		order = "c.id DESC"
	}
	offset := (f.Page - 1) * f.PageSize
	sqlQuery := `
		SELECT c.id, c.email, c.replied_at,
			EXISTS(SELECT 1 FROM contact_suppressions s WHERE s.contact_id = c.id) AS suppressed
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE ` + whereSQL + `
		ORDER BY ` + order + `
		LIMIT ? OFFSET ?`
	queryArgs := append(append([]interface{}{}, args...), f.PageSize, offset)
	memberRows, err := db.Query(sqlQuery, queryArgs...)
	if err != nil {
		return out, err
	}
	defer memberRows.Close()

	var ids []int64
	for memberRows.Next() {
		var row ListContactRow
		var replied sql.NullTime
		var suppressed bool
		if err := memberRows.Scan(&row.ID, &row.Email, &replied, &suppressed); err != nil {
			return out, err
		}
		if replied.Valid {
			t := replied.Time
			row.RepliedAt = &t
		}
		row.Suppressed = suppressed
		ids = append(ids, row.ID)
		out.Items = append(out.Items, row)
	}
	engMap, _ := EnrichContactsEngagement(userID, ids)
	for i := range out.Items {
		if eng, ok := engMap[out.Items[i].ID]; ok {
			out.Items[i].LastCampaignID = eng.LastCampaignID
			out.Items[i].LastCampaignName = eng.LastCampaignName
			out.Items[i].LastSignal = eng.LastSignal
			out.Items[i].LastActivity = eng.LastActivity
		}
	}
	schema, _ := GetListVariableSchema(listID, userID)
	if len(schema) > 0 && len(ids) > 0 {
		varsByContact, _ := loadContactVariablesMap(ids, schema)
		for i := range out.Items {
			if m, ok := varsByContact[out.Items[i].ID]; ok {
				out.Items[i].Variables = m
			} else {
				out.Items[i].Variables = map[string]string{}
			}
		}
	}
	return out, nil
}

func listMembersWhere(listID, userID int64, f ListMembersFilter) ([]string, []interface{}, error) {
	where := []string{"m.list_id = ?", "c.user_id = ?"}
	args := []interface{}{listID, userID}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "LOWER(c.email) LIKE ?")
		args = append(args, "%"+strings.ToLower(q)+"%")
	}
	if err := ValidateEngagementPreset(f.Engagement); err != nil {
		return nil, nil, err
	}
	if clause, extra := engagementFilterSQL(f.Engagement, 0); clause != "" {
		where = append(where, "("+clause+")")
		args = append(args, extra...)
	}
	varClause, varArgs, err := variableFilterSQL(f.VarKey, f.VarOp, f.VarValue)
	if err != nil {
		return nil, nil, err
	}
	if varClause != "" {
		where = append(where, varClause)
		args = append(args, varArgs...)
	}
	return where, args, nil
}

func variableFilterSQL(key, op, value string) (string, []interface{}, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil, nil
	}
	op = strings.TrimSpace(strings.ToLower(op))
	if op == "" {
		op = "equals"
	}
	switch op {
	case "equals":
		return `EXISTS (
			SELECT 1 FROM contact_variables cv
			WHERE cv.contact_id = c.id AND LOWER(cv.key) = LOWER(?) AND LOWER(COALESCE(cv.value, '')) = LOWER(?)
		)`, []interface{}{key, strings.TrimSpace(value)}, nil
	case "not_equals":
		return `EXISTS (
			SELECT 1 FROM contact_variables cv
			WHERE cv.contact_id = c.id AND LOWER(cv.key) = LOWER(?)
		) AND NOT EXISTS (
			SELECT 1 FROM contact_variables cv
			WHERE cv.contact_id = c.id AND LOWER(cv.key) = LOWER(?) AND LOWER(COALESCE(cv.value, '')) = LOWER(?)
		)`, []interface{}{key, key, strings.TrimSpace(value)}, nil
	case "exists":
		return `EXISTS (
			SELECT 1 FROM contact_variables cv
			WHERE cv.contact_id = c.id AND LOWER(cv.key) = LOWER(?) AND TRIM(COALESCE(cv.value, '')) <> ''
		)`, []interface{}{key}, nil
	case "empty":
		return `NOT EXISTS (
			SELECT 1 FROM contact_variables cv
			WHERE cv.contact_id = c.id AND LOWER(cv.key) = LOWER(?) AND TRIM(COALESCE(cv.value, '')) <> ''
		)`, []interface{}{key}, nil
	default:
		return "", nil, fmt.Errorf("unknown variable filter op %q", op)
	}
}

func loadContactVariablesMap(contactIDs []int64, keys []string) (map[int64]map[string]string, error) {
	out := make(map[int64]map[string]string, len(contactIDs))
	if len(contactIDs) == 0 || len(keys) == 0 {
		return out, nil
	}
	idPH := make([]string, len(contactIDs))
	args := make([]interface{}, 0, len(contactIDs)+len(keys))
	for i, id := range contactIDs {
		idPH[i] = "?"
		args = append(args, id)
	}
	keyPH := make([]string, len(keys))
	for i, k := range keys {
		keyPH[i] = "?"
		args = append(args, k)
	}
	rows, err := db.Query(`
		SELECT contact_id, key, value FROM contact_variables
		WHERE contact_id IN (`+strings.Join(idPH, ",")+`) AND key IN (`+strings.Join(keyPH, ",")+`)
	`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var key, val string
		if rows.Scan(&cid, &key, &val) != nil {
			continue
		}
		if out[cid] == nil {
			out[cid] = map[string]string{}
		}
		out[cid][key] = val
	}
	return out, nil
}

// ListMemberIDsMatching returns contact IDs in a list matching the filter (capped).
func ListMemberIDsMatching(listID, userID int64, f ListMembersFilter, max int) ([]int64, error) {
	if max <= 0 || max > 10000 {
		max = 5000
	}
	f.Page = 1
	f.PageSize = max
	where, args, err := listMembersWhere(listID, userID, f)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT c.id
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY c.id ASC
		LIMIT ?
	`, append(args, max)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ListDistinctVariableValues returns distinct non-empty values for a variable key among list members.
func ListDistinctVariableValues(listID, userID int64, key string, limit int) ([]string, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT DISTINCT cv.value
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		INNER JOIN contact_variables cv ON cv.contact_id = c.id
		WHERE m.list_id = ? AND c.user_id = ? AND LOWER(cv.key) = LOWER(?) AND TRIM(COALESCE(cv.value, '')) <> ''
		ORDER BY cv.value ASC
		LIMIT ?
	`, listID, userID, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

// ListEngagementCounts returns quick stats for list header chips.
func ListEngagementCounts(listID, userID int64) (total, interested, replied int, err error) {
	list, err := GetContactListForUser(listID, userID)
	if err != nil {
		return 0, 0, 0, err
	}
	total = list.MemberCount
	interestedIDs, err := ListMemberIDsMatching(listID, userID, ListMembersFilter{Engagement: "interested"}, 10000)
	if err != nil {
		return total, 0, 0, err
	}
	repliedIDs, err := ListMemberIDsMatching(listID, userID, ListMembersFilter{Engagement: "replied"}, 10000)
	if err != nil {
		return total, len(interestedIDs), 0, err
	}
	return total, len(interestedIDs), len(repliedIDs), nil
}

// RemoveContactsFromList removes many members from a list (does not delete contacts).
func RemoveContactsFromList(listID, userID int64, contactIDs []int64) (int, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return 0, err
	}
	n := 0
	for _, cid := range contactIDs {
		if cid <= 0 {
			continue
		}
		if err := RemoveContactFromList(listID, userID, cid); err == nil {
			n++
		}
	}
	return n, nil
}

// ListVariableSample returns schema keys and sample values from the first list member.
func ListVariableSample(listID, userID int64) (keys []string, sample map[string]string, err error) {
	keys, err = GetListVariableSchema(listID, userID)
	if err != nil {
		return nil, nil, err
	}
	sample = map[string]string{}
	ids, err := ListMemberContactIDs(listID, userID)
	if err != nil || len(ids) == 0 {
		return keys, sample, err
	}
	c, vars, err := GetContact(ids[0])
	if err != nil {
		return keys, sample, err
	}
	if c.Email != "" {
		sample["email"] = c.Email
	}
	for _, v := range vars {
		sample[v.Key] = v.Value
	}
	if len(keys) == 0 {
		keys = keysFromVariables(vars)
	}
	if _, ok := sample["email"]; ok {
		keys = appendUniqueKey(keys, "email")
	}
	return keys, sample, nil
}

func appendUniqueKey(keys []string, key string) []string {
	for _, k := range keys {
		if k == key {
			return keys
		}
	}
	return append(keys, key)
}

// ContactVariableSample returns variables for template preview.
func ContactVariableSample(userID, contactID int64) (map[string]string, error) {
	c, vars, err := GetContactForUser(contactID, userID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if c.Email != "" {
		out["email"] = c.Email
	}
	for _, v := range vars {
		out[v.Key] = v.Value
	}
	return out, nil
}
