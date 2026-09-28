package backend

import (
	"math"
	"strconv"
	"testing"
)

func sampledTestTracks(paths ...[]Point) ([]rawParsed, []Point, []segmentTrack) {
	var rows []rawParsed
	var xy []Point
	for source, path := range paths {
		for _, p := range path {
			rows = append(rows, rawParsed{row: []string{strconv.Itoa(source)}})
			xy = append(xy, p)
		}
	}
	return rows, xy, buildSegmentTracks(rows, xy, 0)
}

func testLine(a, b Point) []Point {
	var out []Point
	for i := 0; i <= 50; i++ {
		f := float64(i) / 50
		out = append(out, Point{A: a.A + (b.A-a.A)*f, B: a.B + (b.B-a.B)*f})
	}
	return out
}

func TestSegmentAlignment_TurningContinuationDoesNotFoldBack(t *testing.T) {
	// Dense GPS near the junction used to support both orientations. Picking
	// one by MAD alone folded a perpendicular continuation onto the first leg.
	rows, xy, _ := sampledTestTracks(testLine(Point{0, 0}, Point{1000, 0}), testLine(Point{1000, 0}, Point{1000, 1000}))
	ids, _, dist := buildSegmentAssignments(rows, xy, 0, 100, 1e-9, false, func(int, int) {})
	for i := range xy {
		for j := 0; j < i; j++ {
			if ids[i] == ids[j] && math.Hypot(xy[i].A-xy[j].A, xy[i].B-xy[j].B) > 250 {
				t.Fatalf("distant points share segment %d: %v vs %v", ids[i], xy[i], xy[j])
			}
		}
	}
	if span := maxSlice(dist) - minSlice(dist); span < 1900 {
		t.Fatalf("two 1 km legs collapsed to %.1f m", span)
	}
}

func TestSegmentAlignment_ClosedLoopKeepsFourLegs(t *testing.T) {
	paths := [][]Point{testLine(Point{0, 0}, Point{1200, 0}), testLine(Point{1200, 0}, Point{1200, 1000}), testLine(Point{1200, 1000}, Point{0, 1000}), testLine(Point{0, 1000}, Point{0, 0})}
	// All 24 file orders, also with an independently reverse-recorded leg.
	for a := 0; a < 4; a++ {
		for b := 0; b < 4; b++ {
			for c := 0; c < 4; c++ {
				for d := 0; d < 4; d++ {
					if a == b || a == c || a == d || b == c || b == d || c == d {
						continue
					}
					for _, reverse := range []bool{false, true} {
						chosen := [][]Point{paths[a], paths[b], paths[c], paths[d]}
						if reverse {
							r := append([]Point(nil), chosen[1]...)
							for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
								r[i], r[j] = r[j], r[i]
							}
							chosen[1] = r
						}
						rows, xy, _ := sampledTestTracks(chosen...)
						ids, _, dist := buildSegmentAssignments(rows, xy, 0, 100, 1e-9, false, func(int, int) {})
						for i := range xy {
							for j := 0; j < i; j++ {
								if ids[i] == ids[j] && math.Hypot(xy[i].A-xy[j].A, xy[i].B-xy[j].B) > 250 {
									t.Fatalf("order %d%d%d%d reverse=%v: remote points share segment %d: %v vs %v", a, b, c, d, reverse, ids[i], xy[i], xy[j])
								}
							}
						}
						if span := maxSlice(dist) - minSlice(dist); span < 4200 {
							t.Fatalf("four-leg loop collapsed to %.1f m", span)
						}
					}
				}
			}
		}
	}
}

func minSlice(values []float64) float64 {
	m := math.Inf(1)
	for _, v := range values {
		m = math.Min(m, v)
	}
	return m
}
func maxSlice(values []float64) float64 {
	m := math.Inf(-1)
	for _, v := range values {
		m = math.Max(m, v)
	}
	return m
}

func TestSegmentAlignment_SharedJunctionIsNotSharedRoute(t *testing.T) {
	known := testLine(Point{0, 0}, Point{1000, 0})
	// The first 100 m are shared, then the routes diverge for 900 m.
	unknown := append([]Point(nil), known[:6]...)
	unknown = append(unknown, testLine(Point{100, 0}, Point{100, 900})...)
	_, _, tracks := sampledTestTracks(known, unknown)
	if got := alignTrackToKnown(tracks[0], segmentTrackAssignment{assigned: true}, tracks[1]); got.ok {
		t.Fatalf("junction overlap falsely aligns entire divergent routes: %+v", got)
	}
}

func TestSegmentAlignment_ShortDivergentTailIsNotHiddenByLongOverlap(t *testing.T) {
	known := testLine(Point{0, 0}, Point{100000, 0})
	unknown := append(testLine(Point{0, 0}, Point{98000, 0}), testLine(Point{98000, 0}, Point{98000, 2000})...)
	_, _, tracks := sampledTestTracks(known, unknown)
	if segmentOverlapFitsGeometry(tracks[0], segmentTrackAssignment{}, tracks[1], segmentTrackAssignment{}) {
		t.Fatal("2 km divergent tail hidden by 98% shared route")
	}
}
