package backend

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func frequencyFixture(t *testing.T) ProcessingConfig {
	t.Helper()
	dir := t.TempDir()
	files := []struct{ name, tech, frequency, content string }{
		{"nr-a.csv", "5g", "SSRef", "Latitude;Longitude;NR-ARFCN;SSRef;PCI;MCC;MNC;SSS-RSRP;Add. PLMNs;Note\n48.9;21.2;422000;2110000000;10;231;6;-80;;weak;231/2\n48.9;21.2;422000;2110000000;11;231;6;-40;;winner;231/2\n"},
		{"nr-b.csv", "5g", "SSRef", "Note;MNC;MCC;PCI;SSRef;NR-ARFCN;Longitude;Latitude;RSRP\nother;06;231;12;2110000000;422000;21.2;48.9;-50;231/2\ntie;2;231;13;2110000000;422000;21.2;48.9;-40;\nfrequency;2;231;20;2120000000;424000;21.2;48.9;-70;\noperator;1;231;21;2110000000;422000;21.2;48.9;-20;\n"},
		{"lte-a.csv", "lte", "Frequency", "Latitude;Longitude;EARFCN;Frequency;PCI;MCC;MNC;RSRP;Note\n48.9;21.2;0;2110000000;40;231;2;-10;lte-winner\n"},
		{"lte-b.csv", "lte", "Frequency", "Latitude;Longitude;EARFCN;Frequency;PCI;MCC;MNC;RSRP;Note\n48.9;21.2;0;2110000000;41;231;2;-30;lte-other\n"},
	}
	cfg := DefaultProcessingConfig()
	cfg.FrequencyModeEnabled = true
	cfg.FilterPaths = []string{}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg.FrequencyInputs = append(cfg.FrequencyInputs, FrequencyInput{FilePath: path, Technology: f.tech, FrequencyColumn: f.frequency})
	}
	cfg.Frequency5GOutput = filepath.Join(dir, "zones.csv")
	cfg.FrequencyLTEOutput = filepath.Join(dir, "stats.csv")
	return cfg
}

func readFrequencyCSV(t *testing.T, path string) ([]string, [][]string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = ';'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("missing header")
	}
	return rows[0], rows[1:]
}

func readBothFrequencyCSVs(t *testing.T, result ProcessingResult) ([]string, [][]string) {
	t.Helper()
	h1, r1 := readFrequencyCSV(t, result.Frequency5GFile)
	h2, r2 := readFrequencyCSV(t, result.FrequencyLTEFile)
	for i := range r1 {
		r1[i] = append(r1[i], "5G")
	}
	for i := range r2 {
		r2[i] = append(r2[i], "LTE")
	}
	a := &CSVData{Columns: append(h1, technologyColumn), Rows: r1}
	b := &CSVData{Columns: append(h2, technologyColumn), Rows: r2}
	columns, err := buildUnionColumns([]*CSVData{a, b}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeCSVDataRowsByName(columns, a, []*CSVData{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return merged.Columns, merged.Rows
}

func TestFrequencyMode_MultipleSourcesMaximumAndPLMN(t *testing.T) {
	cfg := frequencyFixture(t)
	// Thresholds must have no influence on selecting the strongest individual row.
	cfg.RSRPThreshold, cfg.SINRThreshold = 100, 100
	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalZoneRows != 4 || result.UniqueOperators != 2 || result.UniqueZones != 1 || result.CorrectedMNC != 3 {
		t.Fatalf("result=%+v", result)
	}
	header, rows := readBothFrequencyCSVs(t, result)
	if indexOf(header, "Ostatne_PCI") < 0 || indexOf(header, csvSourceIndexColumn) >= 0 {
		t.Fatal(header)
	}
	var nr, lte bool
	for _, row := range rows {
		at := func(name string) string { return cellAt(row, indexOf(header, name)) }
		if at("Operator_sedi") != "yes" {
			t.Fatal("disabled filters must return yes", row)
		}
		if at(frequencyHzColumn) == "2110000000" && at("MNC") == "2" {
			switch at(technologyColumn) {
			case "5G":
				nr = true
				if at("SSS-RSRP") != "-40" || at("PCI") != "11" || at("Note") != "winner" || at("Ostatne_PCI") != "10, 12, 13" || at("Pocet_merani") != "4" || at("original_excel_row") != "3" || at("NR-ARFCN") != "422000" {
					t.Fatalf("wrong NR winner: %v", row)
				}
			case "LTE":
				lte = true
				if at("RSRP") != "-10" || at("PCI") != "40" || at("Ostatne_PCI") != "41" || at("Frequency") != "2110000000" {
					t.Fatalf("wrong LTE winner: %v", row)
				}
			}
		}
	}
	if !nr || !lte {
		t.Fatal("technologies must remain separate")
	}
	nrHeader, _ := readFrequencyCSV(t, result.Frequency5GFile)
	lteHeader, _ := readFrequencyCSV(t, result.FrequencyLTEFile)
	if indexOf(nrHeader, technologyColumn) >= 0 || indexOf(lteHeader, technologyColumn) >= 0 || indexOf(nrHeader, "EARFCN") >= 0 || indexOf(nrHeader, "Frequency") >= 0 || indexOf(lteHeader, "SSRef") >= 0 || indexOf(lteHeader, "SSS-RSRP") >= 0 {
		t.Fatal("technology schemas leaked", nrHeader, lteHeader)
	}
}

func TestFrequencyFilters_ThreePointsAndPrecedence(t *testing.T) {
	columns := []string{"MCC", "MNC", frequencyHzColumn, "PCI"}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2, "pci": 3}
	raw := []string{"231", "2", "2110000000", "10"}
	rule := func(name string, mnc, low, high float64) FilterRule {
		return FilterRule{Name: name, Assignments: map[string][]float64{"MNC": {mnc}}, ConditionGroups: [][]Condition{{{Field: "Frequency", Kind: ConditionRange, Low: low, High: high}}}}
	}
	cases := []struct {
		name             string
		rules            []FilterRule
		bw               float64
		center, expanded string
	}{
		{"disabled", nil, 5, "yes", "yes"},
		{"unchanged", []FilterRule{rule("a", 2, 2110000000, 2110000001)}, 0, "yes", "yes"},
		{"changed", []FilterRule{rule("a", 6, 2110000000, 2110000001)}, 0, "no", "no"},
		{"lower endpoint", []FilterRule{rule("a", 6, 2105000000, 2105000001), rule("z", 2, 2100000000, 2120000000)}, 5, "yes", "no"},
		{"upper endpoint", []FilterRule{rule("a", 6, 2115000000, 2115000001), rule("z", 2, 2100000000, 2120000000)}, 5, "yes", "no"},
		{"interior ignored", []FilterRule{rule("a", 6, 2111000000, 2112000000), rule("z", 2, 2100000000, 2120000000)}, 5, "yes", "yes"},
		{"exclusive upper bound", []FilterRule{rule("a", 6, 2104000000, 2105000000), rule("z", 2, 2105000000, 2120000000)}, 5, "yes", "yes"},
		{"alphabetical priority", []FilterRule{rule("z", 6, 2100000000, 2120000000), rule("a", 2, 2100000000, 2120000000)}, 5, "yes", "yes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := compileFrequencyRules(tc.rules, columns, mapping)
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultProcessingConfig()
			cfg.Frequency5GBW = tc.bw
			row := ProcessedRow{Raw: raw, Frequency: "2110000000"}
			center, got := frequencyFilterFlags(row, "5G", cfg, map[string][]frequencyRule{"5G": compiled})
			if center != tc.center || got != tc.expanded {
				t.Fatalf("got %s want %s", got, tc.expanded)
			}
			// LTE has its own delta (zero), even when the rules are identical.
			_, lte := frequencyFilterFlags(row, "LTE", cfg, map[string][]frequencyRule{"LTE": compiled})
			if lte != tc.center {
				t.Fatalf("LTE incorrectly inherited NR BW: %s", lte)
			}
			if strings.Join(raw, ";") != "231;2;2110000000;10" {
				t.Fatal("filters mutated source")
			}
		})
	}
	specific := rule("z", 6, 2100000000, 2120000000)
	specific.ConditionGroups[0] = append(specific.ConditionGroups[0], Condition{Field: "MNC", Kind: ConditionEq, Low: 2})
	compiled, _ := compileFrequencyRules([]FilterRule{rule("a", 2, 2100000000, 2120000000), specific}, columns, mapping)
	if frequencyOperatorMatches(raw, 2110000000, compiled) {
		t.Fatal("specific rule must win")
	}
	multi := rule("a", 2, 2100000000, 2120000000)
	multi.Assignments["MNC"] = []float64{2, 6}
	compiled, _ = compileFrequencyRules([]FilterRule{multi}, columns, mapping)
	if frequencyOperatorMatches(raw, 2110000000, compiled) {
		t.Fatal("a changed assignment combination must return no")
	}
}

func TestFrequencyMode_SharedFilterAndCorrectedOperator(t *testing.T) {
	cfg := frequencyFixture(t)
	dir := filepath.Dir(cfg.Frequency5GOutput)
	path := filepath.Join(dir, "lte-filter.txt")
	os.WriteFile(path, []byte(`("MNC" = 6); ("Frequency" = 2110000000 AND "MNC" = 2)`), 0600)
	cfg.FilterPaths = []string{path}
	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	header, rows := readBothFrequencyCSVs(t, result)
	for _, r := range rows {
		// Matching rows change operator; all remaining rows have no matching rule.
		expected := "no"
		if r[indexOf(header, "Operator_sedi")] != expected {
			t.Fatal(r)
		}
		if r[indexOf(header, "MNC")] == "6" {
			t.Fatal("filter overwrote operator")
		}
	}
}

func TestFrequencyMode_Validation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ProcessingConfig)
	}{
		{"technology", func(c *ProcessingConfig) { c.FrequencyInputs[0].Technology = "" }},
		{"duplicate", func(c *ProcessingConfig) { c.FrequencyInputs = append(c.FrequencyInputs, c.FrequencyInputs[0]) }},
		{"channel", func(c *ProcessingConfig) { c.FrequencyInputs[0].FrequencyColumn = "NR-ARFCN" }},
		{"negative delta", func(c *ProcessingConfig) { c.FrequencyLTEBW = -1 }},
		{"nan delta", func(c *ProcessingConfig) { c.Frequency5GBW = math.NaN() }},
		{"infinite size", func(c *ProcessingConfig) { c.ZoneSizeM = math.Inf(1) }},
		{"same output", func(c *ProcessingConfig) { c.FrequencyLTEOutput = c.Frequency5GOutput }},
		{"overwrite input", func(c *ProcessingConfig) { c.Frequency5GOutput = c.FrequencyInputs[0].FilePath }},
		{"missing column", func(c *ProcessingConfig) { c.FrequencyInputs[0].FrequencyColumn = "missing" }},
		{"wrong PLMN column", func(c *ProcessingConfig) { c.FrequencyInputs[0].PLMNColumn = "SSRef" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := frequencyFixture(t)
			tc.change(&cfg)
			if _, err := RunProcessing(context.Background(), cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	for _, raw := range []string{"231/2,231/6", "231/foo", "231/2 231/6"} {
		if _, err := correctedPLMNMNC([]string{raw}, []int{0}, true); err == nil {
			t.Fatal("ambiguous PLMN accepted", raw)
		}
	}
}

func TestFrequencyMode_SegmentSizeAndTimeWindow(t *testing.T) {
	cfg := frequencyFixture(t)
	path := cfg.FrequencyInputs[0].FilePath
	content := "Date;Time;Latitude;Longitude;PCI;MCC;MNC;SSRef;RSRP\n2026-09-10;09:00:00;48.9;21.2;1;231;2;2110000000;-60\n2026-09-10;09:00:10;48.902;21.2;2;231;2;2110000000;-50\n2026-09-10;09:00:20;48.904;21.2;3;231;2;2110000000;-40\n"
	os.WriteFile(path, []byte(content), 0600)
	cfg.FrequencyInputs = cfg.FrequencyInputs[:1]
	cfg.ZoneSizeM = 100
	small, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ZoneSizeM = 1000
	large, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if small.UniqueZones != 3 || large.UniqueZones != 1 {
		t.Fatalf("zone sizes ignored: %d vs %d", small.UniqueZones, large.UniqueZones)
	}
	cfg.ZoneSizeM = 100
	cfg.TimeWindows = []TimeWindow{{Start: "2026-09-10T09:00:10", End: "2026-09-10T09:00:10"}}
	cut, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cut.UniqueZones != 2 || cut.ExcludedMeasurements != 1 {
		t.Fatalf("cut=%+v", cut)
	}
}

func TestFrequencyMode_Real2100(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("set RUN_LARGE_REAL_DATA_TESTS=1 for the 290 MB 2100 inputs")
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "data", "2100", "*.csv"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultProcessingConfig()
	cfg.FrequencyModeEnabled = true
	cfg.FrequencyColumnMappings = map[string]map[string]string{}
	for _, tech := range []string{"lte", "5g"} {
		cfg.FrequencyColumnMappings[tech] = map[string]string{"latitude": "Latitude", "longitude": "Longitude", "pci": "PCI", "mcc": "MCC", "mnc": "MNC", "rsrp": "RSRP", "sinr": "SINR"}
	}
	cfg.FrequencyColumnMappings["5g"]["rsrp"] = "SSS-RSRP"
	cfg.FrequencyColumnMappings["5g"]["sinr"] = "SSS-SINR"
	cfg.FrequencyLTEBW = 5
	cfg.Frequency5GBW = 5
	for name, value := range map[string]*float64{"FREQUENCY_TEST_LTE_BW": &cfg.FrequencyLTEBW, "FREQUENCY_TEST_5G_BW": &cfg.Frequency5GBW} {
		if raw := os.Getenv(name); raw != "" {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				t.Fatal(err)
			}
			*value = v
		}
	}
	if mode := os.Getenv("FREQUENCY_TEST_ZONE_MODE"); mode != "" {
		cfg.ZoneMode = mode
	}
	for _, path := range paths {
		if strings.Contains(path, "_zones.csv") || strings.Contains(path, "_stats.csv") {
			continue
		}
		input := FrequencyInput{FilePath: path, Technology: "lte", FrequencyColumn: "Frequency"}
		if strings.Contains(path, "5G NR") {
			input.Technology = "5g"
			input.FrequencyColumn = "SSRef"
		}
		cfg.FrequencyInputs = append(cfg.FrequencyInputs, input)
	}
	if len(cfg.FrequencyInputs) != 2 {
		t.Skip("local 2100 inputs not available")
	}
	dir := t.TempDir()
	cfg.Frequency5GOutput = filepath.Join(dir, "2100_frequencies_5g.csv")
	cfg.FrequencyLTEOutput = filepath.Join(dir, "2100_frequencies_lte.csv")
	cfg.FilterPaths, err = DiscoverFilterPaths(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FREQUENCY_TEST_NO_FILTERS") == "1" {
		cfg.FilterPaths = []string{}
	}

	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	header, rows := readBothFrequencyCSVs(t, result)
	seen := map[string]bool{}
	techs := map[string]int{}
	flags := map[string]int{}
	legacyRules := map[string][]FilterRule{}
	for tech, paths := range map[string][]string{"LTE": cfg.FilterPaths, "5G": cfg.FilterPaths} {
		legacyRules[tech], err = LoadFilterRulesFromPaths(paths)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rows {
		at := func(n string) string { return r[indexOf(header, n)] }
		zoneColumn := "Usek"
		if cfg.ZoneMode != "segments" {
			zoneColumn = "Zona"
		}
		key := strings.Join([]string{at(zoneColumn), at("MCC"), at("MNC"), at(technologyColumn), at(frequencyHzColumn)}, "|")
		if seen[key] {
			t.Fatal("duplicate", key)
		}
		seen[key] = true
		f, _ := strconv.ParseFloat(at(frequencyHzColumn), 64)
		if f < 1e8 {
			t.Fatal("channel exported as frequency", f)
		}
		techs[at(technologyColumn)]++
		flags[at("Operator_sedi")+"/"+at("Operator_sedi_BW")]++
		bw := cfg.FrequencyLTEBW
		if at(technologyColumn) == "5G" {
			bw = cfg.Frequency5GBW
		}
		checks := []bool{}
		for _, probe := range []float64{f, f - bw*1e6, f + bw*1e6} {
			checks = append(checks, legacyFrequencyMatch(t,
				[]string{at("MCC"), at("MNC"), at(frequencyHzColumn)},
				[]string{"MCC", "MNC", "Frequency"}, map[string]int{"mcc": 0, "mnc": 1, "frequency": 2},
				legacyRules[at(technologyColumn)], probe))
		}
		if (at("Operator_sedi") == "yes") != checks[0] || (at("Operator_sedi_BW") == "yes") != (checks[0] && checks[1] && checks[2]) {
			t.Fatalf("independent legacy execution disagrees: %s probes=%v row=%v", key, checks, r)
		}
		if strings.Contains(", "+at("Ostatne_PCI")+", ", ", "+at("PCI")+", ") {
			t.Fatal("selected PCI in other PCIs")
		}
	}
	if techs["LTE"] == 0 || techs["5G"] == 0 || result.CorrectedMNC != 105403 {
		t.Fatalf("unexpected result: %+v, tech=%v", result, techs)
	}
	t.Logf("2100: zones=%d operators=%d rows=%d corrected=%d tech=%v flags=%v", result.UniqueZones, result.UniqueOperators, result.TotalZoneRows, result.CorrectedMNC, techs, flags)
	if output := os.Getenv("FREQUENCY_TEST_OUTPUT_DIR"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{result.Frequency5GFile, result.FrequencyLTEFile} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, filepath.Base(path)), raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestFrequencyGrouping_UsesIndividualMaximumNotPCIAverage(t *testing.T) {
	ds := &ProcessedDataset{Columns: []string{technologyColumn}}
	for i, rsrp := range []float64{-20, -120, -40} {
		pci := "1"
		if i == 2 {
			pci = "2"
		}
		ds.Rows = append(ds.Rows, ProcessedRow{Raw: []string{"5G"}, ZonaKey: "segment_0", MCC: "231", MNC: "2", Frequency: "2110000000", PCI: pci, RSRP: rsrp})
	}
	groups := groupFrequencyRows(ds)
	if len(groups) != 1 || groups[0].best != 0 || groups[0].count != 3 || otherFrequencyPCIs(groups[0], "1") != "2" {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestFrequencyFilters_ChannelConditionsRemainChannelNumbers(t *testing.T) {
	columns := []string{"MCC", "MNC", frequencyHzColumn, "NR-ARFCN"}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2}
	rules, err := compileFrequencyRules([]FilterRule{{Name: "channel", Assignments: map[string][]float64{"MNC": {6}}, ConditionGroups: [][]Condition{{{Field: "NR-ARFCN", Kind: ConditionEq, Low: 422000}}}}}, columns, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if frequencyOperatorMatches([]string{"231", "2", "2110000000", "422000"}, 2110000000, rules) {
		t.Fatal("channel condition was compared to physical Hz")
	}
}

func TestFrequencyMode_GridGeometrySizeAndTimeCuts(t *testing.T) {
	ctx := context.Background()
	transformer, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	xy, err := transformer.Forward(ctx, []Point{{A: 21.2, B: 48.9}})
	if err != nil {
		t.Fatal(err)
	}
	x, y := math.Floor(xy[0].A/1000)*1000, math.Floor(xy[0].B/1000)*1000
	positions := []Point{{A: x + 10, B: y + 10}, {A: x + 30, B: y + 30}, {A: x + 110, B: y + 10}}
	gps, err := transformer.Inverse(ctx, positions)
	if err != nil {
		t.Fatal(err)
	}
	centerGPS, err := transformer.Inverse(ctx, []Point{{A: x + 50, B: y + 50}, {A: x + 150, B: y + 50}})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"center", "original"} {
		t.Run(mode, func(t *testing.T) {
			cfg := frequencyFixture(t)
			cfg.ZoneMode = mode
			cfg.ZoneSizeM = 100
			cfg.FrequencyInputs = cfg.FrequencyInputs[:1]
			content := "Date;Time;Latitude;Longitude;PCI;MCC;MNC;SSRef;RSRP\n"
			for i, p := range gps {
				content += fmt.Sprintf("2026-09-10;09:00:%02d;%.9f;%.9f;%d;231;2;2110000000;%d\n", i*10, p.B, p.A, i+1, []int{-80, -40, -50}[i])
			}
			if err := os.WriteFile(cfg.FrequencyInputs[0].FilePath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := RunProcessing(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if result.UniqueZones != 2 || result.TotalZoneRows != 2 {
				t.Fatalf("grid groups: %+v", result)
			}
			header, rows := readFrequencyCSV(t, result.Frequency5GFile)
			if indexOf(header, "Usek") >= 0 || indexOf(header, "Zona") < 0 || indexOf(header, technologyColumn) >= 0 {
				t.Fatal(header)
			}
			at := func(row []string, name string) string { return cellAt(row, indexOf(header, name)) }
			if at(rows[0], "RSRP") != "-40" || at(rows[0], "PCI") != "2" || at(rows[0], "Ostatne_PCI") != "1" {
				t.Fatalf("wrong maximum: %v", rows[0])
			}
			if at(rows[0], "Zona") != fmt.Sprintf("%v_%v", x, y) || at(rows[1], "Zona") != fmt.Sprintf("%v_%v", x+100, y) {
				t.Fatalf("grid keys or ordering: %v", rows)
			}
			expected := []Point{gps[0], gps[2]}
			if mode == "center" {
				expected = centerGPS
			}
			for i, row := range rows {
				lat, _ := strconv.ParseFloat(at(row, "Zona_latitude"), 64)
				lon, _ := strconv.ParseFloat(at(row, "Zona_longitude"), 64)
				if math.Abs(lat-expected[i].B) > 0.000001 || math.Abs(lon-expected[i].A) > 0.000001 {
					t.Fatalf("wrong %s position: %v vs %v", mode, Point{A: lon, B: lat}, expected[i])
				}
			}
			rawLat, _ := strconv.ParseFloat(at(rows[0], "Latitude"), 64)
			if math.Abs(rawLat-gps[1].B) > 1e-8 {
				t.Fatal("strongest measurement GPS was overwritten")
			}
			// A grid time window removes one measurement, not the whole occupied cell.
			cfg.TimeWindows = []TimeWindow{{Start: "2026-09-10T09:00:00", End: "2026-09-10T09:00:00"}}
			cut, err := RunProcessing(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if cut.UniqueZones != 2 || cut.ExcludedMeasurements != 1 || cut.ExcludedZones != 0 {
				t.Fatalf("grid time cut: %+v", cut)
			}
			_, cutRows := readFrequencyCSV(t, cut.Frequency5GFile)
			if at(cutRows[0], "Ostatne_PCI") != "" {
				t.Fatal("removed PCI survived time cut")
			}
			if mode == "original" {
				lat, _ := strconv.ParseFloat(at(cutRows[0], "Zona_latitude"), 64)
				if math.Abs(lat-gps[1].B) > 0.000001 {
					t.Fatal("first point did not follow time cut")
				}
			}
			cfg.TimeWindows = nil
			cfg.ZoneSizeM = 200
			bigger, err := RunProcessing(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if bigger.UniqueZones != 1 {
				t.Fatalf("grid side length ignored: %+v", bigger)
			}
		})
	}
}

func TestFrequencyMode_IndependentTechnologyMappings(t *testing.T) {
	cfg := frequencyFixture(t)
	nr, lte := cfg.FrequencyInputs[0], cfg.FrequencyInputs[2]
	cfg.FrequencyInputs = []FrequencyInput{nr, lte}
	header := "Latitude;Longitude;PCI;MCC;MNC;SSRef;Frequency;RSRP;SSS-RSRP;NR-MNC;LTE-MNC\n"
	nrData := header + "48.9;21.2;1;231;9;2110000000;;-10;-80;2;6\n48.9;21.2;2;231;9;2110000000;;-90;-30;2;6\n"
	lteData := header + "48.9;21.2;10;231;9;;2110000000;-20;-100;2;6\n48.9;21.2;11;231;9;;2110000000;-50;-10;2;6\n"
	if err := os.WriteFile(nr.FilePath, []byte(nrData), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lte.FilePath, []byte(lteData), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.FrequencyColumnMappings = map[string]map[string]string{}
	for _, tech := range []string{"lte", "5g"} {
		cfg.FrequencyColumnMappings[tech] = map[string]string{"latitude": "Latitude", "longitude": "Longitude", "pci": "PCI", "mcc": "MCC", "mnc": "NR-MNC", "rsrp": "SSS-RSRP"}
	}
	cfg.FrequencyColumnMappings["lte"]["rsrp"] = "RSRP"
	cfg.FrequencyColumnMappings["lte"]["mnc"] = "LTE-MNC"
	// These standard-mode settings must have no influence on frequency mode.
	cfg.ColumnMappingNames = map[string]string{"rsrp": "does-not-exist"}
	verify := func(nrPCI, ltePCI string) {
		t.Helper()
		result, err := RunProcessing(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if result.UniqueOperators != 2 || result.TotalZoneRows != 2 {
			t.Fatal(result)
		}
		for _, want := range []struct{ path, pci string }{{result.Frequency5GFile, nrPCI}, {result.FrequencyLTEFile, ltePCI}} {
			h, rows := readFrequencyCSV(t, want.path)
			if len(rows) != 1 || rows[0][indexOf(h, "PCI")] != want.pci {
				t.Fatalf("%s selected wrong maximum: %v", want.path, rows)
			}
		}
	}
	verify("2", "10")
	cfg.FrequencyColumnMappings["lte"]["rsrp"] = "SSS-RSRP"
	verify("2", "11")
	cfg.FrequencyColumnMappings["5g"]["rsrp"] = "RSRP"
	verify("1", "11")
	if cfg.FrequencyColumnMappings["lte"]["rsrp"] != "SSS-RSRP" {
		t.Fatal("caller mapping mutated")
	}
	delete(cfg.FrequencyColumnMappings["lte"], "rsrp")
	if _, err := RunProcessing(context.Background(), cfg); err == nil {
		t.Fatal("incomplete LTE mapping silently borrowed from NR")
	}
}

func TestFrequencyMode_MappingRequiredInEverySameTechnologyFile(t *testing.T) {
	cfg := frequencyFixture(t)
	cfg.FrequencyColumnMappings = map[string]map[string]string{}
	for _, tech := range []string{"lte", "5g"} {
		cfg.FrequencyColumnMappings[tech] = map[string]string{"latitude": "Latitude", "longitude": "Longitude", "pci": "PCI", "mcc": "MCC", "mnc": "MNC", "rsrp": "RSRP"}
	}
	// Known RSRP aliases still allow different exports within the same technology.
	if _, err := RunProcessing(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	cfg.FrequencyColumnMappings["5g"]["rsrp"] = "CustomPower"
	path := cfg.FrequencyInputs[0].FilePath
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "SSS-RSRP", "CustomPower", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	secondPath := cfg.FrequencyInputs[1].FilePath
	secondRaw, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(strings.Replace(string(secondRaw), "RSRP", "UnknownPower", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = RunProcessing(context.Background(), cfg)
	// Error paths use %q, which escapes Windows backslashes.
	wantError := fmt.Sprintf("CSV %q: chýba mapovaný stĺpec rsrp (CustomPower)", secondPath)
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("expected second NR file to fail its technology mapping: %v", err)
	}
}
