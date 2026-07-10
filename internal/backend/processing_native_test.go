package backend

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestProcessDataNative_segmentModeKeys(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows: [][]string{
			{"48.1486", "17.1077", "3500", "1", "231", "01", "-100"},
			{"48.1490", "17.1080", "3500", "1", "231", "01", "-101"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100_000
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Rows) != 2 {
		t.Fatalf("rows: %d", len(ds.Rows))
	}
	if !strings.HasPrefix(ds.Rows[0].ZonaKey, "segment_") {
		t.Fatalf("expected segment key, got %q", ds.Rows[0].ZonaKey)
	}
}

func TestProcessDataNative_segmentsUseCSVSourceTracks(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", csvSourceIndexColumn},
		Rows: [][]string{
			{"48.0000", "17.0000", "3500", "1", "231", "01", "-100", "0"},
			{"48.0000", "17.0200", "3500", "2", "231", "01", "-100", "1"},
			{"48.0000", "17.0020", "3500", "3", "231", "01", "-100", "0"},
			{"48.0000", "17.0220", "3500", "4", "231", "01", "-100", "1"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "segments"
	cfg.ZoneSizeM = 100
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}

	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Rows) != 4 {
		t.Fatalf("rows: %d", len(ds.Rows))
	}
	for _, row := range ds.Rows {
		if strings.HasPrefix(row.ZonaKey, "segment_10") {
			t.Fatalf("source track jump was treated as route distance, got %q for PCI %s", row.ZonaKey, row.PCI)
		}
	}
	if ds.Rows[0].ZonaKey == ds.Rows[1].ZonaKey {
		t.Fatalf("different source tracks must not share the first segment key: %q", ds.Rows[0].ZonaKey)
	}
}

func TestBuildSegmentAssignments_commonRouteSharesOverlappingSegments(t *testing.T) {
	t.Parallel()

	filtered := []rawParsed{
		{row: []string{"0"}}, // track 0: 0 m
		{row: []string{"1"}}, // track 1: 100 m, slightly parallel
		{row: []string{"0"}}, // track 0: 100 m
		{row: []string{"1"}}, // track 1: 200 m
		{row: []string{"0"}}, // track 0: 200 m
		{row: []string{"1"}}, // track 1: 300 m
		{row: []string{"1"}}, // track 1: 400 m
	}
	xy := []Point{
		{A: 0, B: 0},
		{A: 100, B: 8},
		{A: 100, B: 0},
		{A: 200, B: 8},
		{A: 200, B: 0},
		{A: 300, B: 8},
		{A: 400, B: 8},
	}

	segmentIDs, _ := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, false, func(i, n int) {})

	if segmentIDs[2] != segmentIDs[1] {
		t.Fatalf("overlapping 100 m points should share one common segment, got track0=%d track1=%d", segmentIDs[2], segmentIDs[1])
	}
	if segmentIDs[4] != segmentIDs[3] {
		t.Fatalf("overlapping 200 m points should share one common segment, got track0=%d track1=%d", segmentIDs[4], segmentIDs[3])
	}
	if segmentIDs[6] <= segmentIDs[4] {
		t.Fatalf("continuing track should extend common route, got overlap=%d continuation=%d", segmentIDs[4], segmentIDs[6])
	}
}

func TestBuildSegmentAssignments_commonRouteHandlesOppositeDirectionTrack(t *testing.T) {
	t.Parallel()

	filtered := []rawParsed{
		{row: []string{"0"}},
		{row: []string{"0"}},
		{row: []string{"0"}},
		{row: []string{"1"}},
		{row: []string{"1"}},
		{row: []string{"1"}},
	}
	xy := []Point{
		{A: 0, B: 0},
		{A: 100, B: 0},
		{A: 200, B: 0},
		{A: 200, B: 6},
		{A: 100, B: 6},
		{A: 0, B: 6},
	}

	segmentIDs, _ := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, false, func(i, n int) {})

	if segmentIDs[0] != segmentIDs[5] || segmentIDs[1] != segmentIDs[4] || segmentIDs[2] != segmentIDs[3] {
		t.Fatalf("opposite direction track should align to the same common segments, got %v", segmentIDs)
	}
}

func TestBuildSegmentAssignments_endpointGapContinuesRouteAndEmptySegmentsFillIt(t *testing.T) {
	t.Parallel()

	filtered := []rawParsed{
		{row: []string{"0"}},
		{row: []string{"0"}},
		{row: []string{"1"}},
		{row: []string{"1"}},
	}
	xy := []Point{
		{A: 0, B: 0},
		{A: 100, B: 0},
		{A: 600, B: 0},
		{A: 700, B: 0},
	}

	withoutEmpty, withoutMeta := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, false, func(i, n int) {})
	withEmpty, meta := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, true, func(i, n int) {})

	if withoutEmpty[2]-withoutEmpty[1] != 5 {
		t.Fatalf("endpoint gap should preserve route distance even without empty segment export, got %v", withoutEmpty)
	}
	if withEmpty[2]-withEmpty[1] != 5 {
		t.Fatalf("expected large endpoint gap to preserve empty 100 m intervals, got %v", withEmpty)
	}
	if _, ok := withoutMeta[withoutEmpty[1]+1]; ok {
		t.Fatalf("empty segment meta should not be generated when empty segments are disabled; ids=%v", withoutEmpty)
	}
	for id := withEmpty[1] + 1; id <= withEmpty[2]; id++ {
		if _, ok := meta[id]; !ok {
			t.Fatalf("missing interpolated segment meta for empty segment %d; ids=%v", id, withEmpty)
		}
	}
}

func TestBuildSegmentAssignments_disjointContinuationRespectsRecordedDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		continuation []Point
		want         []int
	}{
		{
			name:         "forward",
			continuation: []Point{{A: 200, B: 0}, {A: 300, B: 0}},
			want:         []int{0, 1, 2, 3},
		},
		{
			name:         "recorded_reverse",
			continuation: []Point{{A: 300, B: 0}, {A: 200, B: 0}},
			want:         []int{0, 1, 3, 2},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filtered := []rawParsed{
				{row: []string{"0"}},
				{row: []string{"0"}},
				{row: []string{"1"}},
				{row: []string{"1"}},
			}
			xy := []Point{{A: 0, B: 0}, {A: 100, B: 0}, tc.continuation[0], tc.continuation[1]}

			got, _ := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, false, func(i, n int) {})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("disjoint continuation folded into known segments: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBuildSegmentAssignments_disjointContinuationIsInputOrderInvariant(t *testing.T) {
	t.Parallel()

	type routePoint struct {
		source string
		point  Point
	}
	physical := []routePoint{
		{source: "known", point: Point{A: 0, B: 0}},
		{source: "known", point: Point{A: 100, B: 0}},
		{source: "continuation", point: Point{A: 300, B: 0}},
		{source: "continuation", point: Point{A: 200, B: 0}},
	}
	orders := [][]int{{0, 1, 2, 3}, {2, 3, 0, 1}}
	var baseline map[float64]map[float64]int
	for orderIdx, order := range orders {
		filtered := make([]rawParsed, 0, len(order))
		xy := make([]Point, 0, len(order))
		for _, idx := range order {
			filtered = append(filtered, rawParsed{row: []string{physical[idx].source}})
			xy = append(xy, physical[idx].point)
		}
		ids, _ := buildSegmentAssignments(filtered, xy, 0, 100, 1e-9, false, func(i, n int) {})
		byX := make(map[float64]int, len(order))
		for i, point := range xy {
			byX[point.A] = ids[i]
		}

		// Segment numbering may be globally reflected when the seed track is
		// recorded in the opposite direction. Pairwise segment distances are
		// the input-order-independent route invariant.
		distances := map[float64]map[float64]int{}
		for _, from := range []float64{0, 100, 200, 300} {
			distances[from] = map[float64]int{}
			for _, to := range []float64{0, 100, 200, 300} {
				distances[from][to] = absInt(byX[from] - byX[to])
			}
		}
		if orderIdx == 0 {
			baseline = distances
			continue
		}
		for from, expected := range baseline {
			for to, want := range expected {
				if got := distances[from][to]; got != want {
					t.Fatalf("input order changed route distance %v→%v: got %d want %d (ids=%v)", from, to, got, want, byX)
				}
			}
		}
	}
}

func TestSampleSegmentTrackUsesDistanceInsteadOfStationaryRowCount(t *testing.T) {
	t.Parallel()

	filtered := make([]rawParsed, 0, 102)
	xy := make([]Point, 0, 102)
	for i := 0; i < 100; i++ {
		filtered = append(filtered, rawParsed{row: []string{"0"}})
		xy = append(xy, Point{A: 0, B: 0})
	}
	filtered = append(filtered, rawParsed{row: []string{"0"}}, rawParsed{row: []string{"0"}})
	xy = append(xy, Point{A: 100, B: 0}, Point{A: 200, B: 0})

	track := buildSegmentTracks(filtered, xy, 0)[0]
	samples := sampleSegmentTrack(track, segmentTrackAssignment{})
	if len(samples) != 3 {
		t.Fatalf("stationary burst dominated route samples: got %d samples, want 3", len(samples))
	}
	if samples[0].x != 0 || samples[1].x != 100 || samples[2].x != 200 {
		t.Fatalf("unexpected distance samples: %#v", samples)
	}
}

func TestAlignTrackToKnownRequiresDistinctSpatialCorrespondences(t *testing.T) {
	t.Parallel()

	filtered := make([]rawParsed, 0, 42)
	xy := make([]Point, 0, 42)
	for i := 0; i < 20; i++ {
		filtered = append(filtered, rawParsed{row: []string{"known"}})
		xy = append(xy, Point{A: 0, B: 0})
	}
	filtered = append(filtered, rawParsed{row: []string{"known"}})
	xy = append(xy, Point{A: 100, B: 0})
	for i := 0; i < 20; i++ {
		filtered = append(filtered, rawParsed{row: []string{"unknown"}})
		xy = append(xy, Point{A: 0, B: 0})
	}
	filtered = append(filtered, rawParsed{row: []string{"unknown"}})
	xy = append(xy, Point{A: 0, B: 100})

	tracks := buildSegmentTracks(filtered, xy, 0)
	if got := alignTrackToKnown(tracks[0], segmentTrackAssignment{assigned: true}, tracks[1]); got.ok {
		t.Fatalf("one stationary GPS position must not establish a route overlap: %+v", got)
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func TestProcessDataNative_invalidLatitudeError(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns:  []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows:     [][]string{{"nope", "17.1", "3500", "1", "231", "01", "-100"}},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	_, err = ProcessDataNative(context.Background(), data, cfg, tr)
	if err == nil || !strings.Contains(err.Error(), "latitude") {
		t.Fatalf("expected latitude error, got %v", err)
	}
}

func TestProcessDataNative_skipsRowsWithEmptyCoordinates(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	data := &CSVData{
		Columns: []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"},
		Rows: [][]string{
			{"", "", "3500", "1", "231", "01", "-100"},
			{"48.1486", "17.1077", "3500", "2", "231", "01", "-101"},
		},
		FileInfo: CSVFileInfo{HeaderLine: 0},
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	ds, err := ProcessDataNative(context.Background(), data, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Rows) != 1 {
		t.Fatalf("expected 1 processed row after dropping empty coordinates, got %d", len(ds.Rows))
	}
	if ds.Rows[0].PCI != "2" {
		t.Fatalf("expected surviving row to be PCI 2, got %q", ds.Rows[0].PCI)
	}
}

func TestCalculateZoneStatsNative_picksHigherRSRPFrequency(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	// Same zone cell (center grid), same operator and PCI, two frequencies — better RSRP wins.
	zk := "1000_2000"
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3600", RSRP: -120, ZonaX: 1000, ZonaY: 2000},
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500", RSRP: -80, ZonaX: 1000, ZonaY: 2000},
		},
		Columns: []string{"x"},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.RSRPThreshold = -110
	stats, err := CalculateZoneStatsNative(context.Background(), ds, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].NajcastejsiaFrekvencia != "3500" {
		t.Fatalf("expected freq 3500 (better RSRP), got %q avg=%v", stats[0].NajcastejsiaFrekvencia, stats[0].RSRPAvg)
	}
}

func TestCalculateZoneStatsNative_prefersFirstCandidateMeetingRSRPAndSINRThresholds(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}

	zk := "1000_2000"
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500", RSRP: -80, SINR: -10, HasSINR: true, ZonaX: 1000, ZonaY: 2000},
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "20", Frequency: "3600", RSRP: -90, SINR: 5, HasSINR: true, ZonaX: 1000, ZonaY: 2000},
		},
		Columns: []string{"x"},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.RSRPThreshold = -100
	cfg.SINRThreshold = 0

	stats, err := CalculateZoneStatsNative(context.Background(), ds, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].NajcastejsiaFrekvencia != "3600" || stats[0].PCI != "20" {
		t.Fatalf("expected threshold-qualified candidate 3600/20, got %q/%q", stats[0].NajcastejsiaFrekvencia, stats[0].PCI)
	}
	if stats[0].RSRPKategoria != "RSRP_GOOD" {
		t.Fatalf("expected selected candidate to be GOOD, got %q", stats[0].RSRPKategoria)
	}
}

func TestCalculateZoneStatsNative_fallsBackToHighestRSRPWhenNoCandidateMeetsBothThresholds(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}

	zk := "1000_2000"
	ds := &ProcessedDataset{
		Rows: []ProcessedRow{
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "10", Frequency: "3500", RSRP: -80, SINR: -10, HasSINR: true, ZonaX: 1000, ZonaY: 2000},
			{ZonaKey: zk, OperatorKey: "231_01", MCC: "231", MNC: "01", PCI: "20", Frequency: "3600", RSRP: -90, SINR: -20, HasSINR: true, ZonaX: 1000, ZonaY: 2000},
		},
		Columns: []string{"x"},
	}
	cfg := DefaultProcessingConfig()
	cfg.ZoneMode = "center"
	cfg.ZoneSizeM = 100
	cfg.RSRPThreshold = -100
	cfg.SINRThreshold = 0

	stats, err := CalculateZoneStatsNative(context.Background(), ds, cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].NajcastejsiaFrekvencia != "3500" || stats[0].PCI != "10" {
		t.Fatalf("expected fallback highest-RSRP candidate 3500/10, got %q/%q", stats[0].NajcastejsiaFrekvencia, stats[0].PCI)
	}
}

func TestProcessDataNative_nilData(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultProcessingConfig()
	cfg.ColumnMapping = map[string]int{
		"latitude": 0, "longitude": 1, "frequency": 2, "pci": 3, "mcc": 4, "mnc": 5, "rsrp": 6,
	}
	_, err = ProcessDataNative(context.Background(), nil, cfg, tr)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCalculateZoneStatsNative_nilDataset(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	_, err = CalculateZoneStatsNative(context.Background(), nil, DefaultProcessingConfig(), tr)
	if err == nil {
		t.Fatal("expected error")
	}
}
