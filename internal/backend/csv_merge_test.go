package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndMergeCSVFilesCompatible(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp"
	row1 := "48.1;17.1;3500;10;231;01;-100"
	row2 := "48.2;17.2;3600;20;231;02;-101"

	p1 := filepath.Join(tmpDir, "a.csv")
	p2 := filepath.Join(tmpDir, "b.csv")
	if err := os.WriteFile(p1, []byte(strings.Join([]string{header, row1}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(p2, []byte(strings.Join([]string{header, row2}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}

	data, err := LoadAndMergeCSVFiles([]string{p1, p2})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(data.Rows))
	}
	if data.Rows[0][3] != "10" || data.Rows[1][3] != "20" {
		t.Fatalf("unexpected pci values: %#v", data.Rows)
	}
}

func TestLoadAndMergeCSVFilesIncompatibleHeaders(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	p1 := filepath.Join(tmpDir, "lte.csv")
	p2 := filepath.Join(tmpDir, "nr.csv")
	if err := os.WriteFile(p1, []byte("latitude;longitude;EARFCN;pci;mcc;mnc;rsrp\n48;17;3500;1;231;01;-100\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte("latitude;longitude;NR-ARFCN;pci;mcc;mnc;rsrp\n48;17;650000;2;231;01;-100\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadAndMergeCSVFiles([]string{p1, p2})
	if err == nil {
		t.Fatalf("expected error for mismatched columns")
	}
}

func TestLoadTimeSelectorDataMergedRenumbersOriginalRows(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time"
	p1 := filepath.Join(tmpDir, "t1.csv")
	p2 := filepath.Join(tmpDir, "t2.csv")
	csv1 := strings.Join([]string{
		header,
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:03:23",
	}, "\n") + "\n"
	csv2 := strings.Join([]string{
		header,
		"48.149600;17.108700;3600;20;231;02;-102;05.02.2026;10:04:00",
	}, "\n") + "\n"
	if err := os.WriteFile(p1, []byte(csv1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(csv2), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := LoadTimeSelectorData([]string{p1, p2})
	if err != nil {
		t.Fatalf("load merged time selector: %v", err)
	}
	if data.TotalRows != 2 {
		t.Fatalf("expected 2 total rows, got %d", data.TotalRows)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 timed rows, got %d", len(data.Rows))
	}
	if data.Rows[0].OriginalRow != 1 || data.Rows[1].OriginalRow != 2 {
		t.Fatalf("expected sequential original rows 1,2 got %#v", data.Rows)
	}
}

func TestSortMergedCSVRowsByTimeReordersRows(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time"
	p1 := filepath.Join(tmpDir, "late.csv")
	p2 := filepath.Join(tmpDir, "early.csv")
	csv1 := strings.Join([]string{
		header,
		"48.1;17.1;3500;10;231;01;-100;05.02.2026;10:05:00",
	}, "\n") + "\n"
	csv2 := strings.Join([]string{
		header,
		"48.2;17.2;3600;20;231;02;-101;05.02.2026;10:03:00",
	}, "\n") + "\n"
	if err := os.WriteFile(p1, []byte(csv1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(csv2), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := LoadAndMergeCSVFiles([]string{p1, p2})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	sorted, changed := sortMergedCSVRowsByTime(data)
	if !changed {
		t.Fatalf("expected time sort to reorder rows")
	}
	if sorted.Rows[0][3] != "20" || sorted.Rows[1][3] != "10" {
		t.Fatalf("expected earlier time (PCI 20) first, got PCI columns %#v", []string{sorted.Rows[0][3], sorted.Rows[1][3]})
	}
}

func TestRunProcessingWithMergedInputPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp"
	p1 := filepath.Join(tmpDir, "m1.csv")
	p2 := filepath.Join(tmpDir, "m2.csv")
	if err := os.WriteFile(p1, []byte(strings.Join([]string{
		header,
		"48.148600;17.107700;3500;10;231;01;-100",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(strings.Join([]string{
		header,
		"48.149600;17.108700;3600;20;231;02;-102",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = p1
	cfg.InputFilePaths = []string{p1, p2}
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
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
		t.Fatalf("run processing merged: %v", err)
	}
	if result.TotalZoneRows < 1 {
		t.Fatalf("expected zone rows, got %d", result.TotalZoneRows)
	}
}
