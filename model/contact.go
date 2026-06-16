package model

import (
	"database/sql"

	"emailtracker.com/db"
)

var errNotFound = sql.ErrNoRows

type Contact struct {
	ID     int64
	UserID int64
	Email  string
}

type ContactVariables struct {
	ContactID int64
	Key       string
	Value     string
}

type ContactListItem struct {
	ID        int64
	Email     string
	Variables []ContactVariables
}

func (c *Contact) SaveContact(userID int64, variables []ContactVariables) (int64, error) {
	query := `INSERT INTO contact (email, user_id) VALUES (?, ?) RETURNING id`

	row := db.QueryRow(query, c.Email, userID)
	var contactID int64
	err := row.Scan(&contactID)
	if err != nil {
		return 0, err
	}
	query = `INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`
	for _, v := range variables {
		_, err = db.Exec(query, v.Key, v.Value, contactID)
		if err != nil {
			return 0, err
		}
	}

	return contactID, err
}

func GetContact(contactId int64) (Contact, []ContactVariables, error) {
	query := `SELECT id, COALESCE(user_id, 0), email FROM contact WHERE id = ?`
	row := db.QueryRow(query, contactId)
	var c Contact
	err := row.Scan(&c.ID, &c.UserID, &c.Email)
	if err != nil {
		return Contact{}, nil, err
	}
	query = `SELECT key, value FROM contact_variables WHERE contact_id = ?`
	rows, err := db.Query(query, contactId)
	if err != nil {
		return Contact{}, nil, err
	}
	defer rows.Close()
	var cVars []ContactVariables
	for rows.Next() {
		var cVar ContactVariables
		err = rows.Scan(&cVar.Key, &cVar.Value)
		if err != nil {
			return Contact{}, nil, err
		}
		cVar.ContactID = contactId
		cVars = append(cVars, cVar)
	}
	return c, cVars, nil
}

func GetContactForUser(contactId, userID int64) (Contact, []ContactVariables, error) {
	c, vars, err := GetContact(contactId)
	if err != nil {
		return Contact{}, nil, err
	}
	if userID > 0 && c.UserID > 0 && c.UserID != userID {
		return Contact{}, nil, errNotFound
	}
	return c, vars, nil
}

func ListContacts(userID int64) ([]ContactListItem, error) {
	rows, err := db.Query(`SELECT id, email FROM contact WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ContactListItem
	for rows.Next() {
		var item ContactListItem
		if err := rows.Scan(&item.ID, &item.Email); err != nil {
			return nil, err
		}

		varRows, err := db.Query(
			`SELECT key, value FROM contact_variables WHERE contact_id = ?`, item.ID,
		)
		if err != nil {
			return nil, err
		}
		for varRows.Next() {
			var cv ContactVariables
			if err := varRows.Scan(&cv.Key, &cv.Value); err != nil {
				varRows.Close()
				return nil, err
			}
			cv.ContactID = item.ID
			item.Variables = append(item.Variables, cv)
		}
		varRows.Close()
		items = append(items, item)
	}
	return items, nil
}

func DeleteContact(id, userID int64) error {
	if _, _, err := GetContactForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM contact WHERE id = ?`, id)
	return err
}

func UpdateContact(id, userID int64, email string, variables []ContactVariables) error {
	if _, _, err := GetContactForUser(id, userID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE contact SET email = ? WHERE id = ?`, email, id)
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM contact_variables WHERE contact_id = ?`, id)
	if err != nil {
		return err
	}
	for _, v := range variables {
		if v.Key == "" {
			continue
		}
		_, err = db.Exec(
			`INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`,
			v.Key, v.Value, id,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func FindOrCreateContact(userID int64, email string, variables []ContactVariables) (int64, error) {
	row := db.QueryRow(`SELECT id FROM contact WHERE email = ? AND user_id = ?`, email, userID)
	var id int64
	err := row.Scan(&id)
	if err == nil {
		return id, nil
	}

	c := Contact{Email: email}
	return c.SaveContact(userID, variables)
}

func FindContactByEmail(userID int64, email string) (Contact, error) {
	row := db.QueryRow(`SELECT id, COALESCE(user_id, 0), email FROM contact WHERE email = ? AND user_id = ?`, email, userID)
	var c Contact
	err := row.Scan(&c.ID, &c.UserID, &c.Email)
	return c, err
}
