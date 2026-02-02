# -*- coding: utf-8 -*-

import os
import re
from collections import defaultdict
from itertools import product

import numpy as np
import pandas as pd

from io_utils import get_app_base_dir

FILTER_VALUE_RE = re.compile(r'"([^"]+)"\s*=\s*([-\d.,\s]+)')
RANGE_VALUE_RE = re.compile(r'^(-?\d+(?:[.,]\d+)?)\s*-\s*(-?\d+(?:[.,]\d+)?)$')


def _parse_number(value):
    if value is None:
        return None
    if isinstance(value, (int, float, np.integer, np.floating)):
        try:
            if float(value).is_integer():
                return int(value)
        except Exception:
            pass
        return float(value)
    if isinstance(value, str):
        cleaned = value.strip()
        if not cleaned:
            return None
        cleaned = cleaned.replace(',', '.')
        try:
            num = float(cleaned)
        except ValueError:
            return None
        if num.is_integer():
            return int(num)
        return num
    return None


def _normalize_integer_like(value):
    parsed = _parse_number(value)
    if isinstance(parsed, int):
        return parsed
    return value


def _extract_query_content(text):
    match = re.search(r'<Query>(.*?)</Query>', text, flags=re.IGNORECASE | re.DOTALL)
    if match:
        return match.group(1)
    return text


def _split_assignment_and_conditions(query_text):
    semi_index = query_text.find(';')
    if semi_index == -1:
        return query_text, ''
    return query_text[:semi_index], query_text[semi_index + 1:]


def _parse_assignment_value(raw_value):
    raw_value = raw_value.strip()
    if not raw_value:
        return None
    if RANGE_VALUE_RE.match(raw_value):
        raise ValueError("Assignment hodnoty nemozu byt rozsah.")
    parsed = _parse_number(raw_value)
    if parsed is None:
        raise ValueError(f"Neplatna assignment hodnota: {raw_value}")
    return parsed


def _parse_assignments(text):
    assignments = defaultdict(list)
    for field, raw_value in FILTER_VALUE_RE.findall(text):
        value = _parse_assignment_value(raw_value)
        if value not in assignments[field]:
            assignments[field].append(value)
    return dict(assignments)


def _parse_condition_value(raw_value):
    raw_value = raw_value.strip()
    if not raw_value:
        return None
    range_match = RANGE_VALUE_RE.match(raw_value)
    if range_match:
        start = _parse_number(range_match.group(1))
        end = _parse_number(range_match.group(2))
        if start is None or end is None:
            return None
        low, high = (start, end) if start <= end else (end, start)
        return ("range", low, high)
    parsed = _parse_number(raw_value)
    if parsed is None:
        return None
    return ("eq", parsed)


def _parse_condition_group(text):
    conditions = []
    for field, raw_value in FILTER_VALUE_RE.findall(text):
        parsed = _parse_condition_value(raw_value)
        if parsed:
            conditions.append((field, parsed))
    return conditions


def _parse_condition_groups(text):
    groups = []
    group_texts = re.findall(r'\(([^()]*)\)', text, flags=re.DOTALL)
    for group_text in group_texts:
        conditions = _parse_condition_group(group_text)
        if conditions:
            groups.append(conditions)
    if not groups:
        conditions = _parse_condition_group(text)
        if conditions:
            groups.append(conditions)
    return groups


def _build_assignment_combinations(assignments):
    if not assignments:
        return [{}]
    fields = list(assignments.keys())
    value_lists = [assignments[field] for field in fields]
    combinations = []
    for values in product(*value_lists):
        combinations.append(dict(zip(fields, values)))
    return combinations


def _row_matches_group(row, group):
    for field, condition in group:
        if field not in row:
            return False
        row_value = _parse_number(row[field])
        if row_value is None:
            return False
        condition_type = condition[0]
        if condition_type == "eq":
            if row_value != condition[1]:
                return False
        elif condition_type == "range":
            low, high = condition[1], condition[2]
            if low == high:
                if row_value != low:
                    return False
            else:
                if row_value < low or row_value >= high:
                    return False
    return True


def _row_matches_filter(row, filter_rule):
    for group in filter_rule["condition_groups"]:
        if _row_matches_group(row, group):
            return True
    return False


def load_filter_rules():
    filter_rules = []
    cwd = os.getcwd()
    app_base_dir = get_app_base_dir()
    has_cwd_filters = any(
        os.path.isdir(os.path.join(cwd, folder_name))
        for folder_name in ("filters", "filtre_5G")
    )
    candidate_base_dirs = [cwd] if has_cwd_filters else [app_base_dir]

    filter_dirs = []
    for base_dir in candidate_base_dirs:
        filter_dirs.append(os.path.join(base_dir, "filters"))
        filter_dirs.append(os.path.join(base_dir, "filtre_5G"))
    filter_paths = []

    for filter_dir in filter_dirs:
        if not os.path.isdir(filter_dir):
            continue
        for filename in sorted(os.listdir(filter_dir)):
            if filename.lower().endswith(".txt"):
                filter_paths.append(os.path.join(filter_dir, filename))

    for path in filter_paths:
        name = os.path.basename(path)
        try:
            with open(path, "r", encoding="utf-8") as f:
                raw_text = f.read()
            query_text = _extract_query_content(raw_text)
            assignment_text, conditions_text = _split_assignment_and_conditions(query_text)
            assignments = _parse_assignments(assignment_text)
            condition_groups = _parse_condition_groups(conditions_text)
            if not assignments or not condition_groups:
                print(f"Upozornenie: Filter {name} je neplatny alebo nema podmienky, preskakujem.")
                continue
            filter_rules.append({
                "name": name,
                "assignments": assignments,
                "condition_groups": condition_groups
            })
        except Exception as exc:
            print(f"Upozornenie: Filter {name} sa nepodarilo nacitat ({exc}).")
            continue
    return filter_rules


def apply_filters(df, file_info=None, filter_rules=None, keep_original_on_match=False):
    if filter_rules is None:
        filter_rules = load_filter_rules()
    if not filter_rules:
        return df

    header_line = file_info.get('header_line', 0) if file_info else 0
    output_rows = []
    base_columns = list(df.columns)

    print(f"Nájdených {len(filter_rules)} filtrov. Aplikujem predfiltre...")

    for position, (index, row) in enumerate(df.iterrows()):
        try:
            row_index = int(index)
        except (TypeError, ValueError):
            row_index = position
        row_number = row_index + header_line + 1
        matching_filters = []
        for rule in filter_rules:
            best_group_size = 0
            for group in rule["condition_groups"]:
                if _row_matches_group(row, group):
                    if len(group) > best_group_size:
                        best_group_size = len(group)
            if best_group_size > 0:
                matching_filters.append((best_group_size, rule))

        if len(matching_filters) > 1:
            matching_filters.sort(key=lambda item: (-item[0], item[1]["name"]))
            best_size = matching_filters[0][0]
            best_matches = [rule for size, rule in matching_filters if size == best_size]
            if len(best_matches) > 1:
                names = ", ".join(rule["name"] for rule in best_matches)
                print(
                    f"Upozornenie: Riadok {row_number} vyhovuje viac filtrom ({names}). "
                    f"Použijem prvý podľa poradia."
                )
            rule = matching_filters[0][1]
        elif len(matching_filters) == 1:
            rule = matching_filters[0][1]
        else:
            rule = None

        if rule is not None:
            if keep_original_on_match:
                row_dict = row.to_dict()
                row_dict["original_excel_row"] = row_number
                output_rows.append(row_dict)
            for assignment in _build_assignment_combinations(rule["assignments"]):
                row_dict = row.to_dict()
                for field, value in assignment.items():
                    row_dict[field] = value
                row_dict["original_excel_row"] = row_number
                output_rows.append(row_dict)
        else:
            row_dict = row.to_dict()
            row_dict["original_excel_row"] = row_number
            output_rows.append(row_dict)

    result_df = pd.DataFrame(output_rows)
    assignment_fields = set()
    for rule in filter_rules:
        assignment_fields.update(rule["assignments"].keys())
    for field in assignment_fields:
        if field in result_df.columns:
            numeric = pd.to_numeric(result_df[field], errors='coerce')
            if not ((result_df[field].notna()) & (numeric.isna())).any():
                result_df[field] = numeric.astype("Int64")
            else:
                result_df[field] = result_df[field].apply(_normalize_integer_like)
    if not result_df.empty:
        extra_columns = [col for col in result_df.columns if col not in base_columns]
        result_df = result_df[base_columns + extra_columns]
    return result_df


def maybe_dump_filtered_df(df, original_file):
    debug_output = os.getenv("FILTERS_DEBUG_OUTPUT", "").strip()
    if not debug_output:
        return
    if debug_output.lower() in ("1", "true", "yes", "a"):
        output_path = original_file.replace(".csv", "_filters.csv")
    else:
        output_path = debug_output
    try:
        df.to_csv(output_path, sep=';', index=False, encoding='utf-8')
        print(f"Filtrované dáta uložené do súboru: {output_path}")
    except Exception as exc:
        print(f"Upozornenie: Nepodarilo sa uložiť filtrované dáta ({exc}).")
