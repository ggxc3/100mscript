package backend

import (
	"fmt"
	"slices"
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

// LoadAndMergeCSVFiles loads one or more semicolon CSV files and appends rows when len(paths) > 1.
// All files must have identical column names in the same order (after loader normalization).
func LoadAndMergeCSVFiles(paths []string) (*CSVData, error) {
	paths = NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("žiadna cesta k CSV súboru")
	}
	if len(paths) == 1 {
		return LoadCSVFile(paths[0])
	}

	loaded := make([]*CSVData, 0, len(paths))
	for _, p := range paths {
		d, err := LoadCSVFile(p)
		if err != nil {
			return nil, fmt.Errorf("načítanie %q: %w", p, err)
		}
		loaded = append(loaded, d)
	}

	base := loaded[0].Columns
	for i := 1; i < len(loaded); i++ {
		if !columnsMatch(base, loaded[i].Columns) {
			return nil, fmt.Errorf(
				"CSV súbory nie sú kompatibilné: stĺpce sa nezhodujú medzi %q a %q (potrebné rovnaké názvy a poradie stĺpcov; nemožno miešať napr. LTE a 5G export s rôznymi hlavičkami)",
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
