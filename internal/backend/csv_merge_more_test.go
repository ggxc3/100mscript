package backend

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeInputPaths(t *testing.T) {
	t.Parallel()

	got := NormalizeInputPaths([]string{"  /a.csv  ", "", "/b.csv", "/a.csv"})
	want := []string{"/a.csv", "/b.csv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeInputPaths: got %#v want %#v", got, want)
	}
	if NormalizeInputPaths(nil) != nil {
		t.Fatalf("expected nil for nil input")
	}
}

func TestInputPathsFromConfig(t *testing.T) {
	t.Parallel()

	cfg := ProcessingConfig{
		FilePath:       "/fallback.csv",
		InputFilePaths: []string{"  ", "/first.csv", "/second.csv"},
	}
	got := InputPathsFromConfig(cfg)
	want := []string{"/first.csv", "/second.csv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InputPathsFromConfig (with input_file_paths): got %#v want %#v", got, want)
	}

	cfg2 := ProcessingConfig{FilePath: "/only.csv"}
	got2 := InputPathsFromConfig(cfg2)
	if !reflect.DeepEqual(got2, []string{"/only.csv"}) {
		t.Fatalf("InputPathsFromConfig (file_path only): got %#v", got2)
	}

	cfg3 := ProcessingConfig{FilePath: "/x.csv", InputFilePaths: []string{}}
	got3 := InputPathsFromConfig(cfg3)
	if !reflect.DeepEqual(got3, []string{"/x.csv"}) {
		t.Fatalf("InputPathsFromConfig (empty input_file_paths): got %#v", got3)
	}
}

func TestSortMergedCSVRowsByTime_noDateTimeLeavesOrder(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp"
	p1 := filepath.Join(tmpDir, "a.csv")
	p2 := filepath.Join(tmpDir, "b.csv")
	if err := os.WriteFile(p1, []byte(header+"\n48;17;3500;1;231;01;-100\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(header+"\n48;17;3600;2;231;02;-101\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := LoadAndMergeCSVFiles([]string{p1, p2})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	out, changed := sortMergedCSVRowsByTime(data)
	if changed {
		t.Fatalf("expected no time columns -> no sort")
	}
	if out.Rows[0][3] != "1" || out.Rows[1][3] != "2" {
		t.Fatalf("expected merge order preserved, got PCI %q %q", out.Rows[0][3], out.Rows[1][3])
	}
}

func TestLoadTimeSelectorDataMerged_timestampsChronological(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time"
	p1 := filepath.Join(tmpDir, "later.csv")
	p2 := filepath.Join(tmpDir, "earlier.csv")
	if err := os.WriteFile(p1, []byte(strings.Join([]string{
		header,
		"48;17;3500;1;231;01;-100;05.02.2026;10:10:00",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(strings.Join([]string{
		header,
		"48;17;3600;2;231;02;-101;05.02.2026;10:05:00",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := LoadTimeSelectorData([]string{p1, p2})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 timed rows, got %d", len(data.Rows))
	}
	if data.Rows[0].TimestampMS > data.Rows[1].TimestampMS {
		t.Fatalf("expected ascending timestamps after merge+sort, got %d > %d",
			data.Rows[0].TimestampMS, data.Rows[1].TimestampMS)
	}
	if data.Rows[0].OriginalRow != 1 || data.Rows[1].OriginalRow != 2 {
		t.Fatalf("expected renumbered original rows 1,2 got %#v", data.Rows)
	}
}

func TestRunProcessing_excludeOriginalRowAfterMergeAndTimeSort(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time"
	p1 := filepath.Join(tmpDir, "one.csv")
	p2 := filepath.Join(tmpDir, "two.csv")
	// Merge order: 10:10 then 10:05 — after time sort: 10:05 (PCI 20), 10:10 (PCI 10)
	if err := os.WriteFile(p1, []byte(strings.Join([]string{
		header,
		"48.1;17.1;3500;10;231;01;-100;05.02.2026;10:10:00",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(p2, []byte(strings.Join([]string{
		header,
		"48.2;17.2;3600;20;231;02;-101;05.02.2026;10:05:00",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = p1
	cfg.InputFilePaths = []string{p1, p2}
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.ExcludedOriginalRows = []int{2}
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
		t.Fatalf("run processing: %v", err)
	}
	contentBytes, err := os.ReadFile(result.ZonesFile)
	if err != nil {
		t.Fatalf("read zones: %v", err)
	}
	content := string(contentBytes)
	// After time sort: row 1 = PCI 20 (10:05), row 2 = PCI 10 (10:10). Excluding original row 2 drops PCI 10.
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected zones output: %q", content)
	}
	headerCols := strings.Split(lines[1], ";")
	pciIdx := -1
	for i, h := range headerCols {
		if strings.EqualFold(strings.TrimSpace(h), "pci") {
			pciIdx = i
			break
		}
	}
	if pciIdx < 0 {
		t.Fatalf("PCI column not found in zones header: %q", lines[1])
	}
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if pciIdx < len(parts) && strings.TrimSpace(parts[pciIdx]) == "10" {
			t.Fatalf("expected excluded measurement PCI 10 absent from zones, got line: %q", line)
		}
	}
}
