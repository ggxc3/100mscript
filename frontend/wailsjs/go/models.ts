export namespace backend {
	
	export class CustomOperator {
	    mcc: string;
	    mnc: string;
	    pci: string;
	
	    static createFrom(source: any = {}) {
	        return new CustomOperator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mcc = source["mcc"];
	        this.mnc = source["mnc"];
	        this.pci = source["pci"];
	    }
	}
	export class TimeWindow {
	    start: string;
	    end: string;
	
	    static createFrom(source: any = {}) {
	        return new TimeWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}
	export class ProcessingConfig {
	    file_path: string;
	    input_file_paths?: string[];
	    column_mapping: Record<string, number>;
	    keep_original_rows: boolean;
	    excluded_original_rows: number[];
	    time_windows?: TimeWindow[];
	    zone_mode: string;
	    zone_size_m: number;
	    rsrp_threshold: number;
	    sinr_threshold: number;
	    include_empty_zones: boolean;
	    add_custom_operators: boolean;
	    custom_operators: CustomOperator[];
	    filter_paths?: string[];
	    output_suffix?: string;
	    output_zones_file_path?: string;
	    output_stats_file_path?: string;
	    mobile_mode_enabled: boolean;
	    mobile_lte_file_path?: string;
	    mobile_lte_file_paths?: string[];
	    mobile_time_tolerance_ms: number;
	    mobile_require_nr_yes: boolean;
	    mobile_nr_column_name: string;
	    progress_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProcessingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_path = source["file_path"];
	        this.input_file_paths = source["input_file_paths"];
	        this.column_mapping = source["column_mapping"];
	        this.keep_original_rows = source["keep_original_rows"];
	        this.excluded_original_rows = source["excluded_original_rows"];
	        this.time_windows = this.convertValues(source["time_windows"], TimeWindow);
	        this.zone_mode = source["zone_mode"];
	        this.zone_size_m = source["zone_size_m"];
	        this.rsrp_threshold = source["rsrp_threshold"];
	        this.sinr_threshold = source["sinr_threshold"];
	        this.include_empty_zones = source["include_empty_zones"];
	        this.add_custom_operators = source["add_custom_operators"];
	        this.custom_operators = this.convertValues(source["custom_operators"], CustomOperator);
	        this.filter_paths = source["filter_paths"];
	        this.output_suffix = source["output_suffix"];
	        this.output_zones_file_path = source["output_zones_file_path"];
	        this.output_stats_file_path = source["output_stats_file_path"];
	        this.mobile_mode_enabled = source["mobile_mode_enabled"];
	        this.mobile_lte_file_path = source["mobile_lte_file_path"];
	        this.mobile_lte_file_paths = source["mobile_lte_file_paths"];
	        this.mobile_time_tolerance_ms = source["mobile_time_tolerance_ms"];
	        this.mobile_require_nr_yes = source["mobile_require_nr_yes"];
	        this.mobile_nr_column_name = source["mobile_nr_column_name"];
	        this.progress_enabled = source["progress_enabled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProcessingResult {
	    zones_file: string;
	    stats_file: string;
	    include_empty_zones: boolean;
	    unique_zones: number;
	    unique_operators: number;
	    total_zone_rows: number;
	    min_x?: number;
	    max_x?: number;
	    min_y?: number;
	    max_y?: number;
	    range_x_m?: number;
	    range_y_m?: number;
	    theoretical_total_zones?: number;
	    coverage_percent?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProcessingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zones_file = source["zones_file"];
	        this.stats_file = source["stats_file"];
	        this.include_empty_zones = source["include_empty_zones"];
	        this.unique_zones = source["unique_zones"];
	        this.unique_operators = source["unique_operators"];
	        this.total_zone_rows = source["total_zone_rows"];
	        this.min_x = source["min_x"];
	        this.max_x = source["max_x"];
	        this.min_y = source["min_y"];
	        this.max_y = source["max_y"];
	        this.range_x_m = source["range_x_m"];
	        this.range_y_m = source["range_y_m"];
	        this.theoretical_total_zones = source["theoretical_total_zones"];
	        this.coverage_percent = source["coverage_percent"];
	    }
	}

}

export namespace main {
	
	export class AppInfo {
	    productName: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.productName = source["productName"];
	        this.version = source["version"];
	    }
	}
	export class CSVPreview {
	    filePaths: string[];
	    filePath: string;
	    columns: string[];
	    encoding: string;
	    headerLine: number;
	    originalHeader: string;
	    suggestedMapping: Record<string, number>;
	    inputRadioTech: string;
	
	    static createFrom(source: any = {}) {
	        return new CSVPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePaths = source["filePaths"];
	        this.filePath = source["filePath"];
	        this.columns = source["columns"];
	        this.encoding = source["encoding"];
	        this.headerLine = source["headerLine"];
	        this.originalHeader = source["originalHeader"];
	        this.suggestedMapping = source["suggestedMapping"];
	        this.inputRadioTech = source["inputRadioTech"];
	    }
	}
	export class PresetUIState {
	    input_csv_paths: string[];
	    mobile_lte_paths: string[];
	    custom_filter_paths: string[];
	    use_auto_filters: boolean;
	    use_additional_filters: boolean;
	    enable_time_selector: boolean;
	    custom_operators_text: string;
	    column_mapping: Record<string, number>;
	    time_windows: backend.TimeWindow[];
	
	    static createFrom(source: any = {}) {
	        return new PresetUIState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_csv_paths = source["input_csv_paths"];
	        this.mobile_lte_paths = source["mobile_lte_paths"];
	        this.custom_filter_paths = source["custom_filter_paths"];
	        this.use_auto_filters = source["use_auto_filters"];
	        this.use_additional_filters = source["use_additional_filters"];
	        this.enable_time_selector = source["enable_time_selector"];
	        this.custom_operators_text = source["custom_operators_text"];
	        this.column_mapping = source["column_mapping"];
	        this.time_windows = this.convertValues(source["time_windows"], backend.TimeWindow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProcessingPreset {
	    schemaVersion: number;
	    id: string;
	    name: string;
	    inputRadioTech: string;
	    createdAt: string;
	    updatedAt: string;
	    config: backend.ProcessingConfig;
	    uiState: PresetUIState;
	
	    static createFrom(source: any = {}) {
	        return new ProcessingPreset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaVersion = source["schemaVersion"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.inputRadioTech = source["inputRadioTech"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.config = this.convertValues(source["config"], backend.ProcessingConfig);
	        this.uiState = this.convertValues(source["uiState"], PresetUIState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SavePresetRequest {
	    id?: string;
	    name: string;
	    inputRadioTech: string;
	    config: backend.ProcessingConfig;
	    uiState: PresetUIState;
	
	    static createFrom(source: any = {}) {
	        return new SavePresetRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.inputRadioTech = source["inputRadioTech"];
	        this.config = this.convertValues(source["config"], backend.ProcessingConfig);
	        this.uiState = this.convertValues(source["uiState"], PresetUIState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class DefaultOutputPathsResult {
	    zones: string;
	    stats: string;
	
	    static createFrom(source: any = {}) {
	        return new DefaultOutputPathsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zones = source["zones"];
	        this.stats = source["stats"];
	    }
	}

}

