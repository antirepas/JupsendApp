package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

// UpdateSheetCell updates email or one variable for a contact that belongs to the list.
func UpdateSheetCell(userID, listID, contactID int64, column, value string) error {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE m.list_id = ? AND m.contact_id = ? AND c.user_id = ?
	`, listID, contactID, userID).Scan(&n)
	if err != nil || n == 0 {
		return fmt.Errorf("contact not in sheet")
	}

	column = NormalizeVariableKey(column)
	value = strings.TrimSpace(value)
	if column == "" || column == "email" {
		email := strings.ToLower(value)
		if email == "" {
			return fmt.Errorf("email required")
		}
		_, err := db.Exec(`UPDATE contact SET email = ? WHERE id = ? AND user_id = ?`, email, contactID, userID)
		return err
	}

	// Drop any casing/BOM variants of this key before insert.
	_, err = db.Exec(`
		DELETE FROM contact_variables
		WHERE contact_id = ?
		  AND LOWER(TRIM(BOTH FROM REPLACE(key, E'\uFEFF', ''))) = ?
	`, contactID, column)
	if err != nil {
		return err
	}
	if value == "" {
		return nil
	}
	_, err = db.Exec(
		`INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`,
		column, value, contactID,
	)
	if err != nil {
		return err
	}
	// Ensure column is in schema
	schema, _ := GetListVariableSchema(listID, userID)
	found := false
	for _, k := range schema {
		if NormalizeVariableKey(k) == column {
			found = true
			break
		}
	}
	if !found {
		schema = append(schema, column)
		_ = SetListVariableSchema(listID, userID, schema)
	}
	return nil
}

// AddSheetRow adds a contact (optionally with email) to the list.
func AddSheetRow(userID, listID int64, email string) (int64, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return 0, err
	}
	if email == "" {
		email = fmt.Sprintf("new+%d@placeholder.local", time.Now().UnixNano())
	}
	id, err := FindOrCreateContact(userID, email, nil)
	if err != nil {
		return 0, err
	}
	if err := AddContactsToList(listID, userID, []int64{id}); err != nil {
		return 0, err
	}
	return id, nil
}

// PasteSheetRows upserts contacts from a TSV-like matrix. headers[0] should be email when present.
func PasteSheetRows(userID, listID int64, headers []string, rows [][]string) (int, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return 0, err
	}
	if len(headers) == 0 {
		headers = []string{"email"}
	}
	emailIdx := 0
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), "email") {
			emailIdx = i
			break
		}
	}
	var schemaKeys []string
	for i, h := range headers {
		h = NormalizeVariableKey(h)
		if h == "" || i == emailIdx || h == "email" {
			continue
		}
		schemaKeys = append(schemaKeys, h)
	}
	if len(schemaKeys) > 0 {
		existing, _ := GetListVariableSchema(listID, userID)
		_ = SetListVariableSchema(listID, userID, append(existing, schemaKeys...))
	}

	n := 0
	var ids []int64
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		email := ""
		if emailIdx < len(row) {
			email = strings.ToLower(strings.TrimSpace(row[emailIdx]))
		}
		if email == "" || !strings.Contains(email, "@") {
			continue
		}
		var vars []ContactVariables
		for i, h := range headers {
			h = NormalizeVariableKey(h)
			if h == "" || i == emailIdx || h == "email" {
				continue
			}
			val := ""
			if i < len(row) {
				val = strings.TrimSpace(row[i])
			}
			if val != "" {
				vars = append(vars, ContactVariables{Key: h, Value: val})
			}
		}
		id, err := FindOrCreateContact(userID, email, vars)
		if err != nil {
			continue
		}
		if len(vars) > 0 {
			// Merge vars onto existing contact
			c, existingVars, err := GetContactForUser(id, userID)
			if err == nil {
				merged := map[string]string{}
				for _, v := range existingVars {
					if k := NormalizeVariableKey(v.Key); k != "" {
						merged[k] = v.Value
					}
				}
				for _, v := range vars {
					if k := NormalizeVariableKey(v.Key); k != "" {
						merged[k] = v.Value
					}
				}
				var list []ContactVariables
				for k, v := range merged {
					list = append(list, ContactVariables{Key: k, Value: v})
				}
				_ = UpdateContact(c.ID, userID, email, list)
			}
		}
		ids = append(ids, id)
		n++
	}
	_ = AddContactsToList(listID, userID, ids)
	return n, nil
}
