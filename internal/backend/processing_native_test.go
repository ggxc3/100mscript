package backend

import (
	"context"
	"strings"
	"testing"
)

func TestProcessDataNative_segmentModeKeys(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows: [][]string{
			{"48.1486", "17.1077", "3500", "1", "231", "01", "-100"},
			{"48.1490", "17.1080", "3500", "1", "231", "01", "-101"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100_000
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Rows) != 2 {
		t.Fatalf("rows: %d", len(ds.Rows))
	}
	if !strings.HasPrefix(ds.Rows[0].ZonaKey, "segment_") {
		t.Fatalf("expected segment key, got %q", ds.Rows[0].ZonaKey)
	}
}

func TestProcessDataNative_invalidLatitudeError(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns:  []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows:     [][]string{{"nope", "17.1", "3500", "1", "231", "01", "-100"}},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	_, err = ProcessDataNative(context.Background(), data, cfg, tr)
	if err == nil || !strings.Contains(err.Error(), "latitude") {
		t.Fatalf("expected latitude error, got %v", err)
	}
}

func TestCalculateZoneStatsNative_picksHigherRSRPFrequency(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	// Same zone cell (center grid), same operator and PCI, two frequencies — better RSRP wins.
	zk := "1000_2000"
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3600", RSRP: -120, ZonaX: 1000, ZonaY: 2000},
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500", RSRP: -80, ZonaX: 1000, ZonaY: 2000},
		},
		Columns: []string{"x"},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.RSRPThreshold = -110
	stats, err := CalculateZoneStatsNative(context.Background(), ds, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].NajcastejsiaFrekvencia != "3500" {
		t.Fatalf("expected freq 3500 (better RSRP), got %q avg=%v", stats[0].NajcastejsiaFrekvencia, stats[0].RSRPAvg)
	}
}

func TestProcessDataNative_nilData(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	_, err = ProcessDataNative(context.Background(), nil, cfg, tr)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCalculateZoneStatsNative_nilDataset(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	_, err = CalculateZoneStatsNative(context.Background(), nil, DefaultProcessingConfig(), tr)
	if err == nil {
		t.Fatal("expected error")
	}
}
