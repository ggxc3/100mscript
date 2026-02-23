package main

import (
	"context"
	"fmt"
	"strings"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx      context.Context
	rootPath string
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.rootPath = "."
}

func (a *App) PickInputCSVFile() (string, error) {
	return a.pickCSVFile("Vyber vstupny CSV subor")
}

func (a *App) PickMobileLTECSVFile() (string, error) {
	return a.pickCSVFile("Vyber LTE CSV subor (mobile sync)")
}

func (a *App) PickFilterFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("aplikacia nie je inicializovana")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Vyber filter subory (.txt)",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Text files (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return nil, err
	}
	if files == nil {
		return []string{}, nil
	}
	return files, nil
}

func (a *App) DiscoverAutoFilterPaths() ([]string, error) {
	baseDir := a.rootPath
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	return backendpkg.DiscoverFilterPaths(baseDir)
}

type CSVPreview struct {
	FilePath         string         `json:"filePath"`
	Columns          []string       `json:"columns"`
	Encoding         string         `json:"encoding"`
	HeaderLine       int            `json:"headerLine"`
	OriginalHeader   string         `json:"originalHeader"`
	SuggestedMapping map[string]int `json:"suggestedMapping"`
}

func (a *App) LoadCSVPreview(filePath string) (CSVPreview, error) {
	data, err := backendpkg.LoadCSVFile(filePath)
	if err != nil {
		return CSVPreview{}, err
	}
	return CSVPreview{
		FilePath:         filePath,
		Columns:          data.Columns,
		Encoding:         data.FileInfo.Encoding,
		HeaderLine:       data.FileInfo.HeaderLine,
		OriginalHeader:   data.FileInfo.OriginalHeader,
		SuggestedMapping: suggestMappingForUI(data.Columns),
	}, nil
}

func (a *App) RunProcessingWithConfig(cfg backendpkg.ProcessingConfig) (backendpkg.ProcessingResult, error) {
	if cfg.ProgressEnabled {
		// Wails UI requests should not emit console progress bars.
		cfg.ProgressEnabled = false
	}
	return backendpkg.RunProcessing(a.ctx, cfg)
}

func (a *App) pickCSVFile(title string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("aplikacia nie je inicializovana")
	}
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "CSV files (*.csv)", Pattern: "*.csv"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func suggestMappingForUI(columns []string) map[string]int {
	// Prefer the prompt-style choices (e.g. "Frequency" over "EARFCN") for UX consistency with Python app.
	preferred := map[string][]string{
		"latitude":  {"Latitude", "Lat"},
		"longitude": {"Longitude", "Lon", "Lng"},
		"frequency": {"Frequency", "NR-ARFCN", "EARFCN"},
		"pci":       {"PCI"},
		"mcc":       {"MCC"},
		"mnc":       {"MNC"},
		"rsrp":      {"SSS-RSRP", "RSRP", "NR-SS-RSRP"},
		"sinr":      {"SSS-SINR", "SINR", "NR-SS-SINR"},
	}

	lowerIndex := map[string]int{}
	for i, col := range columns {
		lowerIndex[strings.ToLower(strings.TrimSpace(col))] = i
	}

	out := map[string]int{}
	for key, names := range preferred {
		for _, name := range names {
			if idx, ok := lowerIndex[strings.ToLower(name)]; ok {
				out[key] = idx
				break
			}
		}
	}

	// Fill any missing keys with backend helper.
	for key, idx := range backendpkg.BuildColumnMappingFromHeaders(columns) {
		if _, exists := out[key]; !exists {
			out[key] = idx
		}
	}
	return out
}
