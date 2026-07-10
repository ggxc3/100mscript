package backend

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
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

func syncMobileNRFromNSALTECSVNative(
	ctx context.Context,
	df5g *CSVData,
	columnMapping map[string]int,
	ltePaths []string,
	nrColumnName string,
	timeToleranceMS int,
	filterRules []FilterRule,
	keepOriginalRows bool,
	mainSourcePaths ...[]string,
) (*CSVData, mobileSyncStats, error) {
	if df5g == nil {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: nil 5G dataset")
	}
	ltePaths = NormalizeInputPaths(ltePaths)
	if len(ltePaths) == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: žiadna cesta k NSA LTE CSV")
	}
	nrColumnName = strings.TrimSpace(nrColumnName)
	if nrColumnName == "" {
		nrColumnName = "5G NR"
	}
	dfLTE, err := loadMobileNSALTECSVFiles(ctx, ltePaths)
	if err != nil {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: načítanie NSA LTE CSV: %w", err)
	}
	if len(filterRules) > 0 {
		lteMapping := BuildColumnMappingFromHeaders(dfLTE.Columns)
		dfLTE, err = ApplyFiltersCSV(ctx, dfLTE, filterRules, keepOriginalRows, lteMapping)
		if err != nil {
			return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: apply NSA LTE filters: %w", err)
		}
	}

	lteMCCCol := findColumnNameNative(dfLTE.Columns, []string{"MCC"})
	lteMNCCol := findColumnNameNative(dfLTE.Columns, []string{"MNC"})
	lteNRCol := findColumnNameNative(dfLTE.Columns, []string{"5G NR", "5GNR", "NR"})
	if lteMCCCol == "" || lteMNCCol == "" || lteNRCol == "" {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: NSA LTE súbor musí obsahovať stĺpce MCC, MNC a 5G NR")
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
	var sourcePaths []string
	if len(mainSourcePaths) > 0 {
		sourcePaths = NormalizeInputPaths(mainSourcePaths[0])
	}
	if err := validateMobileFiveGSourceTimes(df5g, fivegTimeMS, sourcePaths); err != nil {
		return nil, mobileSyncStats{}, err
	}

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
		if score == 0 {
			continue
		}
		lteRows = append(lteRows, lteRow{mcc: mcc, mnc: mnc, timeMS: lteTimeMS.Values[i], score: score})
	}
	if len(lteRows) == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: NSA LTE súbor neobsahuje použiteľné MCC/MNC hodnoty")
	}
	yesCountLTE := 0
	for _, r := range lteRows {
		if r.score == 2 {
			yesCountLTE++
		}
	}
	if yesCountLTE == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: v NSA LTE súbore sa nenašli žiadne riadky s 5G NR = yes")
	}

	type fivegCandidate struct {
		index     int
		mcc, mnc  string
		timeMS    int64
		source    int
		hasSource bool
	}
	fivegCandidates := make([]fivegCandidate, 0, len(df5g.Rows))
	fivegSourceIdx := df5g.columnIndexByName(csvSourceIndexColumn)
	candidatesBySource := map[int]int{}
	for i, row := range df5g.Rows {
		if !fivegTimeMS.Valid[i] {
			continue
		}
		candidate := fivegCandidate{
			index:  i,
			mcc:    normalizeKeyStringNative(cellAt(row, fivegMCCIdx)),
			mnc:    normalizeKeyStringNative(cellAt(row, fivegMNCIdx)),
			timeMS: fivegTimeMS.Values[i],
		}
		if fivegSourceIdx >= 0 {
			source, parseErr := strconv.Atoi(strings.TrimSpace(cellAt(row, fivegSourceIdx)))
			if parseErr != nil || source < 0 {
				return nil, mobileSyncStats{}, fmt.Errorf("mobile mode: neplatná interná identita zdroja 5G CSV %q", cellAt(row, fivegSourceIdx))
			}
			candidate.source = source
			candidate.hasSource = true
			candidatesBySource[source]++
		}
		fivegCandidates = append(fivegCandidates, candidate)
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
	matchedBySource := map[int]int{}
	conflictingWindows := 0
	fallbackTimeOnlyRows := 0

	nCand := len(fivegCandidates)
	for i, c := range fivegCandidates {
		maybeEmitRowProgress(ctx, "mobile_sync", i, nCand)
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
			if c.hasSource {
				matchedBySource[c.source]++
			}
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
			if c.hasSource {
				matchedBySource[c.source]++
			}
			fallbackTimeOnlyRows++
			if conflict {
				conflictingWindows++
			}
			windowScores[c.index] = score
		}
	}
	if matchedRows == 0 {
		return nil, mobileSyncStats{}, fmt.Errorf(
			"mobile mode: medzi 5G a NSA LTE dátami sa nenašla žiadna časová zhoda v tolerancii ±%d ms; skontrolujte čas, časové pásmo, MCC/MNC a vybrané súbory",
			tolerance,
		)
	}
	if len(candidatesBySource) > 0 {
		sources := make([]int, 0, len(candidatesBySource))
		for source := range candidatesBySource {
			sources = append(sources, source)
		}
		sort.Ints(sources)
		for _, source := range sources {
			if matchedBySource[source] > 0 {
				continue
			}
			if source < len(sourcePaths) {
				return nil, mobileSyncStats{}, fmt.Errorf(
					"mobile mode: pre 5G CSV %q sa nenašla žiadna MCC/MNC + časová zhoda v NSA LTE dátach v tolerancii ±%d ms",
					sourcePaths[source], tolerance,
				)
			}
			return nil, mobileSyncStats{}, fmt.Errorf(
				"mobile mode: pre 5G CSV zdroj č. %d sa nenašla žiadna MCC/MNC + časová zhoda v NSA LTE dátach v tolerancii ±%d ms",
				source+1, tolerance,
			)
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
		val := "no"
		switch windowScores[i] {
		case 2:
			val = "yes"
			rowsYes++
		case 1:
			rowsNo++
		default:
			rowsNo++
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
	emitProcessingProgress(ctx, "mobile_sync", 100)
	return out, stats, nil
}

func validateMobileFiveGSourceTimes(data *CSVData, series timeSeriesNative, sourcePaths []string) error {
	if data == nil {
		return fmt.Errorf("mobile mode: nil 5G dataset")
	}
	sourceIdx := data.columnIndexByName(csvSourceIndexColumn)
	if sourceIdx < 0 {
		return nil
	}
	type timeCoverage struct {
		total int
		valid int
	}
	coverage := map[int]timeCoverage{}
	for rowIdx, row := range data.Rows {
		rawSource := strings.TrimSpace(cellAt(row, sourceIdx))
		source, err := strconv.Atoi(rawSource)
		if err != nil || source < 0 {
			return fmt.Errorf("mobile mode: neplatná interná identita zdroja 5G CSV %q", rawSource)
		}
		item := coverage[source]
		item.total++
		if rowIdx < len(series.Valid) && series.Valid[rowIdx] {
			item.valid++
		}
		coverage[source] = item
	}
	sources := make([]int, 0, len(coverage))
	for source := range coverage {
		sources = append(sources, source)
	}
	sort.Ints(sources)
	for _, source := range sources {
		item := coverage[source]
		if item.total == 0 || item.valid > 0 {
			continue
		}
		if source < len(sourcePaths) {
			return fmt.Errorf("mobile mode: 5G CSV %q neobsahuje žiadny parsovateľný čas v UTC ani Date + Time", sourcePaths[source])
		}
		return fmt.Errorf("mobile mode: 5G CSV zdroj č. %d neobsahuje žiadny parsovateľný čas v UTC ani Date + Time", source+1)
	}
	return nil
}

func loadMobileNSALTECSVFiles(ctx context.Context, paths []string) (*CSVData, error) {
	nrCanonical := normalizedColumnKey("5G NR")
	paths, loaded, merged, err := loadAndMergeCSVFilesWithOptions(ctx, paths, CSVMergeOptions{
		DisplayNameOverrides: map[string]string{
			nrCanonical: "5G NR",
		},
		EquivalentColumnKeys: map[string]string{
			normalizedColumnKey("5G NR"): nrCanonical,
			normalizedColumnKey("5GNR"):  nrCanonical,
			normalizedColumnKey("NR"):    nrCanonical,
		},
	})
	if err != nil {
		return nil, err
	}
	for i, data := range loaded {
		mccColumn := findColumnNameNative(data.Columns, []string{"MCC"})
		if mccColumn == "" {
			return nil, fmt.Errorf("CSV %q neobsahuje povinný NSA LTE stĺpec MCC", paths[i])
		}
		mncColumn := findColumnNameNative(data.Columns, []string{"MNC"})
		if mncColumn == "" {
			return nil, fmt.Errorf("CSV %q neobsahuje povinný NSA LTE stĺpec MNC", paths[i])
		}
		nrColumn := findColumnNameNative(data.Columns, []string{"5G NR", "5GNR", "NR"})
		if nrColumn == "" {
			return nil, fmt.Errorf("CSV %q neobsahuje povinný NSA LTE stĺpec 5G NR", paths[i])
		}
		utcColumn := findColumnNameNative(data.Columns, []string{"UTC"})
		dateColumn := findColumnNameNative(data.Columns, []string{"Date"})
		timeColumn := findColumnNameNative(data.Columns, []string{"Time"})
		hasUTC := utcColumn != ""
		hasDateTime := dateColumn != "" && timeColumn != ""
		if !hasUTC && !hasDateTime {
			return nil, fmt.Errorf("CSV %q neobsahuje časový stĺpec UTC ani dvojicu Date + Time", paths[i])
		}

		mccIdx := data.columnIndexByName(mccColumn)
		mncIdx := data.columnIndexByName(mncColumn)
		nrIdx := data.columnIndexByName(nrColumn)
		utcIdx := data.columnIndexByName(utcColumn)
		dateIdx := data.columnIndexByName(dateColumn)
		timeIdx := data.columnIndexByName(timeColumn)
		series, strategy := buildTimeMillisSeriesNative(data, utcIdx, dateIdx, timeIdx)
		if strategy == "missing" {
			return nil, fmt.Errorf("CSV %q neobsahuje žiadny parsovateľný čas v UTC ani Date + Time", paths[i])
		}
		usableRows := 0
		for rowIdx, row := range data.Rows {
			if rowIdx >= len(series.Valid) || !series.Valid[rowIdx] {
				continue
			}
			if normalizeKeyStringNative(cellAt(row, mccIdx)) == "" || normalizeKeyStringNative(cellAt(row, mncIdx)) == "" {
				continue
			}
			if nrScore(normalizeNRValueNative(cellAt(row, nrIdx))) == 0 {
				continue
			}
			usableRows++
		}
		if usableRows == 0 {
			return nil, fmt.Errorf("CSV %q neobsahuje žiadny použiteľný riadok s časom, MCC, MNC a hodnotou 5G NR yes/no", paths[i])
		}
	}
	if len(paths) > 1 {
		if sorted, ok := sortMergedCSVRowsByTime(merged); ok {
			merged = sorted
		}
	}
	return merged, nil
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

	// Prefer the explicit Date + Time pair for every row. Some meter exports carry
	// a rounded or otherwise incompatible UTC column next to the more precise local
	// timestamp. When Date + Time is unavailable or malformed for an individual row,
	// fall back to that row's UTC value instead of discarding the whole source file.
	dateTimeRows := 0
	if dateIdx >= 0 && timeIdx >= 0 {
		for i, row := range data.Rows {
			d := strings.TrimSpace(cellAt(row, dateIdx))
			t := strings.TrimSpace(cellAt(row, timeIdx))
			if d == "" || t == "" {
				continue
			}
			if ms, ok := parseDateTimeToMillis(d + " " + t); ok {
				out.Values[i] = ms
				out.Valid[i] = true
				dateTimeRows++
			}
		}
	}

	utcNumericRows := 0
	utcDateTimeRows := 0
	utcFallbackRows := 0
	if utcIdx >= 0 {
		for i, row := range data.Rows {
			if out.Valid[i] {
				continue
			}
			raw := strings.TrimSpace(cellAt(row, utcIdx))
			if raw == "" {
				continue
			}
			if value, ok := parseNumberString(raw); ok {
				if ms, ok := numericEpochToMillis(value); ok {
					out.Values[i] = ms
					out.Valid[i] = true
					utcNumericRows++
					utcFallbackRows++
					continue
				}
			}
			if ms, ok := parseDateTimeToMillis(raw); ok {
				out.Values[i] = ms
				out.Valid[i] = true
				utcDateTimeRows++
				utcFallbackRows++
			}
		}
	}

	switch {
	case dateTimeRows > 0 && utcFallbackRows > 0:
		return out, "date_time_with_utc_fallback"
	case dateTimeRows > 0:
		return out, "date_time"
	case utcNumericRows > 0 && utcDateTimeRows > 0:
		return out, "utc_mixed"
	case utcNumericRows > 0:
		return out, "utc_numeric"
	case utcDateTimeRows > 0:
		return out, "utc_datetime"
	default:
		return out, "missing"
	}
}

// numericEpochToMillis accepts epoch values emitted by different meters in
// seconds, milliseconds, microseconds, or nanoseconds. The unit is determined
// independently for each row so merging files with different export settings is
// safe. Thresholds sit well between contemporary epoch magnitudes.
func numericEpochToMillis(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	abs := math.Abs(value)
	var millis float64
	switch {
	case abs >= 1e17:
		millis = value / 1e6 // nanoseconds
	case abs >= 1e14:
		millis = value / 1e3 // microseconds
	case abs >= 1e11:
		millis = value // milliseconds
	default:
		millis = value * 1e3 // seconds
	}
	if math.IsNaN(millis) || math.IsInf(millis, 0) || math.Abs(millis) > 9.22e18 {
		return 0, false
	}
	return int64(math.Round(millis)), true
}

func parseDateTimeToMillis(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	location := time.Local
	if bratislava, err := time.LoadLocation("Europe/Bratislava"); err == nil {
		location = bratislava
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2.1.2006 15:04:05.999999999",
		"2.1.2006 15:04:05",
		"02.01.2006 15:04:05.999999999",
		"02.01.2006 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, s, location); err == nil {
			return ts.UnixMilli(), true
		}
	}
	return 0, false
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
	// MCC/MNC are numeric identifiers, but CSV exporters disagree about leading
	// zeros and Excel-style decimal formatting (for example 01 vs 1 vs 1.0).
	// Canonicalizing numeric spellings prevents otherwise identical operators from
	// ending up in different sync lookup groups.
	if value, ok := parseNumberString(s); ok && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return normalizeIntLikeString(value)
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
		Columns:        append([]string(nil), data.Columns...),
		Rows:           make([][]string, 0, len(data.Rows)),
		FileInfo:       data.FileInfo,
		InputRadioTech: data.InputRadioTech,
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
