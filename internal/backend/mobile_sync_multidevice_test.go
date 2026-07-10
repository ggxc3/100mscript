package backend

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeMultiDeviceSyncCSV(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
	return path
}

func multiDeviceFiveGData(times []string, mncs []string) *CSVData {
	rows := make([][]string, len(times))
	for i, clock := range times {
		mnc := "01"
		if i < len(mncs) && mncs[i] != "" {
			mnc = mncs[i]
		}
		rows[i] = []string{
			"48.148600", "17.107700", "3500", strconv.Itoa(10 + i),
			"231", mnc, "-100", "05.02.2026", clock,
		}
	}
	return &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "Date", "Time"},
		Rows:    rows,
	}
}

func requireAllMultiDeviceRowsYes(t *testing.T, out *CSVData, stats mobileSyncStats) {
	t.Helper()
	if out == nil {
		t.Fatal("sync returned a nil dataset")
	}
	nrIdx := out.columnIndexByName("5G NR")
	if nrIdx < 0 {
		t.Fatalf("sync output has no 5G NR column: %#v", out.Columns)
	}
	if stats.RowsYes != len(out.Rows) || stats.RowsWithMatch != len(out.Rows) {
		t.Fatalf("not every multi-device row matched: stats=%+v rows=%#v", stats, out.Rows)
	}
	for i, row := range out.Rows {
		if got := normalizeNRValueNative(cellAt(row, nrIdx)); got != "yes" {
			t.Fatalf("row %d: expected 5G NR=yes, got %q (rows=%#v)", i, cellAt(row, nrIdx), out.Rows)
		}
	}
}

func TestSyncMobileNRMultiDeviceMixedTimeColumnsIsOrderIndependent(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	firstMS, ok := parseDateTimeToMillis("05.02.2026 10:00:00")
	if !ok {
		t.Fatal("fixture timestamp is not parseable")
	}
	utcPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-a", "nsa.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;UTC;Device;Note",
		"231;01;yes;" + strconv.FormatInt(firstMS/1000, 10) + ";a;utc-only",
	}, "\n")+"\n")
	dateTimePath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-b", "nsa.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:01:00;b",
	}, "\n")+"\n")

	fiveG := multiDeviceFiveGData([]string{"10:00:00", "10:01:00"}, nil)
	mapping := BuildColumnMappingFromHeaders(fiveG.Columns)
	orders := [][]string{{utcPath, dateTimePath}, {dateTimePath, utcPath}}
	for i, paths := range orders {
		out, stats, err := syncMobileNRFromNSALTECSVNative(
			context.Background(), fiveG, mapping, paths, "5G NR", 0, nil, false,
		)
		if err != nil {
			t.Fatalf("file order %d: sync failed: %v", i, err)
		}
		requireAllMultiDeviceRowsYes(t, out, stats)
	}
}

func TestSyncMobileNRMultiDeviceMixedEpochUnits(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	clocks := []string{"10:10:00", "10:11:00", "10:12:00", "10:13:00"}
	paths := make([]string, 0, len(clocks))
	for i, clock := range clocks {
		ms, ok := parseDateTimeToMillis("05.02.2026 " + clock)
		if !ok {
			t.Fatalf("fixture timestamp %q is not parseable", clock)
		}
		values := []int64{ms / 1000, ms, ms * 1000, ms * 1_000_000}
		path := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-"+strconv.Itoa(i), "nsa.csv"), strings.Join([]string{
			"MCC;MNC;5G NR;UTC;Device;Note",
			"231;01;yes;" + strconv.FormatInt(values[i], 10) + ";" + strconv.Itoa(i) + ";epoch",
		}, "\n")+"\n")
		paths = append(paths, path)
	}
	// Deliberately avoid magnitude order so sync cannot accidentally rely on file order.
	paths = []string{paths[2], paths[0], paths[3], paths[1]}

	fiveG := multiDeviceFiveGData(clocks, nil)
	out, stats, err := syncMobileNRFromNSALTECSVNative(
		context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns), paths, "5G NR", 0, nil, false,
	)
	if err != nil {
		t.Fatalf("sync mixed UTC units: %v", err)
	}
	requireAllMultiDeviceRowsYes(t, out, stats)
}

func TestSyncMobileNRMultiDeviceNormalizesMNCFormatting(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	ltePath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "nsa.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231.0;1.0;yes;05.02.2026;10:20:00;meter-b",
	}, "\n")+"\n")
	fiveG := multiDeviceFiveGData([]string{"10:20:00"}, []string{"01"})

	out, stats, err := syncMobileNRFromNSALTECSVNative(
		context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns), []string{ltePath}, "5G NR", 0, nil, false,
	)
	if err != nil {
		t.Fatalf("sync differently formatted operator codes: %v", err)
	}
	requireAllMultiDeviceRowsYes(t, out, stats)
}

func TestSyncMobileNRMultiDeviceNRAliasesAreMergedPerRow(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveG := multiDeviceFiveGData([]string{"10:30:00", "10:31:00"}, nil)
	mapping := BuildColumnMappingFromHeaders(fiveG.Columns)
	longNamePath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-a", "nsa.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:30:00;a",
	}, "\n")+"\n")
	shortNamePath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-b", "nsa.csv"), strings.Join([]string{
		"MCC;MNC;NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:31:00;b",
	}, "\n")+"\n")

	orders := [][]string{{longNamePath, shortNamePath}, {shortNamePath, longNamePath}}
	for i, paths := range orders {
		out, stats, err := syncMobileNRFromNSALTECSVNative(
			context.Background(), fiveG, mapping, paths, "5G NR", 0, nil, false,
		)
		if err != nil {
			t.Fatalf("file order %d: sync NR aliases: %v", i, err)
		}
		requireAllMultiDeviceRowsYes(t, out, stats)
	}
}

func TestSyncMobileNRMultiDeviceMixedTimeColumnsInMainFiles(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	firstMS, ok := parseDateTimeToMillis("05.02.2026 10:35:00")
	if !ok {
		t.Fatal("fixture timestamp is not parseable")
	}
	utcFiveGPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-a", "5g.csv"), strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;UTC;Device",
		"48.148600;17.107700;3500;10;231;01;-100;" + strconv.FormatInt(firstMS/1000, 10) + ";a",
	}, "\n")+"\n")
	dateTimeFiveGPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-b", "5g.csv"), strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;Device",
		"48.148700;17.107800;3500;11;231;01;-101;05.02.2026;10:36:00;b",
	}, "\n")+"\n")
	ltePath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "nsa.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:35:00;a",
		"231;01;yes;05.02.2026;10:36:00;b",
	}, "\n")+"\n")

	orders := [][]string{{utcFiveGPath, dateTimeFiveGPath}, {dateTimeFiveGPath, utcFiveGPath}}
	for i, paths := range orders {
		fiveG, err := LoadAndMergeCSVFiles(context.Background(), paths)
		if err != nil {
			t.Fatalf("file order %d: merge main files: %v", i, err)
		}
		out, stats, err := syncMobileNRFromNSALTECSVNative(
			context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns),
			[]string{ltePath}, "5G NR", 0, nil, false,
		)
		if err != nil {
			t.Fatalf("file order %d: sync mixed main time columns: %v", i, err)
		}
		requireAllMultiDeviceRowsYes(t, out, stats)
	}
}

func TestSyncMobileNRMultiDeviceReportsMalformedFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	validPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "valid.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:40:00;a",
	}, "\n")+"\n")
	fiveG := multiDeviceFiveGData([]string{"10:40:00"}, nil)

	badFixtures := map[string]string{
		"empty.csv":     "",
		"malformed.csv": "foo;bar;baz;qux;quux;corge\n1;2;3;4;5;6\n",
	}
	for name, content := range badFixtures {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			badPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, name), content)
			_, _, err := syncMobileNRFromNSALTECSVNative(
				context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns),
				[]string{validPath, badPath}, "5G NR", 0, nil, false,
			)
			if err == nil {
				t.Fatal("expected malformed input to fail explicitly")
			}
			if !strings.Contains(err.Error(), badPath) {
				t.Fatalf("error must identify the bad file, got: %v", err)
			}
			if !strings.Contains(err.Error(), "MCC") && !strings.Contains(err.Error(), "decode") {
				t.Fatalf("error must explain why the bad file cannot be used, got: %v", err)
			}
		})
	}
}

func TestSyncMobileNRMultiDeviceRejectsFileWithoutUsableSyncRows(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	validPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "valid.csv"), strings.Join([]string{
		"MCC;MNC;5G NR;Date;Time;Device",
		"231;01;yes;05.02.2026;10:50:00;a",
	}, "\n")+"\n")
	fiveG := multiDeviceFiveGData([]string{"10:50:00"}, nil)

	badFixtures := map[string]string{
		"bad-time.csv": strings.Join([]string{
			"MCC;MNC;5G NR;Date;Time;Device",
			"231;01;yes;not-a-date;not-a-time;b",
		}, "\n") + "\n",
		"bad-operator.csv": strings.Join([]string{
			"MCC;MNC;5G NR;Date;Time;Device",
			";;yes;05.02.2026;10:51:00;b",
		}, "\n") + "\n",
		"bad-nr.csv": strings.Join([]string{
			"MCC;MNC;5G NR;Date;Time;Device",
			"231;01;unknown;05.02.2026;10:51:00;b",
		}, "\n") + "\n",
	}
	for name, content := range badFixtures {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			badPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, name), content)
			_, _, err := syncMobileNRFromNSALTECSVNative(
				context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns),
				[]string{validPath, badPath}, "5G NR", 0, nil, false,
			)
			if err == nil {
				t.Fatal("expected an explicitly selected but unusable device file to fail")
			}
			if !strings.Contains(err.Error(), badPath) {
				t.Fatalf("error must identify the unusable device file, got: %v", err)
			}
		})
	}
}

func TestMultiDeviceMergePrefersSelectedCanonicalColumnDeterministically(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	firstPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-a.csv"), strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;SSS-RSRP;RSRP",
		"48.148600;17.107700;3500;10;231;01;-100;-1",
	}, "\n")+"\n")
	secondPath := writeMultiDeviceSyncCSV(t, filepath.Join(tmp, "meter-b.csv"), strings.Join([]string{
		"latitude;longitude;frequency;pci;mcc;mnc;SSS-RSRP",
		"48.148700;17.107800;3500;11;231;01;-101",
	}, "\n")+"\n")
	cfg := DefaultProcessingConfig()
	cfg.ColumnMappingNames = map[string]string{
		"latitude": "latitude", "longitude": "longitude", "frequency": "frequency",
		"pci": "pci", "mcc": "mcc", "mnc": "mnc", "rsrp": "SSS-RSRP",
	}

	// Go deliberately randomizes map iteration. Repeating this catches a merge that
	// picks an equivalent source column by ranging over a map instead of preferring
	// the exact column selected by the user.
	for attempt := 0; attempt < 100; attempt++ {
		data, mapping, err := LoadAndMergeCSVFilesForProcessing(
			context.Background(), []string{firstPath, secondPath}, cfg,
		)
		if err != nil {
			t.Fatalf("attempt %d: merge: %v", attempt, err)
		}
		if got := cellAt(data.Rows[0], mapping["rsrp"]); got != "-100" {
			t.Fatalf("attempt %d: selected SSS-RSRP was replaced by alias RSRP value %q", attempt, got)
		}
	}
}
