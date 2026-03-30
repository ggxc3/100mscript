package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppVersion should stay in sync with wails.json info.productVersion.
const AppVersion = "0.2.7"

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

// normalizeFilePathForUI trims a path and applies filepath.Clean (removes trailing separators
// after a file name, normalizes separators for the current OS). Same behavior on macOS and Windows.
func normalizeFilePathForUI(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func (a *App) PickInputCSVFile() (string, error) {
	return a.pickCSVFile("Vyber vstupny CSV subor")
}

func (a *App) PickMobileLTECSVFile() (string, error) {
	return a.pickCSVFile("Vyber LTE CSV subor (mobile sync)")
}

// PickMobileLTECSVPaths opens a multi-select dialog for LTE CSV files (mobile sync).
func (a *App) PickMobileLTECSVPaths() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("aplikacia nie je inicializovana")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Vyber jeden alebo viac LTE CSV súborov (rovnaká štruktúra)",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "CSV files (*.csv)", Pattern: "*.csv"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if files == nil {
		return []string{}, nil
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = normalizeFilePathForUI(f)
	}
	return out, nil
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
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = normalizeFilePathForUI(f)
	}
	return out, nil
}

func (a *App) DiscoverAutoFilterPaths() ([]string, error) {
	baseDir := a.rootPath
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	return backendpkg.DiscoverFilterPaths(baseDir)
}

// DefaultOutputPathsResult holds computed default output paths (same rules as backend outputPathsForConfig).
type DefaultOutputPathsResult struct {
	Zones string `json:"zones"`
	Stats string `json:"stats"`
}

// DefaultOutputPaths returns default _zones.csv / _stats.csv paths for the given input and options.
func (a *App) DefaultOutputPaths(filePath string, mobileModeEnabled bool, outputSuffix string) (DefaultOutputPathsResult, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return DefaultOutputPathsResult{}, fmt.Errorf("zadaj cestu k vstupnému CSV")
	}
	cfg := backendpkg.DefaultProcessingConfig()
	cfg.FilePath = filePath
	cfg.MobileModeEnabled = mobileModeEnabled
	cfg.OutputSuffix = outputSuffix
	z, s, _ := backendpkg.OutputPathsForProcessing(cfg)
	return DefaultOutputPathsResult{Zones: z, Stats: s}, nil
}

// PickOutputCSVFile opens a save dialog for a CSV output path.
func (a *App) PickOutputCSVFile(title string, defaultDirectory string, defaultFilename string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("aplikacia nie je inicializovana")
	}
	opts := wailsruntime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: strings.TrimSpace(defaultFilename),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "CSV (*.csv)", Pattern: "*.csv"},
			{DisplayName: "Všetky súbory", Pattern: "*"},
		},
	}
	if dd := strings.TrimSpace(defaultDirectory); dd != "" {
		opts.DefaultDirectory = dd
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, opts)
	if err != nil {
		return "", err
	}
	return normalizeFilePathForUI(path), nil
}

type CSVPreview struct {
	FilePaths        []string       `json:"filePaths"`
	FilePath         string         `json:"filePath"`
	Columns          []string       `json:"columns"`
	Encoding         string         `json:"encoding"`
	HeaderLine       int            `json:"headerLine"`
	OriginalHeader   string         `json:"originalHeader"`
	SuggestedMapping map[string]int `json:"suggestedMapping"`
	// InputRadioTech is backend.InputRadioTech5G, InputRadioTechLTE, or InputRadioTechUnknown.
	InputRadioTech string `json:"inputRadioTech"`
}

func (a *App) LoadCSVPreview(paths []string) (CSVPreview, error) {
	paths = backendpkg.NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return CSVPreview{}, fmt.Errorf("zadaj aspoň jednu cestu k CSV súboru")
	}
	var data *backendpkg.CSVData
	var err error
	if len(paths) == 1 {
		data, err = backendpkg.LoadCSVFile(paths[0])
	} else {
		data, err = backendpkg.LoadAndMergeCSVFiles(a.ctx, paths)
	}
	if err != nil {
		return CSVPreview{}, err
	}
	return CSVPreview{
		FilePaths:        paths,
		FilePath:         paths[0],
		Columns:          data.Columns,
		Encoding:         data.FileInfo.Encoding,
		HeaderLine:       data.FileInfo.HeaderLine,
		OriginalHeader:   data.FileInfo.OriginalHeader,
		SuggestedMapping: suggestMappingForUI(data.Columns),
		InputRadioTech:   data.InputRadioTech,
	}, nil
}

func (a *App) LoadTimeSelectorData(paths []string) (backendpkg.TimeSelectorData, error) {
	return backendpkg.LoadTimeSelectorData(paths)
}

func (a *App) PickInputCSVPaths() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("aplikacia nie je inicializovana")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Vyber jeden alebo viac CSV súborov (rovnaká štruktúra)",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "CSV files (*.csv)", Pattern: "*.csv"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if files == nil {
		return []string{}, nil
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = normalizeFilePathForUI(f)
	}
	return out, nil
}

func (a *App) RunProcessingWithConfig(cfg backendpkg.ProcessingConfig) (backendpkg.ProcessingResult, error) {
	if cfg.ProgressEnabled {
		// Wails UI requests should not emit console progress bars.
		cfg.ProgressEnabled = false
	}
	return backendpkg.RunProcessing(a.ctx, cfg)
}

// AppInfo is exposed to the UI (about dialog, window title hints).
type AppInfo struct {
	ProductName string `json:"productName"`
	Version     string `json:"version"`
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		ProductName: "100mscript",
		Version:     AppVersion,
	}
}

// OpenContainingFolder opens the system file manager at the given file or directory.
func (a *App) OpenContainingFolder(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("prázdna cesta")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("cesta nie je dostupná: %w", err)
	}

	switch goruntime.GOOS {
	case "windows":
		if fi.IsDir() {
			return exec.Command("explorer", abs).Start()
		}
		// Reveal file in Explorer
		return exec.Command("explorer", "/select,"+abs).Start()
	case "darwin":
		if fi.IsDir() {
			return exec.Command("open", abs).Start()
		}
		return exec.Command("open", "-R", abs).Start()
	default:
		dir := abs
		if !fi.IsDir() {
			dir = filepath.Dir(abs)
		}
		return exec.Command("xdg-open", dir).Start()
	}
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
	return normalizeFilePathForUI(path), nil
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
