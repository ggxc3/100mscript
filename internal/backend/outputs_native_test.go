package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunProcessingExportsNRAsBinaryValues(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;5G NR",
		"48.148600;17.107700;3500;10;231;01;-100;yes",
		"48.160000;17.120000;3600;20;231;02;-120;no",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.IncludeEmptyZones = true
	cfg.AddCustomOperators = true
	cfg.CustomOperators = []CustomOperator{
		{MCC: "231", MNC: "03", PCI: "30"},
	}
	cfg.ColumnMapping = map[string]int{
		"latitude":  0,
		"longitude": 1,
		"frequency": 2,
		"pci":       3,
		"mcc":       4,
		"mnc":       5,
		"rsrp":      6,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run processing: %v", err)
	}

	contentBytes, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatalf("read zones file: %v", err)
	}
	content := string(contentBytes)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected zones output, got %d lines", len(lines))
	}

	headerLine := lines[1]
	header := strings.Split(headerLine, ";")
	nrIdx := indexOf(header, "5G NR")
	if nrIdx < 0 {
		t.Fatalf("5G NR column missing in zones header: %q", headerLine)
	}

	sawOne := false
	sawZero := false
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if nrIdx >= len(parts) {
			t.Fatalf("row missing NR column at index %d: %q", nrIdx, line)
		}
		val := strings.TrimSpace(parts[nrIdx])
		switch val {
		case "1":
			sawOne = true
		case "0":
			sawZero = true
		default:
			t.Fatalf("unexpected 5G NR export value %q in row: %q", val, line)
		}
	}

	if !sawOne || !sawZero {
		t.Fatalf("expected both NR values 1 and 0 in export, got sawOne=%v sawZero=%v", sawOne, sawZero)
	}
	if strings.Contains(content, ";yes;") || strings.Contains(content, ";no;") {
		t.Fatalf("zones export still contains textual NR values: %q", content)
	}
}

func TestRunProcessingExcludedOriginalRowsRemovesSelectedMeasurements(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_excluded_rows.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		"48.149100;17.108200;3500;11;231;01;-102",
		"48.149600;17.108700;3600;20;231;02;-105",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.ExcludedOriginalRows = []int{2}
	cfg.ColumnMapping = map[string]int{
		"latitude":  0,
		"longitude": 1,
		"frequency": 2,
		"pci":       3,
		"mcc":       4,
		"mnc":       5,
		"rsrp":      6,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run processing with excluded rows: %v", err)
	}
	if result.TotalZoneRows != 2 {
		t.Fatalf("expected 2 processed zone rows after exclusion, got %d", result.TotalZoneRows)
	}

	contentBytes, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatalf("read zones file: %v", err)
	}
	content := string(contentBytes)
	if strings.Contains(content, ";11;") {
		t.Fatalf("zones output still contains excluded row marker PCI 11: %q", content)
	}
}

func TestLoadTimeSelectorDataBuildsTimeRows(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "time_selector_input.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time",
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:03:23",
		"48.148600;17.107700;3600;20;231;02;-101;05.02.2026;10:03:25",
		"48.149600;17.108700;3600;30;231;03;-102;05.02.2026;10:04:00",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	data, err := LoadTimeSelectorData(inputPath)
	if err != nil {
		t.Fatalf("load time selector data: %v", err)
	}
	if data.TotalRows != 3 {
		t.Fatalf("expected 3 total rows, got %d", data.TotalRows)
	}
	if data.TimedRows != 3 {
		t.Fatalf("expected 3 timed rows, got %d", data.TimedRows)
	}
	if data.Strategy != "date_time" {
		t.Fatalf("expected date_time strategy, got %q", data.Strategy)
	}
	if len(data.Rows) != 3 {
		t.Fatalf("expected 3 selector rows, got %d", len(data.Rows))
	}
	if data.Rows[0].OriginalRow != 1 || data.Rows[1].OriginalRow != 2 || data.Rows[2].OriginalRow != 3 {
		t.Fatalf("unexpected original rows: %#v", data.Rows)
	}
	if !(data.MinTimeMS < data.MaxTimeMS) {
		t.Fatalf("expected min/max time range to be increasing, got min=%d max=%d", data.MinTimeMS, data.MaxTimeMS)
	}
}

func TestLoadTimeSelectorDataParsesMillisecondsInDateAndTime(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "time_selector_millis.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time",
		"48.148600;17.107700;3500;10;231;01;-100;16.02.2025;12:45:03.381",
		"48.148700;17.107800;3500;11;231;01;-101;16.02.2025;12:45:03.582",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	data, err := LoadTimeSelectorData(inputPath)
	if err != nil {
		t.Fatalf("load time selector data with milliseconds: %v", err)
	}
	if data.TimedRows != 2 {
		t.Fatalf("expected 2 timed rows, got %d", data.TimedRows)
	}
	if data.Rows[0].TimestampMS == data.Rows[1].TimestampMS {
		t.Fatalf("expected distinct timestamps for millisecond values, got %#v", data.Rows)
	}
	if got := data.Rows[1].TimestampMS - data.Rows[0].TimestampMS; got != 201 {
		t.Fatalf("expected 201ms difference, got %d", got)
	}
}

func TestLoadTimeSelectorDataPrefersDateTimeOverConflictingUTC(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "time_selector_prefer_datetime.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;UTC",
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;09:46:33.670;1770277588",
		"48.148700;17.107800;3500;11;231;01;-101;05.02.2026;09:46:34.014;1770277589",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	data, err := LoadTimeSelectorData(inputPath)
	if err != nil {
		t.Fatalf("load time selector data with conflicting UTC: %v", err)
	}
	if data.Strategy != "date_time" {
		t.Fatalf("expected date_time strategy, got %q", data.Strategy)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 selector rows, got %d", len(data.Rows))
	}

	expected := time.Date(2026, time.February, 5, 9, 46, 33, 670*int(time.Millisecond), mustLoadLocation(t, "Europe/Bratislava")).UnixMilli()
	if data.Rows[0].TimestampMS != expected {
		t.Fatalf("expected first timestamp %d from Date+Time, got %d", expected, data.Rows[0].TimestampMS)
	}
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %q: %v", name, err)
	}
	return location
}

func TestRunProcessingSegmentsStillGeneratesMissingOperatorsForObservedSegments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_missing_operator.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		"48.148600;17.109800;3600;20;231;02;-101",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.IncludeEmptyZones = true
	cfg.ColumnMapping = map[string]int{
		"latitude":  0,
		"longitude": 1,
		"frequency": 2,
		"pci":       3,
		"mcc":       4,
		"mnc":       5,
		"rsrp":      6,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run processing: %v", err)
	}

	lines := readZoneDataLines(t, result.ZonesFile)
	if len(lines) != 4 {
		t.Fatalf("expected 4 rows (2 measured + 2 generated), got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if got := countLinesContaining(lines, "# Prázdny úsek - automaticky vygenerovaný"); got != 2 {
		t.Fatalf("expected 2 generated rows for missing operators in observed segments, got %d:\n%s", got, strings.Join(lines, "\n"))
	}
}

func readZoneDataLines(t *testing.T, path string) []string {
	t.Helper()

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read zones file: %v", err)
	}

	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(contentBytes), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Riadky_v_zone;Frekvencie_v_zone") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func countLinesContaining(lines []string, needle string) int {
	count := 0
	for _, line := range lines {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}
