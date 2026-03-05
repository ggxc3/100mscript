package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProcessingSegmentsDoesNotGenerateSyntheticGapSegments(t *testing.T) {
	t.Parallel()

	csvPath := writeTestCSV(t, "Latitude;Longitude;Frequency;PCI;MCC;MNC;RSRP\n"+
		"48.148600;17.107700;1800;1;231;01;-90\n"+
		"48.148600;17.113700;1800;1;231;01;-91\n")

	result, err := RunProcessing(context.Background(), testProcessingConfig(csvPath))
	if err != nil {
		t.Fatalf("RunProcessing returned error: %v", err)
	}

	lines := readZoneDataLines(t, result.ZonesFile)
	if len(lines) != 2 {
		t.Fatalf("expected 2 measured segment rows, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if got := countLinesContaining(lines, "# Prázdny úsek - automaticky vygenerovaný"); got != 0 {
		t.Fatalf("expected no synthetic gap segments, got %d:\n%s", got, strings.Join(lines, "\n"))
	}
}

func TestRunProcessingSegmentsStillGeneratesMissingOperatorsForObservedSegments(t *testing.T) {
	t.Parallel()

	csvPath := writeTestCSV(t, "Latitude;Longitude;Frequency;PCI;MCC;MNC;RSRP\n"+
		"48.148600;17.107700;1800;1;231;01;-90\n"+
		"48.148600;17.109800;1800;2;231;02;-91\n")

	result, err := RunProcessing(context.Background(), testProcessingConfig(csvPath))
	if err != nil {
		t.Fatalf("RunProcessing returned error: %v", err)
	}

	lines := readZoneDataLines(t, result.ZonesFile)
	if len(lines) != 4 {
		t.Fatalf("expected 4 rows (2 measured + 2 generated), got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if got := countLinesContaining(lines, "# Prázdny úsek - automaticky vygenerovaný"); got != 2 {
		t.Fatalf("expected 2 generated empty rows for missing operators, got %d:\n%s", got, strings.Join(lines, "\n"))
	}
}

func writeTestCSV(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test CSV: %v", err)
	}
	return path
}

func testProcessingConfig(csvPath string) ProcessingConfig {
	return ProcessingConfig{
		FilePath: csvPath,
		ColumnMapping: map[string]int{
			"latitude":  0,
			"longitude": 1,
			"frequency": 2,
			"pci":       3,
			"mcc":       4,
			"mnc":       5,
			"rsrp":      6,
		},
		ZoneMode:          "segments",
		ZoneSizeM:         100,
		RSRPThreshold:     -110,
		SINRThreshold:     -5,
		IncludeEmptyZones: true,
		FilterPaths:       []string{},
	}
}

func readZoneDataLines(t *testing.T, path string) []string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read zone output: %v", err)
	}

	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
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
