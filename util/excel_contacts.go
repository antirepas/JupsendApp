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

const (
	MapTargetEmail = "email"
	MapTargetSkip  = "skip"
)

type ContactImportRow struct {
	Email     string
	Variables map[string]string
}

// ContactUploadPeek is a legacy preview of columns and sample rows before import.
type ContactUploadPeek struct {
	Columns    []string            `json:"columns"`
	SampleRows []map[string]string `json:"sample_rows"`
	RowCount   int                 `json:"row_count"`
}

// ContactUploadRawPeek is a header/sample preview that does not require an email column.
type ContactUploadRawPeek struct {
	Headers      []string          `json:"headers"`
	SampleRows   [][]string        `json:"sample_rows"`
	RowCount     int               `json:"row_count"`
	SuggestedMap map[string]string `json:"suggested_map"`
	TemplateVars []string          `json:"template_vars,omitempty"`
}

// ContactUploadTable is the full parsed spreadsheet (header + data rows).
type ContactUploadTable struct {
	Headers  []string
	DataRows [][]string
}

// PeekContactsUploadRaw reads headers and sample rows without requiring an email column.
func PeekContactsUploadRaw(reader io.Reader, filename string, templateVars []string) (ContactUploadRawPeek, error) {
	table, err := ReadContactsUploadTable(reader, filename)
	if err != nil {
		return ContactUploadRawPeek{}, err
	}
	peek := ContactUploadRawPeek{
		Headers:      table.Headers,
		RowCount:     len(table.DataRows),
		SuggestedMap: SuggestContactColumnMap(table.Headers, templateVars),
		TemplateVars: append([]string(nil), templateVars...),
	}
	limit := len(table.DataRows)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		peek.SampleRows = append(peek.SampleRows, padRow(table.DataRows[i], len(table.Headers)))
	}
	return peek, nil
}

// SuggestContactColumnMap picks a default target for each header.
// Exactly one header is suggested as email when possible; others become variables
// (or skip when a template is selected and the header does not match a template var).
func SuggestContactColumnMap(headers []string, templateVars []string) map[string]string {
	out := make(map[string]string, len(headers))
	emailHeader := SuggestEmailHeader(headers)
	tmpl := map[string]bool{}
	for _, v := range templateVars {
		tmpl[normalizeHeader(v)] = true
	}
	for _, h := range headers {
		key := strings.TrimSpace(h)
		if key == "" {
			continue
		}
		if emailHeader != "" && key == emailHeader {
			out[key] = MapTargetEmail
			continue
		}
		norm := normalizeHeader(key)
		if len(templateVars) > 0 {
			if tmpl[norm] {
				out[key] = norm
			} else {
				out[key] = MapTargetSkip
			}
			continue
		}
		out[key] = norm
	}
	return out
}

// SuggestEmailHeader returns the best header to treat as email, or "".
func SuggestEmailHeader(headers []string) string {
	var containsMatch string
	for _, h := range headers {
		key := strings.TrimSpace(h)
		if key == "" {
			continue
		}
		norm := normalizeHeader(key)
		switch norm {
		case "email", "e-mail", "e_mail", "mail":
			return key
		}
		if containsMatch == "" && strings.Contains(norm, "email") {
			containsMatch = key
		}
	}
	return containsMatch
}

// ApplyContactColumnMap builds import rows from a table using header→target mapping.
// Targets: "email", "skip", or a variable name. Exactly one column must map to email.
func ApplyContactColumnMap(headers []string, dataRows [][]string, colMap map[string]string) ([]ContactImportRow, error) {
	if len(headers) == 0 {
		return nil, fmt.Errorf("spreadsheet has no headers")
	}
	emailIdx := -1
	type varCol struct {
		name string
		idx  int
	}
	var vars []varCol
	seenVar := map[string]bool{}

	for i, h := range headers {
		key := strings.TrimSpace(h)
		if key == "" {
			continue
		}
		target := strings.TrimSpace(colMap[key])
		if target == "" {
			// Also accept normalized header keys from clients.
			target = strings.TrimSpace(colMap[normalizeHeader(key)])
		}
		if target == "" || strings.EqualFold(target, MapTargetSkip) {
			continue
		}
		if strings.EqualFold(target, MapTargetEmail) {
			if emailIdx >= 0 {
				return nil, fmt.Errorf("map exactly one column to email")
			}
			emailIdx = i
			continue
		}
		vname := normalizeHeader(target)
		if vname == "" || vname == emailColumn {
			continue
		}
		if seenVar[vname] {
			return nil, fmt.Errorf("variable %q is mapped more than once", vname)
		}
		seenVar[vname] = true
		vars = append(vars, varCol{name: vname, idx: i})
	}
	if emailIdx < 0 {
		return nil, fmt.Errorf("map one column to email")
	}

	var contacts []ContactImportRow
	for _, row := range dataRows {
		rawEmail := cellValue(row, emailIdx)
		if rawEmail == "" {
			continue
		}
		email, ok := ResolveImportEmail(rawEmail)
		if !ok {
			continue
		}
		m := make(map[string]string, len(vars))
		for _, v := range vars {
			m[v.name] = cellValue(row, v.idx)
		}
		contacts = append(contacts, ContactImportRow{Email: email, Variables: m})
	}
	if len(contacts) == 0 {
		return nil, fmt.Errorf("no valid contacts found in spreadsheet")
	}
	return contacts, nil
}

// ReadContactsUploadTable parses Excel/CSV into headers + data rows (no email required).
func ReadContactsUploadTable(reader io.Reader, filename string) (ContactUploadTable, error) {
	name := strings.ToLower(strings.TrimSpace(filename))
	var rows [][]string
	var err error
	if strings.HasSuffix(name, ".csv") {
		rows, err = readCSVRows(reader)
	} else {
		rows, err = readExcelRows(reader)
	}
	if err != nil {
		return ContactUploadTable{}, err
	}
	if len(rows) == 0 {
		return ContactUploadTable{}, fmt.Errorf("spreadsheet is empty")
	}
	headers := make([]string, len(rows[0]))
	copy(headers, rows[0])
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}
	return ContactUploadTable{Headers: headers, DataRows: rows[1:]}, nil
}

// PeekContactsUpload reads headers and up to 5 sample rows without requiring a template.
// Prefer PeekContactsUploadRaw for mapping UI; this keeps the old shape for callers that still use it.
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

// ParseContactsUploadWithMap reads a file and applies an explicit column map.
func ParseContactsUploadWithMap(reader io.Reader, filename string, colMap map[string]string) ([]ContactImportRow, error) {
	table, err := ReadContactsUploadTable(reader, filename)
	if err != nil {
		return nil, err
	}
	return ApplyContactColumnMap(table.Headers, table.DataRows, colMap)
}

func ParseContactsExcel(reader io.Reader, templateVars []string) ([]ContactImportRow, error) {
	rows, err := readExcelRows(reader)
	if err != nil {
		return nil, err
	}
	return parseContactRowsFromTable(rows, templateVars)
}

func ParseContactsCSV(reader io.Reader, templateVars []string) ([]ContactImportRow, error) {
	rows, err := readCSVRows(reader)
	if err != nil {
		return nil, err
	}
	return parseContactRowsFromTable(rows, templateVars)
}

func readExcelRows(reader io.Reader) ([][]string, error) {
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
	return rows, nil
}

func readCSVRows(reader io.Reader) ([][]string, error) {
	r := csv.NewReader(reader)
	r.TrimLeadingSpace = true
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV file: %w", err)
	}
	return rows, nil
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

func padRow(row []string, n int) []string {
	out := make([]string, n)
	copy(out, row)
	return out
}

// FormatResolvedEmailSample describes multi-email resolution for UI hints.
func FormatResolvedEmailSample(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	candidates := SplitEmailCandidates(raw)
	resolved, ok := ResolveImportEmail(raw)
	if !ok {
		return raw
	}
	if len(candidates) <= 1 {
		return resolved
	}
	return fmt.Sprintf("%s (from %d in cell)", resolved, len(candidates))
}
