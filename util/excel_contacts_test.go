package util

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseContactsExcel(t *testing.T) {
	data, err := CreateContactSampleExcel([]string{"name", "company"})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := ParseContactsExcel(bytes.NewReader(data), []string{"name", "company"})
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	if rows[0].Email != "recipient@example.com" {
		t.Fatalf("unexpected email: %s", rows[0].Email)
	}

	if rows[0].Variables["name"] != "example_name" {
		t.Fatalf("unexpected name: %s", rows[0].Variables["name"])
	}
}

func TestParseContactsExcelMissingEmailColumn(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	if err := f.SetCellValue(sheet, "A1", "name"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue(sheet, "A2", "John"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	_, err := ParseContactsExcel(bytes.NewReader(buf.Bytes()), []string{"name"})
	if err == nil {
		t.Fatal("expected error for missing email column")
	}
}
