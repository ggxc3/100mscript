package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitSemicolonColumns_trimsTrailingEmpty(t *testing.T) {
	t.Parallel()

	cols := splitSemicolonColumns("a;b;c;;")
	if len(cols) != 3 || cols[2] != "c" {
		t.Fatalf("got %#v", cols)
	}
}

func TestSplitSemicolonColumns_supportsQuotedSemicolons(t *testing.T) {
	t.Parallel()

	cols := splitSemicolonColumns(`a;"meter;rear";"value";`)
	if len(cols) != 3 || cols[1] != "meter;rear" || cols[2] != "value" {
		t.Fatalf("got %#v", cols)
	}
}

func TestMakeUniqueColumnNames_duplicateHeaders(t *testing.T) {
	t.Parallel()

	out := makeUniqueColumnNames([]string{"x", "x", "x"})
	if out[0] != "x" || out[1] != "x_2" || out[2] != "x_3" {
		t.Fatalf("got %#v", out)
	}
}

func TestLoadCSVFile_utf8TwoDataRows(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "in.csv")
	content := "latitude;longitude;frequency;pci;mcc;mnc;rsrp\n" +
		"48.1;17.1;3500;1;231;01;-100\n" +
		"48.2;17.2;3600;2;231;02;-101\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Columns) < 7 || len(data.Rows) != 2 {
		t.Fatalf("cols=%d rows=%d", len(data.Columns), len(data.Rows))
	}
	if data.FileInfo.Encoding != "utf-8" {
		t.Fatalf("encoding: %q", data.FileInfo.Encoding)
	}
	if data.InputRadioTech != InputRadioTechUnknown {
		t.Fatalf("generic frequency column => unknown tech, got %q", data.InputRadioTech)
	}
}

func TestLoadCSVFile_InputRadioTechNR(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "nr.csv")
	content := "latitude;longitude;NR-ARFCN;pci;mcc;mnc;SSS-RSRP\n" +
		"48.1;17.1;650000;1;231;01;-100\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data.InputRadioTech != InputRadioTech5G {
		t.Fatalf("got %q", data.InputRadioTech)
	}
}

func TestLoadCSVFile_InputRadioTechLTE(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "lte.csv")
	content := "latitude;longitude;EARFCN;pci;mcc;mnc;RSRP\n" +
		"48.1;17.1;3500;1;231;01;-100\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data.InputRadioTech != InputRadioTechLTE {
		t.Fatalf("got %q", data.InputRadioTech)
	}
}

func TestLoadCSVFile_extraDataColumnsExtendHeader(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "wide.csv")
	// Fewer header columns than first data row -> extra_col_* names
	content := "a;b;c;d;e;f\n" +
		"1;2;3;4;5;6;7;8\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Columns) < 8 {
		t.Fatalf("expected extended header, got %v", data.Columns)
	}
	if !strings.HasPrefix(data.Columns[6], "extra_col_") {
		t.Fatalf("columns: %v", data.Columns)
	}
}

func TestLoadCSVFile_missingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadCSVFile(filepath.Join(t.TempDir(), "nope.csv"))
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected not exist, got %v", err)
	}
}

func TestLoadCSVFile_quotedDeviceExport(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "quoted.csv")
	content := "\"latitude\";\"longitude\";\"frequency\";\"pci\";\"mcc\";\"mnc\";\"rsrp\";\"note\"\n" +
		"\"48.1\";\"17.1\";\"3500\";\"1\";\"231\";\"01\";\"-100\";\"meter;rear\"\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Columns) != 8 || len(data.Rows) != 1 {
		t.Fatalf("columns=%#v rows=%#v", data.Columns, data.Rows)
	}
	if data.Columns[0] != "latitude" || data.Rows[0][0] != "48.1" || data.Rows[0][7] != "meter;rear" {
		t.Fatalf("quoted fields were not decoded: columns=%#v rows=%#v", data.Columns, data.Rows)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = p
	cfg.ColumnMapping = BuildColumnMappingFromHeaders(data.Columns)
	cfg.ColumnMappingNames = map[string]string{
		"latitude": "latitude", "longitude": "longitude", "frequency": "frequency",
		"pci": "pci", "mcc": "mcc", "mnc": "mnc", "rsrp": "rsrp",
	}
	cfg.FilterPaths = []string{}
	cfg.ZoneMode = "center"
	cfg.ProgressEnabled = false
	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("process quoted export: %v", err)
	}
	zones, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatal(err)
	}
	output := string(zones)
	if !strings.Contains(output, "\nlatitude;longitude;frequency;pci;mcc;mnc;rsrp;note;") {
		t.Fatalf("quoted headers were not mapped into output: %q", output)
	}
	if !strings.Contains(output, `"meter;rear"`) {
		t.Fatalf("embedded semicolon was not safely quoted in output: %q", output)
	}
}

func TestLoadCSVFile_FindsMinimalLTEHeaderAfterMetadataPreamble(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	p := filepath.Join(tmp, "lte-with-preamble.csv")
	content := "Device export version 4\n" +
		"Serial number: abc-123\n" +
		"MCC;MNC;5G NR;Date;Time\n" +
		"231;01;yes;05.02.2026;10:00:00\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := LoadCSVFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data.FileInfo.HeaderLine != 2 {
		t.Fatalf("header line=%d, want 2; columns=%#v", data.FileInfo.HeaderLine, data.Columns)
	}
	if len(data.Rows) != 1 || data.columnIndexByName("MCC") < 0 || data.columnIndexByName("5G NR") < 0 {
		t.Fatalf("minimal LTE export parsed incorrectly: columns=%#v rows=%#v", data.Columns, data.Rows)
	}
}
