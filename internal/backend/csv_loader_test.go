package backend

import (
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
