package backend

import (
	"strings"
	"testing"
)

func TestEnsureOriginalExcelRowColumn_addsColumn(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"a", "b"},
		Rows:    [][]string{{"1", "2"}, {"3", "4"}},
		FileInfo: CSVFileInfo{
			HeaderLine: 1,
		},
	}
	out, err := ensureOriginalExcelRowColumn(data)
	if err != nil {
		t.Fatal(err)
	}
	idx := indexOf(out.Columns, "original_excel_row")
	if idx < 0 {
		t.Fatal("missing column")
	}
	// HeaderLine 1 -> first data row is line 2 -> original 2 and 3
	if out.Rows[0][idx] != "2" || out.Rows[1][idx] != "3" {
		t.Fatalf("rows: %#v, %#v", out.Rows[0], out.Rows[1])
	}
}

func TestAssignSequentialOriginalExcelRows_renumbers(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns:  []string{"x"},
		Rows:     [][]string{{"a"}, {"b"}},
		FileInfo: CSVFileInfo{},
	}
	out, err := assignSequentialOriginalExcelRows(data)
	if err != nil {
		t.Fatal(err)
	}
	idx := indexOf(out.Columns, "original_excel_row")
	if out.Rows[0][idx] != "1" || out.Rows[1][idx] != "2" {
		t.Fatalf("got %#v %#v", out.Rows[0], out.Rows[1])
	}
}

func TestExcludeRowsByOriginalExcelRow(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"v", "original_excel_row"},
		Rows: [][]string{
			{"a", "1"},
			{"b", "2"},
			{"c", "3"},
		},
		FileInfo: CSVFileInfo{},
	}
	out, removed, err := excludeRowsByOriginalExcelRow(data, []int{2})
	if err != nil || removed != 1 || len(out.Rows) != 2 {
		t.Fatalf("err=%v removed=%d rows=%d", err, removed, len(out.Rows))
	}
	if out.Rows[0][0] != "a" || out.Rows[1][0] != "c" {
		t.Fatalf("unexpected rows: %#v", out.Rows)
	}
}

func TestExcludeRowsByOriginalExcelRow_errors(t *testing.T) {
	t.Parallel()

	_, _, err := excludeRowsByOriginalExcelRow(nil, []int{1})
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil error, got %v", err)
	}

	data := &CSVData{Columns: []string{"x"}, Rows: [][]string{{"1"}}}
	_, _, err2 := excludeRowsByOriginalExcelRow(data, []int{1})
	if err2 == nil || !strings.Contains(err2.Error(), "original_excel_row") {
		t.Fatalf("expected missing column error, got %v", err2)
	}

	data2 := &CSVData{
		Columns: []string{"original_excel_row"},
		Rows:    [][]string{{"x"}},
	}
	_, _, err3 := excludeRowsByOriginalExcelRow(data2, []int{1})
	if err3 == nil {
		t.Fatal("expected invalid row id error")
	}
}
