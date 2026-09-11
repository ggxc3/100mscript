package backend

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type frequencyGroupKey struct{ Zone, MCC, MNC, Technology, Frequency string }
type frequencyGroup struct {
	key   frequencyGroupKey
	best  int
	count int
	pcis  map[string]struct{}
}

func groupFrequencyRows(ds *ProcessedDataset) []frequencyGroup {
	groups := map[frequencyGroupKey]*frequencyGroup{}
	techIdx := indexOf(ds.Columns, technologyColumn)
	for i, row := range ds.Rows {
		if row.MCC == "" || row.MNC == "" || row.PCI == "" || row.Frequency == "" {
			continue
		}
		key := frequencyGroupKey{row.ZonaKey, row.MCC, row.MNC, cellAt(row.Raw, techIdx), row.Frequency}
		g := groups[key]
		if g == nil {
			g = &frequencyGroup{key: key, best: i, pcis: map[string]struct{}{}}
			groups[key] = g
		}
		g.count++
		g.pcis[row.PCI] = struct{}{}
		// Stable ties: keep the first source/row in the user's input order.
		if row.RSRP > ds.Rows[g.best].RSRP {
			g.best = i
		}
	}
	out := make([]frequencyGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].key, out[j].key
		if a.Zone != b.Zone {
			if strings.HasPrefix(a.Zone, "segment_") && strings.HasPrefix(b.Zone, "segment_") {
				ai, _ := strconv.Atoi(strings.TrimPrefix(a.Zone, "segment_"))
				bi, _ := strconv.Atoi(strings.TrimPrefix(b.Zone, "segment_"))
				return ai < bi
			}
			ar, br := ds.Rows[out[i].best], ds.Rows[out[j].best]
			if ar.ZonaX != br.ZonaX {
				return ar.ZonaX < br.ZonaX
			}
			if ar.ZonaY != br.ZonaY {
				return ar.ZonaY < br.ZonaY
			}
			return a.Zone < b.Zone
		}
		for _, pair := range [][2]string{{a.MCC, b.MCC}, {a.MNC, b.MNC}, {a.Technology, b.Technology}, {a.Frequency, b.Frequency}} {
			if pair[0] != pair[1] {
				if c := compareNumericStringAsc(pair[0], pair[1]); c != 0 {
					return c < 0
				}
				return pair[0] < pair[1]
			}
		}
		return false
	})
	return out
}

func otherFrequencyPCIs(group frequencyGroup, selected string) string {
	var values []string
	for pci := range group.pcis {
		if pci != selected {
			values = append(values, pci)
		}
	}
	sort.Slice(values, func(i, j int) bool { return compareNumericStringAsc(values[i], values[j]) < 0 })
	return strings.Join(values, ", ")
}

func validateFrequencyOutputPaths(cfg ProcessingConfig, zones, stats string) error {
	same := func(a, b string) bool {
		aa, _ := filepath.Abs(a)
		bb, _ := filepath.Abs(b)
		if aa == bb {
			return true
		}
		sa, ea := os.Stat(a)
		sb, eb := os.Stat(b)
		return ea == nil && eb == nil && os.SameFile(sa, sb)
	}
	for _, path := range []string{zones, stats} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return fmt.Errorf("výstup %q je priečinok, vyber CSV súbor", path)
		}
	}
	if same(zones, stats) {
		return fmt.Errorf("výstupy 5G a LTE musia byť dva rôzne súbory")
	}
	for _, input := range cfg.FrequencyInputs {
		if same(zones, input.FilePath) || same(stats, input.FilePath) {
			return fmt.Errorf("výstup nesmie prepísať vstupný CSV %q", input.FilePath)
		}
	}
	return nil
}

// Both technologies use the same route but retain independent source schemas.
func frequencyOutputPaths(cfg ProcessingConfig) (string, string) {
	base := strings.TrimSuffix(cfg.FilePath, filepath.Ext(cfg.FilePath)) + normalizeOutputSuffix(cfg.OutputSuffix) + "_frequencies"
	nr, lte := base+"_5g.csv", base+"_lte.csv"
	if cfg.Frequency5GOutput != "" {
		nr = cfg.Frequency5GOutput
	}
	if cfg.FrequencyLTEOutput != "" {
		lte = cfg.FrequencyLTEOutput
	}
	return nr, lte
}

func saveFrequencyResults(ctx context.Context, ds *ProcessedDataset, schemas map[string][]string, groups []frequencyGroup, cfg ProcessingConfig, rules map[string][]frequencyRule, transformer *PyProjTransformer, nrFile, lteFile string) (ProcessingResult, error) {
	result := ProcessingResult{Frequency5GFile: nrFile, FrequencyLTEFile: lteFile, TotalZoneRows: len(groups)}
	type target struct {
		file        *os.File
		writer      *csv.Writer
		indexes     []int
		destination string
	}
	targets := map[string]*target{}
	for i, tech := range []string{"5G", "LTE"} {
		path := []string{nrFile, lteFile}[i]
		f, err := os.CreateTemp(filepath.Dir(path), ".frequency-*.csv")
		if err != nil {
			return result, err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		w := csv.NewWriter(f)
		w.Comma = ';'
		// Match the standard export: first physical line empty, header on line 2.
		if err := w.Write(nil); err != nil {
			return result, err
		}
		target := &target{file: f, writer: w, destination: path}
		targets[tech] = target
		header := append([]string(nil), schemas[tech]...)
		normalizedIndex, _ := buildColumnIndexByNormalizedName(ds.Columns)
		for _, col := range header {
			target.indexes = append(target.indexes, normalizedIndex[normalizedColumnKey(col)])
		}
		zoneColumn := "Usek"
		if cfg.ZoneMode != "segments" {
			zoneColumn = "Zona"
		}
		extra := []string{frequencyHzColumn, zoneColumn, zoneColumn + "_latitude", zoneColumn + "_longitude", frequencySourceColumn, "original_excel_row", "Pocet_merani", "Operator_sedi", "Ostatne_PCI"}
		for _, col := range extra {
			for _, existing := range header {
				if normalizedColumnKey(existing) == normalizedColumnKey(col) {
					return result, fmt.Errorf("CSV obsahuje rezervovaný výstupný stĺpec %q", col)
				}
			}
		}
		if err := w.Write(append(header, extra...)); err != nil {
			return result, err
		}
	}
	// All frequencies and technologies in a grid cell share its representative
	// position, independent of which measurement wins the RSRP selection.
	firstPoints := map[string]Point{}
	if cfg.ZoneMode == "original" {
		for _, row := range ds.Rows {
			if _, exists := firstPoints[row.ZonaKey]; !exists {
				firstPoints[row.ZonaKey] = Point{A: row.XMeters, B: row.YMeters}
			}
		}
	}
	points := make([]Point, len(groups))
	for i, g := range groups {
		r := ds.Rows[g.best]
		switch cfg.ZoneMode {
		case "center":
			points[i] = Point{A: r.ZonaX + cfg.ZoneSizeM/2, B: r.ZonaY + cfg.ZoneSizeM/2}
		case "original":
			points[i] = firstPoints[r.ZonaKey]
		default:
			points[i] = Point{A: r.ZonaX, B: r.ZonaY}
		}
	}
	locations, err := transformer.Inverse(ctx, points)
	if err != nil {
		return result, err
	}
	zones, operators := map[string]bool{}, map[string]bool{}
	for i, g := range groups {
		maybeEmitRowProgress(ctx, "export_files", i, len(groups))
		r := ds.Rows[g.best]
		zones[g.key.Zone], operators[r.OperatorKey] = true, true
		operatorMatches := frequencyFilterFlag(r, g.key.Technology, cfg, rules)
		target := targets[g.key.Technology]
		record := make([]string, 0, len(target.indexes)+9)
		for _, idx := range target.indexes {
			record = append(record, cellAt(r.Raw, idx))
		}
		record = append(record, r.Frequency, g.key.Zone, fmt.Sprintf("%.6f", locations[i].B), fmt.Sprintf("%.6f", locations[i].A),
			cellAt(r.Raw, indexOf(ds.Columns, frequencySourceColumn)), strconv.Itoa(r.OriginalExcelRow), strconv.Itoa(g.count), operatorMatches, otherFrequencyPCIs(g, r.PCI))
		if err := target.writer.Write(record); err != nil {
			return result, err
		}
	}
	for _, tech := range []string{"5G", "LTE"} {
		target := targets[tech]
		target.writer.Flush()
		if err := target.writer.Error(); err != nil {
			return result, err
		}
		if err := target.file.Close(); err != nil {
			return result, err
		}
	}
	for _, tech := range []string{"5G", "LTE"} {
		target := targets[tech]
		if err := os.Rename(target.file.Name(), target.destination); err != nil {
			return result, err
		}
	}
	result.UniqueZones, result.UniqueOperators = len(zones), len(operators)
	return result, nil
}
