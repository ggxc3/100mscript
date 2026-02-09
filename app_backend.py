# -*- coding: utf-8 -*-

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Callable, Dict, List, Optional, Sequence, Tuple

from filters import (
    apply_filters,
    load_filter_rules,
    load_filter_rules_from_paths,
    maybe_dump_filtered_df,
)
from io_utils import get_output_suffix, load_csv_file
from outputs import add_custom_operators, save_stats, save_zone_results
from processing import calculate_zone_stats, process_data

StatusCallback = Optional[Callable[[str], None]]


@dataclass
class ProcessingConfig:
    file_path: str
    column_mapping: Dict[str, int]
    keep_original_rows: bool = False
    zone_mode: str = "center"  # center | original | segments
    zone_size_m: float = 100
    rsrp_threshold: float = -110
    sinr_threshold: float = -5
    include_empty_zones: bool = False
    add_custom_operators: bool = False
    custom_operators: List[Tuple[str, str, str]] = field(default_factory=list)
    filter_rules: Optional[List[dict]] = None
    filter_paths: Optional[Sequence[str]] = None
    output_suffix: Optional[str] = None
    progress_enabled: bool = True


@dataclass
class ProcessingResult:
    zones_file: str
    stats_file: str
    include_empty_zones: bool
    unique_zones: int
    unique_operators: int
    total_zone_rows: int
    min_x: Optional[float] = None
    max_x: Optional[float] = None
    min_y: Optional[float] = None
    max_y: Optional[float] = None
    range_x_m: Optional[float] = None
    range_y_m: Optional[float] = None
    theoretical_total_zones: Optional[float] = None
    coverage_percent: Optional[float] = None


class ProcessingError(RuntimeError):
    pass


def _normalize_output_suffix(value: Optional[str]) -> str:
    if value is None:
        return get_output_suffix()
    suffix = str(value).strip()
    if suffix and not suffix.startswith("_"):
        suffix = "_" + suffix
    return suffix


def _emit(status_callback: StatusCallback, message: str) -> None:
    if status_callback:
        status_callback(message)


def _load_rules(filter_paths: Optional[Sequence[str]], filter_rules: Optional[List[dict]]):
    if filter_rules is not None:
        return filter_rules
    if filter_paths is None:
        return load_filter_rules()
    if not filter_paths:
        return []
    return load_filter_rules_from_paths(filter_paths)


def run_processing(config: ProcessingConfig, status_callback: StatusCallback = None) -> ProcessingResult:
    _emit(status_callback, "Načítavam CSV súbor...")
    df, file_info = load_csv_file(config.file_path)
    if df is None:
        raise ProcessingError("Nepodarilo sa načítať CSV súbor.")

    _emit(status_callback, "Načítavam filtre...")
    filter_rules = _load_rules(config.filter_paths, config.filter_rules)
    if filter_rules:
        _emit(status_callback, f"Aplikujem {len(filter_rules)} filtrov...")
        df = apply_filters(
            df,
            file_info,
            filter_rules,
            config.keep_original_rows,
            config.column_mapping,
        )
        maybe_dump_filtered_df(df, config.file_path)

    output_suffix = _normalize_output_suffix(config.output_suffix)
    use_zone_center = config.zone_mode == "center"

    header_line = file_info.get("header_line", 0) if file_info else 0
    _emit(status_callback, "Spracovávam merania...")
    processed_df, column_names, segment_meta = process_data(
        df,
        config.column_mapping,
        header_line,
        config.zone_mode,
        config.zone_size_m,
        progress_enabled=config.progress_enabled,
    )

    _emit(status_callback, "Počítam štatistiky zón/úsekov...")
    zone_stats = calculate_zone_stats(
        processed_df,
        config.column_mapping,
        column_names,
        config.rsrp_threshold,
        config.sinr_threshold,
        config.zone_mode,
        config.zone_size_m,
        progress_enabled=config.progress_enabled,
    )

    _emit(status_callback, "Ukladám výstup zón...")
    include_empty_zones, processed_zones, unique_zones = save_zone_results(
        zone_stats,
        config.file_path,
        processed_df,
        config.column_mapping,
        column_names,
        file_info,
        use_zone_center,
        config.zone_mode,
        output_suffix,
        segment_meta,
        config.zone_size_m,
        generate_empty_zones=config.include_empty_zones,
        progress_enabled=config.progress_enabled,
    )

    if include_empty_zones:
        _emit(status_callback, "Pridávam vlastných operátorov...")
        zone_stats, _ = add_custom_operators(
            zone_stats,
            processed_df,
            config.column_mapping,
            column_names,
            config.file_path.replace('.csv', f'{output_suffix}_zones.csv') if output_suffix else config.file_path.replace('.csv', '_zones.csv'),
            use_zone_center,
            processed_zones,
            unique_zones,
            config.zone_size_m,
            zone_mode=config.zone_mode,
            segment_meta=segment_meta,
            add_operators=config.add_custom_operators,
            custom_operators=config.custom_operators,
            progress_enabled=config.progress_enabled,
        )

    _emit(status_callback, "Ukladám sumárne štatistiky...")
    save_stats(
        zone_stats,
        config.file_path,
        include_empty_zones,
        config.rsrp_threshold,
        config.sinr_threshold,
        output_suffix,
        config.zone_mode,
        segment_meta,
        progress_enabled=config.progress_enabled,
    )

    zones_file = config.file_path.replace('.csv', f'{output_suffix}_zones.csv') if output_suffix else config.file_path.replace('.csv', '_zones.csv')
    stats_file = config.file_path.replace('.csv', f'{output_suffix}_stats.csv') if output_suffix else config.file_path.replace('.csv', '_stats.csv')

    unique_zones_count = zone_stats['zona_key'].nunique()
    min_x = max_x = min_y = max_y = None
    range_x_m = range_y_m = theoretical_total_zones = coverage_percent = None
    if config.zone_mode != "segments" and not zone_stats.empty:
        min_x = float(zone_stats['zona_x'].min())
        max_x = float(zone_stats['zona_x'].max())
        min_y = float(zone_stats['zona_y'].min())
        max_y = float(zone_stats['zona_y'].max())
        range_x_m = max_x - min_x + config.zone_size_m
        range_y_m = max_y - min_y + config.zone_size_m
        theoretical_total_zones = (range_x_m / config.zone_size_m) * (range_y_m / config.zone_size_m)
        if theoretical_total_zones > 0:
            coverage_percent = (unique_zones_count / theoretical_total_zones) * 100

    return ProcessingResult(
        zones_file=zones_file,
        stats_file=stats_file,
        include_empty_zones=include_empty_zones,
        unique_zones=unique_zones_count,
        unique_operators=zone_stats[['mcc', 'mnc']].drop_duplicates().shape[0],
        total_zone_rows=len(zone_stats),
        min_x=min_x,
        max_x=max_x,
        min_y=min_y,
        max_y=max_y,
        range_x_m=range_x_m,
        range_y_m=range_y_m,
        theoretical_total_zones=theoretical_total_zones,
        coverage_percent=coverage_percent,
    )
