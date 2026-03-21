package backend

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ProcessDataNative uses the same S-JTSK grid floor for both "center" and "original"; only zone export
// chooses cell-center vs first measurement coordinates (see outputs_native.go useZoneCenter).
func TestProcessDataNative_centerAndOriginal_shareSameZonaKeys(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows: [][]string{
			{"48.148600", "17.107700", "3500", "10", "231", "01", "-100"},
			{"48.149000", "17.108000", "3600", "20", "231", "02", "-101"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	base := DefaultProcessingConfig()
	base.ZoneSizeM = 100
	base.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	var keysCenter, keysOriginal []string
	for _, mode := range []string{"center", "original"} {
		cfg := base
		cfg.ZoneMode = mode
		ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		for _, r := range ds.Rows {
			if mode == "center" {
				keysCenter = append(keysCenter, r.ZonaKey)
			} else {
				keysOriginal = append(keysOriginal, r.ZonaKey)
			}
		}
	}
	if len(keysCenter) != len(keysOriginal) {
		t.Fatalf("row count mismatch")
	}
	for i := range keysCenter {
		if keysCenter[i] != keysOriginal[i] {
			t.Fatalf("row %d: center key %q != original key %q", i, keysCenter[i], keysOriginal[i])
		}
	}
}

func TestProcessDataNative_segments_producesDistinctSegmentKeysAlongPath(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	// Two measurements hundreds of metres apart — cumulative distance crosses several segment boundaries.
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows: [][]string{
			{"48.148600", "17.107700", "3500", "10", "231", "01", "-100"},
			{"48.160000", "17.120000", "3600", "20", "231", "02", "-101"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 40
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, r := range ds.Rows {
		if !strings.HasPrefix(r.ZonaKey, "segment_") {
			t.Fatalf("expected segment_* key, got %q", r.ZonaKey)
		}
		seen[r.ZonaKey] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("expected at least 2 distinct segments along path, got %v", seen)
	}
}

func TestRunProcessing_centerVsOriginal_exportDifferentLatLon(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inPath := filepath.Join(tmp, "one_point.csv")
	// Single measurement; grid cell center (inverse of zona+size/2) almost never equals this exact WGS84 point.
	csv := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n" +
		"48.716381;21.261074;3500;10;231;01;-100\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	base := DefaultProcessingConfig()
	base.FilePath = inPath
	base.FilterPaths = []string{}
	base.ZoneSizeM = 100
	base.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	latC, lonC := zonesFirstRowLatLon(t, runZonesMode(t, base, "center"))
	latO, lonO := zonesFirstRowLatLon(t, runZonesMode(t, base, "original"))

	if latC == latO && lonC == lonO {
		t.Fatalf("center and original exports should differ for off-center measurement: got both %s,%s", latC, lonC)
	}

	lo, e1 := strconv.ParseFloat(latO, 64)
	li, e2 := strconv.ParseFloat("48.716381", 64)
	if e1 != nil || e2 != nil || mathAbs(lo-li) > 1e-6 {
		t.Fatalf("original latitude should match input measurement, got %q", latO)
	}
	loo, e3 := strconv.ParseFloat(lonO, 64)
	lin, e4 := strconv.ParseFloat("21.261074", 64)
	if e3 != nil || e4 != nil || mathAbs(loo-lin) > 1e-6 {
		t.Fatalf("original longitude should match input measurement, got %q", lonO)
	}
}

func TestRunProcessing_segments_multipleZoneRowsAlongLine(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inPath := filepath.Join(tmp, "line.csv")
	csv := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n" +
		"48.148600;17.107700;3500;10;231;01;-100\n" +
		"48.160000;17.120000;3600;20;231;02;-101\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inPath
	cfg.FilterPaths = []string{}
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 50
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	res, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	lines := readNonEmptyZonesLines(t, res.ZonesFile)
	if len(lines) < 3 {
		t.Fatalf("expected header + ≥2 data rows, got %d lines", len(lines))
	}
	lats := map[string]struct{}{}
	for _, line := range lines[1:] {
		parts := strings.Split(line, ";")
		if len(parts) < 2 {
			continue
		}
		lats[strings.TrimSpace(parts[0])] = struct{}{}
	}
	if len(lats) < 2 {
		t.Fatalf("expected distinct exported coordinates per segment, unique lat count=%d", len(lats))
	}
}

func runZonesMode(t *testing.T, base ProcessingConfig, mode string) string {
	t.Helper()
	cfg := base
	cfg.ZoneMode = mode
	res, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("mode %s: %v", mode, err)
	}
	return res.ZonesFile
}

func zonesFirstRowLatLon(t *testing.T, zonesFile string) (lat, lon string) {
	t.Helper()
	lines := readNonEmptyZonesLines(t, zonesFile)
	if len(lines) < 2 {
		t.Fatalf("zones file too short")
	}
	header := strings.Split(lines[0], ";")
	data := strings.Split(lines[1], ";")
	latIdx, lonIdx := -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "latitude":
			latIdx = i
		case "longitude":
			lonIdx = i
		}
	}
	if latIdx < 0 || lonIdx < 0 || latIdx >= len(data) || lonIdx >= len(data) {
		t.Fatalf("header=%v first=%v", header, data)
	}
	return strings.TrimSpace(data[latIdx]), strings.TrimSpace(data[lonIdx])
}

func readNonEmptyZonesLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
