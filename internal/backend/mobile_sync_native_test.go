package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncMobileNRFromLTECSVNative_fillsNRFromLTE(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath := filepath.Join(tmp, "lte.csv")

	fiveG := "" +
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;5G NR\n" +
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00;\n"
	lte := "" +
		"MCC;MNC;5G NR;Date;Time\n" +
		"231;01;yes;05.02.2026;10:00:00\n"

	if err := os.WriteFile(fiveGPath, []byte(fiveG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath, []byte(lte), 0o644); err != nil {
		t.Fatal(err)
	}

	df5g, err := LoadCSVFile(fiveGPath)
	if err != nil {
		t.Fatal(err)
	}
	df5g, err = ensureOriginalExcelRowColumn(df5g)
	if err != nil {
		t.Fatal(err)
	}
	mapping := BuildColumnMappingFromHeaders(df5g.Columns)

	out, st, err := syncMobileNRFromLTECSVNative(df5g, mapping, ltePath, "5G NR", 1000, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.RowsYes != 1 || st.RowsWithMatch < 1 {
		t.Fatalf("stats: %+v", st)
	}
	nrIdx := out.columnIndexByName("5G NR")
	if nrIdx < 0 {
		t.Fatal("missing 5G NR column")
	}
	if strings.TrimSpace(out.Rows[0][nrIdx]) != "yes" {
		t.Fatalf("expected NR yes, got %q", out.Rows[0][nrIdx])
	}
}

func TestSyncMobileNRFromLTECSVNative_requiresNRYesInLTE(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath := filepath.Join(tmp, "lte.csv")

	fiveG := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time\n48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00\n"
	lte := "MCC;MNC;5G NR;Date;Time\n231;01;no;05.02.2026;10:00:00\n"

	if err := os.WriteFile(fiveGPath, []byte(fiveG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath, []byte(lte), 0o644); err != nil {
		t.Fatal(err)
	}

	df5g, err := LoadCSVFile(fiveGPath)
	if err != nil {
		t.Fatal(err)
	}
	df5g, err = ensureOriginalExcelRowColumn(df5g)
	if err != nil {
		t.Fatal(err)
	}
	mapping := BuildColumnMappingFromHeaders(df5g.Columns)

	_, _, err = syncMobileNRFromLTECSVNative(df5g, mapping, ltePath, "5G NR", 1000, nil, false)
	if err == nil || !strings.Contains(err.Error(), "yes") {
		t.Fatalf("expected error about missing NR yes, got %v", err)
	}
}

func TestNormalizeNRValueNative(t *testing.T) {
	t.Parallel()

	if normalizeNRValueNative("  YES ") != "yes" {
		t.Fatal()
	}
	if normalizeNRValueNative("ÁNO") != "yes" {
		t.Fatal()
	}
	if normalizeNRValueNative("0") != "no" {
		t.Fatal()
	}
	if normalizeNRValueNative("maybe") != "" {
		t.Fatal()
	}
}

func TestNrScore(t *testing.T) {
	t.Parallel()

	if nrScore("yes") != 2 || nrScore("no") != 1 || nrScore("") != 0 {
		t.Fatal()
	}
}

func TestResolveWindowScore_conflict(t *testing.T) {
	t.Parallel()

	l := buildMobileLookup([]int64{100, 101, 102}, []int8{2, 1, 0})
	score, matched, conflict := resolveWindowScore(l, 101, 10)
	if !matched || !conflict || score != 2 {
		t.Fatalf("got score=%d matched=%v conflict=%v", score, matched, conflict)
	}
}
