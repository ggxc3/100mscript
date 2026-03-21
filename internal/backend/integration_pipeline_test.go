package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProcessing_autoDiscoverFiltersFromWorkingDir(t *testing.T) {
	tmp := t.TempDir()
	fdir := filepath.Join(tmp, "filters")
	if err := os.MkdirAll(fdir, 0o755); err != nil {
		t.Fatal(err)
	}
	rule := `<Query>"MCC" = 231; ("RSRP" = -100)</Query>`
	if err := os.WriteFile(filepath.Join(fdir, "match.txt"), []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}

	inPath := filepath.Join(tmp, "in.csv")
	csv := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n" +
		"48.1486;17.1077;3500;10;231;01;-100\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(tmp)

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inPath
	cfg.FilterPaths = nil
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	_, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunProcessing_explicitFilterPath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rulePath := filepath.Join(tmp, "rule.txt")
	if err := os.WriteFile(rulePath, []byte(`<Query>"MCC" = 231; ("MNC" = 1)</Query>`), 0o644); err != nil {
		t.Fatal(err)
	}
	inPath := filepath.Join(tmp, "in.csv")
	csv := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n" +
		"48.1486;17.1077;3500;10;231;01;-100\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inPath
	cfg.FilterPaths = []string{rulePath}
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	zb, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zb), "231") {
		t.Fatalf("expected zones output to contain operator data")
	}
}

func TestFilterRowsByNRYesNative_keepsOnlyYes(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"5G NR", "x"},
		Rows: [][]string{
			{"yes", "a"},
			{"no", "b"},
			{"", "c"},
		},
		FileInfo: CSVFileInfo{},
	}
	out, err := filterRowsByNRYesNative(data, "5G NR")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0][0] != "yes" {
		t.Fatalf("got %#v err=%v", out.Rows, err)
	}
}

func TestRunProcessing_mobileModeEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	fiveG := filepath.Join(tmp, "5g.csv")
	lte := filepath.Join(tmp, "lte.csv")

	fiveGContent := "" +
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;5G NR\n" +
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00;\n"
	lteContent := "" +
		"MCC;MNC;5G NR;Date;Time\n" +
		"231;01;yes;05.02.2026;10:00:00\n"

	if err := os.WriteFile(fiveG, []byte(fiveGContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lte, []byte(lteContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = fiveG
	cfg.FilterPaths = []string{}
	cfg.MobileModeEnabled = true
	cfg.MobileLTEFilePath = lte
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ZonesFile, "_mobile_zones.csv") {
		t.Fatalf("expected mobile suffix in zones path: %s", result.ZonesFile)
	}
	zb, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zb), "231") {
		t.Fatalf("expected zones content")
	}
}

func TestFilterRowsByNRYesNative_emptyAfterFilter(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns:  []string{"5G NR"},
		Rows:     [][]string{{"no"}},
		FileInfo: CSVFileInfo{},
	}
	_, err := filterRowsByNRYesNative(data, "5G NR")
	if err == nil {
		t.Fatal("expected error")
	}
}
