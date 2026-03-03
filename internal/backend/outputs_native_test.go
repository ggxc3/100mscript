package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProcessingExportsNRAsBinaryValues(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.csv")
	inputCSV := strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;5G NR",
		"48.148600;17.107700;3500;10;231;01;-100;yes",
		"48.160000;17.120000;3600;20;231;02;-120;no",
	}, "\n") + "\n"
	if err := os.WriteFile(inputPath, []byte(inputCSV), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = inputPath
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.FilterPaths = []string{}
	cfg.IncludeEmptyZones = true
	cfg.AddCustomOperators = true
	cfg.CustomOperators = []CustomOperator{
		{MCC: "231", MNC: "03", PCI: "30"},
	}
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
		t.Fatalf("read zones file: %v", err)
	}
	content := string(contentBytes)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected zones output, got %d lines", len(lines))
	}

	headerLine := lines[1]
	header := strings.Split(headerLine, ";")
	nrIdx := indexOf(header, "5G NR")
	if nrIdx < 0 {
		t.Fatalf("5G NR column missing in zones header: %q", headerLine)
	}

	sawOne := false
	sawZero := false
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if nrIdx >= len(parts) {
			t.Fatalf("row missing NR column at index %d: %q", nrIdx, line)
		}
		val := strings.TrimSpace(parts[nrIdx])
		switch val {
		case "1":
			sawOne = true
		case "0":
			sawZero = true
		default:
			t.Fatalf("unexpected 5G NR export value %q in row: %q", val, line)
		}
	}

	if !sawOne || !sawZero {
		t.Fatalf("expected both NR values 1 and 0 in export, got sawOne=%v sawZero=%v", sawOne, sawZero)
	}
	if strings.Contains(content, ";yes;") || strings.Contains(content, ";no;") {
		t.Fatalf("zones export still contains textual NR values: %q", content)
	}
}
