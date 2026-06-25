package model

import (
	"database/sql"
	"time"

	"emailtracker.com/db"
)

type ContactList struct {
	ID           int64
	UserID       int64
	Name         string
	MemberCount  int
	CreatedAt    time.Time
}

func ListContactLists(userID int64) ([]ContactList, error) {
	rows, err := db.Query(`
		SELECT cl.id, cl.user_id, cl.name, cl.created_at,
			(SELECT COUNT(*) FROM contact_list_members m WHERE m.list_id = cl.id)
		FROM contact_lists cl
		WHERE cl.user_id = ?
		ORDER BY cl.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ContactList
	for rows.Next() {
		var item ContactList
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.MemberCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func GetContactListForUser(listID, userID int64) (ContactList, error) {
	row := db.QueryRow(`
		SELECT cl.id, cl.user_id, cl.name, cl.created_at,
			(SELECT COUNT(*) FROM contact_list_members m WHERE m.list_id = cl.id)
		FROM contact_lists cl
		WHERE cl.id = ? AND cl.user_id = ?
	`, listID, userID)
	var item ContactList
	err := row.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.MemberCount)
	return item, err
}

func CreateContactList(userID int64, name string) (int64, error) {
	row := db.QueryRow(`
		INSERT INTO contact_lists (user_id, name) VALUES (?, ?) RETURNING id
	`, userID, name)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func RenameContactList(listID, userID int64, name string) error {
	_, err := db.Exec(`UPDATE contact_lists SET name = ? WHERE id = ? AND user_id = ?`, name, listID, userID)
	return err
}

func DeleteContactList(listID, userID int64) error {
	res, err := db.Exec(`DELETE FROM contact_lists WHERE id = ? AND user_id = ?`, listID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func AddContactsToList(listID, userID int64, contactIDs []int64) error {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return err
	}
	for _, cid := range contactIDs {
		if _, _, err := GetContactForUser(cid, userID); err != nil {
			continue
		}
		_, _ = db.Exec(`
			INSERT INTO contact_list_members (list_id, contact_id) VALUES (?, ?)
			ON CONFLICT DO NOTHING
		`, listID, cid)
	}
	return nil
}

func RemoveContactFromList(listID, userID, contactID int64) error {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM contact_list_members WHERE list_id = ? AND contact_id = ?`, listID, contactID)
	return err
}

func SetContactLists(userID, contactID int64, listIDs []int64) error {
	if _, _, err := GetContactForUser(contactID, userID); err != nil {
		return err
	}
	_, err := db.Exec(`
		DELETE FROM contact_list_members
		WHERE contact_id = ? AND list_id IN (SELECT id FROM contact_lists WHERE user_id = ?)
	`, contactID, userID)
	if err != nil {
		return err
	}
	for _, lid := range listIDs {
		if lid <= 0 {
			continue
		}
		if _, err := GetContactListForUser(lid, userID); err != nil {
			continue
		}
		_, _ = db.Exec(`
			INSERT INTO contact_list_members (list_id, contact_id) VALUES (?, ?)
			ON CONFLICT DO NOTHING
		`, lid, contactID)
	}
	return nil
}

func GetListsForContact(userID, contactID int64) ([]ContactList, error) {
	rows, err := db.Query(`
		SELECT cl.id, cl.user_id, cl.name, cl.created_at,
			(SELECT COUNT(*) FROM contact_list_members m WHERE m.list_id = cl.id)
		FROM contact_lists cl
		INNER JOIN contact_list_members m ON m.list_id = cl.id
		WHERE m.contact_id = ? AND cl.user_id = ?
		ORDER BY cl.name ASC
	`, contactID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ContactList
	for rows.Next() {
		var item ContactList
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.CreatedAt, &item.MemberCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func GetListIDsForContacts(userID int64, contactIDs []int64) (map[int64][]ContactList, error) {
	result := make(map[int64][]ContactList)
	if len(contactIDs) == 0 {
		return result, nil
	}
	// Load all lists once for badge display
	allLists, err := ListContactLists(userID)
	if err != nil {
		return nil, err
	}
	listByID := make(map[int64]ContactList)
	for _, l := range allLists {
		listByID[l.ID] = l
	}
	rows, err := db.Query(`
		SELECT m.contact_id, m.list_id
		FROM contact_list_members m
		INNER JOIN contact_lists cl ON cl.id = m.list_id
		WHERE cl.user_id = ?
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := make(map[int64]bool)
	for _, id := range contactIDs {
		want[id] = true
	}
	for rows.Next() {
		var cid, lid int64
		if err := rows.Scan(&cid, &lid); err != nil {
			return nil, err
		}
		if !want[cid] {
			continue
		}
		if l, ok := listByID[lid]; ok {
			result[cid] = append(result[cid], l)
		}
	}
	return result, nil
}

func ListMemberContactIDs(listID, userID int64) ([]int64, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT m.contact_id FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE m.list_id = ? AND c.user_id = ?
		ORDER BY m.contact_id ASC
	`, listID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func SnapshotListToCampaign(listID, campaignID, userID int64) (int, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return 0, err
	}
	if _, err := GetCampaignForUser(campaignID, userID); err != nil {
		return 0, err
	}
	ids, err := ListMemberContactIDs(listID, userID)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := AddContactsToCampaign(campaignID, ids); err != nil {
		return 0, err
	}
	_, _ = db.Exec(`UPDATE campaigns SET contact_list_id = ? WHERE id = ? AND user_id = ?`, listID, campaignID, userID)
	return len(ids), nil
}

func SetCampaignContactList(campaignID, userID int64, listID int64) error {
	var lid interface{}
	if listID > 0 {
		lid = listID
	}
	_, err := db.Exec(`UPDATE campaigns SET contact_list_id = ? WHERE id = ? AND user_id = ?`, lid, campaignID, userID)
	return err
}
