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
			email := strings.TrimSpace(line)
			if strings.Contains(email, "@") {
				rows = append(rows, model.ImportContactRow{Email: email})
			}
		}
	}
	return rows
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
	return rows
}
