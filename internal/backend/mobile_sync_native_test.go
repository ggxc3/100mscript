package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncMobileNRFromNSALTECSVNative_fillsNRFromLTE(t *testing.T) {
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

	out, st, err := syncMobileNRFromNSALTECSVNative(context.Background(), df5g, mapping, []string{ltePath}, "5G NR", 1000, nil, false)
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

func TestSyncMobileNRFromNSALTECSVNative_mergedTwoLTEFiles(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath1 := filepath.Join(tmp, "lte1.csv")
	ltePath2 := filepath.Join(tmp, "lte2.csv")

	header := "MCC;MNC;5G NR;Date;Time\n"
	fiveG := "" +
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;5G NR\n" +
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00;\n" +
		"48.148600;17.107700;3500;11;231;01;-100;05.02.2026;11:00:00;\n"
	lte1 := header + "231;01;yes;05.02.2026;10:00:00\n"
	lte2 := header + "231;01;yes;05.02.2026;11:00:00\n"

	if err := os.WriteFile(fiveGPath, []byte(fiveG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath1, []byte(lte1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath2, []byte(lte2), 0o644); err != nil {
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

	out, st, err := syncMobileNRFromNSALTECSVNative(
		context.Background(),
		df5g,
		mapping,
		[]string{ltePath1, ltePath2},
		"5G NR",
		1000,
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.RowsYes != 2 || st.RowsWithMatch < 2 {
		t.Fatalf("stats: %+v", st)
	}
	nrIdx := out.columnIndexByName("5G NR")
	if nrIdx < 0 {
		t.Fatal("missing 5G NR column")
	}
	if strings.TrimSpace(out.Rows[0][nrIdx]) != "yes" || strings.TrimSpace(out.Rows[1][nrIdx]) != "yes" {
		t.Fatalf("expected NR yes on both rows, got %q %q", out.Rows[0][nrIdx], out.Rows[1][nrIdx])
	}
}

func TestSyncMobileNRFromNSALTECSVNative_mergedLTEFilesWithExtraColumn(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath1 := filepath.Join(tmp, "lte1.csv")
	ltePath2 := filepath.Join(tmp, "lte2.csv")

	fiveG := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time\n48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00\n"
	lte1 := "MCC;MNC;5G NR;Date;Time\n231;01;yes;05.02.2026;10:00:00\n"
	lte2 := "MCC;MNC;ExtraCol;5G NR;Date;Time\n231;01;x;yes;05.02.2026;10:00:00\n"

	if err := os.WriteFile(fiveGPath, []byte(fiveG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath1, []byte(lte1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath2, []byte(lte2), 0o644); err != nil {
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

	_, st, err := syncMobileNRFromNSALTECSVNative(
		context.Background(),
		df5g,
		mapping,
		[]string{ltePath1, ltePath2},
		"5G NR",
		1000,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("expected LTE files to merge by name, got %v", err)
	}
	if st.RowsYes != 1 {
		t.Fatalf("expected one synced yes row, stats=%+v", st)
	}
}

func TestSyncMobileNRFromNSALTECSVNative_ReorderedLTEColumns(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath1 := filepath.Join(tmp, "lte1.csv")
	ltePath2 := filepath.Join(tmp, "lte2.csv")

	fiveG := "" +
		"latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time;5G NR\n" +
		"48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00;\n" +
		"48.148600;17.107700;3500;11;231;01;-100;05.02.2026;11:00:00;\n"
	lte1 := "MCC;MNC;5G NR;Date;Time\n231;01;yes;05.02.2026;10:00:00\n"
	lte2 := "Time;5G NR;MNC;Date;MCC\n11:00:00;yes;01;05.02.2026;231\n"

	if err := os.WriteFile(fiveGPath, []byte(fiveG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath1, []byte(lte1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ltePath2, []byte(lte2), 0o644); err != nil {
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

	out, st, err := syncMobileNRFromNSALTECSVNative(
		context.Background(),
		df5g,
		mapping,
		[]string{ltePath1, ltePath2},
		"5G NR",
		1000,
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.RowsYes != 2 {
		t.Fatalf("expected two synced yes rows, stats=%+v", st)
	}
	nrIdx := out.columnIndexByName("5G NR")
	if strings.TrimSpace(out.Rows[0][nrIdx]) != "yes" || strings.TrimSpace(out.Rows[1][nrIdx]) != "yes" {
		t.Fatalf("expected NR yes on both rows, got %q %q", out.Rows[0][nrIdx], out.Rows[1][nrIdx])
	}
}

func TestSyncMobileNRFromNSALTECSVNative_MissingRequiredLTEColumnErrors(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fiveGPath := filepath.Join(tmp, "5g.csv")
	ltePath := filepath.Join(tmp, "lte_missing_nr.csv")

	fiveG := "latitude;longitude;frequency;pci;mcc;mnc;rsrp;Date;Time\n48.148600;17.107700;3500;10;231;01;-100;05.02.2026;10:00:00\n"
	lte := "MCC;MNC;Date;Time\n231;01;05.02.2026;10:00:00\n"

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

	_, _, err = syncMobileNRFromNSALTECSVNative(context.Background(), df5g, mapping, []string{ltePath}, "5G NR", 1000, nil, false)
	if err == nil || !strings.Contains(err.Error(), "5G NR") {
		t.Fatalf("expected missing 5G NR error, got %v", err)
	}
}

func TestSyncMobileNRFromNSALTECSVNative_requiresNRYesInLTE(t *testing.T) {
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

	_, _, err = syncMobileNRFromNSALTECSVNative(context.Background(), df5g, mapping, []string{ltePath}, "5G NR", 1000, nil, false)
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
