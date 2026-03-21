package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

func TestNormalizePandasFloatString_integerAndFractional(t *testing.T) {
	t.Parallel()

	// Whole-number SINR uses one decimal place (pandas-style).
	if got := normalizePandasFloatString("5"); got != "5.0" {
		t.Fatalf("integer: got %q", got)
	}
	if got := normalizePandasFloatString("-3.25"); got != "-3.25" {
		t.Fatalf("fractional: got %q", got)
	}
	if got := normalizePandasFloatString("n/a"); got != "n/a" {
		t.Fatalf("non-numeric passthrough: got %q", got)
	}
}

func TestEnsureOriginalExcelRowColumn_fillsEmptyAndPadsRow(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"a", "original_excel_row", "b"},
		Rows: [][]string{
			{"x", "", "y"},
			{"x2", "99", "y2"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 2},
	}
	out, err := ensureOriginalExcelRowColumn(data)
	if err != nil {
		t.Fatal(err)
	}
	idx := out.columnIndexByName("original_excel_row")
	if out.Rows[0][idx] != "3" { // row 0 -> line 2+1 = 3
		t.Fatalf("expected filled 3, got %q", out.Rows[0][idx])
	}
	if out.Rows[1][idx] != "99" {
		t.Fatalf("expected preserved 99, got %q", out.Rows[1][idx])
	}
}

func TestAssignSequentialOriginalExcelRows_overwritesExistingColumn(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"v", "original_excel_row"},
		Rows: [][]string{
			{"a", "999"},
			{"b", "998"},
		},
		FileInfo: CSVFileInfo{},
	}
	out, err := assignSequentialOriginalExcelRows(data)
	if err != nil {
		t.Fatal(err)
	}
	idx := out.columnIndexByName("original_excel_row")
	if out.Rows[0][idx] != "1" || out.Rows[1][idx] != "2" {
		t.Fatalf("expected 1,2 got %q %q", out.Rows[0][idx], out.Rows[1][idx])
	}
}

func TestLoadCSVFile_nonUTF8Bytes_usesFallbackDecoder(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "latinish.csv")
	// Invalid UTF-8 (lone 0x9A) but decodable as Latin-1; ASCII body keeps tabular shape.
	line := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n48.1;17.1;3500;1;231;01;-100\n"
	raw := []byte(line)
	raw[0] = 0x9a // was 'l' — invalid UTF-8 sequence
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data.FileInfo.Encoding == "utf-8" {
		t.Fatalf("expected non-utf-8 encoding, got %q", data.FileInfo.Encoding)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("rows: %d", len(data.Rows))
	}
}

func TestLoadCSVFile_windows1250EncodedSlovakHeader(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "cp1250.csv")
	// Header contains š (0xE9 in Windows-1250) — not valid UTF-8 as raw bytes from CP1250 encoder.
	utf8Text := "latšitude;longitude;frequency;pci;mcc;mnc;rsrp\n48.1;17.1;3500;1;231;01;-100\n"
	enc := charmap.Windows1250.NewEncoder()
	raw, err := enc.Bytes([]byte(utf8Text))
	if err != nil {
		t.Fatal(err)
	}
	if utf8.Valid(raw) {
		t.Fatal("expected non-UTF-8 bytes for this fixture")
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data.FileInfo.Encoding == "utf-8" {
		t.Fatalf("expected fallback decoder, got utf-8")
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 row")
	}
}

func TestCalculateZoneStatsNative_sinrGoodAndBadCategories(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	// One stat per (ZonaKey, OperatorKey); use two distinct grid cells.
	good := ProcessedRow{
		ZonaKey: "1000_2000", OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500",
		ZonaX: 1000, ZonaY: 2000, NRValue: "",
		RSRP: -80, SINR: 5, HasSINR: true,
	}
	bad := ProcessedRow{
		ZonaKey: "1100_2000", OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500",
		ZonaX: 1100, ZonaY: 2000, NRValue: "",
		RSRP: -80, SINR: -10, HasSINR: true,
	}

	ds := &ProcessedDataset{
		Rows:    []ProcessedRow{good, bad},
		Columns: []string{"x"},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.RSRPThreshold = -110
	cfg.SINRThreshold = -5

	stats, err := CalculateZoneStatsNative(context.Background(), ds, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	var sawGood, sawBad bool
	for _, s := range stats {
		if !s.HasSINRAvg {
			t.Fatalf("expected SINR on all stats, got %+v", s)
		}
		switch s.RSRPKategoria {
		case "RSRP_GOOD":
			sawGood = true
		case "RSRP_BAD":
			sawBad = true
		}
	}
	if !sawGood || !sawBad {
		t.Fatalf("expected both GOOD and BAD SINR categories, got %+v", stats)
	}
}

func TestProcessDataNative_withSinrColumn(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"},
		Rows: [][]string{
			{"48.148600", "17.107700", "3500", "10", "231", "01", "-100", "4.5"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6, "sinr": 7,
	}
	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if !ds.Rows[0].HasSINR || ds.Rows[0].SINR != 4.5 {
		t.Fatalf("SINR not parsed: %+v", ds.Rows[0])
	}
}

func TestBuildTimeMillisSeriesNative_utcNumericLargeEpochMs(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"UTC"},
		Rows: [][]string{
			{"1770277588000"},
		},
		FileInfo: CSVFileInfo{},
	}
	series, strategy := buildTimeMillisSeriesNative(data, 0, -1, -1)
	if strategy != "utc_numeric" {
		t.Fatalf("strategy %q", strategy)
	}
	if !series.Valid[0] || series.Values[0] != 1770277588000 {
		t.Fatalf("values: valid=%v v=%d", series.Valid[0], series.Values[0])
	}
}

func TestBuildTimeSelectorSeriesNative_fallsBackToUTCWhenDateTimeMissing(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "Date", "Time", "UTC"},
		Rows: [][]string{
			{"48.1", "17.1", "3500", "1", "231", "01", "-100", "05.02.2026", "10:00:00", ""},
			{"48.2", "17.2", "3500", "1", "231", "01", "-101", "", "", "1770277589000"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	utcCol := findColumnNameNative(data.Columns, []string{"UTC"})
	dateCol := findColumnNameNative(data.Columns, []string{"Date"})
	timeCol := findColumnNameNative(data.Columns, []string{"Time"})
	utcIdx := data.columnIndexByName(utcCol)
	dateIdx := data.columnIndexByName(dateCol)
	timeIdx := data.columnIndexByName(timeCol)

	series, strategy := buildTimeSelectorSeriesNative(data, utcIdx, dateIdx, timeIdx)
	if strategy != "date_time_with_utc_fallback" {
		t.Fatalf("strategy %q", strategy)
	}
	if !series.Valid[0] || !series.Valid[1] {
		t.Fatalf("both rows should have time: valid=%v %v", series.Valid[0], series.Valid[1])
	}
}

func TestRunProcessing_centerMode_setsCoverageBoundingFields(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p1 := filepath.Join(tmp, "a.csv")
	p2 := filepath.Join(tmp, "b.csv")
	hdr := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n"
	// Two distinct grid cells (far apart in WGS84) so min/max range is non-trivial.
	if err := os.WriteFile(p1, []byte(hdr+"48.148600;17.107700;3500;10;231;01;-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte(hdr+"48.200000;17.200000;3600;20;231;02;-101\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = p1
	cfg.InputFilePaths = []string{p1, p2}
	cfg.FilterPaths = []string{}
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	res, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.MinX == nil || res.MaxX == nil || res.MinY == nil || res.MaxY == nil {
		t.Fatalf("expected bbox fields, got %+v", res)
	}
	if res.TheoreticalTotalZones == nil || res.CoveragePercent == nil {
		t.Fatalf("expected coverage metrics, got ttz=%v cp=%v", res.TheoreticalTotalZones, res.CoveragePercent)
	}
}

func TestBuildOperatorTemplatesForEmptyRows_normalizesSinrWhenPresent(t *testing.T) {
	t.Parallel()

	ds := &ProcessedDataset{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"},
		Rows: []ProcessedRow{
			{Raw: []string{"48.1", "17.1", "3500", "10", "231", "01", "-100", "3.75"}},
		},
		FileInfo: CSVFileInfo{OriginalHeader: strings.Join([]string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"}, ";")},
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6, "sinr": 7,
	}
	layout := buildZoneExportLayout(ds, cfg)
	if !layout.hasSINRCol {
		t.Fatal("expected SINR in layout")
	}
	zs := []ZoneStat{{MCC: "231", MNC: "01", OperatorKey: "231_01"}}
	_, templateRows := buildOperatorTemplatesForEmptyRows(ds, zs, layout)
	row := templateRows["231_01"]
	if len(row) == 0 {
		t.Fatal("missing template row")
	}
	sinIdx := layout.exportHeaderToIdx[layout.sinrCol]
	if sinIdx < 0 || sinIdx >= len(row) {
		t.Fatalf("sinr index")
	}
	if row[sinIdx] == "" {
		t.Fatalf("expected normalized SINR in template")
	}
}
