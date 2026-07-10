package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncMobileNRRejectsZeroUsableTimeMatches(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	ltePath := filepath.Join(tmp, "lte.csv")
	lte := "MCC;MNC;5G NR;Date;Time;Device\n" +
		"231;01;yes;05.02.2026;11:00:00;a\n"
	if err := os.WriteFile(ltePath, []byte(lte), 0o644); err != nil {
		t.Fatal(err)
	}
	fiveG := multiDeviceFiveGData([]string{"10:00:00"}, nil)

	_, _, err := syncMobileNRFromNSALTECSVNative(
		context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns),
		[]string{ltePath}, "5G NR", 0, nil, false,
	)
	if err == nil {
		t.Fatal("expected zero useful sync matches to fail")
	}
	if !strings.Contains(err.Error(), "žiadna časová zhoda") || !strings.Contains(err.Error(), "±0 ms") {
		t.Fatalf("unexpected zero-match error: %v", err)
	}
}

func TestSyncMobileNRDoesNotCountUnknownNRRowsAsMatches(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	ltePath := filepath.Join(tmp, "lte.csv")
	lte := "MCC;MNC;5G NR;Date;Time;Device\n" +
		"231;01;unknown;05.02.2026;10:00:00;a\n" +
		"231;01;yes;05.02.2026;11:00:00;a\n"
	if err := os.WriteFile(ltePath, []byte(lte), 0o644); err != nil {
		t.Fatal(err)
	}
	fiveG := multiDeviceFiveGData([]string{"10:00:00", "11:00:00"}, nil)

	out, stats, err := syncMobileNRFromNSALTECSVNative(
		context.Background(), fiveG, BuildColumnMappingFromHeaders(fiveG.Columns),
		[]string{ltePath}, "5G NR", 0, nil, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsWithMatch != 1 || stats.RowsYes != 1 || stats.RowsNo != 1 {
		t.Fatalf("unknown NR row counted as a match: %+v", stats)
	}
	nrIdx := out.columnIndexByName("5G NR")
	if cellAt(out.Rows[0], nrIdx) != "no" || cellAt(out.Rows[1], nrIdx) != "yes" {
		t.Fatalf("unexpected synced NR values: %#v", out.Rows)
	}
}

func TestRunProcessingRejectsMainSourceWithoutParseableTime(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	goodMainPath := filepath.Join(tmp, "good-5g.csv")
	badMainPath := filepath.Join(tmp, "bad-5g.csv")
	ltePath := filepath.Join(tmp, "lte.csv")
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time\n"
	if err := os.WriteFile(goodMainPath, []byte(header+"48.1;17.1;3500;10;231;01;-100;05.02.2026;10:00:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badMainPath, []byte(header+"48.2;17.2;3500;11;231;01;-101;bad-date;bad-time\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath, []byte("MCC;MNC;5G NR;Date;Time;Device\n231;01;yes;05.02.2026;10:00:00;a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := []string{goodMainPath, badMainPath}
	cfg := DefaultProcessingConfig()
	cfg.FilePath = goodMainPath
	cfg.InputFilePaths = paths
	cfg.FilterPaths = []string{}
	cfg.MobileModeEnabled = true
	cfg.MobileNSALTEFilePaths = []string{ltePath}
	cfg.ColumnMappingNames = map[string]string{
		"latitude": "latitude", "longitude": "longitude", "frequency": "frequency",
		"pci": "pci", "mcc": "mcc", "mnc": "mnc", "rsrp": "rsrp",
	}
	_, err := RunProcessing(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a main source with no parseable time to fail")
	}
	if !syncErrorMentionsPath(err, badMainPath) || !strings.Contains(err.Error(), "parsovateľný čas") {
		t.Fatalf("error must identify the invalid main source: %v", err)
	}
}

func TestRunProcessingRejectsMainSourceWithoutAnySyncMatch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	matchedMainPath := filepath.Join(tmp, "matched-5g.csv")
	unmatchedMainPath := filepath.Join(tmp, "unmatched-5g.csv")
	ltePath := filepath.Join(tmp, "lte.csv")
	header := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time\n"
	if err := os.WriteFile(matchedMainPath, []byte(header+"48.1;17.1;3500;10;231;01;-100;05.02.2026;10:00:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmatchedMainPath, []byte(header+"48.2;17.2;3500;11;231;01;-101;05.02.2026;12:00:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath, []byte("MCC;MNC;5G NR;Date;Time;Device\n231;01;yes;05.02.2026;10:00:00;a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultProcessingConfig()
	cfg.FilePath = matchedMainPath
	cfg.InputFilePaths = []string{matchedMainPath, unmatchedMainPath}
	cfg.FilterPaths = []string{}
	cfg.MobileModeEnabled = true
	cfg.MobileNSALTEFilePaths = []string{ltePath}
	cfg.MobileTimeToleranceMS = 0
	cfg.ColumnMappingNames = map[string]string{
		"latitude": "latitude", "longitude": "longitude", "frequency": "frequency",
		"pci": "pci", "mcc": "mcc", "mnc": "mnc", "rsrp": "rsrp",
	}

	_, err := RunProcessing(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a main source without any sync match to fail")
	}
	if !syncErrorMentionsPath(err, unmatchedMainPath) || !strings.Contains(err.Error(), "žiadna MCC/MNC + časová zhoda") {
		t.Fatalf("error must identify the unmatched main source: %v", err)
	}
}
