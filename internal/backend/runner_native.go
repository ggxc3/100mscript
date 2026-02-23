package backend

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func nativeSupported(cfg ProcessingConfig) bool {
	return true
}

func runProcessingNative(ctx context.Context, cfg ProcessingConfig) (ProcessingResult, error) {
	data, err := LoadCSVFile(cfg.FilePath)
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("load csv: %w", err)
	}

	rules, err := loadRulesForConfig(cfg)
	if err != nil {
		return ProcessingResult{}, err
	}
	if len(rules) > 0 {
		data, err = ApplyFiltersCSV(data, rules, cfg.KeepOriginalRows, cfg.ColumnMapping)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("apply filters: %w", err)
		}
	}
	if cfg.MobileModeEnabled {
		if strings.TrimSpace(cfg.MobileLTEFilePath) == "" {
			return ProcessingResult{}, fmt.Errorf("mobile mode is enabled but mobile_lte_file_path is empty")
		}
		data, _, err = syncMobileNRFromLTECSVNative(
			data,
			cfg.ColumnMapping,
			cfg.MobileLTEFilePath,
			cfg.MobileNRColumnName,
			cfg.MobileTimeToleranceMS,
			rules,
			cfg.KeepOriginalRows,
		)
		if err != nil {
			return ProcessingResult{}, err
		}
		if cfg.MobileRequireNRYES {
			data, err = filterRowsByNRYesNative(data, cfg.MobileNRColumnName)
			if err != nil {
				return ProcessingResult{}, err
			}
		}
	}

	transformer, err := NewPyProjTransformer()
	if err != nil {
		return ProcessingResult{}, err
	}

	ds, err := ProcessDataNative(ctx, data, cfg, transformer)
	if err != nil {
		return ProcessingResult{}, err
	}
	zoneStats, err := CalculateZoneStatsNative(ctx, ds, cfg, transformer)
	if err != nil {
		return ProcessingResult{}, err
	}

	zonesFile, statsFile, _ := outputPathsForConfig(cfg)
	exportOutcome, err := SaveZoneResultsNative(ctx, ds, zoneStats, cfg, transformer, zonesFile)
	if err != nil {
		return ProcessingResult{}, err
	}
	zoneStats = exportOutcome.ZoneStats
	if err := SaveStatsNative(zoneStats, cfg, statsFile, exportOutcome.UniqueZones); err != nil {
		return ProcessingResult{}, err
	}

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
