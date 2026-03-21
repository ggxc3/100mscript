package backend

import (
	"strings"
	"testing"
)

func TestRowMatchesGroup_eqAndRangeHalfOpen(t *testing.T) {
	t.Parallel()

	row := map[string]string{"f": "100", "x": "5"}
	if !rowMatchesGroup(row, []Condition{
		{Field: "f", Kind: ConditionEq, Low: 100, High: 100},
	}) {
		t.Fatal("eq match")
	}
	if rowMatchesGroup(row, []Condition{{Field: "f", Kind: ConditionEq, Low: 99, High: 99}}) {
		t.Fatal("eq should not match")
	}
	// [5,10) contains 5, not 10
	if !rowMatchesGroup(map[string]string{"x": "5"}, []Condition{{Field: "x", Kind: ConditionRange, Low: 5, High: 10}}) {
		t.Fatal("low inclusive")
	}
	if rowMatchesGroup(map[string]string{"x": "10"}, []Condition{{Field: "x", Kind: ConditionRange, Low: 5, High: 10}}) {
		t.Fatal("high exclusive")
	}
	if rowMatchesGroup(map[string]string{"x": "bad"}, []Condition{{Field: "x", Kind: ConditionEq, Low: 1, High: 1}}) {
		t.Fatal("non-numeric should not match")
	}
}

func TestResolveColumnName_SSRefPreferredWhenNoFrequencyColumn(t *testing.T) {
	t.Parallel()

	// If there is no column whose name matches "frequency", SSRef is used for filter field "frequency".
	cols := []string{"SSRef", "NR-ARFCN", "EARFCN"}
	m := map[string]int{"frequency": 1}
	name := resolveColumnName("frequency", cols, m)
	if name != "SSRef" {
		t.Fatalf("got %q", name)
	}
}

func TestApplyFiltersCSV_matchExpandsAssignments(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"MCC", "MNC", "RSRP"},
		Rows: [][]string{
			{"231", "01", "-100"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	rules := []FilterRule{{
		Name:            "r1",
		Assignments:     map[string][]float64{"Frequency": {3500}},
		ConditionGroups: [][]Condition{{{Field: "MCC", Kind: ConditionEq, Low: 231, High: 231}}},
	}}
	out, err := ApplyFiltersCSV(data, rules, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("rows: %d", len(out.Rows))
	}
	fIdx := indexOf(out.Columns, "Frequency")
	if fIdx < 0 {
		t.Fatalf("columns: %v", out.Columns)
	}
	if strings.TrimSpace(out.Rows[0][fIdx]) != "3500" {
		t.Fatalf("row: %#v", out.Rows[0])
	}
}

func TestApplyFiltersCSV_tieBreakLexicographicRuleName(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"MCC", "RSRP"},
		Rows: [][]string{
			{"231", "-100"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	rules := []FilterRule{
		{
			Name:            "z_rule",
			Assignments:     map[string][]float64{"X": {1}},
			ConditionGroups: [][]Condition{{{Field: "MCC", Kind: ConditionEq, Low: 231, High: 231}}},
		},
		{
			Name:            "a_rule",
			Assignments:     map[string][]float64{"X": {2}},
			ConditionGroups: [][]Condition{{{Field: "MCC", Kind: ConditionEq, Low: 231, High: 231}}},
		},
	}
	out, err := ApplyFiltersCSV(data, rules, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	xIdx := indexOf(out.Columns, "X")
	if out.Rows[0][xIdx] != "2" {
		t.Fatalf("expected lexicographically smaller rule name wins: got %q", out.Rows[0][xIdx])
	}
}

func TestApplyFiltersCSV_keepOriginalDuplicatesRow(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"MCC", "RSRP"},
		Rows: [][]string{
			{"231", "-100"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	rules := []FilterRule{{
		Name:            "r",
		Assignments:     map[string][]float64{"K": {1, 2}},
		ConditionGroups: [][]Condition{{{Field: "MCC", Kind: ConditionEq, Low: 231, High: 231}}},
	}}
	out, err := ApplyFiltersCSV(data, rules, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// original + 2 combinations
	if len(out.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(out.Rows))
	}
}

func TestApplyFiltersCSV_emptyRulesReturnsClone(t *testing.T) {
	t.Parallel()

	data := &CSVData{
		Columns: []string{"a"},
		Rows:    [][]string{{"1"}},
	}
	out, err := ApplyFiltersCSV(data, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == data || len(out.Rows) != 1 || out.Rows[0][0] != "1" {
		t.Fatalf("unexpected clone: %p vs %p %#v", out, data, out.Rows)
	}
}

func TestBuildAssignmentCombinations_cartesian(t *testing.T) {
	t.Parallel()

	combos := buildAssignmentCombinations(map[string][]float64{
		"a": {1},
		"b": {2, 3},
	})
	if len(combos) != 2 {
		t.Fatalf("got %d combos", len(combos))
	}
}
