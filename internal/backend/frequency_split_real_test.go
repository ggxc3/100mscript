package backend

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Keep the stress fixture local: split every supplied measurement into smaller
// files, including the original preamble/header and all trailing PLMN fields.
func TestFrequencyMode_Real2100SplitFiles(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("local 2100 data")
	}
	root := os.Getenv("FREQUENCY_REAL_DATA_ROOT")
	if root == "" {
		root = filepath.Join("..", "..", "data", "2100")
	}
	sources, err := filepath.Glob(filepath.Join(root, "*.csv"))
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	dir := t.TempDir()
	var whole, parts []FrequencyInput
	for source, p := range sources {
		tech, freq := "lte", "Frequency"
		if strings.Contains(p, "5G NR") {
			tech, freq = "5g", "SSRef"
		}
		whole = append(whole, FrequencyInput{FilePath: p, Technology: tech, FrequencyColumn: freq})
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 65536), 16<<20)
		var preamble strings.Builder
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			preamble.WriteString(line + "\n")
			if strings.HasPrefix(line, "Date;Time;UTC;Latitude;Longitude;") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing header", p)
		}
		var out *os.File
		var writer *bufio.Writer
		count := 0
		part := 0
		closePart := func() {
			if writer != nil {
				if err := writer.Flush(); err != nil {
					t.Fatal(err)
				}
				if err := out.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
		for scanner.Scan() {
			if count%100000 == 0 {
				closePart()
				name := filepath.Join(dir, fmt.Sprintf("%d-%s-%02d.csv", source, tech, part))
				part++
				out, err = os.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				writer = bufio.NewWriter(out)
				if _, err := writer.WriteString(preamble.String()); err != nil {
					t.Fatal(err)
				}
				parts = append(parts, FrequencyInput{FilePath: name, Technology: tech, FrequencyColumn: freq})
			}
			if _, err := writer.WriteString(scanner.Text() + "\n"); err != nil {
				t.Fatal(err)
			}
			count++
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		closePart()
		f.Close()
	}
	for _, mode := range []string{"segments", "center", "original"} {
		t.Run(mode, func(t *testing.T) {
			var expected map[string]string
			for _, scenario := range []struct {
				name   string
				inputs []FrequencyInput
			}{{"whole", whole}, {"split", parts}} {
				cfg := DefaultProcessingConfig()
				cfg.FrequencyModeEnabled = true
				cfg.FrequencyInputs = scenario.inputs
				cfg.FilterPaths = []string{}
				cfg.ZoneMode = mode
				cfg.ZoneSizeM = 100
				cfg.FrequencyColumnMappings = map[string]map[string]string{}
				for _, tech := range []string{"lte", "5g"} {
					cfg.FrequencyColumnMappings[tech] = map[string]string{"latitude": "Latitude", "longitude": "Longitude", "mcc": "MCC", "mnc": "MNC", "pci": "PCI", "rsrp": "RSRP", "sinr": "SINR"}
				}
				cfg.FrequencyColumnMappings["5g"]["rsrp"] = "SSS-RSRP"
				cfg.FrequencyColumnMappings["5g"]["sinr"] = "SSS-SINR"
				cfg.FrequencyLTEOutput = filepath.Join(dir, mode+scenario.name+"lte.csv")
				cfg.Frequency5GOutput = filepath.Join(dir, mode+scenario.name+"nr.csv")
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				start := time.Now()
				result, err := RunProcessing(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				runtime.ReadMemStats(&after)
				t.Logf("%s files=%d duration=%s allocated=%.1f MiB zones=%d rows=%d", scenario.name, len(scenario.inputs), time.Since(start), float64(after.TotalAlloc-before.TotalAlloc)/(1<<20), result.UniqueZones, result.TotalZoneRows)
				if result.CorrectedMNC != 105403 {
					t.Fatal(result)
				}
				header, rows := readBothFrequencyCSVs(t, result)
				actual := map[string]string{}
				measurements := 0
				for _, row := range rows {
					at := func(k string) string { return cellAt(row, indexOf(header, k)) }
					zone := at("Zona")
					if mode == "segments" {
						zone = at("Usek")
					}
					key := strings.Join([]string{at(technologyColumn), zone, at("MCC"), at("MNC"), at(frequencyHzColumn)}, "|")
					rsrp := at("RSRP")
					if at(technologyColumn) == "5G" {
						rsrp = at("SSS-RSRP")
					}
					actual[key] = strings.Join([]string{rsrp, at("Pocet_merani"), at("Operator_sedi"), at("Operator_sedi_BW")}, "|")
					n, err := strconv.Atoi(at("Pocet_merani"))
					if err != nil {
						t.Fatal(err)
					}
					measurements += n
					if at("Zdrojovy_subor") == "" || at("original_excel_row") == "" {
						t.Fatal("missing provenance")
					}
				}
				if measurements != 678520 {
					t.Fatalf("lost/duplicated measurements: %d", measurements)
				}
				raw, _ := json.Marshal(actual)
				t.Logf("%s group digest %x", scenario.name, sha256.Sum256(raw))
				if scenario.name == "whole" {
					expected = actual
				} else {
					differences := 0
					for k, v := range expected {
						if actual[k] != v {
							differences++
						}
					}
					if mode != "segments" && (len(actual) != len(expected) || differences > 0) {
						t.Fatalf("split route changed groups: before=%d after=%d differing=%d", len(expected), len(actual), differences)
					}
				}
			}
		})
	}
}
