package backend

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

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

func columnsMatch(a, b []string) bool {
	return slices.Equal(a, b)
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

// LoadAndMergeCSVFiles loads one or more semicolon CSV files and appends rows when len(paths) > 1.
// All files must have identical column names in the same order (after loader normalization).
func LoadAndMergeCSVFiles(ctx context.Context, paths []string) (*CSVData, error) {
	paths = NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("žiadna cesta k CSV súboru")
	}
	if len(paths) == 1 {
		return LoadCSVFile(paths[0])
	}

	loaded := make([]*CSVData, 0, len(paths))
	nPaths := len(paths)
	for i, p := range paths {
		d, err := LoadCSVFile(p)
		if err != nil {
			return nil, fmt.Errorf("načítanie %q: %w", p, err)
		}
		loaded = append(loaded, d)
		emitProcessingProgress(ctx, "load_csv", float64(i+1)/float64(nPaths)*100)
	}

	base := loaded[0].Columns
	for i := 1; i < len(loaded); i++ {
		if !columnsMatch(base, loaded[i].Columns) {
			return nil, fmt.Errorf(
				"CSV súbory nie sú kompatibilné: celá hlavička (všetky stĺpce v rovnakom poradí) sa musí zhodovať medzi %q a %q — napr. iné názvy frekvencie (EARFCN vs NR-ARFCN) sú už rozlišné stĺpce",
				paths[0], paths[i],
			)
		}
	}

	out := mergeCSVDataRows(loaded[0], loaded[1:])
	out.FileInfo = loaded[0].FileInfo
	return out, nil
}

func mergeCSVDataRows(first *CSVData, rest []*CSVData) *CSVData {
	totalRows := len(first.Rows)
	for _, d := range rest {
		totalRows += len(d.Rows)
	}
	outRows := make([][]string, 0, totalRows)
	for _, row := range first.Rows {
		outRows = append(outRows, append([]string(nil), row...))
	}
	for _, d := range rest {
		for _, row := range d.Rows {
			outRows = append(outRows, append([]string(nil), row...))
		}
	}
	return &CSVData{
		Columns:  append([]string(nil), first.Columns...),
		Rows:     outRows,
		FileInfo: first.FileInfo,
	}
}
