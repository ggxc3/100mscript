package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPreviewReal2100Performance(t *testing.T) {
	if os.Getenv("RUN_LARGE_REAL_DATA_TESTS") != "1" {
		t.Skip("local 2100 data")
	}
	paths, err := filepath.Glob("data/2100/*.csv")
	if err != nil || len(paths) != 2 {
		t.Fatal(paths, err)
	}
	app := NewApp()
	app.startup(context.Background())
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	preview, err := app.LoadCSVPreview(paths)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	t.Logf("preview files=%d columns=%d duration=%s allocated=%.1f MiB", len(preview.FileSchemas), len(preview.Columns), time.Since(start), float64(after.TotalAlloc-before.TotalAlloc)/(1<<20))
	for _, s := range preview.FileSchemas {
		if len(s.Columns) == 0 {
			t.Fatal(s)
		}
	}
}

func TestPreviewCacheInvalidationAndCancellation(t *testing.T) {
	app := NewApp()
	app.startup(context.Background())
	dir := t.TempDir()
	p := filepath.Join(dir, "one.csv")
	q := filepath.Join(dir, "two.csv")
	first := "Latitude;Longitude;MCC;MNC;RSRP;Frequency\n48;17;231;1;-80;2112500000\n"
	if err := os.WriteFile(p, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadCSVPreview([]string{p}); err != nil {
		t.Fatal(err)
	}
	cached := app.previewCache[p].schema
	if err := os.WriteFile(q, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadCSVPreview([]string{p, q}); err != nil {
		t.Fatal(err)
	}
	if app.previewCache[p].schema != cached {
		t.Fatal("unchanged file was reloaded")
	}
	updated := "Latitude;Longitude;MCC;MNC;RSRP;Frequency;PCI\n48;17;231;1;-80;2112500000;10\n"
	if err := os.WriteFile(p, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := app.LoadCSVPreview([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Columns) != 7 || len(app.previewCache) != 1 || app.previewCache[p].schema == cached {
		t.Fatal("stale schema cache", preview)
	}
	// Returned objects cannot corrupt cached schemas.
	preview.FileSchemas[0].Columns[0] = "corrupt"
	again, err := app.LoadCSVPreview([]string{p})
	if err != nil || again.Columns[0] != "Latitude" {
		t.Fatal(again, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.loadCSVPreviewContext(ctx, []string{p}); err != context.Canceled {
		t.Fatal(err)
	}
	// A cancelled request must not prevent a subsequent successful load.
	if _, err := app.LoadCSVPreview([]string{p}); err != nil {
		t.Fatal(err)
	}
}
