package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyTimeWindowsAfterSegmentation_RemovesMeasuredAndSyntheticSegments(t *testing.T) {
	t0 := mustParseTimeWindowTestMillis(t, "2026-02-05T10:00:00")
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{TimestampMS: t0, HasTimestamp: true, SourceTrackID: "0", SegmentDistanceM: 0, ZonaKey: "segment_0"},
			{TimestampMS: t0 + 10_000, HasTimestamp: true, SourceTrackID: "0", SegmentDistanceM: 500, ZonaKey: "segment_5"},
			{TimestampMS: t0 + 20_000, HasTimestamp: true, SourceTrackID: "0", SegmentDistanceM: 1000, ZonaKey: "segment_10"},
		},
		SegmentMeta: segmentMetaRange(0, 10),
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.IncludeEmptyZones = true

	out, summary, err := applyTimeWindowsAfterSegmentation(ds, []TimeWindow{{
		Start: "2026-02-05T10:00:05.000",
		End:   "2026-02-05T10:00:15.000",
	}}, cfg)
	if err != nil {
		t.Fatalf("apply final time cut: %v", err)
	}
	if summary.RemovedMeasurements != 1 {
		t.Fatalf("removed measurements=%d, want 1", summary.RemovedMeasurements)
	}
	if summary.RemovedZones != 6 {
		t.Fatalf("removed zones=%d, want 6 (segments 2..7)", summary.RemovedZones)
	}
	if len(out.Rows) != 2 || out.Rows[0].ZonaKey != "segment_0" || out.Rows[1].ZonaKey != "segment_10" {
		t.Fatalf("unexpected remaining measured rows: %#v", out.Rows)
	}
	for id := 2; id <= 7; id++ {
		if _, exists := out.SegmentMeta[id]; exists {
			t.Fatalf("synthetic segment %d survived final cut", id)
		}
	}
	for _, id := range []int{0, 1, 8, 9, 10} {
		if _, exists := out.SegmentMeta[id]; !exists {
			t.Fatalf("segment %d outside final cut was removed", id)
		}
	}
}

func TestApplyTimeWindowsAfterSegmentation_InterpolatesAcrossNoMeasurementGap(t *testing.T) {
	t0 := mustParseTimeWindowTestMillis(t, "2026-02-05T10:00:00")
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{TimestampMS: t0, HasTimestamp: true, SourceTrackID: "0", SegmentDistanceM: 0, ZonaKey: "segment_0"},
			{TimestampMS: t0 + 20_000, HasTimestamp: true, SourceTrackID: "0", SegmentDistanceM: 1000, ZonaKey: "segment_10"},
		},
		SegmentMeta: segmentMetaRange(0, 10),
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.IncludeEmptyZones = true

	out, summary, err := applyTimeWindowsAfterSegmentation(ds, []TimeWindow{{
		Start: "2026-02-05T10:00:05.000",
		End:   "2026-02-05T10:00:15.000",
	}}, cfg)
	if err != nil {
		t.Fatalf("apply final time cut: %v", err)
	}
	if summary.RemovedMeasurements != 0 {
		t.Fatalf("expected no measured endpoint to be removed, got %d", summary.RemovedMeasurements)
	}
	if summary.RemovedZones != 6 {
		t.Fatalf("expected interpolated synthetic segments 2..7 to be removed, got %d", summary.RemovedZones)
	}
	for id := 2; id <= 7; id++ {
		if _, exists := out.SegmentMeta[id]; exists {
			t.Fatalf("no-measurement tunnel segment %d survived final cut", id)
		}
	}
}

func TestApplyTimeWindowsAfterSegmentation_ProjectsEachSourceTrackIndependently(t *testing.T) {
	t0 := mustParseTimeWindowTestMillis(t, "2026-02-05T10:00:00")
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{TimestampMS: t0, HasTimestamp: true, SourceTrackID: "a", SegmentDistanceM: 0, ZonaKey: "segment_0"},
			{TimestampMS: t0 + 10_000, HasTimestamp: true, SourceTrackID: "a", SegmentDistanceM: 500, ZonaKey: "segment_5"},
			{TimestampMS: t0, HasTimestamp: true, SourceTrackID: "b", SegmentDistanceM: 600, ZonaKey: "segment_6"},
			{TimestampMS: t0 + 10_000, HasTimestamp: true, SourceTrackID: "b", SegmentDistanceM: 1000, ZonaKey: "segment_10"},
		},
		SegmentMeta: segmentMetaRange(0, 10),
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.IncludeEmptyZones = true

	out, summary, err := applyTimeWindowsAfterSegmentation(ds, []TimeWindow{{
		Start: "2026-02-05T10:00:02.000",
		End:   "2026-02-05T10:00:04.000",
	}}, cfg)
	if err != nil {
		t.Fatalf("apply final time cut: %v", err)
	}
	if summary.RemovedZones != 4 {
		t.Fatalf("removed zones=%d, want independent ranges {1,2} and {6,7}", summary.RemovedZones)
	}
	for _, id := range []int{1, 2, 6, 7} {
		if _, exists := out.SegmentMeta[id]; exists {
			t.Fatalf("track-specific segment %d survived cut", id)
		}
	}
	for _, id := range []int{0, 3, 4, 5, 8, 9, 10} {
		if _, exists := out.SegmentMeta[id]; !exists {
			t.Fatalf("segment %d between independent track ranges was over-cut", id)
		}
	}
}

func TestApplyTimeWindowsAfterSegmentation_NonSegmentModeFiltersAtEnd(t *testing.T) {
	t0 := mustParseTimeWindowTestMillis(t, "2026-02-05T10:00:00")
	ds := &ProcessedDataset{Rows: []ProcessedRow{
		{TimestampMS: t0, HasTimestamp: true, ZonaKey: "a"},
		{TimestampMS: t0 + 5_500, HasTimestamp: true, ZonaKey: "b"},
		{TimestampMS: t0 + 10_000, HasTimestamp: true, ZonaKey: "c"},
	}}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"

	out, summary, err := applyTimeWindowsAfterSegmentation(ds, []TimeWindow{{
		Start: "2026-02-05T10:00:05",
		End:   "2026-02-05T10:00:05",
	}}, cfg)
	if err != nil {
		t.Fatalf("apply final time cut: %v", err)
	}
	if summary.RemovedMeasurements != 1 || summary.RemovedZones != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(out.Rows) != 2 || out.Rows[0].ZonaKey != "a" || out.Rows[1].ZonaKey != "c" {
		t.Fatalf("unexpected remaining rows: %#v", out.Rows)
	}
}

func TestRunProcessing_TimeWindowsCutFinalSegmentsIncludingEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "tunnel.csv")
	zonesWithoutCut := filepath.Join(tmpDir, "without_cut_zones.csv")
	statsWithoutCut := filepath.Join(tmpDir, "without_cut_stats.csv")
	zonesWithCut := filepath.Join(tmpDir, "with_cut_zones.csv")
	statsWithCut := filepath.Join(tmpDir, "with_cut_stats.csv")
	inputCSV := strings.Join([]string{
		"Date;Time;UTC;latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"05.02.2026;10:00:00.000;;48.148600;17.107700;3500;10;231;01;-100",
		"05.02.2026;10:00:10.000;;48.148600;17.114500;3500;10;231;01;-101",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input CSV: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 50
	cfg.IncludeEmptyZones = true
	cfg.FilterPaths = []string{}
	cfg.OutputZonesFilePath = zonesWithoutCut
	cfg.OutputStatsFilePath = statsWithoutCut
	cfg.ColumnMapping = map[string]int{
		"latitude": 3, "longitude": 4, "frequency": 5, "pci": 6,
		"mcc": 7, "mnc": 8, "rsrp": 9,
	}

	_, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run without final cut: %v", err)
	}
	cfg.TimeWindows = []TimeWindow{{Start: "2026-02-05T10:00:03", End: "2026-02-05T10:00:07"}}
	cfg.OutputZonesFilePath = zonesWithCut
	cfg.OutputStatsFilePath = statsWithCut
	withCut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run with final cut: %v", err)
	}

	if withCut.ExcludedMeasurements != 0 {
		t.Fatalf("only synthetic middle segments should be cut, removed measurements=%d", withCut.ExcludedMeasurements)
	}
	if withCut.ExcludedZones == 0 {
		t.Fatal("expected synthetic middle segments to be reported as cut")
	}
	content, err := os.ReadFile(zonesWithCut)
	if err != nil {
		t.Fatalf("read zones output: %v", err)
	}
	withCutEmptyRows := strings.Count(string(content), "# Prázdny úsek - automaticky vygenerovaný")
	withoutCutEmptyRows := strings.Count(mustReadFile(t, zonesWithoutCut), "# Prázdny úsek - automaticky vygenerovaný")
	if withCutEmptyRows >= withoutCutEmptyRows {
		t.Fatal("final cut did not remove generated empty-segment output rows")
	}
}

func TestRunProcessing_TimeWindowsStillFilterCenterModeAtEnd(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "center.csv")
	inputCSV := strings.Join([]string{
		"Date;Time;latitude;longitude;frequency;pci;mcc;mnc;rsrp",
		"05.02.2026;10:00:00.000;48.148600;17.107700;3500;10;231;01;-100",
		"05.02.2026;10:00:05.500;48.149600;17.108700;3500;11;231;01;-101",
		"05.02.2026;10:00:10.000;48.150600;17.109700;3500;12;231;01;-102",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input CSV: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 50
	cfg.FilterPaths = []string{}
	cfg.OutputZonesFilePath = filepath.Join(tmpDir, "center_zones.csv")
	cfg.OutputStatsFilePath = filepath.Join(tmpDir, "center_stats.csv")
	cfg.TimeWindows = []TimeWindow{{Start: "2026-02-05T10:00:05", End: "2026-02-05T10:00:05"}}
	cfg.ColumnMapping = map[string]int{
		"latitude": 2, "longitude": 3, "frequency": 4, "pci": 5,
		"mcc": 6, "mnc": 7, "rsrp": 8,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run center mode with final time cut: %v", err)
	}
	if result.ExcludedMeasurements != 1 || result.TotalZoneRows != 2 {
		t.Fatalf("unexpected center-mode result: %+v", result)
	}
	content := mustReadFile(t, result.ZonesFile)
	if strings.Contains(content, ";11;") {
		t.Fatalf("center output retained the time-excluded PCI 11 row: %s", content)
	}
}

func mustParseTimeWindowTestMillis(t *testing.T, value string) int64 {
	t.Helper()
	ms, ok := parseDateTimeToMillis(value)
	if !ok {
		t.Fatalf("cannot parse test timestamp %q", value)
	}
	return ms
}

func segmentMetaRange(first, last int) map[int]Point {
	out := make(map[int]Point, last-first+1)
	for id := first; id <= last; id++ {
		out[id] = Point{A: float64(id) * 100, B: 0}
	}
	return out
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(content)
}
