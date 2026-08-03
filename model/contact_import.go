package model

import (
	"fmt"
	"strings"
)

type ImportContactsResult struct {
	Created       int
	Updated       int
	Skipped       int
	Errors        int
	InvalidEmails []string
	ContactIDs    []int64
}

type ImportContactRow struct {
	Email             string
	Variables         map[string]string
	EmailStatus       string
	EmailStatusReason string
}

func ImportContactRows(userID int64, rows []ImportContactRow, listID int64, importKeys []string) (ImportContactsResult, error) {
	var result ImportContactsResult
	if listID > 0 && len(importKeys) > 0 {
		if err := EnsureListVariableSchema(listID, userID, importKeys); err != nil {
			return result, err
		}
	}
	for _, row := range rows {
		email := strings.TrimSpace(row.Email)
		if !strings.Contains(email, "@") {
			result.Skipped++
			continue
		}
		status := row.EmailStatus
		if status == "" {
			status = "unknown"
		}
		if status == "invalid" {
			result.Skipped++
			if len(result.InvalidEmails) < 10 {
				result.InvalidEmails = append(result.InvalidEmails, email)
			}
			continue
		}
		var cvs []ContactVariables
		for k, v := range row.Variables {
			if k == "" {
				continue
			}
			cvs = append(cvs, ContactVariables{Key: k, Value: v})
		}
		created, contactID, err := UpsertContactWithEmailStatus(userID, email, cvs, status, row.EmailStatusReason)
		if err != nil {
			result.Errors++
			continue
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
		result.ContactIDs = append(result.ContactIDs, contactID)
		if listID > 0 {
			if err := addContactToListValidated(listID, userID, contactID); err != nil {
				result.Errors++
				continue
			}
		}
	}
	return result, nil
}

func UpsertContactWithEmailStatus(userID int64, email string, variables []ContactVariables, emailStatus, emailReason string) (created bool, contactID int64, err error) {
	c, err := FindContactByEmail(userID, email)
	if err == nil {
		if err := UpdateContact(c.ID, userID, email, variables); err != nil {
			return false, 0, err
		}
		if emailStatus == "valid" {
			_ = SetContactEmailStatus(c.ID, emailStatus, emailReason)
		}
		return false, c.ID, nil
	}
	contact := Contact{Email: email}
	id, err := contact.SaveContact(userID, variables)
	if err != nil {
		return false, 0, err
	}
	if emailStatus != "" && emailStatus != "unknown" {
		_ = SetContactEmailStatus(id, emailStatus, emailReason)
	} else if emailStatus == "valid" {
		_ = SetContactEmailStatus(id, "valid", "")
	}
	return true, id, nil
}

func UpsertContact(userID int64, email string, variables []ContactVariables) (created bool, contactID int64, err error) {
	return UpsertContactWithEmailStatus(userID, email, variables, "", "")
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
	if len(r.InvalidEmails) > 0 {
		parts = append(parts, formatImportN(len(r.InvalidEmails), "invalid emails"))
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
