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

type rawParsed struct {
	row              []string
	originalExcelRow int
	lat              float64
	lon              float64
	rsrp             float64
	sinr             float64
	hasSINR          bool
}

const (
	commonRoutePointToleranceM = 75.0
	commonRouteMaxSamples      = 2000
	segmentEndpointCumEpsilon  = 1e-9
)

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
	sourceCSVIdx := indexOf(cols, csvSourceIndexColumn)

	filtered := make([]rawParsed, 0, len(data.Rows))
	nIn := len(data.Rows)
	for i, row := range data.Rows {
		maybeEmitProgressInRange(ctx, "compute_zones", i, nIn, 0, 22)
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
		rawLat := strings.TrimSpace(cellAt(rowCopy, latIdx))
		rawLon := strings.TrimSpace(cellAt(rowCopy, lonIdx))
		if rawLat == "" || rawLon == "" {
			continue // rows without coordinates are not spatially relevant
		}
		lat, ok := parseNumberString(rawLat)
		if !ok {
			return nil, fmt.Errorf("invalid latitude at row %d", originalExcelRow)
		}
		lon, ok := parseNumberString(rawLon)
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
	emitProcessingProgress(ctx, "compute_zones", 24)
	xy, err := transformer.Forward(ctx, lonLat)
	if err != nil {
		return nil, err
	}
	emitProcessingProgress(ctx, "compute_zones", 38)

	rows := make([]ProcessedRow, len(filtered))
	segmentMeta := map[int]Point{}
	zoneSize := cfg.ZoneSizeM
	if zoneSize <= 0 {
		zoneSize = 100
	}

	epsilon := 1e-9
	segmentIDs := make([]int, len(filtered))
	if cfg.ZoneMode == "segments" && len(filtered) > 0 {
		segmentIDs, segmentMeta = buildSegmentAssignments(filtered, xy, sourceCSVIdx, zoneSize, epsilon, cfg.IncludeEmptyZones, func(i, n int) {
			if n > 0 {
				maybeEmitProgressInRange(ctx, "compute_zones", i, n, 38, 46)
			}
		})
	}

	nF := len(filtered)
	for i, src := range filtered {
		maybeEmitProgressInRange(ctx, "compute_zones", i, nF, 46, 99)
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

	emitProcessingProgress(ctx, "compute_zones", 100)
	return &ProcessedDataset{
		Rows:        rows,
		Columns:     cols,
		FileInfo:    data.FileInfo,
		SegmentMeta: segmentMeta,
	}, nil
}

type segmentInputPoint struct {
	filteredIndex int
	x             float64
	y             float64
}

type segmentTrack struct {
	id     string
	points []segmentInputPoint
	cum    []float64
	length float64
}

type segmentTrackAssignment struct {
	assigned bool
	reversed bool
	offset   float64
}

type segmentSample struct {
	pointIndex int
	x          float64
	y          float64
	cum        float64
	global     float64
}

type segmentAlignment struct {
	ok       bool
	reversed bool
	offset   float64
	count    int
	mad      float64
}

type segmentGlobalPoint struct {
	x      float64
	y      float64
	global float64
}

func buildSegmentAssignments(filtered []rawParsed, xy []Point, sourceCSVIdx int, zoneSize, epsilon float64, includeEmptySegments bool, progress func(i, n int)) ([]int, map[int]Point) {
	segmentIDs := make([]int, len(filtered))
	segmentMeta := map[int]Point{}
	tracks := buildSegmentTracks(filtered, xy, sourceCSVIdx)
	if len(tracks) == 0 {
		return segmentIDs, segmentMeta
	}
	if len(tracks) == 1 {
		assignSingleTrackSegments(tracks[0], segmentIDs, segmentMeta, zoneSize, epsilon, progress)
		return segmentIDs, segmentMeta
	}
	assignCommonRouteSegments(tracks, segmentIDs, segmentMeta, zoneSize, epsilon, includeEmptySegments, progress)
	return segmentIDs, segmentMeta
}

func buildSegmentTracks(filtered []rawParsed, xy []Point, sourceCSVIdx int) []segmentTrack {
	trackOrder := []string{}
	trackByID := map[string]int{}
	for i := range filtered {
		trackID := segmentTrackID(filtered[i], sourceCSVIdx)
		_, ok := trackByID[trackID]
		if !ok {
			trackByID[trackID] = len(trackOrder)
			trackOrder = append(trackOrder, trackID)
		}
	}

	tracks := make([]segmentTrack, len(trackOrder))
	for i, id := range trackOrder {
		tracks[i].id = id
	}
	for i := range filtered {
		idx := trackByID[segmentTrackID(filtered[i], sourceCSVIdx)]
		tracks[idx].points = append(tracks[idx].points, segmentInputPoint{
			filteredIndex: i,
			x:             xy[i].A,
			y:             xy[i].B,
		})
	}
	for i := range tracks {
		tracks[i].cum = make([]float64, len(tracks[i].points))
		for j := 1; j < len(tracks[i].points); j++ {
			prev := tracks[i].points[j-1]
			curr := tracks[i].points[j]
			tracks[i].cum[j] = tracks[i].cum[j-1] + math.Hypot(curr.x-prev.x, curr.y-prev.y)
		}
		if len(tracks[i].cum) > 0 {
			tracks[i].length = tracks[i].cum[len(tracks[i].cum)-1]
		}
	}
	return tracks
}

func segmentTrackID(row rawParsed, sourceCSVIdx int) string {
	if sourceCSVIdx >= 0 {
		if v := strings.TrimSpace(cellAt(row.row, sourceCSVIdx)); v != "" {
			return v
		}
	}
	return "0"
}

func assignSingleTrackSegments(track segmentTrack, segmentIDs []int, segmentMeta map[int]Point, zoneSize, epsilon float64, progress func(i, n int)) {
	steps := len(track.points) - 1
	for i, p := range track.points {
		progress(i, steps)
		if i > 0 {
			addSegmentBoundaries(track, segmentTrackAssignment{}, i-1, i, segmentMeta, zoneSize, epsilon)
		}
		segID := int(math.Floor((track.cum[i] + epsilon) / zoneSize))
		segmentIDs[p.filteredIndex] = segID
		if _, ok := segmentMeta[segID]; !ok {
			segmentMeta[segID] = Point{A: p.x, B: p.y}
		}
	}
}

func assignCommonRouteSegments(tracks []segmentTrack, segmentIDs []int, segmentMeta map[int]Point, zoneSize, epsilon float64, includeEmptySegments bool, progress func(i, n int)) {
	assignments := alignSegmentTracks(tracks, zoneSize)
	minGlobal := math.Inf(1)
	for i := range tracks {
		for j := range tracks[i].points {
			g := segmentGlobalDistance(tracks[i], assignments[i], j)
			if g < minGlobal {
				minGlobal = g
			}
		}
	}
	if math.IsInf(minGlobal, 1) {
		return
	}
	for i := range assignments {
		assignments[i].offset -= minGlobal
	}

	for i, track := range tracks {
		for j := 1; j < len(track.points); j++ {
			addSegmentBoundaries(track, assignments[i], j-1, j, segmentMeta, zoneSize, epsilon)
		}
	}
	if includeEmptySegments {
		addCommonRouteGapBoundaries(tracks, assignments, segmentMeta, zoneSize, epsilon)
	}

	total := 0
	for _, track := range tracks {
		total += len(track.points)
	}
	done := 0
	for i, track := range tracks {
		for j, p := range track.points {
			progress(done, total)
			done++
			g := segmentGlobalDistance(track, assignments[i], j)
			segID := int(math.Floor((g + epsilon) / zoneSize))
			segmentIDs[p.filteredIndex] = segID
			if _, ok := segmentMeta[segID]; !ok {
				segmentMeta[segID] = Point{A: p.x, B: p.y}
			}
		}
	}
}

func alignSegmentTracks(tracks []segmentTrack, zoneSize float64) []segmentTrackAssignment {
	assignments := make([]segmentTrackAssignment, len(tracks))
	seed := 0
	for i := 1; i < len(tracks); i++ {
		if tracks[i].length > tracks[seed].length {
			seed = i
		}
	}
	assignments[seed] = segmentTrackAssignment{assigned: true}
	for {
		changed := false
		for i := range tracks {
			if assignments[i].assigned {
				continue
			}
			best := segmentAlignment{}
			for j := range tracks {
				if !assignments[j].assigned {
					continue
				}
				candidate := alignTrackToKnown(tracks[j], assignments[j], tracks[i])
				if betterSegmentAlignment(candidate, best) {
					best = candidate
				}
			}
			if best.ok {
				assignments[i] = segmentTrackAssignment{assigned: true, reversed: best.reversed, offset: best.offset}
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	for i := range tracks {
		if assignments[i].assigned {
			continue
		}
		if candidate, ok := connectTrackToKnownEndpoint(tracks, assignments, i); ok {
			assignments[i] = candidate
			continue
		}
		maxGlobal := 0.0
		hasGlobal := false
		for j := range tracks {
			if !assignments[j].assigned {
				continue
			}
			for k := range tracks[j].points {
				g := segmentGlobalDistance(tracks[j], assignments[j], k)
				if !hasGlobal || g > maxGlobal {
					maxGlobal = g
					hasGlobal = true
				}
			}
		}
		assignments[i] = segmentTrackAssignment{assigned: true, offset: maxGlobal + zoneSize}
	}
	return assignments
}

func alignTrackToKnown(known segmentTrack, knownAssignment segmentTrackAssignment, unknown segmentTrack) segmentAlignment {
	knownSamples := sampleSegmentTrack(known, knownAssignment)
	unknownSamples := sampleSegmentTrack(unknown, segmentTrackAssignment{})
	if len(knownSamples) == 0 || len(unknownSamples) == 0 {
		return segmentAlignment{}
	}
	bins := map[string][]segmentSample{}
	for _, sample := range knownSamples {
		cx, cy := segmentCell(sample.x, sample.y, commonRoutePointToleranceM)
		key := strconv.Itoa(cx) + ":" + strconv.Itoa(cy)
		bins[key] = append(bins[key], sample)
	}

	best := segmentAlignment{}
	for _, reversed := range []bool{false, true} {
		deltas := []float64{}
		matchedKnown := map[int]struct{}{}
		matchedUnknown := map[int]struct{}{}
		for _, us := range unknownSamples {
			cx, cy := segmentCell(us.x, us.y, commonRoutePointToleranceM)
			for dx := -1; dx <= 1; dx++ {
				for dy := -1; dy <= 1; dy++ {
					key := strconv.Itoa(cx+dx) + ":" + strconv.Itoa(cy+dy)
					for _, ks := range bins[key] {
						if math.Hypot(ks.x-us.x, ks.y-us.y) > commonRoutePointToleranceM {
							continue
						}
						matchedKnown[ks.pointIndex] = struct{}{}
						matchedUnknown[us.pointIndex] = struct{}{}
						unknownCum := us.cum
						if reversed {
							unknownCum = unknown.length - us.cum
						}
						deltas = append(deltas, ks.global-unknownCum)
					}
				}
			}
		}
		// Two distinct points on each track are enough to establish an offset and
		// orientation for a short partial overlap. Counting distinct samples (not
		// every nearby pair) prevents a long stationary burst at one GPS position
		// from masquerading as a real route overlap.
		requiredCorrespondences := minInt(2, minInt(len(knownSamples), len(unknownSamples)))
		if len(matchedKnown) < requiredCorrespondences || len(matchedUnknown) < requiredCorrespondences {
			continue
		}
		offset := medianFloat64(deltas)
		mad := medianAbsoluteDeviation(deltas, offset)
		candidate := segmentAlignment{ok: true, reversed: reversed, offset: offset, count: len(deltas), mad: mad}
		if candidate.mad <= commonRoutePointToleranceM && betterSegmentAlignment(candidate, best) {
			best = candidate
		}
	}
	return best
}

func betterSegmentAlignment(candidate, current segmentAlignment) bool {
	if !candidate.ok {
		return false
	}
	if !current.ok {
		return true
	}
	if candidate.count != current.count {
		return candidate.count > current.count
	}
	return candidate.mad < current.mad
}

func connectTrackToKnownEndpoint(tracks []segmentTrack, assignments []segmentTrackAssignment, unknownIdx int) (segmentTrackAssignment, bool) {
	unknown := tracks[unknownIdx]
	if len(unknown.points) == 0 {
		return segmentTrackAssignment{assigned: true}, true
	}
	type endpointCandidate struct {
		assignment segmentTrackAssignment
		distance   float64
	}
	best := endpointCandidate{distance: math.Inf(1)}
	for knownIdx, known := range tracks {
		if !assignments[knownIdx].assigned || len(known.points) == 0 {
			continue
		}
		knownEnds := []struct {
			point    segmentInputPoint
			global   float64
			routeEnd int
		}{
			{point: known.points[0], global: segmentGlobalDistance(known, assignments[knownIdx], 0)},
			{point: known.points[len(known.points)-1], global: segmentGlobalDistance(known, assignments[knownIdx], len(known.points)-1)},
		}
		if knownEnds[0].global > knownEnds[1].global {
			knownEnds[0].routeEnd, knownEnds[1].routeEnd = 1, -1
		} else {
			knownEnds[0].routeEnd, knownEnds[1].routeEnd = -1, 1
		}
		for _, reversed := range []bool{false, true} {
			unknownEnds := []struct {
				point segmentInputPoint
				cum   float64
			}{
				{point: unknown.points[0], cum: orientedSegmentCum(unknown, 0, reversed)},
				{point: unknown.points[len(unknown.points)-1], cum: orientedSegmentCum(unknown, len(unknown.points)-1, reversed)},
			}
			for _, ke := range knownEnds {
				for _, ue := range unknownEnds {
					// A track attached after the known route's maximum global
					// distance must start at the joining endpoint. Conversely, a
					// track attached before the minimum must end there. Without
					// this constraint both orientations have the same endpoint
					// distance and the loop order incorrectly favors reversed=false,
					// which can fold a reverse-recorded continuation back over the
					// known segment IDs.
					if ke.routeEnd > 0 && math.Abs(ue.cum) > segmentEndpointCumEpsilon {
						continue
					}
					if ke.routeEnd < 0 && math.Abs(ue.cum-unknown.length) > segmentEndpointCumEpsilon {
						continue
					}
					dist := math.Hypot(ke.point.x-ue.point.x, ke.point.y-ue.point.y)
					if dist >= best.distance {
						continue
					}
					offset := ke.global - ue.cum
					if ke.routeEnd > 0 {
						offset += dist
					} else {
						offset -= dist
					}
					best = endpointCandidate{
						assignment: segmentTrackAssignment{assigned: true, reversed: reversed, offset: offset},
						distance:   dist,
					}
				}
			}
		}
	}
	if math.IsInf(best.distance, 1) {
		return segmentTrackAssignment{}, false
	}
	return best.assignment, true
}

func sampleSegmentTrack(track segmentTrack, assignment segmentTrackAssignment) []segmentSample {
	n := len(track.points)
	if n == 0 {
		return nil
	}
	targetCount := minInt(n, commonRouteMaxSamples)
	out := make([]segmentSample, 0, targetCount)
	appendIndex := func(index int) {
		p := track.points[index]
		candidate := segmentSample{
			pointIndex: index,
			x:          p.x,
			y:          p.y,
			cum:        track.cum[index],
			global:     segmentGlobalDistance(track, assignment, index),
		}
		if len(out) > 0 {
			previous := out[len(out)-1]
			if previous.pointIndex == candidate.pointIndex ||
				(math.Abs(previous.cum-candidate.cum) <= segmentEndpointCumEpsilon &&
					math.Hypot(previous.x-candidate.x, previous.y-candidate.y) <= segmentEndpointCumEpsilon) {
				return
			}
		}
		out = append(out, candidate)
	}

	if targetCount == 1 || track.length <= segmentEndpointCumEpsilon {
		appendIndex(0)
		return out
	}
	// Sample uniformly by travelled distance, not by input row number. Different
	// meters often emit radically different numbers of repeated stationary GPS
	// rows; index-based sampling let those bursts dominate alignment scoring.
	for sampleIndex := 0; sampleIndex < targetCount; sampleIndex++ {
		target := track.length * float64(sampleIndex) / float64(targetCount-1)
		index := sort.Search(n, func(i int) bool { return track.cum[i] >= target })
		if index >= n {
			index = n - 1
		}
		if index > 0 && math.Abs(track.cum[index-1]-target) < math.Abs(track.cum[index]-target) {
			index--
		}
		appendIndex(index)
	}
	appendIndex(n - 1)
	return out
}

func segmentGlobalDistance(track segmentTrack, assignment segmentTrackAssignment, pointIndex int) float64 {
	return orientedSegmentCum(track, pointIndex, assignment.reversed) + assignment.offset
}

func orientedSegmentCum(track segmentTrack, pointIndex int, reversed bool) float64 {
	if !reversed {
		return track.cum[pointIndex]
	}
	return track.length - track.cum[pointIndex]
}

func addSegmentBoundaries(track segmentTrack, assignment segmentTrackAssignment, fromIdx, toIdx int, segmentMeta map[int]Point, zoneSize, epsilon float64) {
	g0 := segmentGlobalDistance(track, assignment, fromIdx)
	g1 := segmentGlobalDistance(track, assignment, toIdx)
	p0 := track.points[fromIdx]
	p1 := track.points[toIdx]
	addGlobalSegmentBoundaries(
		segmentGlobalPoint{x: p0.x, y: p0.y, global: g0},
		segmentGlobalPoint{x: p1.x, y: p1.y, global: g1},
		segmentMeta,
		zoneSize,
		epsilon,
	)
}

func addCommonRouteGapBoundaries(tracks []segmentTrack, assignments []segmentTrackAssignment, segmentMeta map[int]Point, zoneSize, epsilon float64) {
	points := make([]segmentGlobalPoint, 0)
	for i, track := range tracks {
		for j, p := range track.points {
			points = append(points, segmentGlobalPoint{
				x:      p.x,
				y:      p.y,
				global: segmentGlobalDistance(track, assignments[i], j),
			})
		}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].global != points[j].global {
			return points[i].global < points[j].global
		}
		if points[i].x != points[j].x {
			return points[i].x < points[j].x
		}
		return points[i].y < points[j].y
	})
	for i := 1; i < len(points); i++ {
		addGlobalSegmentBoundaries(points[i-1], points[i], segmentMeta, zoneSize, epsilon)
	}
}

func addGlobalSegmentBoundaries(from, to segmentGlobalPoint, segmentMeta map[int]Point, zoneSize, epsilon float64) {
	g0 := from.global
	g1 := to.global
	if g0 == g1 {
		return
	}
	minG, maxG := g0, g1
	if minG > maxG {
		minG, maxG = maxG, minG
	}
	firstSegment := int(math.Floor((minG + epsilon) / zoneSize))
	lastSegment := int(math.Floor((maxG + epsilon) / zoneSize))
	for id := firstSegment + 1; id <= lastSegment; id++ {
		if _, ok := segmentMeta[id]; ok {
			continue
		}
		boundary := float64(id) * zoneSize
		fraction := (boundary - g0) / (g1 - g0)
		if fraction < 0 {
			fraction = 0
		} else if fraction > 1 {
			fraction = 1
		}
		segmentMeta[id] = Point{
			A: from.x + (to.x-from.x)*fraction,
			B: from.y + (to.y-from.y)*fraction,
		}
	}
}

func segmentCell(x, y, size float64) (int, int) {
	return int(math.Floor(x / size)), int(math.Floor(y / size))
}

func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func medianAbsoluteDeviation(values []float64, center float64) float64 {
	if len(values) == 0 {
		return 0
	}
	dev := make([]float64, len(values))
	for i, v := range values {
		dev[i] = math.Abs(v - center)
	}
	return medianFloat64(dev)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	nAgg := len(ds.Rows)
	for i, r := range ds.Rows {
		maybeEmitProgressInRange(ctx, "zone_stats", i, nAgg, 0, 22)
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

	emitProcessingProgress(ctx, "zone_stats", 28)
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
	type chosenSelection struct {
		Fallback     zoneFreqStat
		Selected     zoneFreqStat
		HasQualified bool
	}
	meetsThresholds := func(z zoneFreqStat) bool {
		return z.RSRPAvg >= cfg.RSRPThreshold && z.HasSINR && z.SINRAvg >= cfg.SINRThreshold
	}
	chosen := map[chosenKey]chosenSelection{}
	chosenOrder := make([]chosenKey, 0, len(zoneFreqStats))
	nZf := len(zoneFreqStats)
	for zi, z := range zoneFreqStats {
		maybeEmitProgressInRange(ctx, "zone_stats", zi, nZf, 24, 38)
		key := chosenKey{
			ZonaKey:     z.Key.ZonaKey,
			OperatorKey: z.Key.OperatorKey,
			ZonaX:       z.Key.ZonaX,
			ZonaY:       z.Key.ZonaY,
			MCC:         z.Key.MCC,
			MNC:         z.Key.MNC,
		}
		selection, exists := chosen[key]
		if !exists {
			selection = chosenSelection{
				Fallback: z,
				Selected: z,
			}
			chosenOrder = append(chosenOrder, key)
		}
		if !selection.HasQualified && meetsThresholds(z) {
			selection.Selected = z
			selection.HasQualified = true
		}
		chosen[key] = selection
	}

	points := make([]Point, 0, len(chosenOrder))
	zoneCenters := make([]Point, 0, len(chosenOrder))
	nChosen := len(chosenOrder)
	for pi, key := range chosenOrder {
		maybeEmitProgressInRange(ctx, "zone_stats", pi, nChosen, 38, 52)
		z := chosen[key].Selected
		var center Point
		if cfg.ZoneMode == "segments" {
			center = Point{A: z.Key.ZonaX, B: z.Key.ZonaY}
		} else {
			center = Point{A: z.Key.ZonaX + cfg.ZoneSizeM/2, B: z.Key.ZonaY + cfg.ZoneSizeM/2}
		}
		zoneCenters = append(zoneCenters, center)
		points = append(points, center)
	}
	emitProcessingProgress(ctx, "zone_stats", 55)
	lonLat, err := transformer.Inverse(ctx, points)
	if err != nil {
		return nil, err
	}
	emitProcessingProgress(ctx, "zone_stats", 72)

	stats := make([]ZoneStat, 0, len(chosenOrder))
	for i, key := range chosenOrder {
		maybeEmitProgressInRange(ctx, "zone_stats", i, nChosen, 72, 99)
		z := chosen[key].Selected
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
	emitProcessingProgress(ctx, "zone_stats", 100)
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
