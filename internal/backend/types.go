package backend

type CustomOperator struct {
	MCC string `json:"mcc"`
	MNC string `json:"mnc"`
	PCI string `json:"pci"`
}

type TimeWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// FrequencyInput requires an explicit technology selected by the user. Frequency
// is a physical frequency in Hz, never an EARFCN / NR-ARFCN channel number.
type FrequencyInput struct {
	FilePath        string `json:"file_path"`
	Technology      string `json:"technology"` // lte | 5g
	FrequencyColumn string `json:"frequency_column"`
	PLMNColumn      string `json:"plmn_column,omitempty"` // empty: detect named/trailing PLMN columns
}

type ProcessingConfig struct {
	FrequencyColumnMappings map[string]map[string]string `json:"frequency_column_mappings,omitempty"` // lte / 5g, independent source names

	Frequency5GOutput     string            `json:"frequency_5g_output_path,omitempty"`
	FrequencyLTEOutput    string            `json:"frequency_lte_output_path,omitempty"`
	FrequencyModeEnabled  bool              `json:"frequency_mode_enabled"`
	FrequencyInputs       []FrequencyInput  `json:"frequency_inputs,omitempty"`
	FrequencyLTEBW        float64           `json:"frequency_lte_bv_mhz"` // legacy JSON key retained for existing callers
	Frequency5GBW         float64           `json:"frequency_5g_bv_mhz"`  // BW is the +/- delta in MHz, not half a channel width
	FrequencyLTEFilters   []string          `json:"frequency_lte_filter_paths,omitempty"`
	Frequency5GFilters    []string          `json:"frequency_5g_filter_paths,omitempty"`
	FilePath              string            `json:"file_path"`
	InputFilePaths        []string          `json:"input_file_paths,omitempty"`
	ColumnMapping         map[string]int    `json:"column_mapping"`
	ColumnMappingNames    map[string]string `json:"column_mapping_names,omitempty"`
	KeepOriginalRows      bool              `json:"keep_original_rows"`
	ExcludedOriginalRows  []int             `json:"excluded_original_rows"`
	TimeWindows           []TimeWindow      `json:"time_windows,omitempty"`
	ZoneMode              string            `json:"zone_mode"` // center | original | segments
	ZoneSizeM             float64           `json:"zone_size_m"`
	RSRPThreshold         float64           `json:"rsrp_threshold"`
	SINRThreshold         float64           `json:"sinr_threshold"`
	IncludeEmptyZones     bool              `json:"include_empty_zones"`
	AddCustomOperators    bool              `json:"add_custom_operators"`
	CustomOperators       []CustomOperator  `json:"custom_operators"`
	FilterPaths           []string          `json:"filter_paths,omitempty"`
	OutputSuffix          string            `json:"output_suffix,omitempty"`
	OutputZonesFilePath   string            `json:"output_zones_file_path,omitempty"`
	OutputStatsFilePath   string            `json:"output_stats_file_path,omitempty"`
	MobileModeEnabled     bool              `json:"mobile_mode_enabled"`
	MobileNSALTEFilePath  string            `json:"mobile_nsa_lte_file_path,omitempty"`
	MobileNSALTEFilePaths []string          `json:"mobile_nsa_lte_file_paths,omitempty"`
	MobileTimeToleranceMS int               `json:"mobile_time_tolerance_ms"`
	MobileRequireNRYES    bool              `json:"mobile_require_nr_yes"`
	MobileNRColumnName    string            `json:"mobile_nr_column_name"`
	ProgressEnabled       bool              `json:"progress_enabled"`
}

type ProcessingResult struct {
	Frequency5GFile       string   `json:"frequency_5g_file,omitempty"`
	FrequencyLTEFile      string   `json:"frequency_lte_file,omitempty"`
	CorrectedMNC          int      `json:"corrected_mnc"`
	ZonesFile             string   `json:"zones_file"`
	StatsFile             string   `json:"stats_file"`
	IncludeEmptyZones     bool     `json:"include_empty_zones"`
	UniqueZones           int      `json:"unique_zones"`
	UniqueOperators       int      `json:"unique_operators"`
	TotalZoneRows         int      `json:"total_zone_rows"`
	ExcludedMeasurements  int      `json:"excluded_measurements"`
	ExcludedZones         int      `json:"excluded_zones"`
	MinX                  *float64 `json:"min_x"`
	MaxX                  *float64 `json:"max_x"`
	MinY                  *float64 `json:"min_y"`
	MaxY                  *float64 `json:"max_y"`
	RangeXM               *float64 `json:"range_x_m"`
	RangeYM               *float64 `json:"range_y_m"`
	TheoreticalTotalZones *float64 `json:"theoretical_total_zones"`
	CoveragePercent       *float64 `json:"coverage_percent"`
}

type TimeSelectorRow struct {
	OriginalRow int   `json:"original_row"`
	TimestampMS int64 `json:"timestamp_ms"`
}

type TimeSelectorData struct {
	Rows      []TimeSelectorRow `json:"rows"`
	TotalRows int               `json:"total_rows"`
	TimedRows int               `json:"timed_rows"`
	MinTimeMS int64             `json:"min_time_ms"`
	MaxTimeMS int64             `json:"max_time_ms"`
	Strategy  string            `json:"strategy"`
}

func DefaultProcessingConfig() ProcessingConfig {
	return ProcessingConfig{
		ZoneMode:              "segments",
		ZoneSizeM:             100,
		RSRPThreshold:         -110,
		SINRThreshold:         -5,
		MobileTimeToleranceMS: 1000,
		MobileRequireNRYES:    false,
		MobileNRColumnName:    "5G NR",
		ProgressEnabled:       true,
	}
}
