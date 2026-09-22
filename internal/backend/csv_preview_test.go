package backend

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type previewCountingReader struct {
	io.Reader
	count int
}

func (r *previewCountingReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.count += n
	return n, e
}

func TestCSVSchemaBoundedAndCompatible(t *testing.T) {
	for _, content := range []string{
		"Device;metadata\r\nLatitude;Longitude;MCC;MNC;RSRP;Frequency;Note\r\n48;17;231;1;-80;2112500000;\"a;b\";231/1\r\n",
		"a;b;c;d;e;f\n1;2;3;4;5;6;7\n",
		"metadata\nMCC;MNC;5G NR;Date;Time\n231;1;yes;05.02.2026;10:00:00\n",
		"Latitude;Longitude;MCC;MNC;RSRP;Frequency;Note\n48;17;231;1;-80;2112500000;\xe9\n",
	} {
		schema, err := readCSVSchema(strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		full, err := parseCSVBytes([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(schema.Columns, full.Columns) || schema.FileInfo != full.FileInfo || len(schema.Rows) != 0 {
			t.Fatalf("schema=%+v full=%+v", schema, full)
		}
	}
	prefix := "Latitude;Longitude;MCC;MNC;RSRP;Frequency\n" + strings.Repeat("48;17;231;1;-80;2112500000\n", 50000)
	reader := &previewCountingReader{Reader: strings.NewReader(prefix)}
	schema, err := readCSVSchema(reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader.count > csvPreviewMaxBytes+1 || indexOf(schema.Columns, "Frequency") < 0 {
		t.Fatal(reader.count, schema.Columns)
	}
	for _, content := range []string{"", strings.Repeat("x", csvPreviewMaxBytes+1)} {
		if _, err := readCSVSchema(strings.NewReader(content)); err == nil {
			t.Fatal("expected invalid preview error")
		}
	}
}

func TestCSVLoaderLateExtraColumns(t *testing.T) {
	prefix := "Latitude;Longitude;MCC;MNC;RSRP;Frequency\n" + strings.Repeat("48;17;231;1;-80;2112500000\n", 50000)
	full, err := parseCSVBytes([]byte(prefix + "48;17;231;1;-80;2112500000;231/2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(full.Columns, "extra_col_1") < 0 || len(full.Rows[0]) != 7 || full.Rows[len(full.Rows)-1][6] != "231/2" {
		t.Fatal("lost late PLMN column")
	}
}

func TestFrequencySplitRouteChronologyAndProvenance(t *testing.T) {
	dir := t.TempDir()
	header := "Latitude;Longitude;MCC;MNC;RSRP;Frequency;UTC;PCI\n"
	cfg := DefaultProcessingConfig()
	cfg.FrequencyModeEnabled = true
	cfg.FilterPaths = []string{}
	cfg.ZoneMode = "center"
	// Select the later part first and put its records out of order deliberately.
	for i, content := range []string{
		"48;17;231;1;-50;2112500000;2026-09-22T10:00:03Z;3\n48;17;231;1;-50;2112500000;2026-09-22T10:00:02Z;2\n",
		"48;17;231;1;-50;2112500000;2026-09-22T10:00:01Z;1\n",
	} {
		path := filepath.Join(dir, []string{"later.csv", "earlier.csv"}[i])
		if err := os.WriteFile(path, []byte(header+content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg.FrequencyInputs = append(cfg.FrequencyInputs, FrequencyInput{FilePath: path, Technology: "lte", FrequencyColumn: "Frequency"})
	}
	cfg.FrequencyLTEOutput = filepath.Join(dir, "lte.csv")
	cfg.Frequency5GOutput = filepath.Join(dir, "nr.csv")
	result, err := RunProcessing(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	columns, rows := readFrequencyCSV(t, result.FrequencyLTEFile)
	if len(rows) != 1 {
		t.Fatal(len(rows))
	}
	row := rows[0]
	for key, want := range map[string]string{"PCI": "1", "Pocet_merani": "3", "Ostatne_PCI": "2, 3", "original_excel_row": "2", frequencySourceColumn: cfg.FrequencyInputs[1].FilePath} {
		if got := row[indexOf(columns, key)]; got != want {
			t.Fatalf("%s: got %q want %q", key, got, want)
		}
	}
}

func BenchmarkCSVUnquotedRows(b *testing.B) {
	raw := []byte("Latitude;Longitude;MCC;MNC;RSRP;Frequency\n" + strings.Repeat("48;17;231;1;-80;2112500000\n", 10000))
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := parseCSVBytes(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCSVSchema(b *testing.B) {
	raw := []byte("Latitude;Longitude;MCC;MNC;RSRP;Frequency\n" + strings.Repeat("48;17;231;1;-80;2112500000\n", 100000))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := readCSVSchema(bytes.NewReader(raw)); err != nil {
			b.Fatal(err)
		}
	}
}

// Compare multi-part frequency processing to the original mode's chronological
// merge and spatial assignment, not to concatenating files into a single track.
func TestFrequencySplitSegmentsUseOriginalMergeGeometry(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "later.csv"), filepath.Join(dir, "earlier.csv")}
	header := "Latitude;Longitude;MCC;MNC;RSRP;Frequency;UTC;PCI\n"
	contents := []string{
		"48;17.006;231;1;-80;2113000000;2026-09-22T10:00:03Z;3\n48;17.004;231;1;-80;2112000000;2026-09-22T10:00:02Z;2\n",
		"48;17;231;1;-80;2110000000;2026-09-22T10:00:00Z;0\n48;17.002;231;1;-80;2111000000;2026-09-22T10:00:01Z;1\n",
	}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte(header+contents[i]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"segments", "center", "original"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultProcessingConfig()
			cfg.ZoneMode = mode
			cfg.ZoneSizeM = 100
			cfg.FilterPaths = []string{}
			cfg.ColumnMappingNames = map[string]string{"latitude": "Latitude", "longitude": "Longitude", "mcc": "MCC", "mnc": "MNC", "pci": "PCI", "rsrp": "RSRP", "frequency": "Frequency"}
			data, mapping, err := LoadAndMergeCSVFilesForProcessing(context.Background(), paths, cfg)
			if err != nil {
				t.Fatal(err)
			}
			data, _ = sortMergedCSVRowsByTime(data)
			cfg.ColumnMapping = mapping
			tr, err := NewPyProjTransformer()
			if err != nil {
				t.Fatal(err)
			}
			standard, err := ProcessDataNative(context.Background(), data, cfg, tr)
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{}
			for _, row := range standard.Rows {
				expected[row.PCI] = row.ZonaKey
			}
			cfg.FrequencyModeEnabled = true
			for _, path := range paths {
				cfg.FrequencyInputs = append(cfg.FrequencyInputs, FrequencyInput{FilePath: path, Technology: "lte", FrequencyColumn: "Frequency"})
			}
			cfg.FrequencyLTEOutput = filepath.Join(dir, mode+"-lte.csv")
			cfg.Frequency5GOutput = filepath.Join(dir, mode+"-5g.csv")
			result, err := RunProcessing(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			columns, rows := readFrequencyCSV(t, result.FrequencyLTEFile)
			zone := "Zona"
			if mode == "segments" {
				zone = "Usek"
			}
			if len(rows) != len(expected) {
				t.Fatal("missing frequencies", len(rows))
			}
			for _, row := range rows {
				pci := row[indexOf(columns, "PCI")]
				if got := row[indexOf(columns, zone)]; got != expected[pci] {
					t.Fatalf("PCI %s: frequency=%s original=%s", pci, got, expected[pci])
				}
			}
		})
	}
}
