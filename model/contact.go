package model

import (
	"emailtracker.com/db"
)

type Contact struct {
	ID    int64
	Email string
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

func (c *Contact) SaveContact(variables []ContactVariables) (int64, error) {
	query := `INSERT INTO contact (email) VALUES (?) RETURNING id`

	row := db.DB.QueryRow(query, c.Email)
	var contactID int64
	err := row.Scan(&contactID)
	if err != nil {
		return 0, err
	}
	query = `INSERT INTO contact_variables (key, value, contact_id) VALUES (?, ?, ?)`
	for _, v := range variables {
		_, err = db.DB.Exec(query, v.Key, v.Value, contactID)
		if err != nil {
			return 0, err
		}
	}

	return contactID, err
}

func GetContact(contactId int64) (Contact, []ContactVariables, error) {
	query := `SELECT id, email FROM contact WHERE id = ?`
	row := db.DB.QueryRow(query, contactId)
	var c Contact
	err := row.Scan(&c.ID, &c.Email)
	if err != nil {
		return Contact{}, nil, err
	}
	query = `SELECT key, value FROM contact_variables WHERE contact_id = ?`
	rows, err := db.DB.Query(query, contactId)
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

func ListContacts() ([]ContactListItem, error) {
	rows, err := db.DB.Query(`SELECT id, email FROM contact ORDER BY id DESC`)
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

		varRows, err := db.DB.Query(
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

func DeleteContact(id int64) error {
	_, err := db.DB.Exec(`DELETE FROM contact WHERE id = ?`, id)
	return err
}
