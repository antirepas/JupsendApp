package model

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

type TemplateFolder struct {
	ID            int64
	UserID        int64
	Name          string
	CreatedAt     time.Time
	TemplateCount int
}

func ListTemplateFolders(userID int64) ([]TemplateFolder, error) {
	rows, err := db.Query(`
		SELECT f.id, f.user_id, f.name, f.created_at,
			(SELECT COUNT(*) FROM template t WHERE t.folder_id = f.id AND t.user_id = f.user_id)
		FROM template_folders f
		WHERE f.user_id = ?
		ORDER BY lower(f.name) ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []TemplateFolder
	for rows.Next() {
		var item TemplateFolder
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.TemplateCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func GetTemplateFolderForUser(folderID, userID int64) (TemplateFolder, error) {
	row := db.QueryRow(`
		SELECT f.id, f.user_id, f.name, f.created_at,
			(SELECT COUNT(*) FROM template t WHERE t.folder_id = f.id AND t.user_id = f.user_id)
		FROM template_folders f
		WHERE f.id = ? AND f.user_id = ?
	`, folderID, userID)
	var item TemplateFolder
	err := row.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.TemplateCount)
	if err != nil {
		return TemplateFolder{}, err
	}
	return item, nil
}

func CreateTemplateFolder(userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("folder name required")
	}
	if len(name) > 120 {
		name = name[:120]
	}
	row := db.QueryRow(`
		INSERT INTO template_folders (user_id, name) VALUES (?, ?) RETURNING id
	`, userID, name)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return 0, fmt.Errorf("a folder with that name already exists")
		}
		return 0, err
	}
	return id, nil
}

func RenameTemplateFolder(folderID, userID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("folder name required")
	}
	if len(name) > 120 {
		name = name[:120]
	}
	if _, err := GetTemplateFolderForUser(folderID, userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE template_folders SET name = ? WHERE id = ? AND user_id = ?`, name, folderID, userID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return fmt.Errorf("a folder with that name already exists")
		}
		return err
	}
	return nil
}

func DeleteTemplateFolder(folderID, userID int64) error {
	if _, err := GetTemplateFolderForUser(folderID, userID); err != nil {
		return err
	}
	// ON DELETE SET NULL on template.folder_id unfiles templates.
	res, err := db.Exec(`DELETE FROM template_folders WHERE id = ? AND user_id = ?`, folderID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func CountTemplatesForUser(userID int64) (all, unfiled int, err error) {
	err = db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ?`, userID).Scan(&all)
	if err != nil {
		return 0, 0, err
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ? AND folder_id IS NULL`, userID).Scan(&unfiled)
	return all, unfiled, err
}

func assertTemplateFolderOwned(folderID, userID int64) error {
	if folderID <= 0 {
		return nil
	}
	_, err := GetTemplateFolderForUser(folderID, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("folder not found")
		}
		return err
	}
	return nil
}
