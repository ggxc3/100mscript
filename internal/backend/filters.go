package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	filterValueRE = regexp.MustCompile(`"([^"]+)"\s*=\s*([-\d.,\s]+)`)
	rangeValueRE  = regexp.MustCompile(`^(-?\d+(?:[.,]\d+)?)\s*-\s*(-?\d+(?:[.,]\d+)?)$`)
	queryRE       = regexp.MustCompile(`(?is)<Query>(.*?)</Query>`)
	groupRE       = regexp.MustCompile(`(?s)\(([^()]*)\)`)
)

type ConditionKind string

const (
	ConditionEq    ConditionKind = "eq"
	ConditionRange ConditionKind = "range"
)

type Condition struct {
	Field string
	Kind  ConditionKind
	Low   float64
	High  float64
}

type FilterRule struct {
	Name            string
	Assignments     map[string][]float64
	ConditionGroups [][]Condition
}

func extractQueryContent(text string) string {
	match := queryRE.FindStringSubmatch(text)
	if len(match) == 2 {
		return match[1]
	}
	return text
}

func splitAssignmentAndConditions(queryText string) (string, string) {
	idx := strings.Index(queryText, ";")
	if idx == -1 {
		return queryText, ""
	}
	return queryText[:idx], queryText[idx+1:]
}

func parseNumber(raw string) (float64, bool) {
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

func parseAssignments(text string) (map[string][]float64, error) {
	assignments := map[string][]float64{}
	matches := filterValueRE.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		field := m[1]
		rawValue := strings.TrimSpace(m[2])
		if rangeValueRE.MatchString(rawValue) {
			return nil, fmt.Errorf("assignment hodnoty nemozu byt rozsah: %s", rawValue)
		}
		val, ok := parseNumber(rawValue)
		if !ok {
			return nil, fmt.Errorf("neplatna assignment hodnota: %s", rawValue)
		}
		existing := assignments[field]
		duplicate := false
		for _, e := range existing {
			if e == val {
				duplicate = true
				break
			}
		}
		if !duplicate {
			assignments[field] = append(assignments[field], val)
		}
	}
	return assignments, nil
}

func parseConditionValue(rawValue string) (ConditionKind, float64, float64, bool) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return "", 0, 0, false
	}
	if m := rangeValueRE.FindStringSubmatch(rawValue); len(m) == 3 {
		start, ok1 := parseNumber(m[1])
		end, ok2 := parseNumber(m[2])
		if !ok1 || !ok2 {
			return "", 0, 0, false
		}
		if start <= end {
			return ConditionRange, start, end, true
		}
		return ConditionRange, end, start, true
	}
	val, ok := parseNumber(rawValue)
	if !ok {
		return "", 0, 0, false
	}
	return ConditionEq, val, val, true
}

func parseConditionGroup(text string) []Condition {
	matches := filterValueRE.FindAllStringSubmatch(text, -1)
	conditions := make([]Condition, 0, len(matches))
	for _, m := range matches {
		kind, low, high, ok := parseConditionValue(m[2])
		if !ok {
			continue
		}
		conditions = append(conditions, Condition{
			Field: m[1],
			Kind:  kind,
			Low:   low,
			High:  high,
		})
	}
	return conditions
}

func parseConditionGroups(text string) [][]Condition {
	var groups [][]Condition
	groupMatches := groupRE.FindAllStringSubmatch(text, -1)
	for _, m := range groupMatches {
		group := parseConditionGroup(m[1])
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		group := parseConditionGroup(text)
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func LoadFilterRuleFromFile(path string) (FilterRule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FilterRule{}, err
	}
	queryText := extractQueryContent(string(raw))
	assignmentText, conditionsText := splitAssignmentAndConditions(queryText)

	assignments, err := parseAssignments(assignmentText)
	if err != nil {
		return FilterRule{}, err
	}
	conditionGroups := parseConditionGroups(conditionsText)
	if len(assignments) == 0 || len(conditionGroups) == 0 {
		return FilterRule{}, fmt.Errorf("filter je neplatny alebo nema podmienky")
	}

	return FilterRule{
		Name:            filepath.Base(path),
		Assignments:     assignments,
		ConditionGroups: conditionGroups,
	}, nil
}

func DiscoverFilterPaths(baseDir string) ([]string, error) {
	var result []string
	for _, folder := range []string{"filters", "filtre_5G"} {
		dir := filepath.Join(baseDir, folder)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".txt") {
				result = append(result, filepath.Join(dir, entry.Name()))
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func LoadFilterRulesFromPaths(paths []string) ([]FilterRule, error) {
	rules := make([]FilterRule, 0, len(paths))
	for _, path := range paths {
		rule, err := LoadFilterRuleFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}
