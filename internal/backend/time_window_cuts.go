package backend

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type timeWindowCutSummary struct {
	RemovedMeasurements int
	RemovedZones        int
}

type timedSegmentSample struct {
	timeMS   int64
	distance float64
	zoneID   int
	rowOrder int
}

// applyTimeWindowsAfterSegmentation applies explicit user exclusions only after
// the complete route and its synthetic empty segments have been constructed.
// In segment mode, a time interval is projected onto the route distance and all
// touched segment IDs are removed. This prevents empty-segment generation from
// recreating a tunnel (or another intentionally excluded passage) afterwards.
func applyTimeWindowsAfterSegmentation(
	ds *ProcessedDataset,
	windows []TimeWindow,
	cfg ProcessingConfig,
) (*ProcessedDataset, timeWindowCutSummary, error) {
	if ds == nil {
		return nil, timeWindowCutSummary{}, fmt.Errorf("nil ProcessedDataset")
	}
	intervals, err := parseConfiguredTimeWindows(windows)
	if err != nil {
		return nil, timeWindowCutSummary{}, err
	}
	if len(intervals) == 0 {
		return cloneProcessedDataset(ds), timeWindowCutSummary{}, nil
	}

	out := cloneProcessedDataset(ds)
	if cfg.ZoneMode != "segments" {
		rows := make([]ProcessedRow, 0, len(out.Rows))
		removed := 0
		beforeZoneKeys := map[string]struct{}{}
		afterZoneKeys := map[string]struct{}{}
		for _, row := range out.Rows {
			beforeZoneKeys[row.ZonaKey] = struct{}{}
			if row.HasTimestamp && timeInAnyWindow(row.TimestampMS, intervals) {
				removed++
				continue
			}
			rows = append(rows, row)
			afterZoneKeys[row.ZonaKey] = struct{}{}
		}
		out.Rows = rows
		removedZones := 0
		for zoneKey := range beforeZoneKeys {
			if _, remains := afterZoneKeys[zoneKey]; !remains {
				removedZones++
			}
		}
		return out, timeWindowCutSummary{
			RemovedMeasurements: removed,
			RemovedZones:        removedZones,
		}, nil
	}

	zoneSize := cfg.ZoneSizeM
	if zoneSize <= 0 {
		zoneSize = 100
	}
	blockedIDs := blockedSegmentIDsForTimeWindows(out.Rows, intervals, zoneSize)
	if len(blockedIDs) == 0 {
		return out, timeWindowCutSummary{}, nil
	}
	effectiveBlockedIDs := make(map[int]struct{}, len(blockedIDs))
	for id := range blockedIDs {
		if _, exists := out.SegmentMeta[id]; exists {
			effectiveBlockedIDs[id] = struct{}{}
		}
	}

	rows := make([]ProcessedRow, 0, len(out.Rows))
	removed := 0
	for _, row := range out.Rows {
		id, ok := segmentIDFromZoneKey(row.ZonaKey)
		if ok {
			if _, blocked := effectiveBlockedIDs[id]; blocked {
				removed++
				continue
			}
		}
		rows = append(rows, row)
	}
	out.Rows = rows
	for id := range effectiveBlockedIDs {
		delete(out.SegmentMeta, id)
	}

	return out, timeWindowCutSummary{
		RemovedMeasurements: removed,
		RemovedZones:        len(effectiveBlockedIDs),
	}, nil
}

func blockedSegmentIDsForTimeWindows(rows []ProcessedRow, intervals []parsedTimeWindow, zoneSize float64) map[int]struct{} {
	blocked := map[int]struct{}{}
	tracks := map[string][]timedSegmentSample{}
	trackOrder := []string{}
	seenTrack := map[string]bool{}

	for i, row := range rows {
		if !row.HasTimestamp {
			continue
		}
		id, ok := segmentIDFromZoneKey(row.ZonaKey)
		if !ok {
			continue
		}
		trackID := row.SourceTrackID
		if trackID == "" {
			trackID = "0"
		}
		if !seenTrack[trackID] {
			seenTrack[trackID] = true
			trackOrder = append(trackOrder, trackID)
		}
		tracks[trackID] = append(tracks[trackID], timedSegmentSample{
			timeMS:   row.TimestampMS,
			distance: row.SegmentDistanceM,
			zoneID:   id,
			rowOrder: i,
		})
	}

	for _, trackID := range trackOrder {
		samples := tracks[trackID]
		sort.SliceStable(samples, func(i, j int) bool {
			if samples[i].timeMS != samples[j].timeMS {
				return samples[i].timeMS < samples[j].timeMS
			}
			return samples[i].rowOrder < samples[j].rowOrder
		})
		for _, interval := range intervals {
			blockTrackInterval(samples, interval, zoneSize, blocked)
		}
	}
	return blocked
}

func blockTrackInterval(samples []timedSegmentSample, interval parsedTimeWindow, zoneSize float64, blocked map[int]struct{}) {
	if len(samples) == 0 || interval.endMS < samples[0].timeMS || interval.startMS > samples[len(samples)-1].timeMS {
		return
	}

	startMS := interval.startMS
	if startMS < samples[0].timeMS {
		startMS = samples[0].timeMS
	}
	endMS := interval.endMS
	if endMS > samples[len(samples)-1].timeMS {
		endMS = samples[len(samples)-1].timeMS
	}

	startDistance := interpolateSegmentDistanceAtTime(samples, startMS)
	endDistance := interpolateSegmentDistanceAtTime(samples, endMS)
	minDistance := math.Min(startDistance, endDistance)
	maxDistance := math.Max(startDistance, endDistance)

	for _, sample := range samples {
		if sample.timeMS < startMS || sample.timeMS > endMS {
			continue
		}
		if sample.distance < minDistance {
			minDistance = sample.distance
		}
		if sample.distance > maxDistance {
			maxDistance = sample.distance
		}
		blocked[sample.zoneID] = struct{}{}
	}

	firstID := int(math.Floor((minDistance + segmentEndpointCumEpsilon) / zoneSize))
	lastID := int(math.Floor((maxDistance + segmentEndpointCumEpsilon) / zoneSize))
	if firstID > lastID {
		firstID, lastID = lastID, firstID
	}
	for id := firstID; id <= lastID; id++ {
		blocked[id] = struct{}{}
	}
}

func interpolateSegmentDistanceAtTime(samples []timedSegmentSample, targetMS int64) float64 {
	if len(samples) == 0 {
		return 0
	}
	if targetMS <= samples[0].timeMS {
		return samples[0].distance
	}
	last := samples[len(samples)-1]
	if targetMS >= last.timeMS {
		return last.distance
	}

	right := sort.Search(len(samples), func(i int) bool { return samples[i].timeMS >= targetMS })
	if right <= 0 {
		return samples[0].distance
	}
	if right >= len(samples) {
		return last.distance
	}
	if samples[right].timeMS == targetMS {
		return samples[right].distance
	}
	left := samples[right-1]
	rightSample := samples[right]
	span := rightSample.timeMS - left.timeMS
	if span <= 0 {
		return left.distance
	}
	ratio := float64(targetMS-left.timeMS) / float64(span)
	return left.distance + ratio*(rightSample.distance-left.distance)
}

func segmentIDFromZoneKey(zoneKey string) (int, bool) {
	if !strings.HasPrefix(zoneKey, "segment_") {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(zoneKey, "segment_"))
	return id, err == nil
}

func cloneProcessedDataset(ds *ProcessedDataset) *ProcessedDataset {
	if ds == nil {
		return nil
	}
	out := &ProcessedDataset{
		Rows:        make([]ProcessedRow, len(ds.Rows)),
		Columns:     append([]string(nil), ds.Columns...),
		FileInfo:    ds.FileInfo,
		SegmentMeta: make(map[int]Point, len(ds.SegmentMeta)),
	}
	for i, row := range ds.Rows {
		out.Rows[i] = row
		out.Rows[i].Raw = append([]string(nil), row.Raw...)
	}
	for id, point := range ds.SegmentMeta {
		out.SegmentMeta[id] = point
	}
	return out
}
