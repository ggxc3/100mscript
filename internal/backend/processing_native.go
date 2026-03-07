package backend

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type ProcessedRow struct {
	Raw              []string
	OriginalExcelRow int
	XMeters          float64
	YMeters          float64
	ZonaX            float64
	ZonaY            float64
	ZonaKey          string
	OperatorKey      string
	ZonaOperatorKey  string
	MCC              string
	MNC              string
	PCI              string
	Frequency        string
	NRValue          string
	RSRP             float64
	SINR             float64
	HasSINR          bool
}

type ProcessedDataset struct {
	Rows        []ProcessedRow
	Columns     []string
	FileInfo    CSVFileInfo
	SegmentMeta map[int]Point
}

type ZoneStat struct {
	ZonaKey                string
	OperatorKey            string
	ZonaX                  float64
	ZonaY                  float64
	MCC                    string
	MNC                    string
	PCI                    string
	RSRPAvg                float64
	PocetMerani            int
	NajcastejsiaFrekvencia string
	NRValue                string
	VsetkyFrekvencie       []string
	OriginalExcelRows      []int
	SINRAvg                float64
	HasSINRAvg             bool
	ZonaStredX             float64
	ZonaStredY             float64
	Longitude              float64
	Latitude               float64
	RSRPKategoria          string
}

func ProcessDataNative(ctx context.Context, data *CSVData, cfg ProcessingConfig, transformer *PyProjTransformer) (*ProcessedDataset, error) {
	if data == nil {
		return nil, fmt.Errorf("nil CSVData")
	}
	cols := data.Columns
	requiredKeys := []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"}
	for _, key := range requiredKeys {
		if _, ok := cfg.ColumnMapping[key]; !ok {
			return nil, fmt.Errorf("missing required column mapping: %s", key)
		}
	}

	latIdx := cfg.ColumnMapping["latitude"]
	lonIdx := cfg.ColumnMapping["longitude"]
	freqIdx := cfg.ColumnMapping["frequency"]
	pciIdx := cfg.ColumnMapping["pci"]
	mccIdx := cfg.ColumnMapping["mcc"]
	mncIdx := cfg.ColumnMapping["mnc"]
	rsrpIdx := cfg.ColumnMapping["rsrp"]
	sinrIdx, hasSINRCol := cfg.ColumnMapping["sinr"]
	nrCandidates := []string{"5G NR", "5GNR", "NR"}
	if v := strings.TrimSpace(cfg.MobileNRColumnName); v != "" {
		nrCandidates = append([]string{v}, nrCandidates...)
	}
	nrIdx := data.columnIndexByName(findColumnNameNative(cols, nrCandidates))

	origExcelIdx := indexOf(cols, "original_excel_row")

	type rawParsed struct {
		row              []string
		originalExcelRow int
		lat              float64
		lon              float64
		rsrp             float64
		sinr             float64
		hasSINR          bool
	}

	filtered := make([]rawParsed, 0, len(data.Rows))
	for i, row := range data.Rows {
		rowCopy := append([]string(nil), row...)
		originalExcelRow := i + data.FileInfo.HeaderLine + 1
		if origExcelIdx >= 0 && origExcelIdx < len(rowCopy) {
			if v, err := strconv.Atoi(strings.TrimSpace(rowCopy[origExcelIdx])); err == nil {
				originalExcelRow = v
			}
		}

		rsrp, ok := parseNumberString(cellAt(rowCopy, rsrpIdx))
		if !ok {
			continue // mirror Python: drop rows with missing RSRP
		}
		lat, ok := parseNumberString(cellAt(rowCopy, latIdx))
		if !ok {
			return nil, fmt.Errorf("invalid latitude at row %d", originalExcelRow)
		}
		lon, ok := parseNumberString(cellAt(rowCopy, lonIdx))
		if !ok {
			return nil, fmt.Errorf("invalid longitude at row %d", originalExcelRow)
		}

		var sinr float64
		var hasSINR bool
		if hasSINRCol {
			if v, ok := parseNumberString(cellAt(rowCopy, sinrIdx)); ok {
				sinr = v
				hasSINR = true
			}
		}

		filtered = append(filtered, rawParsed{
			row:              rowCopy,
			originalExcelRow: originalExcelRow,
			lat:              lat,
			lon:              lon,
			rsrp:             rsrp,
			sinr:             sinr,
			hasSINR:          hasSINR,
		})
	}

	lonLat := make([]Point, len(filtered))
	for i, r := range filtered {
		lonLat[i] = Point{A: r.lon, B: r.lat}
	}
	xy, err := transformer.Forward(ctx, lonLat)
	if err != nil {
		return nil, err
	}

	rows := make([]ProcessedRow, len(filtered))
	segmentMeta := map[int]Point{}
	zoneSize := cfg.ZoneSizeM
	if zoneSize <= 0 {
		zoneSize = 100
	}

	epsilon := 1e-9
	segmentIDs := make([]int, len(filtered))
	if cfg.ZoneMode == "segments" && len(filtered) > 0 {
		cumulativeDistance := 0.0
		prevX, prevY := xy[0].A, xy[0].B
		segmentMeta[0] = Point{A: prevX, B: prevY}
		segmentIDs[0] = 0
		for i := 1; i < len(filtered); i++ {
			x, y := xy[i].A, xy[i].B
			stepDistance := math.Hypot(x-prevX, y-prevY)
			if stepDistance > 0 {
				prevCumulative := cumulativeDistance
				cumulativeDistance += stepDistance
				prevSegment := int(math.Floor((prevCumulative + epsilon) / zoneSize))
				newSegment := int(math.Floor((cumulativeDistance + epsilon) / zoneSize))
				if newSegment > prevSegment {
					for segID := prevSegment + 1; segID <= newSegment; segID++ {
						boundaryDistance := float64(segID) * zoneSize
						offset := boundaryDistance - prevCumulative
						fraction := offset / stepDistance
						if fraction < 0 {
							fraction = 0
						} else if fraction > 1 {
							fraction = 1
						}
						segmentMeta[segID] = Point{
							A: prevX + (x-prevX)*fraction,
							B: prevY + (y-prevY)*fraction,
						}
					}
				}
			}
			segmentIDs[i] = int(math.Floor((cumulativeDistance + epsilon) / zoneSize))
			prevX, prevY = x, y
		}
	}

	for i, src := range filtered {
		x := xy[i].A
		y := xy[i].B
		mcc := cellAt(src.row, mccIdx)
		mnc := cellAt(src.row, mncIdx)
		pci := cellAt(src.row, pciIdx)
		freq := cellAt(src.row, freqIdx)
		operatorKey := mcc + "_" + mnc

		var zonaX, zonaY float64
		var zonaKey string
		if cfg.ZoneMode == "segments" {
			segID := segmentIDs[i]
			start, ok := segmentMeta[segID]
			if !ok {
				start = Point{A: xy[0].A, B: xy[0].B}
			}
			zonaX, zonaY = start.A, start.B
			zonaKey = fmt.Sprintf("segment_%d", segID)
		} else {
			zonaX = math.Floor(x/zoneSize) * zoneSize
			zonaY = math.Floor(y/zoneSize) * zoneSize
			zonaKey = fmt.Sprintf("%v_%v", zonaX, zonaY)
		}

		rows[i] = ProcessedRow{
			Raw:              src.row,
			OriginalExcelRow: src.originalExcelRow,
			XMeters:          x,
			YMeters:          y,
			ZonaX:            zonaX,
			ZonaY:            zonaY,
			ZonaKey:          zonaKey,
			OperatorKey:      operatorKey,
			ZonaOperatorKey:  zonaKey + "_" + operatorKey,
			MCC:              mcc,
			MNC:              mnc,
			PCI:              pci,
			Frequency:        freq,
			NRValue:          normalizeNRValueNative(cellAt(src.row, nrIdx)),
			RSRP:             src.rsrp,
			SINR:             src.sinr,
			HasSINR:          src.hasSINR,
		}
	}

	return &ProcessedDataset{
		Rows:        rows,
		Columns:     cols,
		FileInfo:    data.FileInfo,
		SegmentMeta: segmentMeta,
	}, nil
}

func CalculateZoneStatsNative(
	ctx context.Context,
	ds *ProcessedDataset,
	cfg ProcessingConfig,
	transformer *PyProjTransformer,
) ([]ZoneStat, error) {
	if ds == nil {
		return nil, fmt.Errorf("nil ProcessedDataset")
	}

	type aggKey struct {
		ZonaKey     string
		OperatorKey string
		ZonaX       float64
		ZonaY       float64
		MCC         string
		MNC         string
		PCI         string
		Freq        string
	}
	type aggVal struct {
		RSRPSum      float64
		Count        int
		OriginalRows []int
		SINRSum      float64
		SINRCount    int
		NRYesCount   int
		NRNoCount    int
	}

	agg := make(map[aggKey]*aggVal)
	for _, r := range ds.Rows {
		if strings.TrimSpace(r.MCC) == "" || strings.TrimSpace(r.MNC) == "" || strings.TrimSpace(r.PCI) == "" || strings.TrimSpace(r.Frequency) == "" {
			// Mirror pandas groupby behavior: rows with missing grouping keys (blank CSV -> NaN) are excluded.
			continue
		}
		key := aggKey{
			ZonaKey:     r.ZonaKey,
			OperatorKey: r.OperatorKey,
			ZonaX:       r.ZonaX,
			ZonaY:       r.ZonaY,
			MCC:         r.MCC,
			MNC:         r.MNC,
			PCI:         r.PCI,
			Freq:        r.Frequency,
		}
		v := agg[key]
		if v == nil {
			v = &aggVal{}
			agg[key] = v
		}
		v.RSRPSum += r.RSRP
		v.Count++
		v.OriginalRows = append(v.OriginalRows, r.OriginalExcelRow)
		switch r.NRValue {
		case "yes":
			v.NRYesCount++
		case "no":
			v.NRNoCount++
		}
		if r.HasSINR {
			v.SINRSum += r.SINR
			v.SINRCount++
		}
	}

	type zoneFreqStat struct {
		Key     aggKey
		RSRPAvg float64
		Count   int
		Rows    []int
		NRValue string
		SINRAvg float64
		HasSINR bool
	}

	zoneFreqStats := make([]zoneFreqStat, 0, len(agg))
	for k, v := range agg {
		z := zoneFreqStat{
			Key:     k,
			RSRPAvg: v.RSRPSum / float64(v.Count),
			Count:   v.Count,
			Rows:    append([]int(nil), v.OriginalRows...),
		}
		if v.NRYesCount > 0 {
			z.NRValue = "yes"
		} else if v.NRNoCount > 0 {
			z.NRValue = "no"
		}
		if v.SINRCount > 0 {
			z.SINRAvg = v.SINRSum / float64(v.SINRCount)
			z.HasSINR = true
		}
		zoneFreqStats = append(zoneFreqStats, z)
	}

	sort.Slice(zoneFreqStats, func(i, j int) bool {
		a, b := zoneFreqStats[i], zoneFreqStats[j]
		if a.RSRPAvg != b.RSRPAvg {
			return a.RSRPAvg > b.RSRPAvg
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if c := compareNumericStringAsc(a.Key.Freq, b.Key.Freq); c != 0 {
			return c < 0
		}
		if a.Key.Freq != b.Key.Freq {
			return a.Key.Freq < b.Key.Freq
		}
		if c := compareNumericStringAsc(a.Key.PCI, b.Key.PCI); c != 0 {
			return c < 0
		}
		return a.Key.PCI < b.Key.PCI
	})

	type chosenKey struct {
		ZonaKey     string
		OperatorKey string
		ZonaX       float64
		ZonaY       float64
		MCC         string
		MNC         string
	}
	chosen := map[chosenKey]zoneFreqStat{}
	chosenOrder := make([]chosenKey, 0, len(zoneFreqStats))
	for _, z := range zoneFreqStats {
		key := chosenKey{
			ZonaKey:     z.Key.ZonaKey,
			OperatorKey: z.Key.OperatorKey,
			ZonaX:       z.Key.ZonaX,
			ZonaY:       z.Key.ZonaY,
			MCC:         z.Key.MCC,
			MNC:         z.Key.MNC,
		}
		if _, exists := chosen[key]; exists {
			continue
		}
		chosen[key] = z
		chosenOrder = append(chosenOrder, key)
	}

	points := make([]Point, 0, len(chosenOrder))
	zoneCenters := make([]Point, 0, len(chosenOrder))
	for _, key := range chosenOrder {
		z := chosen[key]
		var center Point
		if cfg.ZoneMode == "segments" {
			center = Point{A: z.Key.ZonaX, B: z.Key.ZonaY}
		} else {
			center = Point{A: z.Key.ZonaX + cfg.ZoneSizeM/2, B: z.Key.ZonaY + cfg.ZoneSizeM/2}
		}
		zoneCenters = append(zoneCenters, center)
		points = append(points, center)
	}
	lonLat, err := transformer.Inverse(ctx, points)
	if err != nil {
		return nil, err
	}

	stats := make([]ZoneStat, 0, len(chosenOrder))
	for i, key := range chosenOrder {
		z := chosen[key]
		stat := ZoneStat{
			ZonaKey:                z.Key.ZonaKey,
			OperatorKey:            z.Key.OperatorKey,
			ZonaX:                  z.Key.ZonaX,
			ZonaY:                  z.Key.ZonaY,
			MCC:                    z.Key.MCC,
			MNC:                    z.Key.MNC,
			PCI:                    z.Key.PCI,
			RSRPAvg:                z.RSRPAvg,
			PocetMerani:            z.Count,
			NajcastejsiaFrekvencia: z.Key.Freq,
			NRValue:                z.NRValue,
			VsetkyFrekvencie:       []string{z.Key.Freq},
			OriginalExcelRows:      append([]int(nil), z.Rows...),
			ZonaStredX:             zoneCenters[i].A,
			ZonaStredY:             zoneCenters[i].B,
			Longitude:              lonLat[i].A,
			Latitude:               lonLat[i].B,
		}
		if z.HasSINR {
			stat.SINRAvg = z.SINRAvg
			stat.HasSINRAvg = true
		}
		if stat.HasSINRAvg {
			if stat.RSRPAvg >= cfg.RSRPThreshold && stat.SINRAvg >= cfg.SINRThreshold {
				stat.RSRPKategoria = "RSRP_GOOD"
			} else {
				stat.RSRPKategoria = "RSRP_BAD"
			}
		} else {
			if stat.RSRPAvg >= cfg.RSRPThreshold {
				stat.RSRPKategoria = "RSRP_GOOD"
			} else {
				stat.RSRPKategoria = "RSRP_BAD"
			}
		}
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.ZonaKey != b.ZonaKey {
			return a.ZonaKey < b.ZonaKey
		}
		if a.OperatorKey != b.OperatorKey {
			return a.OperatorKey < b.OperatorKey
		}
		if a.ZonaX != b.ZonaX {
			return a.ZonaX < b.ZonaX
		}
		if a.ZonaY != b.ZonaY {
			return a.ZonaY < b.ZonaY
		}
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

	return stats, nil
}

func cellAt(row []string, idx int) string {
	if idx >= 0 && idx < len(row) {
		return row[idx]
	}
	return ""
}

func compareNumericStringAsc(a, b string) int {
	av, aok := parseNumberString(a)
	bv, bok := parseNumberString(b)
	if aok && bok {
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
		return 0
	}
	// pandas ascending puts NaN after valid numbers
	if aok && !bok {
		return -1
	}
	if !aok && bok {
		return 1
	}
	return 0
}
