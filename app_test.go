package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
)

func TestNormalizeFilePathForUI(t *testing.T) {
	t.Parallel()

	if normalizeFilePathForUI("") != "" {
		t.Fatal("empty string")
	}
	if normalizeFilePathForUI("   ") != "" {
		t.Fatal("whitespace only")
	}

	base := filepath.Join("some", "dir", "file.csv")
	withTrailing := base + string(filepath.Separator)
	got := normalizeFilePathForUI(withTrailing)
	want := filepath.Clean(base)
	if got != want {
		t.Fatalf("trailing separator: got %q want %q", got, want)
	}

	redundant := filepath.Join("a", "b", "..", "c.csv")
	got = normalizeFilePathForUI(redundant)
	if got != filepath.Clean(redundant) {
		t.Fatalf("redundant segments: got %q", got)
	}
}

func TestDefaultOutputPaths_plainNames(t *testing.T) {
	t.Parallel()
	app := NewApp()
	app.startup(context.Background())

	in := filepath.Join("data", "proj", "input.csv")
	res, err := app.DefaultOutputPaths(in, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Zones) != "input_zones.csv" {
		t.Fatalf("zones: %s", res.Zones)
	}
	if filepath.Base(res.Stats) != "input_stats.csv" {
		t.Fatalf("stats: %s", res.Stats)
	}
}

func TestDefaultOutputPaths_mobileSuffix(t *testing.T) {
	t.Parallel()
	app := NewApp()
	app.startup(context.Background())

	in := filepath.Join("data", "input.csv")
	res, err := app.DefaultOutputPaths(in, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Zones, "_mobile_zones.csv") {
		t.Fatalf("zones: %s", res.Zones)
	}
	if !strings.HasSuffix(res.Stats, "_mobile_stats.csv") {
		t.Fatalf("stats: %s", res.Stats)
	}
}

func TestDefaultOutputPaths_emptyPathErrors(t *testing.T) {
	t.Parallel()
	app := NewApp()
	app.startup(context.Background())
	_, err := app.DefaultOutputPaths("  ", false, "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestLoadCSVPreview_InputRadioTechSingleNR(t *testing.T) {
	t.Parallel()
	app := NewApp()
	app.startup(context.Background())

	tmp := t.TempDir()
	p := filepath.Join(tmp, "one.csv")
	content := "latitude;longitude;NR-ARFCN;pci;mcc;mnc;SSS-RSRP\n48;17;1;1;231;01;-100\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := app.LoadCSVPreview([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if prev.InputRadioTech != backendpkg.InputRadioTech5G {
		t.Fatalf("expected 5g, got %q", prev.InputRadioTech)
	}
}

func TestLoadCSVPreview_InputRadioTechMergedLTE(t *testing.T) {
	t.Parallel()
	app := NewApp()
	app.startup(context.Background())

	tmp := t.TempDir()
	header := "latitude;longitude;EARFCN;pci;mcc;mnc;rsrp"
	p1 := filepath.Join(tmp, "a.csv")
	p2 := filepath.Join(tmp, "b.csv")
	if err := os.WriteFile(p1, []byte(header+"\n48;17;1;1;231;01;-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte(header+"\n48;17;2;2;231;02;-101\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := app.LoadCSVPreview([]string{p1, p2})
	if err != nil {
		t.Fatal(err)
	}
	if prev.InputRadioTech != backendpkg.InputRadioTechLTE {
		t.Fatalf("expected lte, got %q", prev.InputRadioTech)
	}
	if len(prev.FilePaths) != 2 {
		t.Fatalf("file paths: %#v", prev.FilePaths)
	}
}
