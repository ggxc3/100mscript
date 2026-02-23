package backend

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type ZoneExportOutcome struct {
	ZoneStats   []ZoneStat
	UniqueZones []string
}

type zoneExportLayout struct {
	exportHeaderCols  []string
	exportHeaderToIdx map[string]int
	expectedColumns   int
	mccCol            string
	mncCol            string
	pciCol            string
	rsrpCol           string
	freqCol           string
	latCol            string
	lonCol            string
	sinrCol           string
	nrCol             string
	hasSINRCol        bool
	nrExportIndex     int
	mccExportIndex    int
	mncExportIndex    int
	pciExportIndex    int
}

func normalizeOutputSuffix(value string) string {
	suffix := strings.TrimSpace(value)
	if suffix != "" && !strings.HasPrefix(suffix, "_") {
		suffix = "_" + suffix
	}
	return suffix
}

func outputPathsForConfig(cfg ProcessingConfig) (zonesFile, statsFile, suffix string) {
	suffix = normalizeOutputSuffix(cfg.OutputSuffix)
	if cfg.MobileModeEnabled {
		if suffix == "" {
			suffix = "_mobile"
		} else if !strings.HasSuffix(suffix, "_mobile") {
			suffix += "_mobile"
		}
	}
	base := strings.TrimSuffix(cfg.FilePath, ".csv")
	if suffix != "" {
		return base + suffix + "_zones.csv", base + suffix + "_stats.csv", suffix
	}
	return base + "_zones.csv", base + "_stats.csv", suffix
}

func SaveStatsNative(zoneStats []ZoneStat, cfg ProcessingConfig, statsFile string, allZones []string) error {
	if len(zoneStats) == 0 {
		return os.WriteFile(statsFile, []byte("\n"), 0o644)
	}
	hasSINR := false
	for _, z := range zoneStats {
		if z.HasSINRAvg {
			hasSINR = true
			break
		}
	}
	goodColumn := fmt.Sprintf("RSRP >= %v", numberLabel(cfg.RSRPThreshold))
	badColumn := fmt.Sprintf("RSRP < %v", numberLabel(cfg.RSRPThreshold))
	if hasSINR {
		goodColumn = fmt.Sprintf("RSRP >= %v a SINR >= %v", numberLabel(cfg.RSRPThreshold), numberLabel(cfg.SINRThreshold))
		badColumn = fmt.Sprintf("RSRP < %v alebo SINR < %v", numberLabel(cfg.RSRPThreshold), numberLabel(cfg.SINRThreshold))
	}
	if cfg.MobileModeEnabled {
		goodColumn += " a 5G NR = yes"
		badColumn += " alebo 5G NR != yes"
	}

	type key struct{ MCC, MNC string }
	type statsBucket struct {
		good          int
		bad           int
		existingZones map[string]struct{}
	}

	group := map[key]*statsBucket{}
	order := []key{}
	for _, z := range zoneStats {
		k := key{MCC: z.MCC, MNC: z.MNC}
		b := group[k]
		if b == nil {
			b = &statsBucket{existingZones: map[string]struct{}{}}
			group[k] = b
			order = append(order, k)
		}
		if isZoneStatGoodForStats(z, cfg) {
			b.good++
		} else {
			b.bad++
		}
		b.existingZones[z.ZonaKey] = struct{}{}
	}
	totalUniqueZones := len(allZones)
	if totalUniqueZones == 0 {
		seen := map[string]struct{}{}
		for _, z := range zoneStats {
			if _, ok := seen[z.ZonaKey]; ok {
				continue
			}
			seen[z.ZonaKey] = struct{}{}
			totalUniqueZones++
		}
	}

	var sb strings.Builder
	sb.WriteString("MNC;MCC;")
	sb.WriteString(goodColumn)
	sb.WriteString(";")
	sb.WriteString(badColumn)
	sb.WriteString("\n")

	for _, k := range order {
		b := group[k]
		good := b.good
		bad := b.bad
		if cfg.IncludeEmptyZones && totalUniqueZones > 0 {
			missingZones := totalUniqueZones - len(b.existingZones)
			if missingZones > 0 {
				bad += missingZones
			}
		}
		sb.WriteString(formatIntLikeString(k.MNC))
		sb.WriteString(";")
		sb.WriteString(formatIntLikeString(k.MCC))
		sb.WriteString(";")
		sb.WriteString(strconv.Itoa(good))
		sb.WriteString(";")
		sb.WriteString(strconv.Itoa(bad))
		sb.WriteString("\n")
	}

	return os.WriteFile(statsFile, []byte(sb.String()), 0o644)
}

func isZoneStatGoodForStats(z ZoneStat, cfg ProcessingConfig) bool {
	good := z.RSRPAvg >= cfg.RSRPThreshold
	if z.HasSINRAvg {
		good = good && z.SINRAvg >= cfg.SINRThreshold
	}
	if cfg.MobileModeEnabled {
		good = good && normalizeNRValueNative(z.NRValue) == "yes"
	}
	return good
}

func SaveZoneResultsNative(
	ctx context.Context,
	ds *ProcessedDataset,
	zoneStats []ZoneStat,
	cfg ProcessingConfig,
	transformer *PyProjTransformer,
	zonesFile string,
) (ZoneExportOutcome, error) {
	if ds == nil {
		return ZoneExportOutcome{ZoneStats: append([]ZoneStat(nil), zoneStats...)}, fmt.Errorf("nil dataset")
	}

	layout := buildZoneExportLayout(ds, cfg)
	lines := []string{"", strings.Join(layout.exportHeaderCols, ";") + ";Riadky_v_zone;Frekvencie_v_zone"}

	type sampleKey struct {
		zonaOperatorKey string
		freq            string
		pci             string
	}
	sampleRowIndexByKey := map[sampleKey]int{}
	for i, r := range ds.Rows {
		k := sampleKey{zonaOperatorKey: r.ZonaOperatorKey, freq: r.Frequency, pci: r.PCI}
		if _, exists := sampleRowIndexByKey[k]; !exists {
			sampleRowIndexByKey[k] = i
		}
	}

	sortedStats := append([]ZoneStat(nil), zoneStats...)
	sort.Slice(sortedStats, func(i, j int) bool {
		a, b := sortedStats[i], sortedStats[j]
		if c := compareNumericStringAsc(a.MCC, b.MCC); c != 0 {
			return c < 0
		}
		if a.MCC != b.MCC {
			return a.MCC < b.MCC
		}
		if c := compareNumericStringAsc(a.MNC, b.MNC); c != 0 {
			return c < 0
		}
		if a.MNC != b.MNC {
			return a.MNC < b.MNC
		}
		if c := compareNumericStringAsc(a.PCI, b.PCI); c != 0 {
			return c < 0
		}
		return a.PCI < b.PCI
	})

	processedZonaOperators := map[string]bool{}
	uniqueZonesOrdered := make([]string, 0, len(sortedStats))
	seenZones := map[string]bool{}
	useZoneCenter := cfg.ZoneMode == "center"
	for _, zs := range sortedStats {
		if !seenZones[zs.ZonaKey] {
			seenZones[zs.ZonaKey] = true
			uniqueZonesOrdered = append(uniqueZonesOrdered, zs.ZonaKey)
		}

		zok := zs.ZonaKey + "_" + zs.OperatorKey
		if processedZonaOperators[zok] {
			continue
		}
		processedZonaOperators[zok] = true

		rowIndex, ok := sampleRowIndexByKey[sampleKey{zonaOperatorKey: zok, freq: zs.NajcastejsiaFrekvencia, pci: zs.PCI}]
		if !ok {
			continue
		}
		baseRowMap := rowValueMap(ds.Columns, ds.Rows[rowIndex].Raw)
		baseRowMap[layout.rsrpCol] = fmt.Sprintf("%.2f", zs.RSRPAvg)
		baseRowMap[layout.freqCol] = zs.NajcastejsiaFrekvencia
		baseRowMap[layout.pciCol] = zs.PCI
		if layout.nrCol != "" {
			baseRowMap[layout.nrCol] = zs.NRValue
		}
		if layout.hasSINRCol && zs.HasSINRAvg {
			baseRowMap[layout.sinrCol] = fmt.Sprintf("%.2f", zs.SINRAvg)
		}
		if cfg.ZoneMode == "segments" || useZoneCenter {
			baseRowMap[layout.latCol] = fmt.Sprintf("%.6f", zs.Latitude)
			baseRowMap[layout.lonCol] = fmt.Sprintf("%.6f", zs.Longitude)
		} else {
			baseRowMap[layout.latCol] = normalizePandasCoordinateString(baseRowMap[layout.latCol])
			baseRowMap[layout.lonCol] = normalizePandasCoordinateString(baseRowMap[layout.lonCol])
		}

		rowValues := buildExportRowValues(layout, baseRowMap)
		excelRowsStr := joinInts(zs.OriginalExcelRows, ",")
		freqsStr := strings.Join(uniqueSortedStrings(zs.VsetkyFrekvencie), ",")
		lines = append(lines, strings.Join(rowValues, ";")+";"+excelRowsStr+";"+freqsStr+fmt.Sprintf(" # Meraní: %d", zs.PocetMerani))
	}

	allZoneKeys := uniqueZonesOrdered
	if cfg.IncludeEmptyZones {
		operatorOrder, operatorTemplateRows := buildOperatorTemplatesForEmptyRows(ds, sortedStats, layout)
		if cfg.ZoneMode == "segments" {
			var err error
			allZoneKeys, err = buildAllSegmentZoneKeys(ds, sortedStats)
			if err != nil {
				return ZoneExportOutcome{ZoneStats: append([]ZoneStat(nil), zoneStats...)}, err
			}
		}
		lines, processedZonaOperators = appendEmptyZonesNative(ctx, lines, ds, layout, cfg, transformer, allZoneKeys, operatorOrder, operatorTemplateRows, processedZonaOperators)
	}

	finalZoneStats := append([]ZoneStat(nil), zoneStats...)
	customRowsAdded := 0
	if cfg.IncludeEmptyZones && cfg.AddCustomOperators && len(cfg.CustomOperators) > 0 {
		var added int
		lines, finalZoneStats, added = appendCustomOperatorsNative(ctx, lines, ds, sortedStats, finalZoneStats, layout, cfg, transformer, allZoneKeys, processedZonaOperators)
		customRowsAdded = added
	}

	content := strings.Join(lines, "\n")
	if customRowsAdded > 0 {
		content += "\n"
	}
	if err := os.WriteFile(zonesFile, []byte(content), 0o644); err != nil {
		return ZoneExportOutcome{ZoneStats: finalZoneStats, UniqueZones: allZoneKeys}, err
	}
	return ZoneExportOutcome{ZoneStats: finalZoneStats, UniqueZones: allZoneKeys}, nil
}

func buildZoneExportLayout(ds *ProcessedDataset, cfg ProcessingConfig) zoneExportLayout {
	headerLine := strings.TrimSpace(ds.FileInfo.OriginalHeader)
	if headerLine == "" || !strings.Contains(headerLine, ";") {
		headerLine = strings.Join(ds.Columns, ";")
	}
	origHeaderCols := strings.Split(headerLine, ";")
	for len(origHeaderCols) > 0 && origHeaderCols[len(origHeaderCols)-1] == "" {
		origHeaderCols = origHeaderCols[:len(origHeaderCols)-1]
	}
	nrCandidates := []string{"5G NR", "5GNR", "NR"}
	if v := strings.TrimSpace(cfg.MobileNRColumnName); v != "" {
		nrCandidates = append([]string{v}, nrCandidates...)
	}
	nrColName := findColumnNameNative(ds.Columns, nrCandidates)
	extraOutputCols := []string{}
	if nrColName != "" && !containsString(origHeaderCols, nrColName) {
		extraOutputCols = append(extraOutputCols, nrColName)
	}
	exportHeaderCols := append(append([]string{}, origHeaderCols...), extraOutputCols...)
	exportHeaderToIdx := make(map[string]int, len(exportHeaderCols))
	for i, c := range exportHeaderCols {
		exportHeaderToIdx[c] = i
	}

	colNames := ds.Columns
	layout := zoneExportLayout{
		exportHeaderCols:  exportHeaderCols,
		exportHeaderToIdx: exportHeaderToIdx,
		expectedColumns:   len(exportHeaderCols),
		rsrpCol:           colNames[cfg.ColumnMapping["rsrp"]],
		freqCol:           colNames[cfg.ColumnMapping["frequency"]],
		latCol:            colNames[cfg.ColumnMapping["latitude"]],
		lonCol:            colNames[cfg.ColumnMapping["longitude"]],
		mccCol:            colNames[cfg.ColumnMapping["mcc"]],
		mncCol:            colNames[cfg.ColumnMapping["mnc"]],
		pciCol:            colNames[cfg.ColumnMapping["pci"]],
		nrCol:             nrColName,
		nrExportIndex:     -1,
		mccExportIndex:    -1,
		mncExportIndex:    -1,
		pciExportIndex:    -1,
	}
	if idx, ok := cfg.ColumnMapping["sinr"]; ok && idx >= 0 && idx < len(colNames) {
		layout.sinrCol = colNames[idx]
		layout.hasSINRCol = true
	}
	if layout.nrCol != "" {
		if idx, ok := exportHeaderToIdx[layout.nrCol]; ok {
			layout.nrExportIndex = idx
		}
	}
	if idx, ok := exportHeaderToIdx[layout.mccCol]; ok {
		layout.mccExportIndex = idx
	}
	if idx, ok := exportHeaderToIdx[layout.mncCol]; ok {
		layout.mncExportIndex = idx
	}
	if idx, ok := exportHeaderToIdx[layout.pciCol]; ok {
		layout.pciExportIndex = idx
	}
	return layout
}

func buildExportRowValues(layout zoneExportLayout, baseRowMap map[string]string) []string {
	rowValues := make([]string, layout.expectedColumns)
	for i, headerCol := range layout.exportHeaderCols {
		val := strings.TrimSpace(baseRowMap[headerCol])
		if i == layout.mccExportIndex || i == layout.mncExportIndex || (layout.pciExportIndex >= 0 && i == layout.pciExportIndex) {
			rowValues[i] = formatIntLikeString(val)
		} else {
			rowValues[i] = val
		}
	}
	return rowValues
}

func normalizePandasCoordinateString(s string) string {
	v, ok := parseNumberString(s)
	if !ok {
		return s
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func normalizePandasFloatString(s string) string {
	v, ok := parseNumberString(s)
	if !ok {
		return s
	}
	if math.Trunc(v) == v {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func buildOperatorTemplatesForEmptyRows(ds *ProcessedDataset, sortedStats []ZoneStat, layout zoneExportLayout) ([][2]string, map[string][]string) {
	operatorOrder := make([][2]string, 0)
	seenOperators := map[string]bool{}
	for _, zs := range sortedStats {
		k := zs.MCC + "_" + zs.MNC
		if seenOperators[k] {
			continue
		}
		seenOperators[k] = true
		operatorOrder = append(operatorOrder, [2]string{zs.MCC, zs.MNC})
	}

	firstRowByOperator := map[string]int{}
	for i, r := range ds.Rows {
		k := normalizeOperatorPair(r.MCC, r.MNC)
		if _, ok := firstRowByOperator[k]; !ok {
			firstRowByOperator[k] = i
		}
	}

	templateRows := make(map[string][]string, len(operatorOrder))
	for _, op := range operatorOrder {
		opKey := op[0] + "_" + op[1]
		rowIdx, ok := firstRowByOperator[normalizeOperatorPair(op[0], op[1])]
		if !ok {
			if len(ds.Rows) == 0 {
				templateRows[opKey] = make([]string, layout.expectedColumns)
				continue
			}
			rowIdx = 0
		}
		baseRowMap := rowValueMap(ds.Columns, ds.Rows[rowIdx].Raw)
		baseRowMap[layout.mccCol] = op[0]
		baseRowMap[layout.mncCol] = op[1]
		baseRowMap[layout.rsrpCol] = "-174"
		if layout.hasSINRCol {
			baseRowMap[layout.sinrCol] = normalizePandasFloatString(baseRowMap[layout.sinrCol])
		}
		rowValues := buildExportRowValues(layout, baseRowMap)
		templateRows[opKey] = rowValues
	}
	return operatorOrder, templateRows
}

func normalizeOperatorPair(mcc, mnc string) string {
	return formatIntLikeString(mcc) + "_" + formatIntLikeString(mnc)
}

func buildAllSegmentZoneKeys(ds *ProcessedDataset, sortedStats []ZoneStat) ([]string, error) {
	if len(ds.SegmentMeta) > 0 {
		ids := make([]int, 0, len(ds.SegmentMeta))
		for id := range ds.SegmentMeta {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = fmt.Sprintf("segment_%d", id)
		}
		return out, nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, zs := range sortedStats {
		if !seen[zs.ZonaKey] {
			seen[zs.ZonaKey] = true
			out = append(out, zs.ZonaKey)
		}
	}
	return out, nil
}

func appendEmptyZonesNative(
	ctx context.Context,
	lines []string,
	ds *ProcessedDataset,
	layout zoneExportLayout,
	cfg ProcessingConfig,
	transformer *PyProjTransformer,
	allZoneKeys []string,
	operatorOrder [][2]string,
	operatorTemplateRows map[string][]string,
	processedZonaOperators map[string]bool,
) ([]string, map[string]bool) {
	if len(allZoneKeys) == 0 || len(operatorOrder) == 0 {
		return lines, processedZonaOperators
	}

	zoneLatLon := computeZoneLatLonStrings(ctx, ds, allZoneKeys, cfg, transformer)
	comment := " # Prázdna zóna - automaticky vygenerovaná"
	if cfg.ZoneMode == "segments" {
		comment = " # Prázdny úsek - automaticky vygenerovaný"
	}

	for _, zonaKey := range allZoneKeys {
		coords, ok := zoneLatLon[zonaKey]
		if !ok {
			continue
		}
		for _, op := range operatorOrder {
			opKey := op[0] + "_" + op[1]
			zok := zonaKey + "_" + opKey
			if processedZonaOperators[zok] {
				continue
			}
			processedZonaOperators[zok] = true

			rowValues := append([]string(nil), operatorTemplateRows[opKey]...)
			if layout.exportHeaderToIdx[layout.latCol] < layout.expectedColumns {
				rowValues[layout.exportHeaderToIdx[layout.latCol]] = coords[0]
			}
			if layout.exportHeaderToIdx[layout.lonCol] < layout.expectedColumns {
				rowValues[layout.exportHeaderToIdx[layout.lonCol]] = coords[1]
			}
			if layout.exportHeaderToIdx[layout.rsrpCol] < layout.expectedColumns {
				rowValues[layout.exportHeaderToIdx[layout.rsrpCol]] = "-174"
			}
			if layout.nrExportIndex >= 0 && layout.nrExportIndex < layout.expectedColumns {
				rowValues[layout.nrExportIndex] = "no"
			}
			lines = append(lines, strings.Join(rowValues, ";")+";;"+comment)
		}
	}
	return lines, processedZonaOperators
}

func appendCustomOperatorsNative(
	ctx context.Context,
	lines []string,
	ds *ProcessedDataset,
	sortedStats []ZoneStat,
	currentZoneStats []ZoneStat,
	layout zoneExportLayout,
	cfg ProcessingConfig,
	transformer *PyProjTransformer,
	allZoneKeys []string,
	processedZonaOperators map[string]bool,
) ([]string, []ZoneStat, int) {
	existingOperatorsAtStart := map[string]bool{}
	for _, zs := range sortedStats {
		existingOperatorsAtStart[zs.OperatorKey] = true
	}

	customOps := dedupeCustomOperators(cfg.CustomOperators, existingOperatorsAtStart)
	if len(customOps) == 0 {
		return lines, currentZoneStats, 0
	}
	if len(allZoneKeys) == 0 {
		allZoneKeys = []string{}
	}

	zoneLatLon := computeZoneLatLonStrings(ctx, ds, allZoneKeys, cfg, transformer)

	baseRowMap := map[string]string{}
	if len(ds.Rows) > 0 {
		baseRowMap = rowValueMap(ds.Columns, ds.Rows[0].Raw)
	}
	if layout.hasSINRCol {
		baseRowMap[layout.sinrCol] = normalizePandasFloatString(baseRowMap[layout.sinrCol])
	}

	addedRows := 0
	for _, zonaKey := range allZoneKeys {
		coords, ok := zoneLatLon[zonaKey]
		if !ok {
			continue
		}
		for _, op := range customOps {
			opKey := op.MCC + "_" + op.MNC
			zok := zonaKey + "_" + opKey
			if processedZonaOperators[zok] {
				continue
			}
			processedZonaOperators[zok] = true

			rowMap := copyStringMap(baseRowMap)
			rowMap[layout.latCol] = coords[0]
			rowMap[layout.lonCol] = coords[1]
			rowMap[layout.rsrpCol] = "-174"
			rowMap[layout.mccCol] = op.MCC
			rowMap[layout.mncCol] = op.MNC
			rowMap[layout.pciCol] = op.PCI
			rowValues := buildExportRowValues(layout, rowMap)
			if layout.nrExportIndex >= 0 && layout.nrExportIndex < layout.expectedColumns {
				rowValues[layout.nrExportIndex] = "no"
			}
			comment := " # Prázdna zóna - vlastný operátor"
			if cfg.ZoneMode == "segments" {
				comment = " # Prázdny úsek - vlastný operátor"
			}
			lines = append(lines, strings.Join(rowValues, ";")+";;"+comment)
			addedRows++
		}
	}

	defaultZonaKey := "0_0"
	if cfg.ZoneMode == "segments" {
		defaultZonaKey = "segment_0"
	}
	if len(allZoneKeys) > 0 {
		defaultZonaKey = allZoneKeys[0]
	}

	var defaultZonaX, defaultZonaY, defaultLon, defaultLat float64
	if len(currentZoneStats) > 0 {
		first := currentZoneStats[0]
		defaultZonaX = first.ZonaX
		defaultZonaY = first.ZonaY
		defaultLon = first.Longitude
		defaultLat = first.Latitude
	}

	for _, op := range customOps {
		placeholder := ZoneStat{
			ZonaKey:                defaultZonaKey,
			OperatorKey:            op.MCC + "_" + op.MNC,
			ZonaX:                  defaultZonaX,
			ZonaY:                  defaultZonaY,
			MCC:                    op.MCC,
			MNC:                    op.MNC,
			PCI:                    op.PCI,
			RSRPAvg:                -174,
			PocetMerani:            0,
			NajcastejsiaFrekvencia: "",
			NRValue:                "no",
			VsetkyFrekvencie:       []string{},
			OriginalExcelRows:      []int{},
			ZonaStredX:             defaultZonaX,
			ZonaStredY:             defaultZonaY,
			Longitude:              defaultLon,
			Latitude:               defaultLat,
			RSRPKategoria:          "RSRP_BAD",
		}
		currentZoneStats = append(currentZoneStats, placeholder)
	}

	return lines, currentZoneStats, addedRows
}

func dedupeCustomOperators(custom []CustomOperator, existingAtStart map[string]bool) []CustomOperator {
	out := make([]CustomOperator, 0, len(custom))
	seen := map[string]bool{}
	for _, op := range custom {
		mcc := strings.TrimSpace(op.MCC)
		mnc := strings.TrimSpace(op.MNC)
		pci := strings.TrimSpace(op.PCI)
		if mcc == "" || mnc == "" {
			continue
		}
		key := mcc + "_" + mnc
		if existingAtStart[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CustomOperator{MCC: mcc, MNC: mnc, PCI: pci})
	}
	return out
}

func computeZoneLatLonStrings(
	ctx context.Context,
	ds *ProcessedDataset,
	allZoneKeys []string,
	cfg ProcessingConfig,
	transformer *PyProjTransformer,
) map[string][2]string {
	out := map[string][2]string{}
	if len(allZoneKeys) == 0 || transformer == nil {
		return out
	}

	if cfg.ZoneMode == "segments" {
		points := make([]Point, 0)
		keys := make([]string, 0)
		for _, zonaKey := range allZoneKeys {
			if strings.HasPrefix(zonaKey, "segment_") {
				idStr := strings.TrimPrefix(zonaKey, "segment_")
				if id, err := strconv.Atoi(idStr); err == nil {
					if p, ok := ds.SegmentMeta[id]; ok {
						keys = append(keys, zonaKey)
						points = append(points, p)
						continue
					}
				}
			}
		}
		if len(points) > 0 {
			if lonLat, err := transformer.Inverse(ctx, points); err == nil {
				for i, k := range keys {
					out[k] = [2]string{fmt.Sprintf("%.6f", lonLat[i].B), fmt.Sprintf("%.6f", lonLat[i].A)}
				}
			}
		}
		return out
	}

	points := make([]Point, 0, len(allZoneKeys))
	keys := make([]string, 0, len(allZoneKeys))
	for _, zonaKey := range allZoneKeys {
		parts := strings.Split(zonaKey, "_")
		if len(parts) != 2 {
			continue
		}
		zx, okX := parseNumberString(parts[0])
		zy, okY := parseNumberString(parts[1])
		if !okX || !okY {
			continue
		}
		keys = append(keys, zonaKey)
		points = append(points, Point{A: zx + cfg.ZoneSizeM/2, B: zy + cfg.ZoneSizeM/2})
	}
	if lonLat, err := transformer.Inverse(ctx, points); err == nil {
		for i, k := range keys {
			out[k] = [2]string{fmt.Sprintf("%.6f", lonLat[i].B), fmt.Sprintf("%.6f", lonLat[i].A)}
		}
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func formatIntLikeString(s string) string {
	if v, ok := parseNumberString(s); ok {
		return normalizeIntLikeString(v)
	}
	return s
}

func numberLabel(v float64) string {
	if mathTruncEqual(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func mathTruncEqual(v float64) bool {
	return math.Trunc(v) == v
}

func joinInts(values []int, sep string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	m := map[string]struct{}{}
	for _, v := range values {
		m[v] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
