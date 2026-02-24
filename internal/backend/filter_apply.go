package backend

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var filterFieldAliases = map[string]string{
	"latitude":  "latitude",
	"lat":       "latitude",
	"longitude": "longitude",
	"lon":       "longitude",
	"frequency": "frequency",
	"freq":      "frequency",
	"earfcn":    "frequency",
	"pci":       "pci",
	"mcc":       "mcc",
	"mnc":       "mnc",
	"rsrp":      "rsrp",
	"sinr":      "sinr",
}

func (d *CSVData) clone() *CSVData {
	if d == nil {
		return nil
	}
	out := &CSVData{
		Columns:  append([]string(nil), d.Columns...),
		Rows:     make([][]string, len(d.Rows)),
		FileInfo: d.FileInfo,
	}
	for i := range d.Rows {
		out.Rows[i] = append([]string(nil), d.Rows[i]...)
	}
	return out
}

func (d *CSVData) columnIndexByName(name string) int {
	for i, col := range d.Columns {
		if col == name {
			return i
		}
	}
	return -1
}

func parseNumberString(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func normalizeIntLikeString(v float64) string {
	if math.Trunc(v) == v {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func resolveColumnName(field string, columns []string, columnMapping map[string]int) string {
	for _, c := range columns {
		if c == field {
			return c
		}
	}
	fieldLower := strings.ToLower(strings.TrimSpace(field))
	for _, c := range columns {
		if strings.ToLower(strings.TrimSpace(c)) == fieldLower {
			return c
		}
	}
	// 5G CSV exports often expose RF frequency under "SSRef" (Hz) instead of "Frequency".
	// Auto filter files use "Frequency" ranges in Hz for 5G shared-operator duplication, so
	// prefer SSRef before falling back to the user-mapped "frequency" column (which may be NR-ARFCN).
	if fieldLower == "frequency" {
		if ssref := findColumnNameNative(columns, []string{"SSRef"}); ssref != "" {
			return ssref
		}
	}
	mappingKey := filterFieldAliases[fieldLower]
	if mappingKey != "" && columnMapping != nil {
		if idx, ok := columnMapping[mappingKey]; ok && idx >= 0 && idx < len(columns) {
			return columns[idx]
		}
	}
	return field
}

func resolveFilterRules(rules []FilterRule, columns []string, columnMapping map[string]int) []FilterRule {
	out := make([]FilterRule, 0, len(rules))
	for _, rule := range rules {
		resolvedAssignments := map[string][]float64{}
		for field, values := range rule.Assignments {
			resolvedField := resolveColumnName(field, columns, columnMapping)
			for _, value := range values {
				existing := resolvedAssignments[resolvedField]
				dup := false
				for _, e := range existing {
					if e == value {
						dup = true
						break
					}
				}
				if !dup {
					resolvedAssignments[resolvedField] = append(resolvedAssignments[resolvedField], value)
				}
			}
		}

		resolvedGroups := make([][]Condition, 0, len(rule.ConditionGroups))
		for _, group := range rule.ConditionGroups {
			resolvedGroup := make([]Condition, 0, len(group))
			for _, cond := range group {
				cond.Field = resolveColumnName(cond.Field, columns, columnMapping)
				resolvedGroup = append(resolvedGroup, cond)
			}
			if len(resolvedGroup) > 0 {
				resolvedGroups = append(resolvedGroups, resolvedGroup)
			}
		}

		out = append(out, FilterRule{
			Name:            rule.Name,
			Assignments:     resolvedAssignments,
			ConditionGroups: resolvedGroups,
		})
	}
	return out
}

func rowValueMap(columns []string, row []string) map[string]string {
	m := make(map[string]string, len(columns))
	for i, c := range columns {
		if i < len(row) {
			m[c] = row[i]
		} else {
			m[c] = ""
		}
	}
	return m
}

func rowMatchesGroup(row map[string]string, group []Condition) bool {
	for _, cond := range group {
		raw, ok := row[cond.Field]
		if !ok {
			return false
		}
		val, ok := parseNumberString(raw)
		if !ok {
			return false
		}
		switch cond.Kind {
		case ConditionEq:
			if val != cond.Low {
				return false
			}
		case ConditionRange:
			if cond.Low == cond.High {
				if val != cond.Low {
					return false
				}
			} else {
				if val < cond.Low || val >= cond.High {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func buildAssignmentCombinations(assignments map[string][]float64) []map[string]float64 {
	if len(assignments) == 0 {
		return []map[string]float64{{}}
	}
	fields := make([]string, 0, len(assignments))
	for field := range assignments {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	combinations := []map[string]float64{{}}
	for _, field := range fields {
		values := assignments[field]
		next := make([]map[string]float64, 0, len(combinations)*maxInt(1, len(values)))
		for _, combo := range combinations {
			for _, v := range values {
				newCombo := make(map[string]float64, len(combo)+1)
				for k, existing := range combo {
					newCombo[k] = existing
				}
				newCombo[field] = v
				next = append(next, newCombo)
			}
		}
		combinations = next
	}
	return combinations
}

type matchingRule struct {
	bestGroupSize int
	rule          FilterRule
}

func ApplyFiltersCSV(
	data *CSVData,
	rules []FilterRule,
	keepOriginalOnMatch bool,
	columnMapping map[string]int,
) (*CSVData, error) {
	if data == nil {
		return nil, fmt.Errorf("nil CSVData")
	}
	if len(rules) == 0 {
		return data.clone(), nil
	}

	baseColumns := append([]string(nil), data.Columns...)
	resolvedRules := resolveFilterRules(rules, baseColumns, columnMapping)

	outputColumns := append([]string(nil), baseColumns...)
	if idx := indexOf(outputColumns, "original_excel_row"); idx == -1 {
		outputColumns = append(outputColumns, "original_excel_row")
	}
	// Add any assignment columns not present in source.
	assignmentFields := map[string]struct{}{}
	for _, rule := range resolvedRules {
		for field := range rule.Assignments {
			assignmentFields[field] = struct{}{}
			if indexOf(outputColumns, field) == -1 {
				outputColumns = append(outputColumns, field)
			}
		}
	}

	headerLine := data.FileInfo.HeaderLine
	outputRows := make([][]string, 0, len(data.Rows))

	for pos, row := range data.Rows {
		rowMap := rowValueMap(baseColumns, row)
		rowNumber := pos + headerLine + 1

		var matches []matchingRule
		for _, rule := range resolvedRules {
			bestGroupSize := 0
			for _, group := range rule.ConditionGroups {
				if rowMatchesGroup(rowMap, group) && len(group) > bestGroupSize {
					bestGroupSize = len(group)
				}
			}
			if bestGroupSize > 0 {
				matches = append(matches, matchingRule{bestGroupSize: bestGroupSize, rule: rule})
			}
		}

		var selected *FilterRule
		if len(matches) > 0 {
			sort.Slice(matches, func(i, j int) bool {
				if matches[i].bestGroupSize != matches[j].bestGroupSize {
					return matches[i].bestGroupSize > matches[j].bestGroupSize
				}
				return matches[i].rule.Name < matches[j].rule.Name
			})
			selected = &matches[0].rule
		}

		appendOutputRow := func(base map[string]string, assignments map[string]float64, includeOriginalRow bool) {
			rowOut := make([]string, len(outputColumns))
			for i, col := range outputColumns {
				rowOut[i] = base[col]
			}
			if includeOriginalRow {
				if idx := indexOf(outputColumns, "original_excel_row"); idx >= 0 {
					rowOut[idx] = strconv.Itoa(rowNumber)
				}
			}
			for field, v := range assignments {
				if idx := indexOf(outputColumns, field); idx >= 0 {
					rowOut[idx] = normalizeIntLikeString(v)
				}
			}
			outputRows = append(outputRows, rowOut)
		}

		if selected != nil {
			if keepOriginalOnMatch {
				appendOutputRow(rowMap, map[string]float64{}, true)
			}
			for _, combo := range buildAssignmentCombinations(selected.Assignments) {
				appendOutputRow(rowMap, combo, true)
			}
		} else {
			appendOutputRow(rowMap, map[string]float64{}, true)
		}
	}

	return &CSVData{
		Columns:  outputColumns,
		Rows:     outputRows,
		FileInfo: data.FileInfo,
	}, nil
}

func indexOf(items []string, needle string) int {
	for i, v := range items {
		if v == needle {
			return i
		}
	}
	return -1
}
