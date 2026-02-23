package backend

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type mobileSyncStats struct {
	SyncStrategy         string
	RowsTotal            int
	RowsYes              int
	RowsNo               int
	RowsBlank            int
	RowsWithMatch        int
	ConflictingWindows   int
	FallbackTimeOnlyRows int
	WindowMS             int
}

type mobileLookup struct {
	times     []int64
	yesPrefix []int
	noPrefix  []int
}

func syncMobileNRFromLTECSVNative(
	df5g *CSVData,
	columnMapping map[string]int,
	lteFilePath string,
	nrColumnName string,
	timeToleranceMS int,
	filterRules []FilterRule,
	keepOriginalRows bool,
) (*CSVData, mobileSyncStats, error) {
	if df5g == nil {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: nil 5G dataset")
	}
	dfLTE, err := LoadCSVFile(lteFilePath)
	if err != nil {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: load LTE CSV: %w", err)
	}
	if len(filterRules) > 0 {
		lteMapping := BuildColumnMappingFromHeaders(dfLTE.Columns)
		dfLTE, err = ApplyFiltersCSV(dfLTE, filterRules, keepOriginalRows, lteMapping)
		if err != nil {
			return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: apply LTE filters: %w", err)
		}
	}

	lteMCCCol := findColumnNameNative(dfLTE.Columns, []string{"MCC"})
	lteMNCCol := findColumnNameNative(dfLTE.Columns, []string{"MNC"})
	lteNRCol := findColumnNameNative(dfLTE.Columns, []string{"5G NR", "5GNR", "NR"})
	if lteMCCCol == "" || lteMNCCol == "" || lteNRCol == "" {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: LTE súbor musí obsahovať stĺpce MCC, MNC a 5G NR")
	}
	lteUTCCol := findColumnNameNative(dfLTE.Columns, []string{"UTC"})
	lteDateCol := findColumnNameNative(dfLTE.Columns, []string{"Date"})
	lteTimeCol := findColumnNameNative(dfLTE.Columns, []string{"Time"})

	if columnMapping == nil {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: missing 5G column mapping")
	}
	fivegMCCIdx, okMCC := columnMapping["mcc"]
	fivegMNCIdx, okMNC := columnMapping["mnc"]
	if !okMCC || !okMNC || fivegMCCIdx < 0 || fivegMCCIdx >= len(df5g.Columns) || fivegMNCIdx < 0 || fivegMNCIdx >= len(df5g.Columns) {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: neplatné mapovanie stĺpcov pre 5G súbor")
	}
	fivegMCCCol := df5g.Columns[fivegMCCIdx]
	fivegMNCCol := df5g.Columns[fivegMNCIdx]
	fivegUTCCol := findColumnNameNative(df5g.Columns, []string{"UTC"})
	fivegDateCol := findColumnNameNative(df5g.Columns, []string{"Date"})
	fivegTimeCol := findColumnNameNative(df5g.Columns, []string{"Time"})

	lteMCCIdx := dfLTE.columnIndexByName(lteMCCCol)
	lteMNCIdx := dfLTE.columnIndexByName(lteMNCCol)
	lteNRIdx := dfLTE.columnIndexByName(lteNRCol)
	lteUTCIdx := dfLTE.columnIndexByName(lteUTCCol)
	lteDateIdx := dfLTE.columnIndexByName(lteDateCol)
	lteTimeIdx := dfLTE.columnIndexByName(lteTimeCol)

	fivegMCCIdx = df5g.columnIndexByName(fivegMCCCol)
	fivegMNCIdx = df5g.columnIndexByName(fivegMNCCol)
	fivegUTCIdx := df5g.columnIndexByName(fivegUTCCol)
	fivegDateIdx := df5g.columnIndexByName(fivegDateCol)
	fivegTimeIdx := df5g.columnIndexByName(fivegTimeCol)

	lteTimeMS, lteStrategy := buildTimeMillisSeriesNative(dfLTE, lteUTCIdx, lteDateIdx, lteTimeIdx)
	fivegTimeMS, fivegStrategy := buildTimeMillisSeriesNative(df5g, fivegUTCIdx, fivegDateIdx, fivegTimeIdx)

	type lteRow struct {
		mcc, mnc string
		timeMS   int64
		score    int8
	}
	lteRows := make([]lteRow, 0, len(dfLTE.Rows))
	for i, row := range dfLTE.Rows {
		if !lteTimeMS.Valid[i] {
			continue
		}
		mcc := normalizeKeyStringNative(cellAt(row, lteMCCIdx))
		mnc := normalizeKeyStringNative(cellAt(row, lteMNCIdx))
		if mcc == "" || mnc == "" {
			continue
		}
		score := nrScore(normalizeNRValueNative(cellAt(row, lteNRIdx)))
		lteRows = append(lteRows, lteRow{mcc: mcc, mnc: mnc, timeMS: lteTimeMS.Values[i], score: score})
	}
	if len(lteRows) == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: LTE súbor neobsahuje použiteľné MCC/MNC hodnoty")
	}
	yesCountLTE := 0
	for _, r := range lteRows {
		if r.score == 2 {
			yesCountLTE++
		}
	}
	if yesCountLTE == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: v LTE súbore sa nenašli žiadne riadky s 5G NR = yes")
	}

	type fivegCandidate struct {
		index    int
		mcc, mnc string
		timeMS   int64
	}
	fivegCandidates := make([]fivegCandidate, 0, len(df5g.Rows))
	for i, row := range df5g.Rows {
		if !fivegTimeMS.Valid[i] {
			continue
		}
		fivegCandidates = append(fivegCandidates, fivegCandidate{
			index:  i,
			mcc:    normalizeKeyStringNative(cellAt(row, fivegMCCIdx)),
			mnc:    normalizeKeyStringNative(cellAt(row, fivegMNCIdx)),
			timeMS: fivegTimeMS.Values[i],
		})
	}
	if len(fivegCandidates) == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: nepodarilo sa načítať čas pre porovnanie (UTC alebo Date+Time) v jednom zo súborov")
	}

	groupedLTE := map[string][]lteRow{}
	globalTimes := make([]int64, 0, len(lteRows))
	globalScores := make([]int8, 0, len(lteRows))
	for _, r := range lteRows {
		k := r.mcc + "\x1f" + r.mnc
		groupedLTE[k] = append(groupedLTE[k], r)
		globalTimes = append(globalTimes, r.timeMS)
		globalScores = append(globalScores, r.score)
	}
	lteLookups := make(map[string]mobileLookup, len(groupedLTE))
	for k, rows := range groupedLTE {
		times := make([]int64, len(rows))
		scores := make([]int8, len(rows))
		for i, r := range rows {
			times[i] = r.timeMS
			scores[i] = r.score
		}
		lteLookups[k] = buildMobileLookup(times, scores)
	}
	globalLookup := buildMobileLookup(globalTimes, globalScores)

	tolerance := timeToleranceMS
	if tolerance < 0 {
		tolerance = 0
	}
	windowScores := make([]int8, len(df5g.Rows))
	matchedRows := 0
	conflictingWindows := 0
	fallbackTimeOnlyRows := 0

	for _, c := range fivegCandidates {
		if c.mcc == "" || c.mnc == "" {
			continue
		}
		lookup, ok := lteLookups[c.mcc+"\x1f"+c.mnc]
		if !ok {
			continue
		}
		score, matched, conflict := resolveWindowScore(lookup, c.timeMS, int64(tolerance))
		if matched {
			matchedRows++
			if conflict {
				conflictingWindows++
			}
			windowScores[c.index] = score
		}
	}
	for _, c := range fivegCandidates {
		if c.mcc != "" && c.mnc != "" {
			continue
		}
		score, matched, conflict := resolveWindowScore(globalLookup, c.timeMS, int64(tolerance))
		if matched {
			matchedRows++
			fallbackTimeOnlyRows++
			if conflict {
				conflictingWindows++
			}
			windowScores[c.index] = score
		}
	}

	out := df5g.clone()
	nrIdx := out.columnIndexByName(nrColumnName)
	if nrIdx < 0 {
		out.Columns = append(out.Columns, nrColumnName)
		nrIdx = len(out.Columns) - 1
		for i := range out.Rows {
			out.Rows[i] = append(out.Rows[i], "")
		}
	}

	rowsYes, rowsNo, rowsBlank := 0, 0, 0
	for i := range out.Rows {
		val := ""
		switch windowScores[i] {
		case 2:
			val = "yes"
			rowsYes++
		case 1:
			val = "no"
			rowsNo++
		default:
			rowsBlank++
		}
		if nrIdx >= len(out.Rows[i]) {
			padded := make([]string, nrIdx+1)
			copy(padded, out.Rows[i])
			out.Rows[i] = padded
		}
		out.Rows[i][nrIdx] = val
	}

	stats := mobileSyncStats{
		SyncStrategy:         fivegStrategy + "->" + lteStrategy,
		RowsTotal:            len(out.Rows),
		RowsYes:              rowsYes,
		RowsNo:               rowsNo,
		RowsBlank:            rowsBlank,
		RowsWithMatch:        matchedRows,
		ConflictingWindows:   conflictingWindows,
		FallbackTimeOnlyRows: fallbackTimeOnlyRows,
		WindowMS:             tolerance,
	}
	return out, stats, nil
}

func nrScore(v string) int8 {
	switch v {
	case "yes":
		return 2
	case "no":
		return 1
	default:
		return 0
	}
}

type timeSeriesNative struct {
	Values []int64
	Valid  []bool
}

func buildTimeMillisSeriesNative(data *CSVData, utcIdx, dateIdx, timeIdx int) (timeSeriesNative, string) {
	n := 0
	if data != nil {
		n = len(data.Rows)
	}
	out := timeSeriesNative{
		Values: make([]int64, n),
		Valid:  make([]bool, n),
	}
	if data == nil {
		return out, "missing"
	}

	if utcIdx >= 0 {
		validVals := make([]float64, 0, n)
		tmpVals := make([]float64, n)
		tmpOK := make([]bool, n)
		for i, row := range data.Rows {
			if v, ok := parseNumberString(cellAt(row, utcIdx)); ok {
				tmpVals[i] = v
				tmpOK[i] = true
				abs := v
				if abs < 0 {
					abs = -abs
				}
				validVals = append(validVals, abs)
			}
		}
		if len(validVals) > 0 {
			sort.Float64s(validVals)
			medianAbs := validVals[len(validVals)/2]
			factor := float64(1000)
			if medianAbs >= 1e11 {
				factor = 1
			}
			for i := 0; i < n; i++ {
				if !tmpOK[i] {
					continue
				}
				out.Values[i] = int64(mathRound(tmpVals[i] * factor))
				out.Valid[i] = true
			}
			return out, "utc_numeric"
		}
		anyDT := false
		for i, row := range data.Rows {
			if ms, ok := parseDateTimeToMillis(strings.TrimSpace(cellAt(row, utcIdx))); ok {
				out.Values[i] = ms
				out.Valid[i] = true
				anyDT = true
			}
		}
		if anyDT {
			return out, "utc_datetime"
		}
	}

	if dateIdx >= 0 && timeIdx >= 0 {
		anyDT := false
		for i, row := range data.Rows {
			d := strings.TrimSpace(cellAt(row, dateIdx))
			t := strings.TrimSpace(cellAt(row, timeIdx))
			if d == "" || t == "" {
				continue
			}
			if ms, ok := parseDateTimeToMillis(d + " " + t); ok {
				out.Values[i] = ms
				out.Valid[i] = true
				anyDT = true
			}
		}
		if anyDT {
			return out, "date_time"
		}
	}

	return out, "missing"
}

func parseDateTimeToMillis(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2.1.2006 15:04:05",
		"02.01.2006 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return ts.UnixMilli(), true
		}
	}
	return 0, false
}

func mathRound(v float64) float64 {
	if v >= 0 {
		return float64(int64(v + 0.5))
	}
	return float64(int64(v - 0.5))
}

func buildMobileLookup(times []int64, scores []int8) mobileLookup {
	idx := make([]int, len(times))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return times[idx[i]] < times[idx[j]] })

	sortedTimes := make([]int64, len(times))
	sortedScores := make([]int8, len(times))
	for i, original := range idx {
		sortedTimes[i] = times[original]
		sortedScores[i] = scores[original]
	}
	yesPrefix := make([]int, len(sortedScores)+1)
	noPrefix := make([]int, len(sortedScores)+1)
	for i, score := range sortedScores {
		yesPrefix[i+1] = yesPrefix[i]
		noPrefix[i+1] = noPrefix[i]
		if score == 2 {
			yesPrefix[i+1]++
		} else if score == 1 {
			noPrefix[i+1]++
		}
	}
	return mobileLookup{times: sortedTimes, yesPrefix: yesPrefix, noPrefix: noPrefix}
}

func resolveWindowScore(lookup mobileLookup, timeMS int64, tolerance int64) (int8, bool, bool) {
	if len(lookup.times) == 0 {
		return 0, false, false
	}
	left := sort.Search(len(lookup.times), func(i int) bool { return lookup.times[i] >= timeMS-tolerance })
	right := sort.Search(len(lookup.times), func(i int) bool { return lookup.times[i] > timeMS+tolerance })
	if right <= left {
		return 0, false, false
	}
	yes := lookup.yesPrefix[right] - lookup.yesPrefix[left]
	no := lookup.noPrefix[right] - lookup.noPrefix[left]
	conflict := yes > 0 && no > 0
	if yes > 0 {
		return 2, true, conflict
	}
	if no > 0 {
		return 1, true, conflict
	}
	return 0, true, false
}

func findColumnNameNative(columns []string, candidates []string) string {
	normalizedMap := map[string]string{}
	for _, c := range columns {
		normalizedMap[normalizeHeaderToken(c)] = c
	}
	for _, cand := range candidates {
		if v, ok := normalizedMap[normalizeHeaderToken(cand)]; ok {
			return v
		}
	}
	return ""
}

func normalizeKeyStringNative(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return ""
	}
	for strings.Contains(s, ".") && strings.HasSuffix(s, "0") {
		s = strings.TrimSuffix(s, "0")
	}
	s = strings.TrimSuffix(s, ".")
	return s
}

func normalizeNRValueNative(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "yes", "true", "1", "y", "t", "a", "ano", "áno":
		return "yes"
	case "no", "false", "0", "n", "f":
		return "no"
	default:
		return ""
	}
}

func filterRowsByNRYesNative(data *CSVData, nrColumnName string) (*CSVData, error) {
	if data == nil {
		return nil, fmt.Errorf("nil dataset")
	}
	nrIdx := data.columnIndexByName(nrColumnName)
	if nrIdx < 0 {
		return nil, fmt.Errorf("mobile mode: 5G NR column not found after sync")
	}
	out := &CSVData{
		Columns:  append([]string(nil), data.Columns...),
		Rows:     make([][]string, 0, len(data.Rows)),
		FileInfo: data.FileInfo,
	}
	for _, row := range data.Rows {
		if normalizeNRValueNative(cellAt(row, nrIdx)) == "yes" {
			out.Rows = append(out.Rows, append([]string(nil), row...))
		}
	}
	if len(out.Rows) == 0 {
		return nil, fmt.Errorf("mobile mode: po synchronizácii neostali žiadne riadky s 5G NR = yes")
	}
	return out, nil
}
