package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFrequencyMode_RealCustomerMerge(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("requires local new_bad_merge customer CSVs")
	}

	root, err := filepath.Abs(filepath.Join("..", "..", "data", "new_bad_merge"))
	if err != nil {
		t.Fatal(err)
	}
	if input := os.Getenv("CUSTOMER_MERGE_INPUT_DIR"); input != "" {
		root = input
	}
	dest := os.Getenv("CUSTOMER_MERGE_AUDIT_DIR")
	if dest == "" {
		dest = t.TempDir()
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tech := range []string{"lte", "5g"} {
		t.Run(tech, func(t *testing.T) {
			paths, err := filepath.Glob(filepath.Join(root, "*.csv"))
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultProcessingConfig()
			cfg.FrequencyModeEnabled = true
			cfg.ZoneMode = "segments"
			cfg.ZoneSizeM = 100
			cfg.FilterPaths = []string{}
			cfg.FrequencyColumnMappings = map[string]map[string]string{tech: {"latitude": "Latitude", "longitude": "Longitude", "mcc": "MCC", "mnc": "MNC", "pci": "PCI", "rsrp": "RSRP", "sinr": "SINR"}}
			freq := "Frequency"
			if tech == "5g" {
				freq = "SSRef"
				cfg.FrequencyColumnMappings[tech]["rsrp"] = "SSS-RSRP"
				cfg.FrequencyColumnMappings[tech]["sinr"] = "SSS-SINR"
			}
			for _, p := range paths {
				isNR := strings.Contains(p, "5G NR")
				if isNR != (tech == "5g") {
					continue
				}
				cfg.FrequencyInputs = append(cfg.FrequencyInputs, FrequencyInput{FilePath: p, Technology: tech, FrequencyColumn: freq})
			}
			if len(cfg.FrequencyInputs) != 4 {
				t.Fatal(cfg.FrequencyInputs)
			}
			cfg.FilePath = cfg.FrequencyInputs[0].FilePath
			cfg.Frequency5GOutput = filepath.Join(dest, tech+"_merged_5g.csv")
			cfg.FrequencyLTEOutput = filepath.Join(dest, tech+"_merged_lte.csv")
			start := time.Now()
			ctx := context.Background()
			loaded, mapping, corrected, err := loadFrequencyData(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ColumnMapping = mapping
			loaded.CSVData, _ = sortCSVRowsByTime(loaded.CSVData, false)
			tr, err := NewPyProjTransformer()
			if err != nil {
				t.Fatal(err)
			}
			ds, err := ProcessDataNative(ctx, loaded.CSVData, cfg, tr)
			if err != nil {
				t.Fatal(err)
			}
			type stat struct {
				Input, Usable, Winning                           int
				MinSegment, MaxSegment                           int
				MinX, MaxX, MinY, MaxY, MinDistance, MaxDistance float64
				Zones                                            map[string]bool
			}
			stats := map[string]*stat{}
			for _, inp := range cfg.FrequencyInputs {
				stats[inp.FilePath] = &stat{MinSegment: 1 << 30, MinX: 1e30, MinY: 1e30, MaxX: -1e30, MaxY: -1e30, MinDistance: 1e30, MaxDistance: -1e30, Zones: map[string]bool{}}
			}
			sourceIdx := indexOf(ds.Columns, frequencySourceColumn)
			for _, row := range loaded.Rows {
				stats[row[sourceIdx]].Input++
			}
			for _, r := range ds.Rows {
				s := stats[r.Raw[sourceIdx]]
				s.Usable++
				s.Zones[r.ZonaKey] = true
				n, _ := strconv.Atoi(strings.TrimPrefix(r.ZonaKey, "segment_"))
				s.MinSegment = min(s.MinSegment, n)
				s.MaxSegment = max(s.MaxSegment, n)
				s.MinX = min(s.MinX, r.XMeters)
				s.MaxX = max(s.MaxX, r.XMeters)
				s.MinY = min(s.MinY, r.YMeters)
				s.MaxY = max(s.MaxY, r.YMeters)
				s.MinDistance = min(s.MinDistance, r.SegmentDistanceM)
				s.MaxDistance = max(s.MaxDistance, r.SegmentDistanceM)
			}
			type box struct{ minX, maxX, minY, maxY float64 }
			boxes := map[string]box{}
			for _, row := range ds.Rows {
				b, ok := boxes[row.ZonaKey]
				if !ok {
					b = box{row.XMeters, row.XMeters, row.YMeters, row.YMeters}
				}
				b.minX = math.Min(b.minX, row.XMeters)
				b.maxX = math.Max(b.maxX, row.XMeters)
				b.minY = math.Min(b.minY, row.YMeters)
				b.maxY = math.Max(b.maxY, row.YMeters)
				boxes[row.ZonaKey] = b
			}
			maxDiameter := 0.0
			for _, b := range boxes {
				maxDiameter = math.Max(maxDiameter, math.Hypot(b.maxX-b.minX, b.maxY-b.minY))
			}
			t.Logf("maximum 100m-segment bounding diameter: %.1f m", maxDiameter)
			groups := groupFrequencyRows(ds)
			for _, g := range groups {
				stats[ds.Rows[g.best].Raw[sourceIdx]].Winning++
			}
			rules, err := loadFrequencyRules(cfg, ds.Columns)
			if err != nil {
				t.Fatal(err)
			}
			result, err := saveFrequencyResults(ctx, ds, loaded.exportColumns, groups, cfg, rules, tr, cfg.Frequency5GOutput, cfg.FrequencyLTEOutput)
			if err != nil {
				t.Fatal(err)
			}
			result.CorrectedMNC = corrected
			if maxDiameter > 300 {
				t.Errorf("geographically distant rows merged into one 100m segment: %.1f m", maxDiameter)
			}
			if result.UniqueZones < 1800 {
				t.Errorf("four route legs collapsed: %+v", result)
			}
			for path, stat := range stats {
				if stat.Winning < 250 {
					t.Errorf("missing route part %s: %+v", path, stat)
				}
			}
			for _, p := range cfg.FrequencyInputs {
				s := stats[p.FilePath]
				t.Logf("%s input=%d usable=%d winners=%d zones=%d segments=%d..%d distance=%.1f..%.1f XY=(%.0f..%.0f,%.0f..%.0f)", filepath.Base(p.FilePath), s.Input, s.Usable, s.Winning, len(s.Zones), s.MinSegment, s.MaxSegment, s.MinDistance, s.MaxDistance, s.MinX, s.MaxX, s.MinY, s.MaxY)
			}
			t.Logf("TOTAL tech=%s elapsed=%s result=%+v", tech, time.Since(start), result)
			raw, err := json.MarshalIndent(map[string]any{"config": cfg, "stats": stats, "result": result, "max_segment_diameter_m": maxDiameter}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dest, fmt.Sprintf("%s_report.json", tech)), raw, 0644); err != nil {
				t.Fatal(err)
			}
			// Exercise the app's public entry point as well as the instrumented
			// pipeline above, and compare complete exports including provenance.
			runCfg := cfg
			runDir := t.TempDir()
			runCfg.FrequencyLTEOutput = filepath.Join(runDir, "lte.csv")
			runCfg.Frequency5GOutput = filepath.Join(runDir, "5g.csv")
			if _, err := RunProcessing(ctx, runCfg); err != nil {
				t.Fatal(err)
			}
			for expected, actual := range map[string]string{cfg.FrequencyLTEOutput: runCfg.FrequencyLTEOutput, cfg.Frequency5GOutput: runCfg.Frequency5GOutput} {
				want, err := os.ReadFile(expected)
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(actual)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("RunProcessing export differs from audited pipeline: %s", actual)
				}
			}
		})
	}
}
