package backend

import "strings"

// Input radio access technology inferred from CSV column headers (frequency naming).
const (
	InputRadioTech5G      = "5g"
	InputRadioTechLTE     = "lte"
	InputRadioTechUnknown = "unknown"
)

// DetectInputRadioTech returns InputRadioTech5G when any column name indicates NR-ARFCN,
// InputRadioTechLTE for EARFCN-style columns, otherwise InputRadioTechUnknown.
// NR is checked before LTE so names containing both tokens classify as 5G.
func DetectInputRadioTech(columns []string) string {
	for _, c := range columns {
		t := normalizeHeaderToken(c)
		if strings.Contains(t, "nrarfcn") {
			return InputRadioTech5G
		}
	}
	for _, c := range columns {
		t := normalizeHeaderToken(c)
		if strings.Contains(t, "earfcn") {
			return InputRadioTechLTE
		}
	}
	return InputRadioTechUnknown
}
