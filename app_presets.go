package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
)

const presetSchemaVersion = 1

type PresetUIState struct {
	InputCSVPaths        []string                `json:"input_csv_paths,omitempty"`
	MobileLTEPaths       []string                `json:"mobile_lte_paths,omitempty"`
	CustomFilterPaths    []string                `json:"custom_filter_paths,omitempty"`
	UseAutoFilters       bool                    `json:"use_auto_filters"`
	UseAdditionalFilters bool                    `json:"use_additional_filters"`
	EnableTimeSelector   bool                    `json:"enable_time_selector"`
	CustomOperatorsText  string                  `json:"custom_operators_text,omitempty"`
	ColumnMapping        map[string]int          `json:"column_mapping,omitempty"`
	TimeWindows          []backendpkg.TimeWindow `json:"time_windows,omitempty"`
}

type ProcessingPreset struct {
	SchemaVersion  int                         `json:"schemaVersion"`
	ID             string                      `json:"id"`
	Name           string                      `json:"name"`
	InputRadioTech string                      `json:"inputRadioTech"`
	CreatedAt      string                      `json:"createdAt"`
	UpdatedAt      string                      `json:"updatedAt"`
	Config         backendpkg.ProcessingConfig `json:"config"`
	UIState        PresetUIState               `json:"uiState"`
}

type SavePresetRequest struct {
	ID             string                      `json:"id,omitempty"`
	Name           string                      `json:"name"`
	InputRadioTech string                      `json:"inputRadioTech"`
	Config         backendpkg.ProcessingConfig `json:"config"`
	UIState        PresetUIState               `json:"uiState"`
}

func (a *App) presetStorageDir() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("nepodarilo sa zistiť user config adresár: %w", err)
	}
	dir := filepath.Join(cfgDir, "100mscript", "presets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("nepodarilo sa vytvoriť adresár presetov: %w", err)
	}
	return dir, nil
}

func randomID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func normalizePresetID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, trimmed)
	mapped = strings.Trim(mapped, "-_")
	if mapped == "" {
		return ""
	}
	return mapped
}

func presetPathForID(dir string, id string) string {
	return filepath.Join(dir, id+".json")
}

func (a *App) SavePreset(req SavePresetRequest) (ProcessingPreset, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProcessingPreset{}, fmt.Errorf("zadaj názov presetu")
	}
	dir, err := a.presetStorageDir()
	if err != nil {
		return ProcessingPreset{}, err
	}
	id := normalizePresetID(req.ID)
	if id == "" {
		id = fmt.Sprintf("preset-%d-%s", time.Now().Unix(), randomID())
	}
	path := presetPathForID(dir, id)
	now := time.Now().UTC().Format(time.RFC3339)
	preset := ProcessingPreset{
		SchemaVersion:  presetSchemaVersion,
		ID:             id,
		Name:           name,
		InputRadioTech: strings.TrimSpace(req.InputRadioTech),
		CreatedAt:      now,
		UpdatedAt:      now,
		Config:         req.Config,
		UIState:        req.UIState,
	}
	if existing, err := a.LoadPreset(id); err == nil {
		preset.CreatedAt = existing.CreatedAt
	}
	if preset.UIState.ColumnMapping == nil {
		preset.UIState.ColumnMapping = map[string]int{}
	}

	data, err := json.MarshalIndent(preset, "", "  ")
	if err != nil {
		return ProcessingPreset{}, fmt.Errorf("serializácia presetu zlyhala: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return ProcessingPreset{}, fmt.Errorf("zápis presetu zlyhal: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return ProcessingPreset{}, fmt.Errorf("uloženie presetu zlyhalo: %w", err)
	}
	return preset, nil
}

func (a *App) LoadPreset(id string) (ProcessingPreset, error) {
	normID := normalizePresetID(id)
	if normID == "" {
		return ProcessingPreset{}, fmt.Errorf("neplatné ID presetu")
	}
	dir, err := a.presetStorageDir()
	if err != nil {
		return ProcessingPreset{}, err
	}
	path := presetPathForID(dir, normID)
	data, err := os.ReadFile(path)
	if err != nil {
		return ProcessingPreset{}, fmt.Errorf("preset sa nepodarilo načítať: %w", err)
	}
	var preset ProcessingPreset
	if err := json.Unmarshal(data, &preset); err != nil {
		return ProcessingPreset{}, fmt.Errorf("preset je poškodený: %w", err)
	}
	if preset.ID == "" {
		preset.ID = normID
	}
	return preset, nil
}

func (a *App) ListPresets() ([]ProcessingPreset, error) {
	dir, err := a.presetStorageDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("nepodarilo sa načítať zoznam presetov: %w", err)
	}
	out := make([]ProcessingPreset, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p ProcessingPreset
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if p.ID == "" {
			p.ID = strings.TrimSuffix(name, filepath.Ext(name))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].UpdatedAt) > strings.ToLower(out[j].UpdatedAt)
	})
	return out, nil
}

func (a *App) DeletePreset(id string) error {
	normID := normalizePresetID(id)
	if normID == "" {
		return fmt.Errorf("neplatné ID presetu")
	}
	dir, err := a.presetStorageDir()
	if err != nil {
		return err
	}
	path := presetPathForID(dir, normID)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("zmazanie presetu zlyhalo: %w", err)
	}
	return nil
}
