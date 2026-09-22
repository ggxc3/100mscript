package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	backendpkg "github.com/jakubvysocan/100mscript/internal/backend"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppVersion should stay in sync with wails.json info.productVersion.
const AppVersion = "0.3.5"

type App struct {
	ctx              context.Context
	rootPath         string
	previewMu        sync.Mutex
	previewCache     map[string]csvSchemaCacheEntry
	previewRequestMu sync.Mutex
	previewCancel    context.CancelFunc
}

type csvSchemaCacheEntry struct {
	size     int64
	modified time.Time
	schema   *backendpkg.CSVData
}

const csvPreviewLoadedEvent = "csv-preview:loaded"

type CSVPreviewLoadResult struct {
	RequestID int         `json:"requestId"`
	Preview   *CSVPreview `json:"preview,omitempty"`
	Error     string      `json:"error,omitempty"`
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

func (a *App) PickMobileNSALTECSVFile() (string, error) {
	return a.pickCSVFile("Vyber NSA LTE CSV súbor (mobile sync)")
}

// PickMobileNSALTECSVPaths opens a multi-select dialog for NSA LTE CSV files (mobile sync).
func (a *App) PickMobileNSALTECSVPaths() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("aplikacia nie je inicializovana")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Vyber jeden alebo viac kompatibilných NSA LTE CSV súborov",
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
	FilePaths        []string               `json:"filePaths"`
	FilePath         string                 `json:"filePath"`
	Columns          []string               `json:"columns"`
	FileSchemas      []CSVPreviewFileSchema `json:"fileSchemas"`
	Encoding         string                 `json:"encoding"`
	HeaderLine       int                    `json:"headerLine"`
	OriginalHeader   string                 `json:"originalHeader"`
	SuggestedMapping map[string]int         `json:"suggestedMapping"`
	// InputRadioTech is backend.InputRadioTech5G, InputRadioTechLTE, or InputRadioTechUnknown.
	InputRadioTech string `json:"inputRadioTech"`
}

type CSVPreviewFileSchema struct {
	FilePath string   `json:"filePath"`
	Columns  []string `json:"columns"`
}

func (a *App) loadCSVPreview(paths []string) (CSVPreview, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.loadCSVPreviewContext(ctx, paths)
}

func (a *App) loadCSVPreviewContext(ctx context.Context, paths []string) (CSVPreview, error) {
	paths = backendpkg.NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return CSVPreview{}, fmt.Errorf("zadaj aspoň jednu cestu k CSV súboru")
	}
	// Serialize bounded reads; superseded requests stop before the next file.
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if err := ctx.Err(); err != nil {
		return CSVPreview{}, err
	}
	if a.previewCache == nil {
		a.previewCache = make(map[string]csvSchemaCacheEntry)
	}
	schemas := make([]*backendpkg.CSVData, 0, len(paths))
	fileSchemas := make([]CSVPreviewFileSchema, 0, len(paths))
	keep := make(map[string]bool, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return CSVPreview{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return CSVPreview{}, err
		}
		cached, ok := a.previewCache[path]
		if !ok || cached.size != info.Size() || !cached.modified.Equal(info.ModTime()) {
			schema, err := backendpkg.LoadCSVSchema(path)
			if err != nil {
				return CSVPreview{}, fmt.Errorf("CSV %q: %w", path, err)
			}
			cached = csvSchemaCacheEntry{size: info.Size(), modified: info.ModTime(), schema: schema}
			a.previewCache[path] = cached
		}
		keep[path] = true
		schemas = append(schemas, cached.schema)
		fileSchemas = append(fileSchemas, CSVPreviewFileSchema{FilePath: path, Columns: append([]string(nil), cached.schema.Columns...)})
	}
	for path := range a.previewCache {
		if !keep[path] {
			delete(a.previewCache, path)
		}
	}
	if err := ctx.Err(); err != nil {
		return CSVPreview{}, err
	}
	data, err := backendpkg.MergeCSVSchema(schemas)
	if err != nil {
		return CSVPreview{}, err
	}
	return CSVPreview{
		FilePaths: paths, FilePath: paths[0], Columns: data.Columns, FileSchemas: fileSchemas,
		Encoding: data.FileInfo.Encoding, HeaderLine: data.FileInfo.HeaderLine,
		OriginalHeader: data.FileInfo.OriginalHeader, SuggestedMapping: suggestMappingForUI(data.Columns), InputRadioTech: data.InputRadioTech,
	}, nil
}

func (a *App) LoadCSVPreview(paths []string) (CSVPreview, error) {
	return a.loadCSVPreview(paths)
}

func (a *App) StartLoadCSVPreview(requestID int, paths []string) error {
	if a.ctx == nil {
		return fmt.Errorf("aplikacia nie je inicializovana")
	}
	normalizedPaths := append([]string(nil), paths...)
	a.previewRequestMu.Lock()
	if a.previewCancel != nil {
		a.previewCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.previewCancel = cancel
	a.previewRequestMu.Unlock()
	go func() {
		defer cancel()
		payload := CSVPreviewLoadResult{RequestID: requestID}
		preview, err := a.loadCSVPreviewContext(ctx, normalizedPaths)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			payload.Error = err.Error()
		} else {
			payload.Preview = &preview
		}
		wailsruntime.EventsEmit(ctx, csvPreviewLoadedEvent, payload)
	}()
	return nil
}

func (a *App) PickInputCSVPaths() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("aplikacia nie je inicializovana")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Vyber jeden alebo viac kompatibilných CSV súborov",
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
