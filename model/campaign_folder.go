package model

import (
	"database/sql"
	"fmt"
	"strings"

	"emailtracker.com/db"
)

// CampaignDraftMutable reports whether folder sync may rewrite bindings.
func CampaignDraftMutable(c Campaign) bool {
	if c.IsSending {
		return false
	}
	switch c.Status {
	case "sent", "stopped":
		return false
	}
	if c.ScheduledAt != nil {
		return false
	}
	return true
}

// GetCampaignByLibraryFolder returns the campaign linked to a library folder.
func GetCampaignByLibraryFolder(folderID, userID int64) (Campaign, error) {
	if folderID <= 0 {
		return Campaign{}, sql.ErrNoRows
	}
	row := db.QueryRow(`
		SELECT id, COALESCE(user_id, 0), name, template_a_id, template_b_id, status, created_at, scheduled_at,
			COALESCE(execution_mode, 'bulk'), COALESCE(workflow_version_id, 0), COALESCE(is_sending, 0),
			COALESCE(contact_list_id, 0), COALESCE(library_folder_id, 0),
			COALESCE(experiment_variable, ''), COALESCE(experiment_hypothesis, ''), COALESCE(success_metric, 'reply'),
			COALESCE(open_tracking_enabled, FALSE), COALESCE(click_tracking_enabled, FALSE), COALESCE(temperature_rules_json, ''),
			COALESCE(stop_on_reply, TRUE), COALESCE(stop_on_hot, FALSE)
		FROM campaigns WHERE library_folder_id = ? AND user_id = ?
	`, folderID, userID)
	return scanCampaignRow(row)
}

// EnsureCampaignForLibraryFolder creates a draft campaign for a folder if missing.
func EnsureCampaignForLibraryFolder(userID, folderID int64, name string) (int64, error) {
	if folderID <= 0 {
		return 0, fmt.Errorf("folder required")
	}
	if existing, err := GetCampaignByLibraryFolder(folderID, userID); err == nil && existing.ID > 0 {
		return existing.ID, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		f, err := GetLibraryFolderForUser(folderID, userID)
		if err != nil {
			return 0, err
		}
		name = f.Name
	}
	tplID, err := ensurePlaceholderTemplate(userID, folderID)
	if err != nil {
		return 0, err
	}
	id, err := CreateCampaignWithFolder(userID, name, tplID, 0, "bulk", 0, "", "", folderID)
	if err != nil {
		// Race: another request created it.
		if existing, e2 := GetCampaignByLibraryFolder(folderID, userID); e2 == nil && existing.ID > 0 {
			return existing.ID, nil
		}
		return 0, err
	}
	_ = SyncCampaignFromFolder(userID, id)
	return id, nil
}

func ensurePlaceholderTemplate(userID, folderID int64) (int64, error) {
	_ = folderID // campaign folder must not get a stub file — keep placeholders unfiled
	id, err := FirstTemplateIDForUser(userID)
	if err == nil && id > 0 {
		return id, nil
	}
	// Unfiled stub so campaign.template_a_id stays NOT NULL without polluting the folder.
	t := Template{Name: "Email 1", Subject: "", Body: "<p></p>", FolderID: 0}
	return t.SaveTemplate(userID, nil)
}

// SyncCampaignFromFolder applies ordered folder defaults to a draft campaign.
// 1st template → A, 2nd → B, 1st contact sheet → audience, 1st workflow → sequence.
func SyncCampaignFromFolder(userID, campaignID int64) error {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return err
	}
	if !CampaignDraftMutable(c) {
		return nil
	}
	if c.LibraryFolderID <= 0 {
		return nil
	}

	templates, sheets, workflows, err := listFolderAssetsOrdered(userID, c.LibraryFolderID)
	if err != nil {
		return err
	}

	var templateA, templateB int64
	if len(templates) > 0 {
		templateA = templates[0]
	} else {
		templateA = c.TemplateAID
	}
	if len(templates) > 1 {
		templateB = templates[1]
	}
	if templateA <= 0 {
		templateA, err = ensurePlaceholderTemplate(userID, c.LibraryFolderID)
		if err != nil {
			return err
		}
	}
	if err := UpdateCampaignTemplates(campaignID, userID, templateA, templateB); err != nil {
		return err
	}

	mode := "bulk"
	var wfVer int64
	if len(workflows) > 0 {
		pub, err := LatestPublishedVersionID(workflows[0])
		if err == nil && pub > 0 {
			wfVer = pub
			if templateB > 0 {
				mode = "workflow_ab"
			} else {
				mode = "workflow"
			}
		}
	}
	if err := updateCampaignExecutionFromFolder(campaignID, userID, mode, wfVer); err != nil {
		return err
	}

	if len(sheets) > 0 {
		listID := sheets[0]
		if c.ContactListID != listID {
			_, _ = db.Exec(`DELETE FROM campaign_contacts WHERE campaign_id = ?`, campaignID)
		}
		if _, err := SnapshotListToCampaign(listID, campaignID, userID); err != nil {
			return err
		}
	} else if c.ContactListID > 0 {
		_ = SetCampaignContactList(campaignID, userID, 0)
	}

	if wfVer > 0 && len(templates) > 0 {
		_ = syncWorkflowStepTemplatesFromFolder(campaignID, wfVer, templates, mode)
	}
	return nil
}

func updateCampaignExecutionFromFolder(campaignID, userID int64, mode string, workflowVersionID int64) error {
	var wfArg interface{}
	if workflowVersionID > 0 {
		wfArg = workflowVersionID
	}
	_, err := db.Exec(`
		UPDATE campaigns SET execution_mode = ?, workflow_version_id = ?
		WHERE id = ? AND user_id = ? AND status = 'draft' AND COALESCE(is_sending, 0) = 0 AND scheduled_at IS NULL
	`, mode, wfArg, campaignID, userID)
	return err
}

func syncWorkflowStepTemplatesFromFolder(campaignID, versionID int64, templateIDs []int64, mode string) error {
	steps, err := ListSendEmailSteps(versionID)
	if err != nil || len(steps) == 0 {
		return err
	}
	existing, _ := GetCampaignWorkflowTemplates(campaignID)
	mappings := make(map[string]int64, len(existing))
	for k, v := range existing {
		mappings[k] = v
	}
	tplIdx := 0
	if mode == "workflow_ab" && len(templateIDs) > 0 {
		// First send uses A/B on campaign row; map remaining steps from template 3+.
		tplIdx = 2
		if tplIdx > len(templateIDs) {
			tplIdx = len(templateIDs)
		}
		firstKey := steps[0].NodeKey
		if _, ok := mappings[firstKey]; !ok && len(templateIDs) > 0 {
			mappings[firstKey] = templateIDs[0]
		}
		for i := 1; i < len(steps); i++ {
			key := steps[i].NodeKey
			if mappings[key] > 0 {
				continue
			}
			if tplIdx < len(templateIDs) {
				mappings[key] = templateIDs[tplIdx]
				tplIdx++
			} else if len(templateIDs) > 0 {
				mappings[key] = templateIDs[len(templateIDs)-1]
			}
		}
	} else {
		for i, step := range steps {
			if mappings[step.NodeKey] > 0 {
				continue
			}
			if i < len(templateIDs) {
				mappings[step.NodeKey] = templateIDs[i]
			} else if len(templateIDs) > 0 {
				mappings[step.NodeKey] = templateIDs[len(templateIDs)-1]
			}
		}
	}
	return SaveCampaignWorkflowTemplates(campaignID, mappings)
}

func listFolderAssetsOrdered(userID, folderID int64) (templates, sheets, workflows []int64, err error) {
	tRows, err := db.Query(`
		SELECT id FROM template WHERE user_id = ? AND folder_id = ? ORDER BY id ASC
	`, userID, folderID)
	if err != nil {
		return nil, nil, nil, err
	}
	for tRows.Next() {
		var id int64
		if err := tRows.Scan(&id); err != nil {
			tRows.Close()
			return nil, nil, nil, err
		}
		templates = append(templates, id)
	}
	tRows.Close()

	cRows, err := db.Query(`
		SELECT id FROM contact_lists WHERE user_id = ? AND folder_id = ? ORDER BY created_at ASC, id ASC
	`, userID, folderID)
	if err != nil {
		return nil, nil, nil, err
	}
	for cRows.Next() {
		var id int64
		if err := cRows.Scan(&id); err != nil {
			cRows.Close()
			return nil, nil, nil, err
		}
		sheets = append(sheets, id)
	}
	cRows.Close()

	wRows, err := db.Query(`
		SELECT id FROM workflows WHERE tenant_id = ? AND folder_id = ? AND status = 'active' ORDER BY created_at ASC, id ASC
	`, userID, folderID)
	if err != nil {
		return nil, nil, nil, err
	}
	for wRows.Next() {
		var id int64
		if err := wRows.Scan(&id); err != nil {
			wRows.Close()
			return nil, nil, nil, err
		}
		workflows = append(workflows, id)
	}
	wRows.Close()
	return templates, sheets, workflows, nil
}

// SyncDraftCampaignsForFolder syncs the folder's linked draft campaign after asset moves.
func SyncDraftCampaignsForFolder(userID, folderID int64) {
	if folderID <= 0 {
		return
	}
	c, err := GetCampaignByLibraryFolder(folderID, userID)
	if err != nil || c.ID <= 0 {
		return
	}
	_ = SyncCampaignFromFolder(userID, c.ID)
}

// WorkspaceNavItem is one stop in the campaign folder carousel.
type WorkspaceNavItem struct {
	Kind  string // manage | template | contacts | workflow
	ID    int64
	Label string
	URL   string
}

// BuildCampaignWorkspaceNav returns Manage + folder files in carousel order.
func BuildCampaignWorkspaceNav(userID, folderID, campaignID int64) ([]WorkspaceNavItem, error) {
	if folderID <= 0 || campaignID <= 0 {
		return nil, nil
	}
	items := []WorkspaceNavItem{{
		Kind:  "manage",
		ID:    campaignID,
		Label: "Manage",
		URL:   fmt.Sprintf("/campaigns/%d", campaignID),
	}}
	templates, sheets, workflows, err := listFolderAssetsOrdered(userID, folderID)
	if err != nil {
		return nil, err
	}
	for _, id := range templates {
		name := "Template"
		if t, err := GetTemplateForUser(id, userID); err == nil && strings.TrimSpace(t.Name) != "" {
			name = t.Name
		}
		items = append(items, WorkspaceNavItem{
			Kind:  LibraryKindTemplate,
			ID:    id,
			Label: name,
			URL:   fmt.Sprintf("/templates/%d/edit", id),
		})
	}
	for _, id := range sheets {
		name := "Contacts"
		if list, err := GetContactListForUser(id, userID); err == nil && strings.TrimSpace(list.Name) != "" {
			name = list.Name
		}
		items = append(items, WorkspaceNavItem{
			Kind:  LibraryKindContacts,
			ID:    id,
			Label: name,
			URL:   fmt.Sprintf("/library/sheets/%d", id),
		})
	}
	for _, id := range workflows {
		name := "Workflow"
		if w, err := GetWorkflowForUser(id, userID); err == nil && strings.TrimSpace(w.Name) != "" {
			name = w.Name
		}
		items = append(items, WorkspaceNavItem{
			Kind:  LibraryKindWorkflow,
			ID:    id,
			Label: name,
			URL:   fmt.Sprintf("/workflows/%d/edit", id),
		})
	}
	return items, nil
}

// ListContactsInLibraryFolder returns contacts that belong to any contact sheet in the folder.
func ListContactsInLibraryFolder(userID, folderID int64) ([]ContactListItem, error) {
	if folderID <= 0 {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT DISTINCT c.id, c.email
		FROM contact c
		INNER JOIN contact_list_members m ON m.contact_id = c.id
		INNER JOIN contact_lists cl ON cl.id = m.list_id
		WHERE c.user_id = ? AND cl.user_id = ? AND cl.folder_id = ?
		ORDER BY c.email ASC
		LIMIT 2000
	`, userID, userID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContactListItem
	for rows.Next() {
		var c ContactListItem
		if err := rows.Scan(&c.ID, &c.Email); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// ListContactListsInLibraryFolder returns contact sheets in a folder (oldest first).
func ListContactListsInLibraryFolder(userID, folderID int64) ([]ContactList, error) {
	if folderID <= 0 {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT cl.id, cl.user_id, cl.name, cl.created_at,
			(SELECT COUNT(*) FROM contact_list_members m WHERE m.list_id = cl.id)
		FROM contact_lists cl
		WHERE cl.user_id = ? AND cl.folder_id = ?
		ORDER BY cl.created_at ASC, cl.id ASC
	`, userID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContactList
	for rows.Next() {
		var list ContactList
		if err := rows.Scan(&list.ID, &list.UserID, &list.Name, &list.CreatedAt, &list.MemberCount); err != nil {
			return nil, err
		}
		list.FolderID = folderID
		out = append(out, list)
	}
	return out, nil
}
