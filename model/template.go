package model

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"emailtracker.com/db"
)

type Template struct {
	ID       int64
	UserID   int64
	Name     string
	Subject  string
	Body     string
	FolderID int64 // 0 = unfiled
}

type TemplateVariable struct {
	TemplateID int64
	Key        string
}

type TemplateListItem struct {
	ID         int64
	Name       string
	Subject    string
	Variables  []string
	FolderID   int64
	FolderName string
}

func (t *Template) SaveTemplate(userID int64, variables []TemplateVariable) (int64, error) {
	if err := assertTemplateFolderOwned(t.FolderID, userID); err != nil {
		return 0, err
	}
	var folderArg interface{}
	if t.FolderID > 0 {
		folderArg = t.FolderID
	}
	query := `INSERT INTO template (name, subject, body, user_id, folder_id) VALUES (?,?,?,?,?) RETURNING id`

	row := db.QueryRow(query, t.Name, t.Subject, t.Body, userID, folderArg)
	var tID int64
	err := row.Scan(&tID)
	if err != nil {
		log.Print(err)
		return 0, err
	}

	for _, v := range variables {
		if v.Key == "" {
			continue
		}
		_, err = db.Exec(
			`INSERT INTO template_variables (template_id, key) VALUES (?, ?)`,
			tID, v.Key,
		)
		if err != nil {
			return 0, err
		}
	}
	return tID, nil
}

func GetTemplate(templateId int64) (Template, error) {
	query := `SELECT id, COALESCE(user_id, 0), name, subject, body, COALESCE(folder_id, 0) FROM template WHERE id = ?`
	row := db.QueryRow(query, templateId)
	var t Template
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Subject, &t.Body, &t.FolderID)
	if err != nil {
		return Template{}, err
	}
	return t, nil
}

func GetTemplateForUser(templateId, userID int64) (Template, error) {
	t, err := GetTemplate(templateId)
	if err != nil {
		return Template{}, err
	}
	if userID > 0 && t.UserID > 0 && t.UserID != userID {
		return Template{}, errNotFound
	}
	return t, nil
}

func GetTemplateByID(id, userID int64) (Template, []string, error) {
	t, err := GetTemplateForUser(id, userID)
	if err != nil {
		return Template{}, nil, err
	}

	rows, err := db.Query(`SELECT key FROM template_variables WHERE template_id = ?`, id)
	if err != nil {
		return Template{}, nil, err
	}
	defer rows.Close()

	var vars []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return Template{}, nil, err
		}
		vars = append(vars, key)
	}
	return t, vars, nil
}

// ListTemplates returns all templates for the user (newest first).
func ListTemplates(userID int64) ([]TemplateListItem, error) {
	return ListTemplatesFiltered(userID, "all")
}

// ListTemplatesFiltered filters by folder: "all", "unfiled", or a folder id string.
func ListTemplatesFiltered(userID int64, folderFilter string) ([]TemplateListItem, error) {
	folderFilter = strings.TrimSpace(strings.ToLower(folderFilter))
	if folderFilter == "" {
		folderFilter = "all"
	}

	query := `
		SELECT t.id, t.name, t.subject, COALESCE(t.folder_id, 0), COALESCE(f.name, '')
		FROM template t
		LEFT JOIN template_folders f ON f.id = t.folder_id
		WHERE t.user_id = ?`
	args := []interface{}{userID}
	switch {
	case folderFilter == "all":
		// no extra filter
	case folderFilter == "unfiled":
		query += ` AND t.folder_id IS NULL`
	default:
		fid, err := strconv.ParseInt(folderFilter, 10, 64)
		if err != nil || fid <= 0 {
			return nil, fmt.Errorf("invalid folder")
		}
		if err := assertTemplateFolderOwned(fid, userID); err != nil {
			return nil, err
		}
		query += ` AND t.folder_id = ?`
		args = append(args, fid)
	}
	query += ` ORDER BY t.id DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TemplateListItem
	for rows.Next() {
		var item TemplateListItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Subject, &item.FolderID, &item.FolderName); err != nil {
			return nil, err
		}

		varRows, err := db.Query(`SELECT key FROM template_variables WHERE template_id = ?`, item.ID)
		if err != nil {
			return nil, err
		}
		for varRows.Next() {
			var key string
			if err := varRows.Scan(&key); err != nil {
				varRows.Close()
				return nil, err
			}
			item.Variables = append(item.Variables, key)
		}
		varRows.Close()
		items = append(items, item)
	}
	return items, nil
}

// ListTemplatePickerItems returns templates for dropdowns without loading variables (fast).
func ListTemplatePickerItems(userID int64) ([]TemplateListItem, error) {
	rows, err := db.Query(`SELECT id, name, subject FROM template WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TemplateListItem
	for rows.Next() {
		var item TemplateListItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Subject); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// TemplateNameMapForUser loads id → name for all user templates in one query.
func TemplateNameMapForUser(userID int64) (map[int64]string, error) {
	rows, err := db.Query(`SELECT id, name FROM template WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, nil
}

func FirstTemplateIDForUser(userID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM template WHERE user_id = ? ORDER BY id ASC LIMIT 1`, userID).Scan(&id)
	return id, err
}

func UpdateTemplate(id, userID int64, name, subject, body string, variables []string, folderID int64) error {
	if _, err := GetTemplateForUser(id, userID); err != nil {
		return err
	}
	if err := assertTemplateFolderOwned(folderID, userID); err != nil {
		return err
	}
	var folderArg interface{}
	if folderID > 0 {
		folderArg = folderID
	}
	_, err := db.Exec(
		`UPDATE template SET name = ?, subject = ?, body = ?, folder_id = ? WHERE id = ?`,
		name, subject, body, folderArg, id,
	)
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM template_variables WHERE template_id = ?`, id)
	if err != nil {
		return err
	}

	for _, key := range variables {
		if key == "" {
			continue
		}
		_, err = db.Exec(
			`INSERT INTO template_variables (template_id, key) VALUES (?, ?)`,
			id, key,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func DeleteTemplate(id, userID int64) error {
	if _, err := GetTemplateForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM template WHERE id = ?`, id)
	return err
}

func DuplicateTemplate(userID, sourceID int64, newName string) (int64, error) {
	t, varKeys, err := GetTemplateByID(sourceID, userID)
	if err != nil {
		return 0, err
	}
	copy := Template{Name: newName, Subject: t.Subject, Body: t.Body, FolderID: t.FolderID}
	vars := make([]TemplateVariable, len(varKeys))
	for i, k := range varKeys {
		vars[i] = TemplateVariable{Key: k}
	}
	return copy.SaveTemplate(userID, vars)
}

// ParseFolderIDForm reads folder_id from a form value (empty/0 = unfiled).
func ParseFolderIDForm(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}
