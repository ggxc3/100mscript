package backend

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const csvSourceIndexColumn = "__100mscript_source_csv_index"

// NormalizeInputPaths trims, drops empties, and removes duplicate paths (order preserved).
func NormalizeInputPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// InputPathsFromConfig returns paths to load: InputFilePaths when set, otherwise FilePath.
func InputPathsFromConfig(cfg ProcessingConfig) []string {
	if raw := cfg.InputFilePaths; len(raw) > 0 {
		out := NormalizeInputPaths(raw)
		if len(out) > 0 {
			return out
		}
	}
	if p := strings.TrimSpace(cfg.FilePath); p != "" {
		return []string{p}
	}
	return nil
}

// MobileNSALTEPathsFromConfig returns NSA LTE paths: MobileNSALTEFilePaths when set, otherwise a single MobileNSALTEFilePath.
func MobileNSALTEPathsFromConfig(cfg ProcessingConfig) []string {
	if raw := cfg.MobileNSALTEFilePaths; len(raw) > 0 {
		out := NormalizeInputPaths(raw)
		if len(out) > 0 {
			return out
		}
	}
	if p := strings.TrimSpace(cfg.MobileNSALTEFilePath); p != "" {
		return []string{p}
	}
	return nil
}

func isAutoExtraColumn(name string) bool {
	return strings.HasPrefix(name, "extra_col_")
}

func trimTrailingAutoExtraColumns(columns []string) []string {
	end := len(columns)
	for end > 0 && isAutoExtraColumn(columns[end-1]) {
		end--
	}
	return columns[:end]
}

func normalizedColumnKey(name string) string {
	return normalizeHeaderToken(name)
}

func buildColumnIndexByNormalizedName(columns []string) (map[string]int, error) {
	out := make(map[string]int, len(columns))
	original := make(map[string]string, len(columns))
	for i, col := range columns {
		key := normalizedColumnKey(col)
		if key == "" {
			continue
		}
		if prev, exists := original[key]; exists {
			return nil, fmt.Errorf("nejednoznačné stĺpce %q a %q", prev, col)
		}
		original[key] = col
		out[key] = i
	}
	return out, nil
}

type CSVMergeOptions struct {
	RequiredColumnNames  map[string]string
	DisplayNameOverrides map[string]string
	EquivalentColumnKeys map[string]string
}

func canonicalColumnKey(key string, equivalents map[string]string) string {
	if v := equivalents[key]; v != "" {
		return v
	}
	return key
}

func buildUnionColumns(loaded []*CSVData, displayOverrides map[string]string, equivalents map[string]string) ([]string, error) {
	columns := []string(nil)
	seen := map[string]struct{}{}
	for _, d := range loaded {
		if d == nil {
			continue
		}
		if _, err := buildColumnIndexByNormalizedName(d.Columns); err != nil {
			return nil, err
		}
		for _, col := range d.Columns {
			key := canonicalColumnKey(normalizedColumnKey(col), equivalents)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			displayName := col
			if override := strings.TrimSpace(displayOverrides[key]); override != "" {
				displayName = override
			}
			columns = append(columns, displayName)
		}
	}
	return columns, nil
}

func validateRequiredColumnsInEveryFile(paths []string, loaded []*CSVData, required map[string]string, equivalents map[string]string) error {
	if len(required) == 0 {
		return nil
	}
	for i, data := range loaded {
		rawIndex, err := buildColumnIndexByNormalizedName(data.Columns)
		if err != nil {
			return fmt.Errorf("CSV %q má nejednoznačnú hlavičku: %w", paths[i], err)
		}
		index := map[string]int{}
		for key, idx := range rawIndex {
			index[canonicalColumnKey(key, equivalents)] = idx
		}
		for logicalKey, name := range required {
			if strings.TrimSpace(name) == "" {
				continue
			}
			if _, ok := index[canonicalColumnKey(normalizedColumnKey(name), equivalents)]; !ok {
				return fmt.Errorf("CSV %q neobsahuje povinný mapovaný stĺpec %s: %q", paths[i], logicalKey, name)
			}
		}
	}
	return nil
}

func normalizeRowWidth(row []string, width int) []string {
	switch {
	case len(row) == width:
		return append([]string(nil), row...)
	case len(row) > width:
		return append([]string(nil), row[:width]...)
	default:
		out := make([]string, width)
		copy(out, row)
		return out
	}
}

// sortMergedCSVRowsByTime reorders rows by ascending timestamp when UTC or Date+Time is available
// (same rules as časové okná). Rows without a parseable time stay at the end in original order.
// Returns the second value true if any reordering happened.
func sortMergedCSVRowsByTime(data *CSVData) (*CSVData, bool) {
	if data == nil || len(data.Rows) <= 1 {
		return data, false
	}
	series, strategy := timeSeriesForSorting(data)
	if strategy == "missing" {
		return data, false
	}
	validCount := 0
	for _, v := range series.Valid {
		if v {
			validCount++
		}
	}
	if validCount == 0 {
		return data, false
	}

	n := len(data.Rows)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		ri, rj := indices[i], indices[j]
		vi, vj := series.Valid[ri], series.Valid[rj]
		if vi && vj {
			if series.Values[ri] != series.Values[rj] {
				return series.Values[ri] < series.Values[rj]
			}
			return ri < rj
		}
		if vi != vj {
			return vi
		}
		return ri < rj
	})

	changed := false
	for i := range indices {
		if indices[i] != i {
			changed = true
			break
		}
	}
	if !changed {
		return data, false
	}

	out := data.clone()
	for i, src := range indices {
		out.Rows[i] = append([]string(nil), data.Rows[src]...)
	}
	return out, true
}

func loadCSVFilesForMerge(ctx context.Context, paths []string) ([]string, []*CSVData, error) {
	paths = NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("žiadna cesta k CSV súboru")
	}

	loaded := make([]*CSVData, 0, len(paths))
	nPaths := len(paths)
	for i, p := range paths {
		d, err := LoadCSVFile(p)
		if err != nil {
			return nil, nil, fmt.Errorf("načítanie %q: %w", p, err)
		}
		loaded = append(loaded, d)
		emitProcessingProgress(ctx, "load_csv", float64(i+1)/float64(nPaths)*100)
	}
	return paths, loaded, nil
}

func loadAndMergeCSVFilesWithOptions(ctx context.Context, paths []string, opts CSVMergeOptions) ([]string, []*CSVData, *CSVData, error) {
	paths, loaded, err := loadCSVFilesForMerge(ctx, paths)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(loaded) == 1 {
		if _, err := buildColumnIndexByNormalizedName(loaded[0].Columns); err != nil {
			return nil, nil, nil, fmt.Errorf("CSV %q má nejednoznačnú hlavičku: %w", paths[0], err)
		}
		if err := validateRequiredColumnsInEveryFile(paths, loaded, opts.RequiredColumnNames, opts.EquivalentColumnKeys); err != nil {
			return nil, nil, nil, err
		}
		out := loaded[0].clone()
		return paths, loaded, out, nil
	}
	if err := validateRequiredColumnsInEveryFile(paths, loaded, opts.RequiredColumnNames, opts.EquivalentColumnKeys); err != nil {
		return nil, nil, nil, err
	}
	mergedColumns, err := buildUnionColumns(loaded, opts.DisplayNameOverrides, opts.EquivalentColumnKeys)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("CSV súbory nie sú kompatibilné: %w", err)
	}
	out, err := mergeCSVDataRowsByName(mergedColumns, loaded[0], loaded[1:], opts.EquivalentColumnKeys)
	if err != nil {
		return nil, nil, nil, err
	}
	out.FileInfo = loaded[0].FileInfo
	out.FileInfo.OriginalHeader = strings.Join(out.Columns, ";")
	out.InputRadioTech = DetectInputRadioTech(out.Columns)
	return paths, loaded, out, nil
}

// LoadAndMergeCSVFiles loads one or more semicolon CSV files and appends rows when len(paths) > 1.
// Multiple files are aligned by normalized column name and merged into a union schema.
func LoadAndMergeCSVFiles(ctx context.Context, paths []string) (*CSVData, error) {
	_, _, out, err := loadAndMergeCSVFilesWithOptions(ctx, paths, CSVMergeOptions{})
	return out, err
}

func mergeCSVDataRowsByName(columns []string, first *CSVData, rest []*CSVData, equivalents map[string]string) (*CSVData, error) {
	totalRows := len(first.Rows)
	for _, d := range rest {
		totalRows += len(d.Rows)
	}
	loaded := append([]*CSVData{first}, rest...)
	targetKeys := make([]string, len(columns))
	for i, col := range columns {
		targetKeys[i] = canonicalColumnKey(normalizedColumnKey(col), equivalents)
	}
	outRows := make([][]string, 0, totalRows)
	for _, d := range loaded {
		sourceIndex, err := buildColumnIndexByNormalizedName(d.Columns)
		if err != nil {
			return nil, err
		}
		canonicalSourceIndex := map[string]int{}
		for key, idx := range sourceIndex {
			canonicalKey := canonicalColumnKey(key, equivalents)
			if _, exists := canonicalSourceIndex[canonicalKey]; !exists {
				canonicalSourceIndex[canonicalKey] = idx
			}
		}
		for _, row := range d.Rows {
			out := make([]string, len(columns))
			for targetIdx, key := range targetKeys {
				srcIdx, ok := canonicalSourceIndex[key]
				if !ok || srcIdx >= len(row) {
					continue
				}
				out[targetIdx] = row[srcIdx]
			}
			outRows = append(outRows, out)
		}
	}
	return &CSVData{
		Columns:        append([]string(nil), columns...),
		Rows:           outRows,
		FileInfo:       first.FileInfo,
		InputRadioTech: DetectInputRadioTech(columns),
	}, nil
}

func resolveColumnMappingNames(columns []string, cfg ProcessingConfig) (map[string]string, error) {
	out := map[string]string{}
	for key, name := range cfg.ColumnMappingNames {
		name = strings.TrimSpace(name)
		if name != "" {
			out[key] = name
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	for key, idx := range cfg.ColumnMapping {
		if idx < 0 || idx >= len(columns) {
			return nil, fmt.Errorf("neplatné mapovanie stĺpca %s: index %d mimo rozsahu", key, idx)
		}
		out[key] = columns[idx]
	}
	return out, nil
}

func displayOverridesFromMappingNames(mappingNames map[string]string) map[string]string {
	out := map[string]string{}
	for _, name := range mappingNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[normalizedColumnKey(name)] = name
	}
	return out
}

func equivalentColumnKeysFromMappingNames(mappingNames map[string]string) map[string]string {
	out := map[string]string{}
	addAliasGroup := func(selected string, aliases []string) {
		selected = strings.TrimSpace(selected)
		if selected == "" {
			return
		}
		canonical := normalizedColumnKey(selected)
		if canonical == "" {
			return
		}
		for _, alias := range aliases {
			key := normalizedColumnKey(alias)
			if key != "" {
				out[key] = canonical
			}
		}
		out[canonical] = canonical
	}
	addAliasGroup(mappingNames["rsrp"], []string{"SSS-RSRP", "SS-RSRP", "NR-SS-RSRP", "RSRP"})
	addAliasGroup(mappingNames["sinr"], []string{"SSS-SINR", "SS-SINR", "NR-SS-SINR", "SINR"})
	return out
}

func requiredMappingNames(mappingNames map[string]string) map[string]string {
	required := map[string]string{}
	for _, key := range []string{"latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"} {
		if name := strings.TrimSpace(mappingNames[key]); name != "" {
			required[key] = name
		}
	}
	return required
}

func resolveColumnMappingIndexes(columns []string, mappingNames map[string]string) (map[string]int, error) {
	index, err := buildColumnIndexByNormalizedName(columns)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for key, name := range mappingNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		idx, ok := index[normalizedColumnKey(name)]
		if !ok {
			return nil, fmt.Errorf("mapovaný stĺpec %s: %q nie je vo výslednej schéme", key, name)
		}
		out[key] = idx
	}
	return out, nil
}

func appendCSVSourceIndexColumn(data *CSVData, loaded []*CSVData) *CSVData {
	if data == nil || len(loaded) <= 1 {
		return data
	}
	out := data.clone()
	out.Columns = append(out.Columns, csvSourceIndexColumn)
	rowOffset := 0
	for sourceIdx, loadedData := range loaded {
		for i := 0; i < len(loadedData.Rows) && rowOffset+i < len(out.Rows); i++ {
			out.Rows[rowOffset+i] = append(out.Rows[rowOffset+i], strconv.Itoa(sourceIdx))
		}
		rowOffset += len(loadedData.Rows)
	}
	return out
}

func LoadAndMergeCSVFilesForProcessing(ctx context.Context, paths []string, cfg ProcessingConfig) (*CSVData, map[string]int, error) {
	paths, loaded, err := loadCSVFilesForMerge(ctx, paths)
	if err != nil {
		return nil, nil, err
	}
	previewColumns, err := buildUnionColumns(loaded, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("CSV súbory nie sú kompatibilné: %w", err)
	}
	mappingNames, err := resolveColumnMappingNames(previewColumns, cfg)
	if err != nil {
		return nil, nil, err
	}
	opts := CSVMergeOptions{
		RequiredColumnNames:  requiredMappingNames(mappingNames),
		DisplayNameOverrides: displayOverridesFromMappingNames(mappingNames),
		EquivalentColumnKeys: equivalentColumnKeysFromMappingNames(mappingNames),
	}
	if err := validateRequiredColumnsInEveryFile(paths, loaded, opts.RequiredColumnNames, opts.EquivalentColumnKeys); err != nil {
		return nil, nil, err
	}
	mergedColumns, err := buildUnionColumns(loaded, opts.DisplayNameOverrides, opts.EquivalentColumnKeys)
	if err != nil {
		return nil, nil, fmt.Errorf("CSV súbory nie sú kompatibilné: %w", err)
	}
	var out *CSVData
	if len(loaded) == 1 {
		out = loaded[0].clone()
	} else {
		out, err = mergeCSVDataRowsByName(mergedColumns, loaded[0], loaded[1:], opts.EquivalentColumnKeys)
		if err != nil {
			return nil, nil, err
		}
		out.FileInfo = loaded[0].FileInfo
		out.FileInfo.OriginalHeader = strings.Join(out.Columns, ";")
		out.InputRadioTech = DetectInputRadioTech(out.Columns)
	}
	out = appendCSVSourceIndexColumn(out, loaded)
	resolvedMapping, err := resolveColumnMappingIndexes(out.Columns, mappingNames)
	if err != nil {
		return nil, nil, err
	}
	return out, resolvedMapping, nil
}
