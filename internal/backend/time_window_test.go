package backend

import "testing"

func TestExcludeRowsByTimeWindows_RemovesRowsInsideConfiguredInterval(t *testing.T) {
	data := &CSVData{
		Columns: []string{"UTC", "value", "original_excel_row"},
		Rows: [][]string{
			{"1714557600", "keep-1", "1"},
			{"1714561200", "drop", "2"},
			{"1714564800", "keep-2", "3"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}

	out, removed, err := excludeRowsByTimeWindows(data, []TimeWindow{
		// 1714561200 is 2024-05-01 11:00:00 UTC, which is 13:00 in Europe/Bratislava.
		{Start: "2024-05-01T13:00:00", End: "2024-05-01T13:30:00"},
	})
	if err != nil {
		t.Fatalf("exclude rows by time windows: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed row, got %d", removed)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("expected 2 remaining rows, got %d", len(out.Rows))
	}
	if got := out.Rows[0][1]; got != "keep-1" {
		t.Fatalf("expected first remaining row keep-1, got %q", got)
	}
	if got := out.Rows[1][1]; got != "keep-2" {
		t.Fatalf("expected second remaining row keep-2, got %q", got)
	}
}

func TestExcludeRowsByTimeWindows_IncludesWholeSelectedEndSecond(t *testing.T) {
	data := &CSVData{
		Columns: []string{"Date", "Time", "value"},
		Rows: [][]string{
			{"05.02.2026", "10:04:00.670", "drop"},
			{"05.02.2026", "10:04:01.000", "keep"},
		},
	}

	out, removed, err := excludeRowsByTimeWindows(data, []TimeWindow{{
		Start: "2026-02-05T10:03:59",
		End:   "2026-02-05T10:04:00",
	}})
	if err != nil {
		t.Fatalf("exclude rows by time windows: %v", err)
	}
	if removed != 1 || len(out.Rows) != 1 || out.Rows[0][2] != "keep" {
		t.Fatalf("whole end second was not applied: removed=%d rows=%v", removed, out.Rows)
	}
}

func TestParseDateTimeToMillis_AcceptsDateTimeLocalFormat(t *testing.T) {
	if _, ok := parseDateTimeToMillis("2024-05-01T11:15"); !ok {
		t.Fatalf("expected datetime-local format to parse")
	}
}
