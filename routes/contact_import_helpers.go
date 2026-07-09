package routes

import (
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

func parseImportRowsFromPaste(paste string, variableKeys []string) []model.ImportContactRow {
	utilRows := util.ParseContactPasteWithHeaders(paste, variableKeys)
	rows := make([]model.ImportContactRow, 0, len(utilRows))
	for _, r := range utilRows {
		rows = append(rows, model.ImportContactRow{Email: r.Email, Variables: r.Variables})
	}
	if len(variableKeys) == 0 && len(rows) == 0 {
		for _, line := range strings.Split(paste, "\n") {
			email, ok := util.ResolveImportEmail(line)
			if ok {
				rows = append(rows, model.ImportContactRow{Email: email})
			}
		}
	}
	return applyEmailValidation(rows)
}

func parseImportRowsFromExcel(excelRows []util.ContactImportRow, variableKeys []string) []model.ImportContactRow {
	rows := make([]model.ImportContactRow, 0, len(excelRows))
	for _, r := range excelRows {
		vars := make(map[string]string)
		for _, k := range variableKeys {
			vars[k] = r.Variables[k]
		}
		rows = append(rows, model.ImportContactRow{Email: r.Email, Variables: vars})
	}
	return applyEmailValidation(rows)
}

func applyEmailValidation(rows []model.ImportContactRow) []model.ImportContactRow {
	for i := range rows {
		ok, reason := util.ValidateEmail(rows[i].Email)
		if ok {
			rows[i].EmailStatus = "valid"
		} else {
			rows[i].EmailStatus = "invalid"
			rows[i].EmailStatusReason = reason
		}
	}
	return rows
}
