package backend

import (
	"context"
	"fmt"
	"os"
)

func nativeSupported(cfg ProcessingConfig) bool {
	return true
}

func runProcessingNative(ctx context.Context, cfg ProcessingConfig) (ProcessingResult, error) {
	paths := InputPathsFromConfig(cfg)
	if len(paths) == 0 {
		return ProcessingResult{}, fmt.Errorf("chýba vstupný CSV súbor")
	}
	emitProcessingPhase(ctx, "load_csv")
	data, err := LoadAndMergeCSVFiles(ctx, paths)
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("načítanie CSV: %w", err)
	}
	emitProcessingProgress(ctx, "load_csv", 100)
	if len(paths) > 1 {
		if sorted, ok := sortMergedCSVRowsByTime(data); ok {
			data = sorted
		}
	}
	if len(paths) == 1 {
		data, err = ensureOriginalExcelRowColumn(data)
	} else {
		data, err = assignSequentialOriginalExcelRows(data)
	}
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("original excel row: %w", err)
	}
	if len(cfg.ExcludedOriginalRows) > 0 {
		data, _, err = excludeRowsByOriginalExcelRow(data, cfg.ExcludedOriginalRows)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("exclude original rows: %w", err)
		}
	}
	emitProcessingPhase(ctx, "prepare_rows")

	rules, err := loadRulesForConfig(cfg)
	if err != nil {
		return ProcessingResult{}, err
	}
	emitProcessingPhase(ctx, "apply_filters")
	if len(rules) > 0 {
		data, err = ApplyFiltersCSV(ctx, data, rules, cfg.KeepOriginalRows, cfg.ColumnMapping)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("apply filters: %w", err)
		}
	}
	if cfg.MobileModeEnabled {
		emitProcessingPhase(ctx, "mobile_sync")
		ltePaths := MobileLTEPathsFromConfig(cfg)
		if len(ltePaths) == 0 {
			return ProcessingResult{}, fmt.Errorf("mobile mode is enabled but no LTE CSV path(s) were provided")
		}
		data, _, err = syncMobileNRFromLTECSVNative(
			ctx,
			data,
			cfg.ColumnMapping,
			ltePaths,
			cfg.MobileNRColumnName,
			cfg.MobileTimeToleranceMS,
			rules,
			cfg.KeepOriginalRows,
		)
		if err != nil {
			return ProcessingResult{}, err
		}
		// Legacy flag kept in config for backward compatibility, but the NR=YES-only
		// filtering is intentionally disabled because it can silently drop operators.
	}

	transformer, err := NewPyProjTransformer()
	if err != nil {
		return ProcessingResult{}, err
	}

	emitProcessingPhase(ctx, "compute_zones")
	ds, err := ProcessDataNative(ctx, data, cfg, transformer)
	if err != nil {
		return ProcessingResult{}, err
	}
	emitProcessingPhase(ctx, "zone_stats")
	zoneStats, err := CalculateZoneStatsNative(ctx, ds, cfg, transformer)
	if err != nil {
		return ProcessingResult{}, err
	}

	zonesFile, statsFile, _ := OutputPathsForProcessing(cfg)
	if zonesFile == statsFile {
		return ProcessingResult{}, fmt.Errorf("výstup zón a štatistík musí byť do dvoch rôznych súborov")
	}
	emitProcessingPhase(ctx, "export_files")
	exportOutcome, err := SaveZoneResultsNative(ctx, ds, zoneStats, cfg, transformer, zonesFile)
	if err != nil {
		return ProcessingResult{}, err
	}
	zoneStats = exportOutcome.ZoneStats
	if err := SaveStatsNative(zoneStats, cfg, statsFile, exportOutcome.UniqueZones); err != nil {
		return ProcessingResult{}, err
	}
	emitProcessingProgress(ctx, "export_files", 100)

	uniqueZones := map[string]struct{}{}
	uniqueOperators := map[string]struct{}{}
	for _, z := range zoneStats {
		uniqueZones[z.ZonaKey] = struct{}{}
		uniqueOperators[z.MCC+"_"+z.MNC] = struct{}{}
	}

	var minX, maxX, minY, maxY *float64
	var rangeXM, rangeYM, theoreticalTotalZones, coveragePercent *float64
	if cfg.ZoneMode != "segments" && len(zoneStats) > 0 {
		minXV, maxXV := zoneStats[0].ZonaX, zoneStats[0].ZonaX
		minYV, maxYV := zoneStats[0].ZonaY, zoneStats[0].ZonaY
		for _, z := range zoneStats[1:] {
			if z.ZonaX < minXV {
				minXV = z.ZonaX
			}
			if z.ZonaX > maxXV {
				maxXV = z.ZonaX
			}
			if z.ZonaY < minYV {
				minYV = z.ZonaY
			}
			if z.ZonaY > maxYV {
				maxYV = z.ZonaY
			}
		}
		rx := maxXV - minXV + cfg.ZoneSizeM
		ry := maxYV - minYV + cfg.ZoneSizeM
		ttz := (rx / cfg.ZoneSizeM) * (ry / cfg.ZoneSizeM)
		var cp *float64
		if ttz > 0 {
			v := (float64(len(uniqueZones)) / ttz) * 100
			cp = &v
		}
		minX, maxX = &minXV, &maxXV
		minY, maxY = &minYV, &maxYV
		rangeXM, rangeYM = &rx, &ry
		theoreticalTotalZones = &ttz
		coveragePercent = cp
	}

	return ProcessingResult{
		ZonesFile:             zonesFile,
		StatsFile:             statsFile,
		IncludeEmptyZones:     cfg.IncludeEmptyZones,
		UniqueZones:           len(uniqueZones),
		UniqueOperators:       len(uniqueOperators),
		TotalZoneRows:         len(zoneStats),
		MinX:                  minX,
		MaxX:                  maxX,
		MinY:                  minY,
		MaxY:                  maxY,
		RangeXM:               rangeXM,
		RangeYM:               rangeYM,
		TheoreticalTotalZones: theoreticalTotalZones,
		CoveragePercent:       coveragePercent,
	}, nil
}

func loadRulesForConfig(cfg ProcessingConfig) ([]FilterRule, error) {
	if cfg.FilterPaths == nil {
		// Mirror Python default: auto-discover filters from repo root/current dir
		cwd, _ := os.Getwd()
		paths, err := DiscoverFilterPaths(cwd)
		if err != nil {
			return nil, err
		}
		return LoadFilterRulesFromPaths(paths)
	}
	if len(cfg.FilterPaths) == 0 {
		return nil, nil
	}
	return LoadFilterRulesFromPaths(cfg.FilterPaths)
}
