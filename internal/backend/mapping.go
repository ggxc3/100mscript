package backend

import "strings"

func normalizeHeaderToken(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func suggestColumnMappingFromHeaders(columns []string) map[string]int {
	candidates := map[string][]string{
		"latitude":  {"Latitude"},
		"longitude": {"Longitude"},
		"frequency": {"NR-ARFCN", "EARFCN", "Frequency"},
		"pci":       {"PCI"},
		"mcc":       {"MCC"},
		"mnc":       {"MNC"},
		"rsrp":      {"SSS-RSRP", "RSRP"},
		"sinr":      {"SSS-SINR", "SINR"},
	}

	normalized := make(map[string]int, len(columns))
	for i, c := range columns {
		normalized[normalizeHeaderToken(c)] = i
	}

	result := map[string]int{}
	for key, names := range candidates {
		for _, name := range names {
			if idx, ok := normalized[normalizeHeaderToken(name)]; ok {
				result[key] = idx
				break
			}
		}
	}
	return result
}

func BuildColumnMappingFromHeaders(columns []string) map[string]int {
	return suggestColumnMappingFromHeaders(columns)
}
