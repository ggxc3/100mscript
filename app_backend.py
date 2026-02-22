# -*- coding: utf-8 -*-

from __future__ import annotations

from dataclasses import dataclass, field
import re
from typing import Callable, Dict, List, Optional, Sequence, Tuple

import numpy as np
import pandas as pd

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
    mobile_mode_enabled: bool = False
    mobile_lte_file_path: Optional[str] = None
    mobile_time_tolerance_ms: int = 1000
    mobile_require_nr_yes: bool = True
    mobile_nr_column_name: str = "5G NR"
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


def _column_index_to_letter(index: int) -> str:
    """Prevedie 0-based index stĺpca na Excel písmeno (A, B, ..., AA...)."""
    if index < 0:
        return "?"
    value = index + 1
    letters = []
    while value > 0:
        value, remainder = divmod(value - 1, 26)
        letters.append(chr(ord("A") + remainder))
    return "".join(reversed(letters))


def _suggest_mapping_from_headers(columns: Sequence[str]) -> Dict[str, Tuple[int, str, str]]:
    """Nájde odporúčané stĺpce podľa názvov hlavičky."""
    candidates = {
        "latitude": ["Latitude"],
        "longitude": ["Longitude"],
        "frequency": ["NR-ARFCN", "EARFCN", "Frequency"],
        "pci": ["PCI"],
        "mcc": ["MCC"],
        "mnc": ["MNC"],
        "rsrp": ["SSS-RSRP", "RSRP"],
        "sinr": ["SSS-SINR", "SINR"],
    }
    lower_map = {str(name).strip().lower(): idx for idx, name in enumerate(columns)}
    suggestions = {}
    for key, names in candidates.items():
        for candidate in names:
            idx = lower_map.get(candidate.lower())
            if idx is not None:
                suggestions[key] = (idx, str(columns[idx]), _column_index_to_letter(idx))
                break
    return suggestions


def _numeric_count(series: pd.Series) -> int:
    normalized = series.astype(str).str.replace(",", ".", regex=False).str.strip()
    normalized = normalized.where(series.notna(), None)
    return int(pd.to_numeric(normalized, errors="coerce").notna().sum())


def _validate_column_mapping(df: pd.DataFrame, column_mapping: Dict[str, int]) -> None:
    required = ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp"]
    optional = ["sinr"]
    all_keys = required + [k for k in optional if k in column_mapping]

    for key in all_keys:
        idx = column_mapping.get(key)
        if not isinstance(idx, int) or idx < 0 or idx >= len(df.columns):
            raise ProcessingError(
                f"Neplatné mapovanie stĺpca '{key}' (index={idx}). "
                f"Súbor má {len(df.columns)} stĺpcov."
            )

    critical_numeric = ["latitude", "longitude", "frequency", "rsrp"]
    bad_keys = []
    for key in critical_numeric + ([k for k in optional if k in column_mapping]):
        idx = column_mapping[key]
        if _numeric_count(df.iloc[:, idx]) == 0:
            bad_keys.append(key)

    if bad_keys:
        bad_desc = ", ".join(
            f"{key}={_column_index_to_letter(column_mapping[key])}" for key in bad_keys
        )
        suggestions = _suggest_mapping_from_headers(list(df.columns))
        suggestion_parts = []
        for key in ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"]:
            if key in suggestions:
                idx, name, letter = suggestions[key]
                suggestion_parts.append(f"{key}={letter} ({name})")
        suggestion_text = ""
        if suggestion_parts:
            suggestion_text = " Odporúčané mapovanie podľa hlavičky: " + ", ".join(suggestion_parts) + "."

        raise ProcessingError(
            "Mapovanie stĺpcov pravdepodobne nesedí pre tento CSV súbor "
            f"(bez číselných hodnôt v: {bad_desc}).{suggestion_text}"
        )


def _normalize_header_token(value: str) -> str:
    return re.sub(r"[^a-z0-9]+", "", str(value).strip().lower())


def _find_column_name(columns: Sequence[str], candidates: Sequence[str]) -> Optional[str]:
    normalized_map = {
        _normalize_header_token(column): str(column)
        for column in columns
    }
    for candidate in candidates:
        column_name = normalized_map.get(_normalize_header_token(candidate))
        if column_name is not None:
            return column_name
    return None


def _normalize_key_series(series: pd.Series) -> pd.Series:
    normalized = series.astype("string").fillna("").str.strip()
    normalized = normalized.str.replace(",", ".", regex=False)
    normalized = normalized.str.replace(r"\.0+$", "", regex=True)
    return normalized.fillna("")


def _normalize_nr_series(series: pd.Series) -> pd.Series:
    normalized = series.astype("string").fillna("").str.strip().str.lower()
    yes_tokens = {"yes", "true", "1", "y", "t", "a", "ano", "áno"}
    no_tokens = {"no", "false", "0", "n", "f"}
    return pd.Series(
        np.where(
            normalized.isin(yes_tokens),
            "yes",
            np.where(normalized.isin(no_tokens), "no", ""),
        ),
        index=series.index,
        dtype="string",
    )


def _build_column_mapping_from_headers(columns: Sequence[str]) -> Dict[str, int]:
    suggestions = _suggest_mapping_from_headers(columns)
    return {key: value[0] for key, value in suggestions.items()}


def _build_time_ms_series(
    df: pd.DataFrame,
    utc_col: Optional[str],
    date_col: Optional[str],
    time_col: Optional[str],
) -> Tuple[pd.Series, str]:
    time_ms = pd.Series(np.nan, index=df.index, dtype="float64")

    if utc_col and utc_col in df.columns:
        utc_text = df[utc_col].astype("string").fillna("").str.strip()
        utc_numeric = pd.to_numeric(
            utc_text.str.replace(",", ".", regex=False),
            errors="coerce",
        )
        if utc_numeric.notna().any():
            median_abs = float(utc_numeric.dropna().abs().median())
            factor = 1.0 if median_abs >= 1e11 else 1000.0
            time_ms.loc[utc_numeric.notna()] = utc_numeric.loc[utc_numeric.notna()] * factor
            return time_ms, "utc_numeric"

        utc_datetime = pd.to_datetime(
            utc_text.where(utc_text != "", pd.NA),
            errors="coerce",
            dayfirst=True,
        )
        valid_utc_datetime = utc_datetime.notna()
        if valid_utc_datetime.any():
            time_ms.loc[valid_utc_datetime] = (
                utc_datetime.loc[valid_utc_datetime].astype("int64") // 1_000_000
            ).astype("float64")
            return time_ms, "utc_datetime"

    if date_col and time_col and date_col in df.columns and time_col in df.columns:
        date_text = df[date_col].astype("string").fillna("").str.strip()
        time_text = df[time_col].astype("string").fillna("").str.strip()
        datetime_text = date_text.str.cat(time_text, sep=" ")
        valid_datetime_text = (date_text != "") & (time_text != "")
        datetime_series = pd.to_datetime(
            datetime_text.where(valid_datetime_text, pd.NA),
            errors="coerce",
            dayfirst=True,
        )
        valid_datetime = datetime_series.notna()
        if valid_datetime.any():
            time_ms.loc[valid_datetime] = (
                datetime_series.loc[valid_datetime].astype("int64") // 1_000_000
            ).astype("float64")
            return time_ms, "date_time"

    return time_ms, "missing"


def _build_lte_time_lookup(lte_work: pd.DataFrame):
    lookup = {}
    for group_key, group_df in lte_work.groupby(["_mcc_key", "_mnc_key"], sort=False):
        times = group_df["_time_ms"].to_numpy(dtype=np.int64)
        order = np.argsort(times, kind="mergesort")
        times_sorted = times[order]
        scores_sorted = group_df["_nr_score"].to_numpy(dtype=np.int8)[order]
        yes_prefix = np.concatenate(([0], np.cumsum(scores_sorted == 2, dtype=np.int64)))
        no_prefix = np.concatenate(([0], np.cumsum(scores_sorted == 1, dtype=np.int64)))
        lookup[group_key] = (times_sorted, yes_prefix, no_prefix)
    return lookup


def _sync_mobile_nr_from_lte(
    df_5g: pd.DataFrame,
    column_mapping: Dict[str, int],
    lte_file_path: str,
    nr_column_name: str,
    time_tolerance_ms: int,
    filter_rules: Optional[List[dict]],
    keep_original_rows: bool,
) -> Tuple[pd.DataFrame, Dict[str, object]]:
    df_lte, lte_file_info = load_csv_file(lte_file_path)
    if df_lte is None:
        raise ProcessingError("Mobile režim: Nepodarilo sa načítať LTE CSV súbor.")

    if filter_rules:
        lte_mapping = _build_column_mapping_from_headers(list(df_lte.columns))
        df_lte = apply_filters(
            df_lte,
            lte_file_info,
            filter_rules,
            keep_original_rows,
            lte_mapping,
        )

    lte_columns = list(df_lte.columns)
    lte_mcc_col = _find_column_name(lte_columns, ["MCC"])
    lte_mnc_col = _find_column_name(lte_columns, ["MNC"])
    lte_nr_col = _find_column_name(lte_columns, ["5G NR", "5GNR", "NR"])
    if not lte_mcc_col or not lte_mnc_col or not lte_nr_col:
        raise ProcessingError(
            "Mobile režim: LTE súbor musí obsahovať stĺpce MCC, MNC a 5G NR."
        )

    lte_utc_col = _find_column_name(lte_columns, ["UTC"])
    lte_date_col = _find_column_name(lte_columns, ["Date"])
    lte_time_col = _find_column_name(lte_columns, ["Time"])

    fiveg_columns = list(df_5g.columns)
    try:
        fiveg_mcc_col = fiveg_columns[column_mapping["mcc"]]
        fiveg_mnc_col = fiveg_columns[column_mapping["mnc"]]
    except Exception as exc:
        raise ProcessingError(f"Mobile režim: Neplatné mapovanie stĺpcov pre 5G súbor ({exc}).")

    fiveg_utc_col = _find_column_name(fiveg_columns, ["UTC"])
    fiveg_date_col = _find_column_name(fiveg_columns, ["Date"])
    fiveg_time_col = _find_column_name(fiveg_columns, ["Time"])

    lte_work = pd.DataFrame({
        "_mcc_key": _normalize_key_series(df_lte[lte_mcc_col]),
        "_mnc_key": _normalize_key_series(df_lte[lte_mnc_col]),
        "_nr_raw": _normalize_nr_series(df_lte[lte_nr_col]),
    })
    lte_work["_time_ms"], lte_time_strategy = _build_time_ms_series(
        df_lte,
        lte_utc_col,
        lte_date_col,
        lte_time_col,
    )
    lte_work = lte_work[lte_work[["_mcc_key", "_mnc_key"]].ne("").all(axis=1)].copy()
    if lte_work.empty:
        raise ProcessingError("Mobile režim: LTE súbor neobsahuje použiteľné MCC/MNC hodnoty.")

    lte_work["_nr_score"] = np.where(
        lte_work["_nr_raw"] == "yes",
        2,
        np.where(lte_work["_nr_raw"] == "no", 1, 0),
    )
    if (lte_work["_nr_score"] == 2).sum() == 0:
        raise ProcessingError("Mobile režim: V LTE súbore sa nenašli žiadne riadky s 5G NR = yes.")

    fiveg_work = pd.DataFrame({
        "_mcc_key": _normalize_key_series(df_5g[fiveg_mcc_col]),
        "_mnc_key": _normalize_key_series(df_5g[fiveg_mnc_col]),
    })
    fiveg_work["_time_ms"], fiveg_time_strategy = _build_time_ms_series(
        df_5g,
        fiveg_utc_col,
        fiveg_date_col,
        fiveg_time_col,
    )

    valid_lte_time = lte_work["_time_ms"].notna()
    valid_5g_time = fiveg_work["_time_ms"].notna()
    if not valid_lte_time.any() or not valid_5g_time.any():
        raise ProcessingError(
            "Mobile režim: Nepodarilo sa načítať čas pre porovnanie (UTC alebo Date+Time) "
            "v jednom zo súborov."
        )

    lte_work = lte_work.loc[valid_lte_time].copy()
    lte_work["_time_ms"] = np.rint(lte_work["_time_ms"]).astype(np.int64)

    fiveg_candidates = fiveg_work.loc[
        valid_5g_time & fiveg_work[["_mcc_key", "_mnc_key"]].ne("").all(axis=1)
    ].copy()
    fiveg_candidates["_time_ms"] = np.rint(fiveg_candidates["_time_ms"]).astype(np.int64)

    tolerance = int(max(0, round(float(time_tolerance_ms))))
    lte_time_lookup = _build_lte_time_lookup(lte_work)

    window_scores = pd.Series(0, index=df_5g.index, dtype="int64")
    matched_rows = 0
    conflicting_windows = 0

    for group_key, group_df in fiveg_candidates.groupby(["_mcc_key", "_mnc_key"], sort=False):
        lte_data = lte_time_lookup.get(group_key)
        if lte_data is None:
            continue
        lte_times, yes_prefix, no_prefix = lte_data

        fiveg_times = group_df["_time_ms"].to_numpy(dtype=np.int64)
        left = np.searchsorted(lte_times, fiveg_times - tolerance, side="left")
        right = np.searchsorted(lte_times, fiveg_times + tolerance, side="right")
        has_match = right > left
        if not has_match.any():
            continue

        yes_counts = yes_prefix[right] - yes_prefix[left]
        no_counts = no_prefix[right] - no_prefix[left]
        conflicting_windows += int(np.sum((yes_counts > 0) & (no_counts > 0)))
        matched_rows += int(np.sum(has_match))

        resolved_scores = np.where(yes_counts > 0, 2, np.where(no_counts > 0, 1, 0))
        resolved_scores = np.where(has_match, resolved_scores, 0)
        window_scores.loc[group_df.index] = resolved_scores

    result_df = df_5g.copy()
    result_df[nr_column_name] = np.where(
        window_scores == 2,
        "yes",
        np.where(window_scores == 1, "no", ""),
    )

    return result_df, {
        "sync_strategy": f"{fiveg_time_strategy}->{lte_time_strategy}",
        "rows_total": int(len(result_df)),
        "rows_yes": int((window_scores == 2).sum()),
        "rows_no": int((window_scores == 1).sum()),
        "rows_blank": int((window_scores == 0).sum()),
        "rows_with_match": int(matched_rows),
        "conflicting_windows": int(conflicting_windows),
        "window_ms": tolerance,
    }


def run_processing(config: ProcessingConfig, status_callback: StatusCallback = None) -> ProcessingResult:
    _emit(status_callback, "Načítavam CSV súbor...")
    df, file_info = load_csv_file(config.file_path)
    if df is None:
        raise ProcessingError("Nepodarilo sa načítať CSV súbor.")
    _validate_column_mapping(df, config.column_mapping)

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

    if config.mobile_mode_enabled:
        if not config.mobile_lte_file_path:
            raise ProcessingError("Mobile režim je zapnutý, ale cesta k LTE súboru nie je zadaná.")
        _emit(status_callback, "Mobile režim: synchronizujem 5G dáta podľa LTE súboru...")
        df, mobile_sync_stats = _sync_mobile_nr_from_lte(
            df,
            config.column_mapping,
            config.mobile_lte_file_path,
            config.mobile_nr_column_name,
            config.mobile_time_tolerance_ms,
            filter_rules,
            config.keep_original_rows,
        )
        _emit(
            status_callback,
            (
                "Mobile režim: synchronizácia hotová "
                f"(strategy={mobile_sync_stats['sync_strategy']}, "
                f"window={mobile_sync_stats['window_ms']}ms, "
                f"matched={mobile_sync_stats['rows_with_match']}, "
                f"yes={mobile_sync_stats['rows_yes']}, "
                f"conflicts={mobile_sync_stats['conflicting_windows']})."
            ),
        )
        if mobile_sync_stats["conflicting_windows"] > 0:
            conflict_message = (
                "Upozornenie (Mobile režim): "
                f"nájdených {mobile_sync_stats['conflicting_windows']} časových okien s mixom "
                "hodnôt 5G NR (yes/no). V takom okne sa uprednostní 'yes'."
            )
            _emit(status_callback, conflict_message)
            if status_callback is None:
                print(conflict_message)
        if config.mobile_require_nr_yes:
            nr_yes_mask = _normalize_nr_series(df[config.mobile_nr_column_name]).eq("yes")
            if not nr_yes_mask.any():
                raise ProcessingError(
                    "Mobile režim: Po synchronizácii neostali žiadne riadky s 5G NR = yes."
                )
            df = df.loc[nr_yes_mask].copy()

    output_suffix = _normalize_output_suffix(config.output_suffix)
    if config.mobile_mode_enabled:
        if output_suffix:
            if not output_suffix.endswith("_mobile"):
                output_suffix = f"{output_suffix}_mobile"
        else:
            output_suffix = "_mobile"
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
