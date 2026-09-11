package backend

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestFrequencyFilters_ChannelConditionsKeepChannelUnits(t *testing.T) {
	for _, channel := range []string{"EARFCN", "NR-ARFCN"} {
		t.Run(channel, func(t *testing.T) {
			columns := []string{"MCC", "MNC", frequencyHzColumn, channel}
			mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2}
			rule := FilterRule{Name: "channel", Assignments: map[string][]float64{"MNC": {6}}, ConditionGroups: [][]Condition{{{Field: channel, Kind: ConditionEq, Low: 422000}}}}
			rules, err := compileFrequencyRules([]FilterRule{rule}, columns, mapping)
			if err != nil {
				t.Fatal(err)
			}
			if frequencyOperatorMatches([]string{"231", "2", "2110000000", "422000"}, 2110000000, rules) {
				t.Fatal("channel condition compared against physical Hz instead of channel number")
			}
			if _, err := compileFrequencyRules([]FilterRule{rule}, columns[:3], mapping); err == nil {
				t.Fatal("missing channel silently treated as Hz")
			}
		})
	}
}

func TestFrequencyFilters_AssignmentAliasesCombine(t *testing.T) {
	columns := []string{"MCC", "MNC", frequencyHzColumn}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2}
	rule := FilterRule{Name: "aliases", Assignments: map[string][]float64{"MNC": {2}, "mnc": {6}}, ConditionGroups: [][]Condition{{{Field: "Frequency", Kind: ConditionEq, Low: 2110000000}}}}
	for i := 0; i < 100; i++ {
		rules, err := compileFrequencyRules([]FilterRule{rule}, columns, mapping)
		if err != nil {
			t.Fatal(err)
		}
		if frequencyOperatorMatches([]string{"231", "2", "2110000000"}, 2110000000, rules) {
			t.Fatal("alias assignment overwrote a different operator assignment")
		}
	}
}

// Compare the frequency checker with the independent, existing filter execution
// engine. The oracle actually applies every assignment combination to a copy.
func legacyFrequencyMatch(t *testing.T, raw []string, columns []string, mapping map[string]int, rules []FilterRule, f float64) bool {
	t.Helper()
	row := append([]string(nil), raw...)
	row[mapping["frequency"]] = strconv.FormatFloat(f, 'f', -1, 64)
	data, err := ApplyFiltersCSV(context.Background(), &CSVData{Columns: columns, Rows: [][]string{row}}, rules, false, mapping)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range data.Rows {
		for _, key := range []string{"mcc", "mnc"} {
			got, gok := strconv.ParseFloat(out[mapping[key]], 64)
			want, wok := strconv.ParseFloat(row[mapping[key]], 64)
			if gok != nil || wok != nil || got != want {
				return false
			}
		}
	}
	return true
}

func TestFrequencyFilters_DifferentialLegacy(t *testing.T) {
	columns := []string{"MCC", "MNC", "Frequency", "PCI", "RSRP", "EARFCN"}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2, "pci": 3, "rsrp": 4}
	rng := rand.New(rand.NewSource(2100))
	checks := 0
	for trial := 0; trial < 5000; trial++ {
		var rules []FilterRule
		for n := 0; n < 1+rng.Intn(7); n++ {
			rule := FilterRule{Name: fmt.Sprintf("%02d.txt", n), Assignments: map[string][]float64{}}
			for _, field := range []string{"MCC", "MNC", "PCI"} {
				if rng.Intn(3) == 0 {
					continue
				}
				value := float64(rng.Intn(4) + 1)
				if field == "MCC" {
					value += 229
				}
				rule.Assignments[field] = []float64{value}
				if rng.Intn(4) == 0 {
					rule.Assignments[field] = append(rule.Assignments[field], value+1)
				}
			}
			for g := 0; g < 1+rng.Intn(3); g++ {
				var group []Condition
				for c := 0; c < 1+rng.Intn(4); c++ {
					field := []string{"Frequency", "MNC", "MCC", "PCI", "RSRP", "EARFCN"}[rng.Intn(6)]
					low := float64(rng.Intn(4) + 1)
					high := low
					switch field {
					case "Frequency":
						low = 2110000000 + float64(rng.Intn(9)-4)*2500000
					case "MCC":
						low += 229
					case "RSRP":
						low = -100 + low*10
					case "EARFCN":
						low = 100 + low
					}
					kind := ConditionEq
					if rng.Intn(2) == 0 {
						kind = ConditionRange
						high = low + float64(rng.Intn(4))
						if field == "Frequency" {
							high = low + float64(rng.Intn(4))*2500000
						}
					} else {
						high = low
					}
					group = append(group, Condition{Field: field, Kind: kind, Low: low, High: high})
				}
				rule.ConditionGroups = append(rule.ConditionGroups, group)
			}
			rules = append(rules, rule)
		}
		rng.Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
		compiled, err := compileFrequencyRules(rules, columns, mapping)
		if err != nil {
			t.Fatal(err)
		}
		raw := []string{"231", strconv.Itoa(1 + rng.Intn(4)), "2110000000", strconv.Itoa(1 + rng.Intn(4)), "-80", strconv.Itoa(101 + rng.Intn(4))}
		before := append([]string(nil), raw...)
		for _, bw := range []float64{0, 0.000001, 0.1, 2.5, 5, 10, 20} {
			f := 2110000000.0
			expected := []bool{}
			for _, probe := range []float64{f, f - bw*1e6, f + bw*1e6} {
				want := legacyFrequencyMatch(t, raw, columns, mapping, rules, probe)
				if got := frequencyOperatorMatches(raw, probe, compiled); got != want {
					t.Fatalf("trial %d probe %.9f: got %v want %v rules=%+v raw=%v", trial, probe, got, want, rules, raw)
				}
				expected = append(expected, want)
				checks++
			}
			cfg := DefaultProcessingConfig()
			cfg.FrequencyLTEBW = bw
			a, b := frequencyFilterFlags(ProcessedRow{Raw: raw, Frequency: raw[2]}, "LTE", cfg, map[string][]frequencyRule{"LTE": compiled})
			if (a == "yes") != expected[0] || (b == "yes") != (expected[0] && expected[1] && expected[2]) {
				t.Fatalf("wrong three-point flags %s/%s", a, b)
			}
		}
		if !reflect.DeepEqual(raw, before) {
			t.Fatal("checker mutated original row")
		}
	}
	t.Logf("%d independent legacy comparisons, 5000 rule sets, 7 BW values", checks)
}

func TestFrequencyFilters_AllThreePointCombinations(t *testing.T) {
	columns := []string{"MCC", "MNC", "Frequency"}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2}
	for mask := 0; mask < 8; mask++ {
		for _, tech := range []string{"LTE", "5G"} {
			var rules []FilterRule
			for i, f := range []float64{2110000000, 2105000000, 2115000000} {
				target := 2.
				if mask&(1<<i) != 0 {
					target = 6
				}
				rules = append(rules, FilterRule{Name: fmt.Sprint(i), Assignments: map[string][]float64{"MNC": {target}}, ConditionGroups: [][]Condition{{{Field: "Frequency", Kind: ConditionEq, Low: f}}}})
			}
			compiled, err := compileFrequencyRules(rules, columns, mapping)
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultProcessingConfig()
			if tech == "LTE" {
				cfg.FrequencyLTEBW = 5
			} else {
				cfg.Frequency5GBW = 5
			}
			a, b := frequencyFilterFlags(ProcessedRow{Raw: []string{"231", "2", "2110000000"}, Frequency: "2110000000"}, tech, cfg, map[string][]frequencyRule{tech: compiled})
			if (a == "yes") != (mask&1 == 0) || (b == "yes") != (mask == 0) {
				t.Fatalf("tech %s mask %03b: %s/%s", tech, mask, a, b)
			}
		}
	}
}

func TestFrequencyFilters_EveryShippedBoundary(t *testing.T) {
	paths, err := DiscoverFilterPaths(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadFilterRulesFromPaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 8 {
		t.Fatalf("expected 8 supplied filters, got %d", len(rules))
	}
	columns := []string{"MCC", "MNC", "Frequency"}
	mapping := map[string]int{"mcc": 0, "mnc": 1, "frequency": 2}
	checks := 0
	for _, rule := range rules {
		compiled, err := compileFrequencyRules([]FilterRule{rule}, columns, mapping)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range rule.ConditionGroups {
			for _, cond := range group {
				if cond.Field != "Frequency" {
					continue
				}
				for _, bound := range []float64{cond.Low, cond.High} {
					for _, f := range []float64{math.Nextafter(bound, math.Inf(-1)), bound, math.Nextafter(bound, math.Inf(1))} {
						for _, mcc := range []string{"230", "231", "232"} {
							for _, mnc := range []string{"1", "2", "3", "6"} {
								raw := []string{mcc, mnc, strconv.FormatFloat(f, 'f', -1, 64)}
								want := legacyFrequencyMatch(t, raw, columns, mapping, []FilterRule{rule}, f)
								if got := frequencyOperatorMatches(raw, f, compiled); got != want {
									t.Fatalf("%s f=%.9f operator=%s/%s got %v want %v", rule.Name, f, mcc, mnc, got, want)
								}
								checks++
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d supplied-filter boundary checks (exact, previous and next representable float)", checks)
}

func TestFrequencyOutputs_LeadingBlankLine(t *testing.T) {
	for _, mode := range []string{"segments", "center", "original"} {
		cfg := frequencyFixture(t)
		cfg.ZoneMode = mode
		// Include a header-only LTE output as well as populated 5G output.
		cfg.FrequencyInputs = cfg.FrequencyInputs[:2]
		result, err := RunProcessing(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{result.Frequency5GFile, result.FrequencyLTEFile} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(raw), "\n")
			if len(lines) < 3 || lines[0] != "" || lines[1] == "" || !strings.Contains(lines[1], "Operator_sedi_BW") || strings.Contains(lines[1], "Operator_sedi_bV") {
				t.Fatalf("invalid export prefix %s: %q", mode, lines[:2])
			}
		}
	}
}

// Full pipeline: actual 290 MB input files, each spatial mode, independent LTE
// and NR deltas, and filters explicitly disabled. Exported files can additionally
// be audited by scripts/audit_frequency_outputs.py without any Go helpers.
func TestFrequencyMode_Real2100BWAudit(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("set RUN_LARGE_REAL_DATA_TESTS=1 for full 2100 audit")
	}
	root := os.Getenv("FREQUENCY_AUDIT_OUTPUT_DIR")
	for _, mode := range []string{"segments", "center", "original"} {
		for _, bw := range []struct{ label, lte, nr, off string }{
			{"bw_0_0", "0", "0", "0"}, {"bw_0.1_0.5", "0.1", "0.5", "0"}, {"bw_2.5_5", "2.5", "5", "0"},
			{"bw_5_5", "5", "5", "0"}, {"bw_10_20", "10", "20", "0"}, {"bw_20_10", "20", "10", "0"}, {"filters_off", "5", "5", "1"},
		} {
			t.Run(mode+"/"+bw.label, func(t *testing.T) {
				t.Setenv("FREQUENCY_TEST_ZONE_MODE", mode)
				t.Setenv("FREQUENCY_TEST_LTE_BW", bw.lte)
				t.Setenv("FREQUENCY_TEST_5G_BW", bw.nr)
				t.Setenv("FREQUENCY_TEST_NO_FILTERS", bw.off)
				if root != "" {
					t.Setenv("FREQUENCY_TEST_OUTPUT_DIR", filepath.Join(root, mode, bw.label))
				}
				TestFrequencyMode_Real2100(t)
			})
		}
	}
}

func TestFrequencyMode_FilterUsesCustomOperatorMapping(t *testing.T) {
	cfg := frequencyFixture(t)
	cfg.FrequencyInputs = cfg.FrequencyInputs[2:]
	cfg.FrequencyColumnMappings = map[string]map[string]string{"lte": {"latitude": "Latitude", "longitude": "Longitude", "pci": "PCI", "mcc": "MCC", "mnc": "NetworkCode", "rsrp": "RSRP"}}
	for _, input := range cfg.FrequencyInputs {
		raw, err := os.ReadFile(input.FilePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(input.FilePath, []byte(strings.Replace(string(raw), "MNC", "NetworkCode", 1)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	filter := filepath.Join(filepath.Dir(cfg.FrequencyLTEOutput), "custom.txt")
	if err := os.WriteFile(filter, []byte(`("NetworkCode" = 6); ("Frequency" = 2110000000 AND "NetworkCode" = 2)`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.FrequencyLTEFilters = []string{filter}
	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	header, rows := readFrequencyCSV(t, result.FrequencyLTEFile)
	if len(rows) != 1 || rows[0][indexOf(header, "Operator_sedi")] != "no" || rows[0][indexOf(header, "NetworkCode")] != "2" {
		t.Fatalf("custom mapped MNC not checked correctly: %v", rows)
	}
}
