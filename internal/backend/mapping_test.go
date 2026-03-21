package backend

import (
	"testing"
)

func TestNormalizeHeaderToken_stripsNonAlnum(t *testing.T) {
	t.Parallel()

	if got := normalizeHeaderToken("  EARFCN (DL) "); got != "earfcndl" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildColumnMappingFromHeaders_prefersKnownNames(t *testing.T) {
	t.Parallel()

	cols := []string{"Latitude", "Longitude", "NR-ARFCN", "PCI", "MCC", "MNC", "SSS-RSRP", "SSS-SINR"}
	m := BuildColumnMappingFromHeaders(cols)
	if m["latitude"] != 0 || m["longitude"] != 1 || m["frequency"] != 2 || m["rsrp"] != 6 || m["sinr"] != 7 {
		t.Fatalf("mapping: %#v", m)
	}
}

func TestBuildColumnMappingFromHeaders_earfcnFallback(t *testing.T) {
	t.Parallel()

	cols := []string{"Latitude", "Longitude", "EARFCN", "PCI", "MCC", "MNC", "RSRP"}
	m := BuildColumnMappingFromHeaders(cols)
	if m["frequency"] != 2 {
		t.Fatalf("expected EARFCN as frequency, got %#v", m)
	}
}
