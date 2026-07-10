package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"emailtracker.com/db"
)

// ListContactRow is a contact with variables for list detail views.
type ListContactRow struct {
	ID        int64
	Email     string
	Variables map[string]string
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

// ListContactsInList returns members and the column keys to display.
func ListContactsInList(listID, userID int64) (list ContactList, rows []ListContactRow, columns []string, err error) {
	list, err = GetContactListForUser(listID, userID)
	if err != nil {
		return list, nil, nil, err
	}
	columns, err = GetListVariableSchema(listID, userID)
	if err != nil {
		return list, nil, nil, err
	}

	memberRows, err := db.Query(`
		SELECT c.id, c.email
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE m.list_id = ? AND c.user_id = ?
		ORDER BY c.email ASC
	`, listID, userID)
	if err != nil {
		return list, nil, nil, err
	}
	defer memberRows.Close()

	colSet := map[string]bool{}
	for _, k := range columns {
		colSet[k] = true
	}

	for memberRows.Next() {
		var row ListContactRow
		if err := memberRows.Scan(&row.ID, &row.Email); err != nil {
			return list, nil, nil, err
		}
		_, vars, err := GetContact(row.ID)
		if err != nil {
			return list, nil, nil, err
		}
		row.Variables = map[string]string{}
		for _, v := range vars {
			row.Variables[v.Key] = v.Value
			if len(columns) == 0 && v.Key != "" {
				colSet[v.Key] = true
			}
		}
		rows = append(rows, row)
	}
	if len(columns) == 0 {
		for k := range colSet {
			columns = append(columns, k)
		}
		sort.Strings(columns)
	}
	return list, rows, columns, nil
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
