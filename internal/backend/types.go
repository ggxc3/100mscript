package backend

type CustomOperator struct {
	MCC string `json:"mcc"`
	MNC string `json:"mnc"`
	PCI string `json:"pci"`
}

type ProcessingConfig struct {
	FilePath              string           `json:"file_path"`
	ColumnMapping         map[string]int   `json:"column_mapping"`
	KeepOriginalRows      bool             `json:"keep_original_rows"`
	ZoneMode              string           `json:"zone_mode"` // center | original | segments
	ZoneSizeM             float64          `json:"zone_size_m"`
	RSRPThreshold         float64          `json:"rsrp_threshold"`
	SINRThreshold         float64          `json:"sinr_threshold"`
	IncludeEmptyZones     bool             `json:"include_empty_zones"`
	AddCustomOperators    bool             `json:"add_custom_operators"`
	CustomOperators       []CustomOperator `json:"custom_operators"`
	FilterPaths           []string         `json:"filter_paths,omitempty"`
	OutputSuffix          string           `json:"output_suffix,omitempty"`
	MobileModeEnabled     bool             `json:"mobile_mode_enabled"`
	MobileLTEFilePath     string           `json:"mobile_lte_file_path,omitempty"`
	MobileTimeToleranceMS int              `json:"mobile_time_tolerance_ms"`
	MobileRequireNRYES    bool             `json:"mobile_require_nr_yes"`
	MobileNRColumnName    string           `json:"mobile_nr_column_name"`
	ProgressEnabled       bool             `json:"progress_enabled"`
}

type ProcessingResult struct {
	ZonesFile             string   `json:"zones_file"`
	StatsFile             string   `json:"stats_file"`
	IncludeEmptyZones     bool     `json:"include_empty_zones"`
	UniqueZones           int      `json:"unique_zones"`
	UniqueOperators       int      `json:"unique_operators"`
	TotalZoneRows         int      `json:"total_zone_rows"`
	MinX                  *float64 `json:"min_x"`
	MaxX                  *float64 `json:"max_x"`
	MinY                  *float64 `json:"min_y"`
	MaxY                  *float64 `json:"max_y"`
	RangeXM               *float64 `json:"range_x_m"`
	RangeYM               *float64 `json:"range_y_m"`
	TheoreticalTotalZones *float64 `json:"theoretical_total_zones"`
	CoveragePercent       *float64 `json:"coverage_percent"`
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
