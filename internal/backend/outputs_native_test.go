package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunProcessingSkipRowsWithoutGPSToggle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_missing_gps.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		";17.120000;3600;20;231;01;-105",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	baseCfg := DefaultProcessingConfig()
	baseCfg.FilePath = inputPath
	baseCfg.ZoneMode = "center"
	baseCfg.ZoneSizeM = 100
	baseCfg.FilterPaths = []string{}
	baseCfg.ColumnMapping = map[string]int{
		"latitude":  0,
		"longitude": 1,
		"frequency": 2,
		"pci":       3,
		"mcc":       4,
		"mnc":       5,
		"rsrp":      6,
	}

	t.Run("default_false_keeps_previous_behavior", func(t *testing.T) {
		cfg := baseCfg
		cfg.SkipRowsWithoutGPS = false

		_, err := RunProcessing(context.Background(), cfg)
		if err == nil {
			t.Fatalf("expected error for missing latitude when skip flag is false")
		}
		if !strings.Contains(err.Error(), "invalid latitude") {
			t.Fatalf("expected invalid latitude error, got: %v", err)
		}
	})

	t.Run("true_skips_rows_without_gps", func(t *testing.T) {
		cfg := baseCfg
		cfg.SkipRowsWithoutGPS = true

		result, err := RunProcessing(context.Background(), cfg)
		if err != nil {
			t.Fatalf("run processing with skip flag: %v", err)
		}
		if result.TotalZoneRows != 1 {
			t.Fatalf("expected exactly one processed zone row, got %d", result.TotalZoneRows)
		}

		contentBytes, err := os.ReadFile(result.ZonesFile)
		if err != nil {
			t.Fatalf("read zones file: %v", err)
		}
		content := string(contentBytes)
		if strings.Contains(content, ";20;") {
			t.Fatalf("zones output still contains row without GPS coordinates: %q", content)
		}
	})
}

func TestRunProcessingSkipRowsWithoutGPSDoesNotGenerateSyntheticGapSegments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_gap.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		";17.110700;3500;10;231;01;-101",
		";17.111700;3500;10;231;01;-102",
		"48.148600;17.113700;3500;10;231;01;-103",
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
	cfg.SkipRowsWithoutGPS = true
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
		t.Fatalf("run processing with skip flag: %v", err)
	}

	lines := readZoneDataLines(t, result.ZonesFile)
	if len(lines) != 2 {
		t.Fatalf("expected only 2 measured segment rows after skipping no-GPS gap, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if got := countLinesContaining(lines, "# Prázdny úsek - automaticky vygenerovaný"); got != 0 {
		t.Fatalf("expected no synthetic gap segments, got %d:\n%s", got, strings.Join(lines, "\n"))
	}
}

func TestRunProcessingSkipRowsWithoutGPSAndIncludeEmptyZonesDoesNotReAddTunnelSegments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_tunnel_multi_operator.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		"48.148600;17.107700;3600;20;231;02;-101",
		";17.110700;3500;10;231;01;-102",
		";17.110700;3600;20;231;02;-103",
		"48.148600;17.114700;3500;10;231;01;-104",
		"48.148600;17.114700;3600;20;231;02;-105",
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
	cfg.SkipRowsWithoutGPS = true
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
		t.Fatalf("run processing with skip+empty-zones flags: %v", err)
	}

	lines := readZoneDataLines(t, result.ZonesFile)
	if len(lines) != 4 {
		t.Fatalf("expected only 4 measured rows (2 segments x 2 operators), got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if got := countLinesContaining(lines, "# Prázdny úsek - automaticky vygenerovaný"); got != 0 {
		t.Fatalf("expected no generated tunnel rows when skip rows without GPS is enabled, got %d:\n%s", got, strings.Join(lines, "\n"))
	}

	statsBytes, err := os.ReadFile(result.StatsFile)
	if err != nil {
		t.Fatalf("read stats file: %v", err)
	}
	statsLines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(statsBytes), "\r\n", "\n")), "\n")
	if len(statsLines) != 3 {
		t.Fatalf("expected stats header + 2 operator lines, got %d lines:\n%s", len(statsLines), strings.Join(statsLines, "\n"))
	}
	for _, line := range statsLines[1:] {
		parts := strings.Split(line, ";")
		if len(parts) < 4 {
			t.Fatalf("unexpected stats line format: %q", line)
		}
		bad := strings.TrimSpace(parts[3])
		if bad != "0" {
			t.Fatalf("expected 0 bad zones for each operator (no synthetic tunnel zones), got bad=%q in line: %q", bad, line)
		}
	}
}

func TestRunProcessingSkipRowsWithoutGPSRestartsSegmentsAfterGap(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_gap_coordinates.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		";;3500;10;231;01;-101",
		"48.148600;17.114321;3500;10;231;01;-103",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.SkipRowsWithoutGPS = true
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
		t.Fatalf("run processing with skip flag: %v", err)
	}

	coords := readZoneCoordinates(t, result.ZonesFile)
	if len(coords) != 2 {
		t.Fatalf("expected 2 measured segment rows, got %d", len(coords))
	}
	if countCoordinate(coords, "48.148600", "17.107700") != 1 {
		t.Fatalf("expected one exported point at the first measured coordinate, got: %#v", coords)
	}
	if countCoordinate(coords, "48.148600", "17.114321") != 1 {
		t.Fatalf("expected post-gap point to keep the real measured coordinate instead of an interpolated tunnel point, got: %#v", coords)
	}
}

func TestRunProcessingSkipRowsWithoutGPSPreservesGapsAcrossFilters(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input_gap_filters.csv")
	filterPath := filepath.Join(tmpDir, "duplicate_operator.txt")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"48.148600;17.107700;3500;10;231;01;-100",
		";;3500;10;231;01;-101",
		"48.148600;17.114321;3500;10;231;01;-103",
	}, "\n") + "\n"
	filterRule := `<Query>("MCC" = 231 AND "MNC" = 1 AND "MNC" = 2);("Frequency" = 3500 AND "MCC" = 231 AND "MNC" = 1)</Query>`
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}
	if err := os.WriteFile(filterPath, []byte(filterRule), 0o644); err != nil {
		t.Fatalf("write filter rule: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{filterPath}
	cfg.SkipRowsWithoutGPS = true
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
		t.Fatalf("run processing with skip flag + filters: %v", err)
	}

	coords := readZoneCoordinates(t, result.ZonesFile)
	if len(coords) != 4 {
		t.Fatalf("expected 4 measured segment rows after duplicating operators, got %d", len(coords))
	}
	if countCoordinate(coords, "48.148600", "17.114321") != 2 {
		t.Fatalf("expected both filtered operator rows after the gap to keep the real measured coordinate, got: %#v", coords)
	}
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

type zoneCoordinate struct {
	Latitude  string
	Longitude string
}

func readZoneCoordinates(t *testing.T, path string) []zoneCoordinate {
	t.Helper()

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read zones file: %v", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(contentBytes), "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected zones output: %q", string(contentBytes))
	}

	header := strings.Split(strings.TrimSpace(lines[1]), ";")
	latIdx := indexOf(header, "latitude")
	lonIdx := indexOf(header, "longitude")
	if latIdx < 0 || lonIdx < 0 {
		t.Fatalf("latitude/longitude columns not found in header: %q", lines[1])
	}

	coords := []zoneCoordinate{}
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if latIdx >= len(parts) || lonIdx >= len(parts) {
			t.Fatalf("row missing latitude/longitude columns: %q", line)
		}
		coords = append(coords, zoneCoordinate{
			Latitude:  strings.TrimSpace(parts[latIdx]),
			Longitude: strings.TrimSpace(parts[lonIdx]),
		})
	}
	return coords
}

func countCoordinate(coords []zoneCoordinate, latitude, longitude string) int {
	count := 0
	for _, coord := range coords {
		if coord.Latitude == latitude && coord.Longitude == longitude {
			count++
		}
	}
	return count
}
