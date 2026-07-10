package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndMergeCSVFilesForProcessing_CommonMappedAliases(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	firstPath := filepath.Join(tmp, "first.csv")
	secondPath := filepath.Join(tmp, "second.csv")
	first := "Latitude;Longitude;NR-ARFCN;PCI;MCC;MNC;SSS-RSRP\n48.1;17.1;650000;10;231;01;-100\n"
	second := "Lat;Lng;Frequency;PCI;MCC;MNC;RSRP\n48.2;17.2;650001;11;231;1.0;-101\n"
	if err := os.WriteFile(firstPath, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ProcessingConfig{ColumnMappingNames: map[string]string{
		"latitude":  "Latitude",
		"longitude": "Longitude",
		"frequency": "NR-ARFCN",
		"pci":       "PCI",
		"mcc":       "MCC",
		"mnc":       "MNC",
		"rsrp":      "SSS-RSRP",
	}}
	data, mapping, err := LoadAndMergeCSVFilesForProcessing(
		context.Background(), []string{firstPath, secondPath}, cfg,
	)
	if err != nil {
		t.Fatalf("merge common aliases: %v", err)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(data.Rows))
	}
	wantSecond := map[string]string{
		"latitude": "48.2", "longitude": "17.2", "frequency": "650001", "rsrp": "-101",
	}
	for key, want := range wantSecond {
		idx, ok := mapping[key]
		if !ok || idx < 0 || idx >= len(data.Rows[1]) {
			t.Fatalf("missing resolved mapping for %s: %#v", key, mapping)
		}
		if got := data.Rows[1][idx]; got != want {
			t.Fatalf("second row %s=%q, want %q; columns=%#v row=%#v", key, got, want, data.Columns, data.Rows[1])
		}
	}
}

func TestMergeCSVDataRowsByName_PrefersExactMappedColumnOverAlias(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"RSRP", "SSS-RSRP"},
		Rows:    [][]string{{"-70", "-100"}},
	}
	equivalents := map[string]string{
		normalizedColumnKey("RSRP"):     normalizedColumnKey("SSS-RSRP"),
		normalizedColumnKey("SSS-RSRP"): normalizedColumnKey("SSS-RSRP"),
	}
	merged, err := mergeCSVDataRowsByName([]string{"SSS-RSRP"}, data, nil, equivalents)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.Rows[0][0]; got != "-100" {
		t.Fatalf("exact mapped SSS-RSRP value=%q, want -100", got)
	}
}
