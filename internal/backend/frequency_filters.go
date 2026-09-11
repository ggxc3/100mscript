package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type frequencyCondition struct {
	Condition
	index             int
	physicalFrequency bool
}
type frequencyRule struct {
	name                string
	groups              [][]frequencyCondition
	operatorAssignments map[int][]float64
}

func loadFrequencyRules(cfg ProcessingConfig, columns []string) (map[string][]frequencyRule, error) {
	paths := map[string][]string{"LTE": cfg.FrequencyLTEFilters, "5G": cfg.Frequency5GFilters}
	if cfg.FrequencyLTEFilters == nil || cfg.Frequency5GFilters == nil {
		cwd, _ := os.Getwd()
		auto, err := DiscoverFilterPaths(cwd)
		if err != nil {
			return nil, err
		}
		for _, path := range auto {
			tech := "LTE"
			if filepath.Base(filepath.Dir(path)) == "filtre_5G" {
				tech = "5G"
			}
			if (tech == "LTE" && cfg.FrequencyLTEFilters == nil) || (tech == "5G" && cfg.Frequency5GFilters == nil) {
				paths[tech] = append(paths[tech], path)
			}
		}
	}
	out := map[string][]frequencyRule{}
	for _, tech := range []string{"LTE", "5G"} {
		rawRules, err := LoadFilterRulesFromPaths(NormalizeInputPaths(paths[tech]))
		if err != nil {
			return nil, fmt.Errorf("filtre %s: %w", tech, err)
		}
		out[tech], err = compileFrequencyRulesWithNames(rawRules, columns, cfg.ColumnMapping, cfg.FrequencyColumnMappings[strings.ToLower(tech)])
		if err != nil {
			return nil, fmt.Errorf("filtre %s: %w", tech, err)
		}
	}
	return out, nil
}

func compileFrequencyRules(rules []FilterRule, columns []string, mapping map[string]int) ([]frequencyRule, error) {
	return compileFrequencyRulesWithNames(rules, columns, mapping, nil)
}

func compileFrequencyRulesWithNames(rules []FilterRule, columns []string, mapping map[string]int, names map[string]string) ([]frequencyRule, error) {
	logicalKey := func(field string) string {
		token := normalizeHeaderToken(field)
		if key := filterFieldAliases[token]; key != "" {
			return key
		}
		// Recognize source names from this technology's independent mapping,
		// including custom operator headers, as well as canonical field names.
		for _, key := range frequencyLogicalKeys {
			if token == normalizeHeaderToken(frequencyInternalPrefix+key) ||
				(names[key] != "" && token == normalizeHeaderToken(names[key])) {
				return key
			}
		}
		return ""
	}
	var out []frequencyRule
	for _, rule := range rules {
		r := frequencyRule{name: rule.Name, operatorAssignments: map[int][]float64{}}
		for field, values := range rule.Assignments {
			idx := indexOf(columns, resolveColumnName(field, columns, mapping))
			if key := logicalKey(field); key != "" {
				if mapped, ok := mapping[key]; ok {
					idx = mapped
				}
			}
			if idx == mapping["mnc"] || idx == mapping["mcc"] {
				// Several spellings (MNC / mnc) can resolve to one operator
				// field. Preserve all alternatives, independent of map order.
				r.operatorAssignments[idx] = append(r.operatorAssignments[idx], values...)
			}
		}
		for _, group := range rule.ConditionGroups {
			var g []frequencyCondition
			for _, cond := range group {
				token := normalizeHeaderToken(cond.Field)
				physical := token == "frequency" || token == "freq" || token == "ssref" || token == normalizeHeaderToken(frequencyHzColumn)
				idx := indexOf(columns, resolveColumnName(cond.Field, columns, mapping))
				if token == "earfcn" || token == "nrarfcn" {
					// Channel conditions require an actual channel column. The
					// legacy resolver may otherwise fall back to physical Hz.
					idx = -1
					for i, col := range columns {
						if normalizeHeaderToken(col) == token {
							idx = i
							break
						}
					}
				}
				if key := logicalKey(cond.Field); key != "" && key != "frequency" && !physical {
					if mapped, ok := mapping[key]; ok {
						idx = mapped
					}
				}
				if physical {
					idx = mapping["frequency"]
				}
				if idx < 0 {
					return nil, fmt.Errorf("filter %q: chýba stĺpec %q", rule.Name, cond.Field)
				}
				g = append(g, frequencyCondition{Condition: cond, index: idx, physicalFrequency: physical})
			}
			r.groups = append(r.groups, g)
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// Select exactly the same winning rule as the legacy filters: most conditions,
// then alphabetical filename. Only simulate operator assignments; never edit rows.
func frequencyOperatorMatches(row []string, frequency float64, rules []frequencyRule) bool {
	bestSize, bestIndex := 0, -1
	for i, rule := range rules {
		for _, group := range rule.groups {
			matches := true
			for _, cond := range group {
				value, ok := finiteNumber(cellAt(row, cond.index))
				if cond.physicalFrequency {
					value, ok = frequency, true
				}
				if !ok {
					matches = false
					break
				}
				if cond.Kind == ConditionEq || (cond.Kind == ConditionRange && cond.Low == cond.High) {
					matches = value == cond.Low
				} else if cond.Kind == ConditionRange {
					matches = value >= cond.Low && value < cond.High
				} else {
					matches = false
				}
				if !matches {
					break
				}
			}
			if matches && len(group) > bestSize {
				bestSize, bestIndex = len(group), i
			}
		}
	}
	if bestIndex < 0 {
		return true
	}
	for idx, values := range rules[bestIndex].operatorAssignments {
		current, ok := finiteNumber(cellAt(row, idx))
		if !ok {
			return false
		}
		for _, assigned := range values {
			if assigned != current {
				return false
			}
		}
	}
	return true
}

func frequencyFilterFlags(row ProcessedRow, technology string, cfg ProcessingConfig, rules map[string][]frequencyRule) (string, string) {
	frequency, _ := finiteNumber(row.Frequency)
	selectedRules := rules[technology]
	center := frequencyOperatorMatches(row.Raw, frequency, selectedRules)
	bw := cfg.FrequencyLTEBW
	if strings.EqualFold(technology, "5G") {
		bw = cfg.Frequency5GBW
	}
	delta := bw * 1e6
	withBW := center && frequencyOperatorMatches(row.Raw, frequency-delta, selectedRules) && frequencyOperatorMatches(row.Raw, frequency+delta, selectedRules)
	yesNo := func(value bool) string {
		if value {
			return "yes"
		}
		return "no"
	}
	return yesNo(center), yesNo(withBW)
}
