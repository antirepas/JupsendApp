package model

import (
	"fmt"
	"strings"
)

type ImportContactsResult struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

type ImportContactRow struct {
	Email     string
	Variables map[string]string
}

func ImportContactRows(userID int64, rows []ImportContactRow, listID int64) (ImportContactsResult, error) {
	var result ImportContactsResult
	for _, row := range rows {
		email := strings.TrimSpace(row.Email)
		if !strings.Contains(email, "@") {
			result.Skipped++
			continue
		}
		var cvs []ContactVariables
		for k, v := range row.Variables {
			if k == "" {
				continue
			}
			cvs = append(cvs, ContactVariables{Key: k, Value: v})
		}
		created, contactID, err := UpsertContact(userID, email, cvs)
		if err != nil {
			result.Errors++
			continue
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
		if listID > 0 {
			_ = AddContactsToList(listID, userID, []int64{contactID})
		}
	}
	return result, nil
}

func UpsertContact(userID int64, email string, variables []ContactVariables) (created bool, contactID int64, err error) {
	c, err := FindContactByEmail(userID, email)
	if err == nil {
		if err := UpdateContact(c.ID, userID, email, variables); err != nil {
			return false, 0, err
		}
		return false, c.ID, nil
	}
	contact := Contact{Email: email}
	id, err := contact.SaveContact(userID, variables)
	if err != nil {
		return false, 0, err
	}
	return true, id, nil
}

func FormatImportResultMessage(r ImportContactsResult) string {
	parts := []string{}
	if r.Created > 0 {
		parts = append(parts, formatImportN(r.Created, "created"))
	}
	if r.Updated > 0 {
		parts = append(parts, formatImportN(r.Updated, "updated"))
	}
	if r.Skipped > 0 {
		parts = append(parts, formatImportN(r.Skipped, "skipped"))
	}
	if r.Errors > 0 {
		parts = append(parts, formatImportN(r.Errors, "errors"))
	}
	if len(parts) == 0 {
		return "No contacts imported"
	}
	return strings.Join(parts, ", ")
}

func formatImportN(n int, word string) string {
	suffix := word
	if n == 1 {
		if strings.HasSuffix(word, "s") {
			suffix = strings.TrimSuffix(word, "s")
		}
		return fmt.Sprintf("%d %s", n, suffix)
	}
	return fmt.Sprintf("%d %s", n, word)
}
