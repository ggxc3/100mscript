package backend

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const frequencyHzColumn = "Frekvencia_Hz"
const technologyColumn = "technologia"
const frequencySourceColumn = "Zdrojovy_subor"

var plmnValueRE = regexp.MustCompile(`^\s*[0-9]{3}\s*/\s*([0-9]{1,3})\s*$`)

func finiteNumber(raw string) (float64, bool) {
	v, ok := parseNumberString(raw)
	return v, ok && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func validateFrequencyConfig(cfg ProcessingConfig) error {
	if len(cfg.FrequencyInputs) == 0 {
		return fmt.Errorf("frekvenčný režim: pridaj aspoň jeden CSV súbor a vyber jeho technológiu")
	}
	if cfg.MobileModeEnabled || cfg.IncludeEmptyZones || cfg.AddCustomOperators {
		return fmt.Errorf("frekvenčný režim spracúva namerané frekvencie; vypni mobile režim, prázdne zóny a vlastných operátorov")
	}
	if cfg.ZoneMode != "segments" && cfg.ZoneMode != "center" && cfg.ZoneMode != "original" {
		return fmt.Errorf("neplatný režim zón: %q", cfg.ZoneMode)
	}
	if cfg.ZoneSizeM <= 0 || math.IsNaN(cfg.ZoneSizeM) || math.IsInf(cfg.ZoneSizeM, 0) {
		return fmt.Errorf("veľkosť zóny/úseku musí byť kladné konečné číslo")
	}
	for _, bv := range []float64{cfg.FrequencyLTEBV, cfg.Frequency5GBV} {
		if bv < 0 || math.IsNaN(bv) || math.IsInf(bv, 0) || math.IsInf(bv*1e6, 0) {
			return fmt.Errorf("bV musí byť nezáporné konečné číslo v MHz")
		}
	}
	seen := map[string]bool{}
	for _, input := range cfg.FrequencyInputs {
		path := strings.TrimSpace(input.FilePath)
		if path == "" {
			return fmt.Errorf("chýba cesta k frekvenčnému CSV")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if seen[abs] {
			return fmt.Errorf("CSV %q je vybrané viackrát", path)
		}
		seen[abs] = true
		if input.Technology != "lte" && input.Technology != "5g" {
			return fmt.Errorf("CSV %q: vyber technológiu LTE alebo 5G", path)
		}
		token := normalizeHeaderToken(input.FrequencyColumn)
		if token == "" || token == "earfcn" || token == "nrarfcn" {
			return fmt.Errorf("CSV %q: vyber fyzickú frekvenciu v Hz (Frequency alebo SSRef), nie číslo kanála", path)
		}
	}
	return nil
}

func frequencyPLMNIndexes(data *CSVData, selected string) ([]int, error) {
	if selected != "" {
		idx := data.columnIndexByName(selected)
		if idx < 0 {
			return nil, fmt.Errorf("stĺpec PLMN %q neexistuje", selected)
		}
		return []int{idx}, nil
	}
	var out []int
	for i, col := range data.Columns {
		token := normalizeHeaderToken(col)
		// ROMES 2100 puts PLMN one position beyond the named Add. PLMNs column.
		if strings.Contains(token, "plmn") || isAutoExtraColumn(col) {
			out = append(out, i)
		}
	}
	return out, nil
}

func correctedPLMNMNC(row []string, indexes []int, explicit bool) (string, error) {
	result := ""
	for _, idx := range indexes {
		raw := strings.TrimSpace(cellAt(row, idx))
		if raw == "" {
			continue
		}
		m := plmnValueRE.FindStringSubmatch(raw)
		if m == nil {
			if explicit || strings.Contains(raw, "/") {
				return "", fmt.Errorf("nejednoznačná alebo neplatná hodnota PLMN %q; očakáva sa napr. 231/2", raw)
			}
			continue
		}
		n, _ := strconv.Atoi(m[1])
		value := strconv.Itoa(n)
		if result != "" && result != value {
			return "", fmt.Errorf("viac rozdielnych PLMN; vyber konkrétny stĺpec pre opravu MNC")
		}
		result = value
	}
	return result, nil
}

// Load per-source metadata before union alignment, so technology, PLMN, channel
// numbers and physical frequency can never leak between LTE and NR schemas.
type frequencyLoadedData struct {
	*CSVData
	exportColumns map[string][]string
}

const frequencyInternalPrefix = "__frequency_"

var frequencyLogicalKeys = []string{"latitude", "longitude", "pci", "mcc", "mnc", "rsrp", "sinr"}

func loadFrequencyData(ctx context.Context, cfg ProcessingConfig) (*frequencyLoadedData, map[string]int, int, error) {
	exportColumns := map[string][]string{}
	exportSeen := map[string]map[string]bool{"LTE": {}, "5G": {}}
	loaded := make([]*CSVData, 0, len(cfg.FrequencyInputs))
	corrected := 0
	mappingsByTechnology := map[string]map[string]string{}
	var legacyMappingNames map[string]string
	for source, input := range cfg.FrequencyInputs {
		d, err := LoadCSVFile(input.FilePath)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("CSV %q: %w", input.FilePath, err)
		}
		if _, err := buildColumnIndexByNormalizedName(d.Columns); err != nil {
			return nil, nil, 0, fmt.Errorf("CSV %q: %w", input.FilePath, err)
		}
		mappingNames, exists := mappingsByTechnology[input.Technology]
		if !exists {
			mappingNames = map[string]string{}
			if cfg.FrequencyColumnMappings != nil {
				selected, ok := cfg.FrequencyColumnMappings[input.Technology]
				if !ok {
					return nil, nil, 0, fmt.Errorf("CSV %q: chýba mapovanie pre %s", input.FilePath, input.Technology)
				}
				for key, name := range selected {
					mappingNames[key] = strings.TrimSpace(name)
				}
			} else {
				// Compatibility for existing callers. New UI sends independent
				// technology maps and never consults the standard-mode mapping.
				if legacyMappingNames == nil {
					legacyMappingNames, err = resolveColumnMappingNames(d.Columns, cfg)
					if err != nil {
						return nil, nil, 0, err
					}
				}
				for key, name := range legacyMappingNames {
					mappingNames[key] = name
				}
				if len(mappingNames) == 0 {
					for key, idx := range BuildColumnMappingFromHeaders(d.Columns) {
						mappingNames[key] = d.Columns[idx]
					}
					if input.Technology == "lte" {
						for key, name := range map[string]string{"rsrp": "RSRP", "sinr": "SINR"} {
							if indexOf(d.Columns, name) >= 0 {
								mappingNames[key] = name
							}
						}
					}
				}
			}
			delete(mappingNames, "frequency")
			mappingsByTechnology[input.Technology] = mappingNames
		}
		localMapping := map[string]int{}
		equivalents := equivalentColumnKeysFromMappingNames(mappingNames)
		for _, key := range []string{"latitude", "longitude", "pci", "mcc", "mnc", "rsrp", "sinr"} {
			name := mappingNames[key]
			if name == "" && key == "sinr" {
				continue
			}
			idx := -1
			for i, col := range d.Columns {
				if normalizeHeaderToken(col) == normalizeHeaderToken(name) {
					idx = i
					break
				}
			}
			if idx < 0 {
				for i, col := range d.Columns {
					if canonicalColumnKey(normalizedColumnKey(col), equivalents) == normalizedColumnKey(name) && name != "" {
						if idx >= 0 {
							return nil, nil, 0, fmt.Errorf("CSV %q: nejednoznačné mapovanie %s", input.FilePath, key)
						}
						idx = i
					}
				}
			}
			if idx < 0 {
				if key == "sinr" {
					continue
				}
				return nil, nil, 0, fmt.Errorf("CSV %q: chýba mapovaný stĺpec %s (%s)", input.FilePath, key, name)
			}
			localMapping[key] = idx
		}
		freqIdx := d.columnIndexByName(input.FrequencyColumn)
		if freqIdx < 0 {
			return nil, nil, 0, fmt.Errorf("CSV %q: stĺpec frekvencie %q neexistuje", input.FilePath, input.FrequencyColumn)
		}
		plmnIndexes, err := frequencyPLMNIndexes(d, input.PLMNColumn)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("CSV %q: %w", input.FilePath, err)
		}
		for _, reserved := range []string{frequencyHzColumn, technologyColumn, frequencySourceColumn, csvSourceIndexColumn, "original_excel_row"} {
			for _, col := range d.Columns {
				if normalizeHeaderToken(col) == normalizeHeaderToken(reserved) {
					return nil, nil, 0, fmt.Errorf("CSV %q obsahuje rezervovaný výstupný stĺpec %q", input.FilePath, col)
				}
			}
		}
		technology := map[string]string{"lte": "LTE", "5g": "5G"}[input.Technology]
		for _, col := range d.Columns {
			if strings.HasPrefix(col, frequencyInternalPrefix) {
				return nil, nil, 0, fmt.Errorf("CSV obsahuje rezervovaný stĺpec %q", col)
			}
			key := normalizedColumnKey(col)
			if !exportSeen[technology][key] {
				exportColumns[technology] = append(exportColumns[technology], col)
				exportSeen[technology][key] = true
			}
		}
		d.Columns = append(d.Columns, frequencyHzColumn, technologyColumn, frequencySourceColumn, csvSourceIndexColumn, "original_excel_row")
		for _, key := range frequencyLogicalKeys {
			d.Columns = append(d.Columns, frequencyInternalPrefix+key)
		}
		usableFrequency := false
		for i, row := range d.Rows {
			mnc, err := correctedPLMNMNC(row, plmnIndexes, input.PLMNColumn != "")
			if err != nil {
				return nil, nil, 0, fmt.Errorf("CSV %q, riadok %d: %w", input.FilePath, i+d.FileInfo.HeaderLine+2, err)
			}
			if mnc != "" {
				old, ok := finiteNumber(row[localMapping["mnc"]])
				newMNC, _ := strconv.ParseFloat(mnc, 64)
				if !ok || old != newMNC {
					corrected++
				}
				row[localMapping["mnc"]] = mnc
			}
			for _, key := range []string{"mcc", "mnc", "pci"} {
				idx := localMapping[key]
				if n, ok := finiteNumber(row[idx]); ok {
					row[idx] = normalizeIntLikeString(n)
				} else {
					row[idx] = ""
				}
			}
			frequency := ""
			if f, ok := finiteNumber(cellAt(row, freqIdx)); ok && f > 0 {
				frequency = normalizeIntLikeString(f)
				usableFrequency = true
			}
			// Invalid RSRP is excluded by the shared spatial processor.
			if _, ok := finiteNumber(row[localMapping["rsrp"]]); !ok {
				row[localMapping["rsrp"]] = ""
			}
			for _, key := range []string{"latitude", "longitude"} {
				raw := row[localMapping[key]]
				if raw != "" {
					if v, ok := finiteNumber(raw); !ok || (key == "latitude" && math.Abs(v) > 90) || (key == "longitude" && math.Abs(v) > 180) {
						return nil, nil, 0, fmt.Errorf("CSV %q, riadok %d: neplatná súradnica %s", input.FilePath, i+d.FileInfo.HeaderLine+2, key)
					}
				}
			}
			canonical := make([]string, len(frequencyLogicalKeys))
			for k, key := range frequencyLogicalKeys {
				if idx, ok := localMapping[key]; ok {
					canonical[k] = cellAt(row, idx)
				}
			}
			d.Rows[i] = append(row, frequency, technology, input.FilePath, strconv.Itoa(source), strconv.Itoa(i+d.FileInfo.HeaderLine+2))
			d.Rows[i] = append(d.Rows[i], canonical...)
		}
		if !usableFrequency {
			return nil, nil, 0, fmt.Errorf("CSV %q: stĺpec %q neobsahuje platné frekvencie v Hz", input.FilePath, input.FrequencyColumn)
		}
		loaded = append(loaded, d)
		emitProcessingProgress(ctx, "load_csv", float64(source+1)/float64(len(cfg.FrequencyInputs))*100)
	}
	columns, err := buildUnionColumns(loaded, nil, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	out, err := mergeCSVDataRowsByName(columns, loaded[0], loaded[1:], nil)
	if err != nil {
		return nil, nil, 0, err
	}
	mappingNames := map[string]string{}
	for _, key := range frequencyLogicalKeys {
		mappingNames[key] = frequencyInternalPrefix + key
	}
	mappingNames["frequency"] = frequencyHzColumn
	mapping, err := resolveColumnMappingIndexes(out.Columns, mappingNames)
	return &frequencyLoadedData{CSVData: out, exportColumns: exportColumns}, mapping, corrected, err
}

func runFrequencyProcessing(ctx context.Context, cfg ProcessingConfig) (ProcessingResult, error) {
	if err := validateFrequencyConfig(cfg); err != nil {
		return ProcessingResult{}, err
	}
	cfg.FilePath = cfg.FrequencyInputs[0].FilePath
	zonesFile, statsFile := frequencyOutputPaths(cfg)
	if err := validateFrequencyOutputPaths(cfg, zonesFile, statsFile); err != nil {
		return ProcessingResult{}, err
	}
	emitProcessingPhase(ctx, "load_csv")
	data, mapping, corrected, err := loadFrequencyData(ctx, cfg)
	if err != nil {
		return ProcessingResult{}, err
	}
	cfg.ColumnMapping = mapping
	emitProcessingPhase(ctx, "prepare_rows")
	if len(cfg.ExcludedOriginalRows) > 0 {
		return ProcessingResult{}, fmt.Errorf("vo frekvenčnom režime použi časové okná namiesto čísel riadkov")
	}
	emitProcessingPhase(ctx, "apply_filters")
	rules, err := loadFrequencyRules(cfg, data.Columns)
	if err != nil {
		return ProcessingResult{}, err
	}
	transformer, err := NewPyProjTransformer()
	if err != nil {
		return ProcessingResult{}, err
	}
	emitProcessingPhase(ctx, "compute_zones")
	ds, err := ProcessDataNative(ctx, data.CSVData, cfg, transformer)
	if err != nil {
		return ProcessingResult{}, err
	}
	cuts := timeWindowCutSummary{}
	if len(cfg.TimeWindows) > 0 {
		if _, strategy := timeSeriesForSorting(data.CSVData); strategy == "missing" {
			return ProcessingResult{}, fmt.Errorf("časové okná: v CSV chýbajú použiteľné časové údaje")
		}
		ds, cuts, err = applyTimeWindowsAfterSegmentation(ds, cfg.TimeWindows, cfg)
		if err != nil {
			return ProcessingResult{}, err
		}
	}
	emitProcessingPhase(ctx, "zone_stats")
	groups := groupFrequencyRows(ds)
	emitProcessingPhase(ctx, "export_files")
	result, err := saveFrequencyResults(ctx, ds, data.exportColumns, groups, cfg, rules, transformer, zonesFile, statsFile)
	result.CorrectedMNC = corrected
	result.ExcludedMeasurements = cuts.RemovedMeasurements
	result.ExcludedZones = cuts.RemovedZones
	emitProcessingProgress(ctx, "export_files", 100)
	return result, err
}
