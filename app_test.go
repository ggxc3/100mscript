package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
)

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
