package backend

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeOutputSuffix(t *testing.T) {
	t.Parallel()

	if normalizeOutputSuffix("dev") != "_dev" {
		t.Fatal(normalizeOutputSuffix("dev"))
	}
	if normalizeOutputSuffix("_x") != "_x" {
		t.Fatal()
	}
	if normalizeOutputSuffix("  ") != "" {
		t.Fatal()
	}
}

func TestOutputPathsForConfig_mobileSuffix(t *testing.T) {
	t.Parallel()

	cfg := ProcessingConfig{
		FilePath:          filepath.Join("dir", "input.csv"),
		MobileModeEnabled: true,
	}
	z, s, suf := outputPathsForConfig(cfg)
	if !strings.HasSuffix(z, "_mobile_zones.csv") || !strings.HasSuffix(s, "_mobile_stats.csv") {
		t.Fatalf("zones=%s stats=%s suffix=%q", z, s, suf)
	}
	cfg2 := ProcessingConfig{
		FilePath:          filepath.Join("dir", "input.csv"),
		OutputSuffix:      "x",
		MobileModeEnabled: true,
	}
	z2, _, _ := outputPathsForConfig(cfg2)
	if !strings.Contains(z2, "_x_mobile_zones.csv") {
		t.Fatalf("got %s", z2)
	}
}

func TestOutputPathsForConfig_plain(t *testing.T) {
	t.Parallel()

	cfg := ProcessingConfig{FilePath: "/tmp/a.csv"}
	z, s, suf := outputPathsForConfig(cfg)
	if z != "/tmp/a_zones.csv" || s != "/tmp/a_stats.csv" || suf != "" {
		t.Fatalf("%s %s %s", z, s, suf)
	}
}
