package model

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

// LibraryFolder is a user-owned flat folder in the unified library.
type LibraryFolder struct {
	ID            int64
	UserID        int64
	Name          string
	CreatedAt     time.Time
	ItemCount     int
	TemplateCount int // same as ItemCount; kept for older templates/tests
}

// TemplateFolder is an alias kept for older call sites/tests.
type TemplateFolder = LibraryFolder

func ListLibraryFolders(userID int64) ([]LibraryFolder, error) {
	rows, err := db.Query(`
		SELECT f.id, f.user_id, f.name, f.created_at,
			(
				(SELECT COUNT(*) FROM template t WHERE t.folder_id = f.id AND t.user_id = f.user_id) +
				(SELECT COUNT(*) FROM workflows w WHERE w.folder_id = f.id AND w.tenant_id = f.user_id AND w.status = 'active') +
				(SELECT COUNT(*) FROM contact_lists cl WHERE cl.folder_id = f.id AND cl.user_id = f.user_id)
			)
		FROM library_folders f
		WHERE f.user_id = ?
		ORDER BY lower(f.name) ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []LibraryFolder
	for rows.Next() {
		var item LibraryFolder
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.ItemCount); err != nil {
			return nil, err
		}
		item.TemplateCount = item.ItemCount
		items = append(items, item)
	}
	return items, nil
}

func ListTemplateFolders(userID int64) ([]TemplateFolder, error) {
	return ListLibraryFolders(userID)
}

func GetLibraryFolderForUser(folderID, userID int64) (LibraryFolder, error) {
	row := db.QueryRow(`
		SELECT f.id, f.user_id, f.name, f.created_at,
			(
				(SELECT COUNT(*) FROM template t WHERE t.folder_id = f.id AND t.user_id = f.user_id) +
				(SELECT COUNT(*) FROM workflows w WHERE w.folder_id = f.id AND w.tenant_id = f.user_id AND w.status = 'active') +
				(SELECT COUNT(*) FROM contact_lists cl WHERE cl.folder_id = f.id AND cl.user_id = f.user_id)
			)
		FROM library_folders f
		WHERE f.id = ? AND f.user_id = ?
	`, folderID, userID)
	var item LibraryFolder
	err := row.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.ItemCount)
	if err != nil {
		return LibraryFolder{}, err
	}
	item.TemplateCount = item.ItemCount
	return item, nil
}

func GetTemplateFolderForUser(folderID, userID int64) (TemplateFolder, error) {
	return GetLibraryFolderForUser(folderID, userID)
}

func CreateLibraryFolder(userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("folder name required")
	}
	if len(name) > 120 {
		name = name[:120]
	}
	row := db.QueryRow(`
		INSERT INTO library_folders (user_id, name) VALUES (?, ?) RETURNING id
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

func CreateTemplateFolder(userID int64, name string) (int64, error) {
	return CreateLibraryFolder(userID, name)
}

func RenameLibraryFolder(folderID, userID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("folder name required")
	}
	if len(name) > 120 {
		name = name[:120]
	}
	if _, err := GetLibraryFolderForUser(folderID, userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE library_folders SET name = ? WHERE id = ? AND user_id = ?`, name, folderID, userID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return fmt.Errorf("a folder with that name already exists")
		}
		return err
	}
	return nil
}

func RenameTemplateFolder(folderID, userID int64, name string) error {
	return RenameLibraryFolder(folderID, userID, name)
}

func DeleteLibraryFolder(folderID, userID int64) error {
	if _, err := GetLibraryFolderForUser(folderID, userID); err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM library_folders WHERE id = ? AND user_id = ?`, folderID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func DeleteTemplateFolder(folderID, userID int64) error {
	return DeleteLibraryFolder(folderID, userID)
}

func CountLibraryItemsForUser(userID int64) (all, unfiled int, err error) {
	err = db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM template WHERE user_id = ?) +
			(SELECT COUNT(*) FROM workflows WHERE tenant_id = ? AND status = 'active') +
			(SELECT COUNT(*) FROM contact_lists WHERE user_id = ?)
	`, userID, userID, userID).Scan(&all)
	if err != nil {
		return 0, 0, err
	}
	err = db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM template WHERE user_id = ? AND folder_id IS NULL) +
			(SELECT COUNT(*) FROM workflows WHERE tenant_id = ? AND status = 'active' AND folder_id IS NULL) +
			(SELECT COUNT(*) FROM contact_lists WHERE user_id = ? AND folder_id IS NULL)
	`, userID, userID, userID).Scan(&unfiled)
	return all, unfiled, err
}

func CountTemplatesForUser(userID int64) (all, unfiled int, err error) {
	return CountLibraryItemsForUser(userID)
}

func assertLibraryFolderOwned(folderID, userID int64) error {
	if folderID <= 0 {
		return nil
	}
	_, err := GetLibraryFolderForUser(folderID, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("folder not found")
		}
		return err
	}
	return nil
}

func assertTemplateFolderOwned(folderID, userID int64) error {
	return assertLibraryFolderOwned(folderID, userID)
}

// MoveTemplatesToFolder sets folder_id for owned templates. folderID 0 = Unfiled.
func MoveTemplatesToFolder(userID int64, ids []int64, folderID int64) (int, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	var folderArg interface{}
	if folderID > 0 {
		folderArg = folderID
	}
	n := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, err := GetTemplateForUser(id, userID); err != nil {
			continue
		}
		if _, err := db.Exec(`UPDATE template SET folder_id = ? WHERE id = ? AND user_id = ?`, folderArg, id, userID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// CopyTemplatesToFolder duplicates templates into folderID (0 = Unfiled).
func CopyTemplatesToFolder(userID int64, ids []int64, folderID int64) (int, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		t, varKeys, err := GetTemplateByID(id, userID)
		if err != nil {
			continue
		}
		copy := Template{
			Name:     uniqueTemplateCopyName(userID, t.Name),
			Subject:  t.Subject,
			Body:     t.Body,
			FolderID: folderID,
		}
		vars := make([]TemplateVariable, len(varKeys))
		for i, k := range varKeys {
			vars[i] = TemplateVariable{Key: k}
		}
		if _, err := copy.SaveTemplate(userID, vars); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func RenameTemplate(id, userID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	if len(name) > 200 {
		name = name[:200]
	}
	if _, err := GetTemplateForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE template SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	return err
}

func DeleteTemplates(userID int64, ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if err := DeleteTemplate(id, userID); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func uniqueTemplateCopyName(userID int64, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Template"
	}
	candidate := base + " (copy)"
	for i := 2; i < 100; i++ {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ? AND name = ?`, userID, candidate).Scan(&n)
		if n == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (copy %d)", base, i)
	}
	return fmt.Sprintf("%s (copy %d)", base, time.Now().Unix()%10000)
}

func uniqueCopyName(table, nameCol, userCol string, userID int64, base, fallback string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = fallback
	}
	candidate := base + " (copy)"
	for i := 2; i < 100; i++ {
		var n int
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = ? AND %s = ?`, table, userCol, nameCol)
		_ = db.QueryRow(q, userID, candidate).Scan(&n)
		if n == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (copy %d)", base, i)
	}
	return fmt.Sprintf("%s (copy %d)", base, time.Now().Unix()%10000)
}
