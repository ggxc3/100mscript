package backend

import (
	"context"
	"math"
	"testing"
)

// Known point near Bratislava (WGS84) — forward/inverse round-trip should stay sub-metre.
func TestPyProjTransformer_forwardInverseRoundTrip(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lon, lat := 17.1077, 48.1486
	xy, err := tr.Forward(ctx, []Point{{A: lon, B: lat}})
	if err != nil {
		t.Fatal(err)
	}
	if len(xy) != 1 {
		t.Fatal(len(xy))
	}
	back, err := tr.Inverse(ctx, xy)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Fatal(len(back))
	}
	dLon := math.Abs(back[0].A - lon)
	dLat := math.Abs(back[0].B - lat)
	if dLon > 1e-5 || dLat > 1e-5 {
		t.Fatalf("round-trip drift too large: lonΔ=%g latΔ=%g", dLon, dLat)
	}
}

func TestPyProjTransformer_forwardRejectsNaN(t *testing.T) {
	t.Parallel()

	tr, err := NewPyProjTransformer()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Forward(context.Background(), []Point{{A: math.NaN(), B: 0}})
	if err == nil {
		t.Fatal("expected error for NaN")
	}
}

func TestCtxErr_nilContextOk(t *testing.T) {
	t.Parallel()

	if err := ctxErr(nil); err != nil {
		t.Fatal(err)
	}
}
