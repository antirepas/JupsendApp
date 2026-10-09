package model

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/db"
)

const (
	LibraryKindTemplate = "template"
	LibraryKindWorkflow = "workflow"
	LibraryKindContacts = "contacts"
)

// LibraryItem is one row in the unified library file manager.
type LibraryItem struct {
	Kind       string // template | workflow | contacts
	ID         int64
	Name       string
	Subtitle   string // subject / status / "N contacts"
	FolderID   int64
	FolderName string
	UpdatedAt  time.Time
}

type LibraryRef struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

// ListLibraryItems returns mixed library rows filtered by folder (all|unfiled|:id).
func ListLibraryItems(userID int64, folderFilter string) ([]LibraryItem, error) {
	folderFilter = strings.TrimSpace(strings.ToLower(folderFilter))
	if folderFilter == "" {
		folderFilter = "all"
	}

	var folderClauseT, folderClauseW, folderClauseC string
	argsT := []interface{}{userID}
	argsW := []interface{}{userID}
	argsC := []interface{}{userID}

	switch {
	case folderFilter == "all":
		// no filter
	case folderFilter == "unfiled":
		folderClauseT = ` AND t.folder_id IS NULL`
		folderClauseW = ` AND w.folder_id IS NULL`
		folderClauseC = ` AND cl.folder_id IS NULL`
	default:
		fid, err := strconv.ParseInt(folderFilter, 10, 64)
		if err != nil || fid <= 0 {
			return nil, fmt.Errorf("invalid folder")
		}
		if err := assertLibraryFolderOwned(fid, userID); err != nil {
			return nil, err
		}
		folderClauseT = ` AND t.folder_id = ?`
		folderClauseW = ` AND w.folder_id = ?`
		folderClauseC = ` AND cl.folder_id = ?`
		argsT = append(argsT, fid)
		argsW = append(argsW, fid)
		argsC = append(argsC, fid)
	}

	var items []LibraryItem

	campaignFolderView := folderFilter != "all" && folderFilter != "unfiled"

	tRows, err := db.Query(`
		SELECT t.id, t.name, COALESCE(t.subject, ''), COALESCE(t.folder_id, 0), COALESCE(f.name, ''), t.id
		FROM template t
		LEFT JOIN library_folders f ON f.id = t.folder_id
		WHERE t.user_id = ?`+folderClauseT+`
		ORDER BY t.id ASC
	`, argsT...)
	if err != nil {
		return nil, err
	}
	for tRows.Next() {
		var it LibraryItem
		var dummy int64
		if err := tRows.Scan(&it.ID, &it.Name, &it.Subtitle, &it.FolderID, &it.FolderName, &dummy); err != nil {
			tRows.Close()
			return nil, err
		}
		it.Kind = LibraryKindTemplate
		it.UpdatedAt = time.Unix(it.ID, 0) // stable-ish sort key fallback
		items = append(items, it)
	}
	tRows.Close()

	wRows, err := db.Query(`
		SELECT w.id, w.name, w.status, COALESCE(w.folder_id, 0), COALESCE(f.name, ''), w.created_at
		FROM workflows w
		LEFT JOIN library_folders f ON f.id = w.folder_id
		WHERE w.tenant_id = ? AND w.status = 'active'`+folderClauseW+`
		ORDER BY w.created_at ASC, w.id ASC
	`, argsW...)
	if err != nil {
		return nil, err
	}
	for wRows.Next() {
		var it LibraryItem
		if err := wRows.Scan(&it.ID, &it.Name, &it.Subtitle, &it.FolderID, &it.FolderName, &it.UpdatedAt); err != nil {
			wRows.Close()
			return nil, err
		}
		it.Kind = LibraryKindWorkflow
		items = append(items, it)
	}
	wRows.Close()

	cRows, err := db.Query(`
		SELECT cl.id, cl.name,
			(SELECT COUNT(*) FROM contact_list_members m WHERE m.list_id = cl.id),
			COALESCE(cl.folder_id, 0), COALESCE(f.name, ''), cl.created_at
		FROM contact_lists cl
		LEFT JOIN library_folders f ON f.id = cl.folder_id
		WHERE cl.user_id = ?`+folderClauseC+`
		ORDER BY cl.created_at ASC, cl.id ASC
	`, argsC...)
	if err != nil {
		return nil, err
	}
	for cRows.Next() {
		var it LibraryItem
		var members int
		if err := cRows.Scan(&it.ID, &it.Name, &members, &it.FolderID, &it.FolderName, &it.UpdatedAt); err != nil {
			cRows.Close()
			return nil, err
		}
		it.Kind = LibraryKindContacts
		it.Subtitle = fmt.Sprintf("%d contact%s", members, map[bool]string{true: "", false: "s"}[members == 1])
		items = append(items, it)
	}
	cRows.Close()

	if campaignFolderView {
		// Campaign workspace order: templates → contact sheets → workflows (oldest first).
		kindRank := map[string]int{
			LibraryKindTemplate: 0,
			LibraryKindContacts: 1,
			LibraryKindWorkflow: 2,
		}
		sort.SliceStable(items, func(i, j int) bool {
			ri, rj := kindRank[items[i].Kind], kindRank[items[j].Kind]
			if ri != rj {
				return ri < rj
			}
			if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				return items[i].UpdatedAt.Before(items[j].UpdatedAt)
			}
			return items[i].ID < items[j].ID
		})
	} else {
		sort.SliceStable(items, func(i, j int) bool {
			if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				return items[i].UpdatedAt.After(items[j].UpdatedAt)
			}
			if items[i].Kind != items[j].Kind {
				return items[i].Kind < items[j].Kind
			}
			return items[i].ID > items[j].ID
		})
	}
	return items, nil
}

func MoveLibraryItems(userID int64, refs []LibraryRef, folderID int64) (int, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	var folderArg interface{}
	if folderID > 0 {
		folderArg = folderID
	}
	n := 0
	for _, ref := range refs {
		switch strings.ToLower(ref.Kind) {
		case LibraryKindTemplate:
			if _, err := GetTemplateForUser(ref.ID, userID); err != nil {
				continue
			}
			if _, err := db.Exec(`UPDATE template SET folder_id = ? WHERE id = ? AND user_id = ?`, folderArg, ref.ID, userID); err != nil {
				return n, err
			}
			n++
		case LibraryKindWorkflow:
			if _, err := GetWorkflowForUser(ref.ID, userID); err != nil {
				continue
			}
			if _, err := db.Exec(`UPDATE workflows SET folder_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND tenant_id = ?`, folderArg, ref.ID, userID); err != nil {
				return n, err
			}
			n++
		case LibraryKindContacts:
			if _, err := GetContactListForUser(ref.ID, userID); err != nil {
				continue
			}
			if _, err := db.Exec(`UPDATE contact_lists SET folder_id = ? WHERE id = ? AND user_id = ?`, folderArg, ref.ID, userID); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func CopyLibraryItems(userID int64, refs []LibraryRef, folderID int64) (int, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	n := 0
	for _, ref := range refs {
		switch strings.ToLower(ref.Kind) {
		case LibraryKindTemplate:
			c, err := CopyTemplatesToFolder(userID, []int64{ref.ID}, folderID)
			if err != nil {
				return n, err
			}
			n += c
		case LibraryKindWorkflow:
			if _, err := DuplicateWorkflowToFolder(userID, ref.ID, folderID); err != nil {
				continue
			}
			n++
		case LibraryKindContacts:
			if _, err := DuplicateContactListToFolder(userID, ref.ID, folderID); err != nil {
				continue
			}
			n++
		}
	}
	return n, nil
}

func RenameLibraryItem(userID int64, kind string, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	switch strings.ToLower(kind) {
	case LibraryKindTemplate:
		return RenameTemplate(id, userID, name)
	case LibraryKindWorkflow:
		if _, err := GetWorkflowForUser(id, userID); err != nil {
			return err
		}
		if len(name) > 200 {
			name = name[:200]
		}
		_, err := db.Exec(`UPDATE workflows SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND tenant_id = ?`, name, id, userID)
		return err
	case LibraryKindContacts:
		return RenameContactList(id, userID, name)
	case "folder":
		return RenameLibraryFolder(id, userID, name)
	default:
		return fmt.Errorf("invalid kind")
	}
}

func DeleteLibraryItems(userID int64, refs []LibraryRef) (int, error) {
	n := 0
	for _, ref := range refs {
		switch strings.ToLower(ref.Kind) {
		case LibraryKindTemplate:
			if err := DeleteTemplate(ref.ID, userID); err != nil {
				continue
			}
			n++
		case LibraryKindWorkflow:
			if err := ArchiveWorkflow(ref.ID, userID, false); err != nil {
				continue
			}
			n++
		case LibraryKindContacts:
			if err := DeleteContactList(ref.ID, userID); err != nil {
				continue
			}
			n++
		}
	}
	return n, nil
}

func DuplicateWorkflowToFolder(userID, sourceID, folderID int64) (int64, error) {
	src, err := GetWorkflowForUser(sourceID, userID)
	if err != nil {
		return 0, err
	}
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	newName := uniqueCopyName("workflows", "name", "tenant_id", userID, src.Name, "Workflow")
	newID, err := CreateWorkflow(userID, newName, src.Description)
	if err != nil {
		return 0, err
	}
	var folderArg interface{}
	if folderID > 0 {
		folderArg = folderID
	}
	_, _ = db.Exec(`UPDATE workflows SET folder_id = ? WHERE id = ?`, folderArg, newID)

	dst, err := GetWorkflowForUser(newID, userID)
	if err != nil {
		return newID, err
	}
	if src.CurrentVersionID > 0 && dst.CurrentVersionID > 0 {
		_ = CopyWorkflowGraph(src.CurrentVersionID, dst.CurrentVersionID)
	}
	return newID, nil
}

func DuplicateContactListToFolder(userID, sourceID, folderID int64) (int64, error) {
	src, err := GetContactListForUser(sourceID, userID)
	if err != nil {
		return 0, err
	}
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	newName := uniqueCopyName("contact_lists", "name", "user_id", userID, src.Name, "Contacts")
	newID, err := CreateContactList(userID, newName)
	if err != nil {
		return 0, err
	}
	var folderArg interface{}
	if folderID > 0 {
		folderArg = folderID
	}
	_, _ = db.Exec(`UPDATE contact_lists SET folder_id = ? WHERE id = ?`, folderArg, newID)

	if schema, err := GetListVariableSchema(sourceID, userID); err == nil && len(schema) > 0 {
		_ = SetListVariableSchema(newID, userID, schema)
	}

	rows, err := db.Query(`SELECT contact_id FROM contact_list_members WHERE list_id = ?`, sourceID)
	if err != nil {
		return newID, nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var cid int64
		if rows.Scan(&cid) == nil {
			ids = append(ids, cid)
		}
	}
	_ = AddContactsToList(newID, userID, ids)
	return newID, nil
}

// CreateLibraryContactSheet creates an empty contact list in a folder.
func CreateLibraryContactSheet(userID int64, name string, folderID int64) (int64, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	id, err := CreateContactList(userID, name)
	if err != nil {
		return 0, err
	}
	if folderID > 0 {
		_, _ = db.Exec(`UPDATE contact_lists SET folder_id = ? WHERE id = ? AND user_id = ?`, folderID, id, userID)
	}
	return id, nil
}

// CreateLibraryWorkflow creates a workflow in a folder.
func CreateLibraryWorkflow(userID int64, name string, folderID int64) (int64, error) {
	if err := assertLibraryFolderOwned(folderID, userID); err != nil {
		return 0, err
	}
	id, err := CreateWorkflow(userID, name, "")
	if err != nil {
		return 0, err
	}
	if folderID > 0 {
		_, _ = db.Exec(`UPDATE workflows SET folder_id = ? WHERE id = ? AND tenant_id = ?`, folderID, id, userID)
	}
	return id, nil
}
