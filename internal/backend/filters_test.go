package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractQueryContent_andSplit(t *testing.T) {
	t.Parallel()

	inner := extractQueryContent(`prefix<Query>"MCC" = 1; ("x" = 2)</Query>suffix`)
	if !strings.Contains(inner, `"MCC" = 1`) {
		t.Fatalf("expected inner query, got %q", inner)
	}
	assign, cond := splitAssignmentAndConditions(`"a" = 1; ("b" = 2)`)
	if !strings.Contains(assign, `"a" = 1`) || !strings.Contains(cond, `"b" = 2`) {
		t.Fatalf("split failed: assign=%q cond=%q", assign, cond)
	}
	a, c := splitAssignmentAndConditions(`no semicolon here`)
	if a != `no semicolon here` || c != "" {
		t.Fatalf("expected no split, got %q / %q", a, c)
	}
}

func TestParseAssignments_rejectsRangeInAssignment(t *testing.T) {
	t.Parallel()

	_, err := parseAssignments(`"x" = 1-5`)
	if err == nil {
		t.Fatal("expected error for range in assignment")
	}
}

func TestParseAssignments_deduplicatesValues(t *testing.T) {
	t.Parallel()

	m, err := parseAssignments(`"k" = 1; "k" = 1; "k" = 2`)
	if err != nil {
		t.Fatal(err)
	}
	if len(m["k"]) != 2 {
		t.Fatalf("got %#v", m["k"])
	}
}

func TestParseConditionValue_eqAndRange(t *testing.T) {
	t.Parallel()

	kind, lo, hi, ok := parseConditionValue("  -3,5 ")
	if !ok || kind != ConditionEq || lo != -3.5 || hi != -3.5 {
		t.Fatalf("eq: kind=%s lo=%v hi=%v ok=%v", kind, lo, hi, ok)
	}

	kind2, lo2, hi2, ok2 := parseConditionValue("10-5")
	if !ok2 || kind2 != ConditionRange || lo2 != 5 || hi2 != 10 {
		t.Fatalf("range swap: kind=%s lo=%v hi=%v", kind2, lo2, hi2)
	}

	kind3, lo3, hi3, ok3 := parseConditionValue("5-5")
	if !ok3 || kind3 != ConditionRange || lo3 != 5 || hi3 != 5 {
		t.Fatalf("equal endpoints: kind=%s lo=%v hi=%v", kind3, lo3, hi3)
	}

	_, _, _, ok4 := parseConditionValue("not_a_number")
	if ok4 {
		t.Fatal("expected parse failure")
	}
}

func TestParseNumber(t *testing.T) {
	t.Parallel()

	v, ok := parseNumber("1,25")
	if !ok || v != 1.25 {
		t.Fatalf("got %v %v", v, ok)
	}
	_, ok2 := parseNumber("")
	if ok2 {
		t.Fatal("expected false")
	}
}

func TestLoadFilterRuleFromFile_roundTrip(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "rule.txt")
	content := `<Query>
"MCC" = 231; ("MNC" = 1)
</Query>
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	rule, err := LoadFilterRuleFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Name != "rule.txt" {
		t.Fatalf("name: %q", rule.Name)
	}
	if len(rule.Assignments["MCC"]) != 1 || rule.Assignments["MCC"][0] != 231 {
		t.Fatalf("assignments: %#v", rule.Assignments)
	}
	if len(rule.ConditionGroups) != 1 || len(rule.ConditionGroups[0]) != 1 {
		t.Fatalf("groups: %#v", rule.ConditionGroups)
	}
}

func TestLoadFilterRulesFromPaths_firstErrorStops(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	good := filepath.Join(tmp, "good.txt")
	if err := os.WriteFile(good, []byte(`<Query>"MCC" = 1; ("MNC" = 2)</Query>`), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(tmp, "bad.txt")
	if err := os.WriteFile(bad, []byte(`not a valid filter`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFilterRulesFromPaths([]string{good, bad})
	if err == nil {
		t.Fatal("expected error from bad filter")
	}
}

func TestDiscoverFilterPaths_sortsAndFiltersTxt(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "filters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "filtre_5G"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, "filters", "b.txt"), `<Query>"MCC" = 1; ("MNC" = 2)</Query>`)
	mustWrite(t, filepath.Join(tmp, "filtre_5G", "a.txt"), `<Query>"MCC" = 3; ("MNC" = 4)</Query>`)
	// ignored
	if err := os.WriteFile(filepath.Join(tmp, "filters", "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := DiscoverFilterPaths(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths: %v", len(paths), paths)
	}
	// Lexicographic full path: "filters" before "filtre_5G" (compare at shared prefix "filte": 'r' vs 'r' — see byte-by-byte).
	want0 := filepath.Join(tmp, "filters", "b.txt")
	want1 := filepath.Join(tmp, "filtre_5G", "a.txt")
	if paths[0] != want0 || paths[1] != want1 {
		t.Fatalf("order/paths: got %v want [%s %s]", paths, want0, want1)
	}
}

func TestDiscoverFilterPaths_missingDirsOk(t *testing.T) {
	t.Parallel()

	paths, err := DiscoverFilterPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty, got %v", paths)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
