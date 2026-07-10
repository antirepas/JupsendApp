package util

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

const emailColumn = "email"

type ContactImportRow struct {
	Email     string
	Variables map[string]string
}

// ContactUploadPeek is a preview of columns and sample rows before import.
type ContactUploadPeek struct {
	Columns    []string
	SampleRows []map[string]string
	RowCount   int
}

// PeekContactsUpload reads headers and up to 5 sample rows without requiring a template.
func PeekContactsUpload(reader io.Reader, filename string, templateVars []string) (ContactUploadPeek, error) {
	rows, err := ParseContactsUpload(reader, filename, templateVars)
	if err != nil {
		return ContactUploadPeek{}, err
	}
	cols := ExpectedContactColumns(templateVars)
	if len(templateVars) == 0 && len(rows) > 0 {
		cols = variableKeysFromRows(rows)
	}
	peek := ContactUploadPeek{
		Columns:  cols,
		RowCount: len(rows),
	}
	limit := len(rows)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		row := map[string]string{"email": rows[i].Email}
		for k, v := range rows[i].Variables {
			row[k] = v
		}
		peek.SampleRows = append(peek.SampleRows, row)
	}
	return peek, nil
}

func variableKeysFromRows(rows []ContactImportRow) []string {
	seen := map[string]bool{}
	var keys []string
	for _, row := range rows {
		for k := range row.Variables {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func ExpectedContactColumns(templateVars []string) []string {
	cols := []string{emailColumn}
	cols = append(cols, templateVars...)
	return cols
}

// DefaultSampleVariableColumns are used for generic sample downloads and hints.
func DefaultSampleVariableColumns() []string {
	return []string{"name", "company", "description"}
}

func CreateContactSampleExcel(templateVars []string) ([]byte, error) {
	if len(templateVars) == 0 {
		templateVars = DefaultSampleVariableColumns()
	}
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
		if len(templateVars) > 0 {
			expected := strings.Join(ExpectedContactColumns(templateVars), ", ")
			return nil, fmt.Errorf("missing required column \"email\". Expected columns: %s", expected)
		}
		return nil, fmt.Errorf("missing required column \"email\"")
	}

	varKeys := templateVars
	if len(varKeys) == 0 {
		varKeys = detectVariableKeysFromHeaders(headerRow)
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
		for _, v := range varKeys {
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

func detectVariableKeysFromHeaders(headerRow []string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, h := range headerRow {
		key := normalizeHeader(h)
		if key == "" || key == emailColumn || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
