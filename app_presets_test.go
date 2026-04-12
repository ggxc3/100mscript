package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
)

func setUserConfigDirForTest(t *testing.T, dir string) {
	t.Helper()
	// Cover all major OS env variants used by os.UserConfigDir.
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

func samplePresetRequest(name string) SavePresetRequest {
	return SavePresetRequest{
		Name:           name,
		InputRadioTech: "5g",
		Config: backendpkg.ProcessingConfig{
			FilePath:              "/tmp/input.csv",
			InputFilePaths:        []string{"/tmp/input.csv", "/tmp/input2.csv"},
			ColumnMapping:         map[string]int{"latitude": 1, "longitude": 2},
			KeepOriginalRows:      true,
			ExcludedOriginalRows:  []int{1, 2, 3},
			TimeWindows:           []backendpkg.TimeWindow{{Start: "2025-01-01T08:00:00", End: "2025-01-01T09:00:00"}},
			ZoneMode:              "segments",
			ZoneSizeM:             100,
			RSRPThreshold:         -110,
			SINRThreshold:         -5,
			IncludeEmptyZones:     true,
			AddCustomOperators:    true,
			CustomOperators:       []backendpkg.CustomOperator{{MCC: "231", MNC: "01", PCI: "10"}},
			FilterPaths:           []string{"/tmp/f1.txt", "/tmp/f2.txt"},
			OutputSuffix:          "_preset",
			OutputZonesFilePath:   "/tmp/out_zones.csv",
			OutputStatsFilePath:   "/tmp/out_stats.csv",
			MobileModeEnabled:     true,
			MobileLTEFilePath:     "/tmp/lte.csv",
			MobileLTEFilePaths:    []string{"/tmp/lte.csv", "/tmp/lte2.csv"},
			MobileTimeToleranceMS: 1500,
			MobileRequireNRYES:    false,
			MobileNRColumnName:    "5G NR",
			ProgressEnabled:       false,
		},
		UIState: PresetUIState{
			InputCSVPaths:        []string{"/tmp/input.csv", "/tmp/input2.csv"},
			MobileLTEPaths:       []string{"/tmp/lte.csv", "/tmp/lte2.csv"},
			CustomFilterPaths:    []string{"/tmp/f1.txt", "/tmp/f2.txt"},
			UseAutoFilters:       true,
			UseAdditionalFilters: true,
			EnableTimeSelector:   true,
			CustomOperatorsText:  "231:01",
			ColumnMapping:        map[string]int{"latitude": 1, "longitude": 2},
			TimeWindows:          []backendpkg.TimeWindow{{Start: "2025-01-01T08:00:00", End: "2025-01-01T09:00:00"}},
		},
	}
}

func TestPresetCRUDRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	setUserConfigDirForTest(t, tmp)

	app := &App{}
	req := samplePresetRequest("Kompletný preset")

	saved, err := app.SavePreset(req)
	if err != nil {
		t.Fatalf("SavePreset failed: %v", err)
	}
	if saved.ID == "" {
		t.Fatalf("expected non-empty preset ID")
	}
	if saved.SchemaVersion != presetSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", presetSchemaVersion, saved.SchemaVersion)
	}

	loaded, err := app.LoadPreset(saved.ID)
	if err != nil {
		t.Fatalf("LoadPreset failed: %v", err)
	}
	if loaded.Name != req.Name {
		t.Fatalf("expected name %q, got %q", req.Name, loaded.Name)
	}
	if loaded.Config.ZoneMode != req.Config.ZoneMode {
		t.Fatalf("expected zone mode %q, got %q", req.Config.ZoneMode, loaded.Config.ZoneMode)
	}
	if got, want := len(loaded.Config.InputFilePaths), len(req.Config.InputFilePaths); got != want {
		t.Fatalf("expected %d input paths, got %d", want, got)
	}
	if got, want := len(loaded.UIState.MobileLTEPaths), len(req.UIState.MobileLTEPaths); got != want {
		t.Fatalf("expected %d mobile LTE paths, got %d", want, got)
	}
	if loaded.UIState.CustomOperatorsText != req.UIState.CustomOperatorsText {
		t.Fatalf("expected custom operators text %q, got %q", req.UIState.CustomOperatorsText, loaded.UIState.CustomOperatorsText)
	}

	list, err := app.ListPresets()
	if err != nil {
		t.Fatalf("ListPresets failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 preset in list, got %d", len(list))
	}
	if list[0].ID != saved.ID {
		t.Fatalf("expected listed preset ID %q, got %q", saved.ID, list[0].ID)
	}

	if err := app.DeletePreset(saved.ID); err != nil {
		t.Fatalf("DeletePreset failed: %v", err)
	}
	list, err = app.ListPresets()
	if err != nil {
		t.Fatalf("ListPresets after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 presets after delete, got %d", len(list))
	}

	// Delete non-existing preset should be no-op.
	if err := app.DeletePreset(saved.ID); err != nil {
		t.Fatalf("DeletePreset non-existing should be nil, got: %v", err)
	}
}

func TestSavePresetUpdatePreservesCreatedAt(t *testing.T) {
	tmp := t.TempDir()
	setUserConfigDirForTest(t, tmp)

	app := &App{}
	first, err := app.SavePreset(samplePresetRequest("Pôvodný"))
	if err != nil {
		t.Fatalf("first SavePreset failed: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	updatedReq := samplePresetRequest("Aktualizovaný")
	updatedReq.ID = first.ID
	second, err := app.SavePreset(updatedReq)
	if err != nil {
		t.Fatalf("second SavePreset failed: %v", err)
	}

	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("expected createdAt to be preserved (%q), got %q", first.CreatedAt, second.CreatedAt)
	}
	if second.UpdatedAt == first.UpdatedAt {
		t.Fatalf("expected updatedAt to change; both are %q", second.UpdatedAt)
	}
	if second.Name != "Aktualizovaný" {
		t.Fatalf("expected updated name, got %q", second.Name)
	}
}

func TestListPresetsSkipsInvalidJSONFiles(t *testing.T) {
	tmp := t.TempDir()
	setUserConfigDirForTest(t, tmp)
	app := &App{}

	if _, err := app.SavePreset(samplePresetRequest("Valid")); err != nil {
		t.Fatalf("SavePreset failed: %v", err)
	}

	dir, err := app.presetStorageDir()
	if err != nil {
		t.Fatalf("presetStorageDir failed: %v", err)
	}
	badPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(badPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("failed to write broken preset file: %v", err)
	}

	list, err := app.ListPresets()
	if err != nil {
		t.Fatalf("ListPresets failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected only valid preset to be listed, got %d", len(list))
	}
}
