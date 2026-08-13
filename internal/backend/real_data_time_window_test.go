package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture is a real ROMES export with a roughly 147-second measurement
// gap between two moving-car positions. It exercises the same shape as a tunnel:
// empty segments are first interpolated over the complete route, then a time
// window containing no measured point must remove a spatial slice of them.
func TestRealROMESData_TimeWindowCutsGeneratedGapSegments(t *testing.T) {
	inputPath := filepath.Join("..", "..", "data", "real_test", "LTE_D1_S.csv")
	requireLocalRealData(t, inputPath)
	tmpDir := t.TempDir()
	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ColumnMappingNames = map[string]string{
		"latitude":  "Latitude",
		"longitude": "Longitude",
		"frequency": "Frequency",
		"pci":       "PCI",
		"mcc":       "MCC",
		"mnc":       "MNC",
		"rsrp":      "RSRP",
		"sinr":      "SINR",
	}
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 50
	cfg.IncludeEmptyZones = true
	cfg.FilterPaths = []string{}
	cfg.OutputZonesFilePath = filepath.Join(tmpDir, "real_without_cut_zones.csv")
	cfg.OutputStatsFilePath = filepath.Join(tmpDir, "real_without_cut_stats.csv")

	withoutCut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("process real ROMES fixture without time cut: %v", err)
	}
	withoutCutContent := mustReadFile(t, withoutCut.ZonesFile)
	withoutCutEmptyRows := strings.Count(withoutCutContent, "# Prázdny úsek - automaticky vygenerovaný")
	if withoutCutEmptyRows == 0 {
		t.Fatal("real ROMES fixture did not generate the expected empty gap segments")
	}

	cfg.TimeWindows = []TimeWindow{{
		Start: "2025-01-16T07:59:30",
		End:   "2025-01-16T08:01:00",
	}}
	cfg.OutputZonesFilePath = filepath.Join(tmpDir, "real_with_cut_zones.csv")
	cfg.OutputStatsFilePath = filepath.Join(tmpDir, "real_with_cut_stats.csv")
	withCut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("process real ROMES fixture with final time cut: %v", err)
	}
	withCutContent := mustReadFile(t, withCut.ZonesFile)
	withCutEmptyRows := strings.Count(withCutContent, "# Prázdny úsek - automaticky vygenerovaný")

	if withCut.ExcludedMeasurements != 0 {
		t.Fatalf("window lies inside a real measurement gap; removed measured rows=%d, want 0", withCut.ExcludedMeasurements)
	}
	if withCut.ExcludedZones == 0 {
		t.Fatal("real measurement gap was not projected to any generated segment")
	}
	if withCutEmptyRows >= withoutCutEmptyRows {
		t.Fatalf("real final cut did not reduce empty segments: without=%d with=%d cut=%d",
			withoutCutEmptyRows, withCutEmptyRows, withCut.ExcludedZones)
	}
	t.Logf("real ROMES gap: empty segments before=%d after=%d, cut segments=%d",
		withoutCutEmptyRows, withCutEmptyRows, withCut.ExcludedZones)
}

func TestLargeRealROMESData_TimeWindowStress(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("set RUN_LARGE_REAL_DATA_TESTS=1 to process the 28 MB real ROMES fixture")
	}
	inputPath := filepath.Join("..", "..", "data", "empty_problem", "5G_NR_empty_problem.csv")
	requireLocalRealData(t, inputPath)
	tmpDir := t.TempDir()
	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ColumnMappingNames = map[string]string{
		"latitude":  "Latitude",
		"longitude": "Longitude",
		"frequency": "NR-ARFCN",
		"pci":       "PCI",
		"mcc":       "MCC",
		"mnc":       "MNC",
		"rsrp":      "SSS-RSRP",
		"sinr":      "SSS-SINR",
	}
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.IncludeEmptyZones = true
	cfg.FilterPaths = []string{}
	cfg.OutputZonesFilePath = filepath.Join(tmpDir, "large_without_cut_zones.csv")
	cfg.OutputStatsFilePath = filepath.Join(tmpDir, "large_without_cut_stats.csv")

	withoutCut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("process large real ROMES fixture without time cut: %v", err)
	}
	withoutCutEmptyRows := strings.Count(mustReadFile(t, withoutCut.ZonesFile), "# Prázdny úsek - automaticky vygenerovaný")

	cfg.TimeWindows = []TimeWindow{{
		Start: "2026-03-25T06:50:00",
		End:   "2026-03-25T06:50:10",
	}}
	cfg.OutputZonesFilePath = filepath.Join(tmpDir, "large_with_cut_zones.csv")
	cfg.OutputStatsFilePath = filepath.Join(tmpDir, "large_with_cut_stats.csv")
	withCut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("process large real ROMES fixture with final time cut: %v", err)
	}
	withCutEmptyRows := strings.Count(mustReadFile(t, withCut.ZonesFile), "# Prázdny úsek - automaticky vygenerovaný")

	if withCut.ExcludedMeasurements == 0 || withCut.ExcludedZones == 0 {
		t.Fatalf("large real window did not cut measurements and segments: %+v", withCut)
	}
	if withCutEmptyRows >= withoutCutEmptyRows {
		t.Fatalf("large real final cut did not reduce empty output: before=%d after=%d", withoutCutEmptyRows, withCutEmptyRows)
	}
	t.Logf("large ROMES file: removed measurements=%d, cut segments=%d, empty rows before=%d after=%d",
		withCut.ExcludedMeasurements, withCut.ExcludedZones, withoutCutEmptyRows, withCutEmptyRows)
}

func requireLocalRealData(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("real data fixture is local-only and not available: %s", path)
		}
		t.Fatalf("inspect real data fixture %q: %v", path, err)
	}
}
