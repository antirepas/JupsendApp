package model

import (
	"log"

	"emailtracker.com/db"
)

type Template struct {
	ID      int64
	Name    string
	Subject string
	Body    string
}

type TemplateVariable struct {
	TemplateID int64
	Key        string
}

type TemplateListItem struct {
	ID        int64
	Name      string
	Subject   string
	Variables []string
}

func (t *Template) SaveTemplate(variables []TemplateVariable) (int64, error) {
	query := `INSERT INTO template (name, subject, body) VALUES (?,?,?) RETURNING id`

	row := db.DB.QueryRow(query, t.Name, t.Subject, t.Body)
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
		_, err = db.DB.Exec(
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
	query := `SELECT id, name, subject, body FROM template WHERE id = ?`
	row := db.DB.QueryRow(query, templateId)
	var t Template
	err := row.Scan(&t.ID, &t.Name, &t.Subject, &t.Body)
	if err != nil {
		return Template{}, err
	}
	return t, nil
}

func GetTemplateByID(id int64) (Template, []string, error) {
	t, err := GetTemplate(id)
	if err != nil {
		return Template{}, nil, err
	}

	rows, err := db.DB.Query(`SELECT key FROM template_variables WHERE template_id = ?`, id)
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

func ListTemplates() ([]TemplateListItem, error) {
	query := `SELECT id, name, subject FROM template ORDER BY id DESC`
	rows, err := db.DB.Query(query)
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

		varRows, err := db.DB.Query(`SELECT key FROM template_variables WHERE template_id = ?`, item.ID)
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

func UpdateTemplate(id int64, name, subject, body string, variables []string) error {
	_, err := db.DB.Exec(
		`UPDATE template SET name = ?, subject = ?, body = ? WHERE id = ?`,
		name, subject, body, id,
	)
	if err != nil {
		return err
	}

	_, err = db.DB.Exec(`DELETE FROM template_variables WHERE template_id = ?`, id)
	if err != nil {
		return err
	}

	for _, key := range variables {
		if key == "" {
			continue
		}
		_, err = db.DB.Exec(
			`INSERT INTO template_variables (template_id, key) VALUES (?, ?)`,
			id, key,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func DeleteTemplate(id int64) error {
	_, err := db.DB.Exec(`DELETE FROM template WHERE id = ?`, id)
	return err
}
