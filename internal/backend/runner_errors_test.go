package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProcessing_missingInputPath(t *testing.T) {
	t.Parallel()

	cfg := DefaultProcessingConfig()
	cfg.FilePath = ""
	cfg.InputFilePaths = nil
	cfg.FilterPaths = []string{}

	_, err := RunProcessing(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "vstup") {
		t.Fatalf("expected missing input error, got %v", err)
	}
}

func TestRunProcessing_mobileModeMissingLTE(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "in.csv")
	if err := os.WriteFile(inputPath, []byte("latitude;longitude;frequency;pci;mcc;mnc;rsrp\n48;17;3500;1;231;01;-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.MobileModeEnabled = true
	cfg.MobileLTEFilePath = ""
	cfg.FilterPaths = []string{}
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	_, err := RunProcessing(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "mobile") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyFiltersCSV_nilData(t *testing.T) {
	t.Parallel()

	_, err := ApplyFiltersCSV(context.Background(), nil, []FilterRule{{Name: "x"}}, false, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadTimeSelectorData_emptyPaths(t *testing.T) {
	t.Parallel()

	_, err := LoadTimeSelectorData(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
