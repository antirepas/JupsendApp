package util

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

const emailColumn = "email"

type ContactImportRow struct {
	Email     string
	Variables map[string]string
}

func ExpectedContactColumns(templateVars []string) []string {
	cols := []string{emailColumn}
	cols = append(cols, templateVars...)
	return cols
}

func CreateContactSampleExcel(templateVars []string) ([]byte, error) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := ExpectedContactColumns(templateVars)

	for i, header := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}

	examples := []string{"hello@example.com;contact@example.com"}
	for _, v := range templateVars {
		examples = append(examples, "example_"+v)
	}
	for i, value := range examples {
		cell, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellValue(sheet, cell, value); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseContactsUpload reads Excel (.xlsx/.xls) or CSV based on the filename.
func ParseContactsUpload(reader io.Reader, filename string, templateVars []string) ([]ContactImportRow, error) {
	name := strings.ToLower(strings.TrimSpace(filename))
	if strings.HasSuffix(name, ".csv") {
		return ParseContactsCSV(reader, templateVars)
	}
	return ParseContactsExcel(reader, templateVars)
}

func ParseContactsExcel(reader io.Reader, templateVars []string) ([]ContactImportRow, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("could not read Excel file: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("could not read sheet rows: %w", err)
	}
	return parseContactRowsFromTable(rows, templateVars)
}

func ParseContactsCSV(reader io.Reader, templateVars []string) ([]ContactImportRow, error) {
	r := csv.NewReader(reader)
	r.TrimLeadingSpace = true
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV file: %w", err)
	}
	return parseContactRowsFromTable(rows, templateVars)
}

func parseContactRowsFromTable(rows [][]string, templateVars []string) ([]ContactImportRow, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("spreadsheet is empty")
	}

	headerRow := rows[0]
	colIndex := mapHeaders(headerRow)

	if _, ok := colIndex[emailColumn]; !ok {
		expected := strings.Join(ExpectedContactColumns(templateVars), ", ")
		return nil, fmt.Errorf("missing required column \"email\". Expected columns: %s", expected)
	}

	var missing []string
	for _, v := range templateVars {
		if _, ok := colIndex[normalizeHeader(v)]; !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required columns for template: %s", strings.Join(missing, ", "))
	}

	var contacts []ContactImportRow
	for _, row := range rows[1:] {
		rawEmail := cellValue(row, colIndex[emailColumn])
		if rawEmail == "" {
			continue
		}

		email, ok := ResolveImportEmail(rawEmail)
		if !ok {
			continue
		}

		vars := make(map[string]string)
		for _, v := range templateVars {
			idx := colIndex[normalizeHeader(v)]
			vars[v] = cellValue(row, idx)
		}

		contacts = append(contacts, ContactImportRow{
			Email:     email,
			Variables: vars,
		})
	}

	if len(contacts) == 0 {
		return nil, fmt.Errorf("no valid contacts found in spreadsheet")
	}

	return contacts, nil
}

func mapHeaders(headerRow []string) map[string]int {
	index := make(map[string]int)
	for i, h := range headerRow {
		key := normalizeHeader(h)
		if key != "" {
			index[key] = i
		}
	}
	return index
}

func normalizeHeader(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func cellValue(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
