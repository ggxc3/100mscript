#!/usr/bin/env python3
# -*- coding: utf-8 -*-

from __future__ import annotations

import sys
from itertools import product
from pathlib import Path

import pandas as pd

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from python.main import (
    _extract_query_content,
    _split_assignment_and_conditions,
    _parse_assignments,
    _parse_condition_groups,
    _row_matches_filter,
    apply_filters,
)


def load_rule_from_file(path: Path):
    raw_text = path.read_text(encoding="utf-8")
    query_text = _extract_query_content(raw_text)
    assignment_text, conditions_text = _split_assignment_and_conditions(query_text)
    assignments = _parse_assignments(assignment_text)
    condition_groups = _parse_condition_groups(conditions_text)
    if not assignments or not condition_groups:
        raise ValueError(f"Neplatny filter: {path}")
    return {
        "name": path.name,
        "assignments": assignments,
        "condition_groups": condition_groups,
    }


def normalize_value(value):
    if isinstance(value, float) and value.is_integer():
        return int(value)
    return value


def build_matching_rows(condition_groups):
    rows = []
    for group in condition_groups:
        base = {}
        ranges = []
        for field, condition in group:
            if condition[0] == "eq":
                base[field] = condition[1]
            elif condition[0] == "range":
                ranges.append((field, condition[1], condition[2]))

        # Default values to ensure assignments actually change something (mainly for 5G filters)
        if "MCC" not in base:
            base["MCC"] = 999
        if "MNC" not in base:
            base["MNC"] = 99

        if ranges:
            value_lists = []
            for field, low, high in ranges:
                value_lists.append([(field, low), (field, high)])
            for combo in product(*value_lists):
                row = base.copy()
                for field, val in combo:
                    row[field] = val
                rows.append(row)
        else:
            rows.append(base)
    return rows


def build_out_of_range_rows(condition_groups):
    rows = []
    for group in condition_groups:
        for field, condition in group:
            if condition[0] == "range":
                low, high = condition[1], condition[2]
                base = {"MCC": 999, "MNC": 99}
                if field != "Frequency":
                    base[field] = low - 1
                else:
                    base["Frequency"] = low - 1
                rows.append(base.copy())
                base["Frequency" if field == "Frequency" else field] = high + 1
                rows.append(base.copy())
                break
    return rows


def expected_assignment_combinations(assignments):
    fields = list(assignments.keys())
    values_lists = [assignments[field] for field in fields]
    combos = [dict(zip(fields, values)) for values in product(*values_lists)]
    return fields, combos


def assert_filter_rule(rule):
    condition_groups = rule["condition_groups"]
    assignments = rule["assignments"]

    matching_rows = build_matching_rows(condition_groups)
    non_matching_rows = build_out_of_range_rows(condition_groups)
    non_matching_rows.append({"MCC": 0, "MNC": 0, "Frequency": 0})

    test_rows = matching_rows + non_matching_rows
    df = pd.DataFrame(test_rows)
    file_info = {"header_line": 0}

    filtered = apply_filters(df, file_info, [rule], False)

    assignment_fields, assignment_combos = expected_assignment_combinations(assignments)

    for idx, row in df.iterrows():
        row_number = idx + 1
        output_rows = filtered[filtered["original_excel_row"] == row_number]
        matches = _row_matches_filter(row, rule)
        expected_count = len(assignment_combos) if matches else 1

        if len(output_rows) != expected_count:
            raise AssertionError(
                f"{rule['name']}: Riadok {row_number} ma {len(output_rows)} vystupov, ocakavane {expected_count}."
            )

        if matches:
            output_values = set()
            for _, out_row in output_rows.iterrows():
                output_values.add(tuple(normalize_value(out_row[field]) for field in assignment_fields))
            expected_values = set(
                tuple(normalize_value(combo[field]) for field in assignment_fields) for combo in assignment_combos
            )
            if output_values != expected_values:
                raise AssertionError(
                    f"{rule['name']}: Riadok {row_number} nema spravne assignment hodnoty."
                )
        else:
            original_values = {field: normalize_value(row.get(field)) for field in assignment_fields}
            out_row = output_rows.iloc[0]
            for field in assignment_fields:
                if normalize_value(out_row.get(field)) != original_values.get(field):
                    raise AssertionError(
                        f"{rule['name']}: Riadok {row_number} nemal byt upraveny."
                    )

    # Test keep_original_on_match=True
    filtered_keep = apply_filters(df, file_info, [rule], True)
    for idx, row in df.iterrows():
        row_number = idx + 1
        output_rows = filtered_keep[filtered_keep["original_excel_row"] == row_number]
        matches = _row_matches_filter(row, rule)
        expected_count = (len(assignment_combos) + 1) if matches else 1
        if len(output_rows) != expected_count:
            raise AssertionError(
                f"{rule['name']}: keep_original_on_match zly pocet riadkov pre {row_number}."
            )
        if matches:
            original_found = False
            for _, out_row in output_rows.iterrows():
                same = True
                for field in row.index:
                    if normalize_value(out_row.get(field)) != normalize_value(row.get(field)):
                        same = False
                        break
                if same:
                    original_found = True
                    break
            if not original_found:
                raise AssertionError(
                    f"{rule['name']}: keep_original_on_match neobsahuje povodny riadok {row_number}."
                )


def test_duplicate_assignments():
    rule = {
        "name": "TEST_DUPLICATE_ASSIGNMENTS",
        "assignments": {"MCC": [231, 232], "MNC": [1, 2]},
        "condition_groups": [[("Frequency", ("eq", 111))]],
    }
    df = pd.DataFrame([{"MCC": 0, "MNC": 0, "Frequency": 111}])
    filtered = apply_filters(df, {"header_line": 0}, [rule], False)
    if len(filtered) != 4:
        raise AssertionError("Duplicate assignment test: ocakavane 4 riadky.")


def test_multiple_filter_collision():
    rule_a = {
        "name": "TEST_COLLISION_A",
        "assignments": {"MCC": [231], "MNC": [1]},
        "condition_groups": [[("Frequency", ("eq", 222))]],
    }
    rule_b = {
        "name": "TEST_COLLISION_B",
        "assignments": {"MCC": [231], "MNC": [2]},
        "condition_groups": [[("Frequency", ("eq", 222))]],
    }
    df = pd.DataFrame([{"MCC": 0, "MNC": 0, "Frequency": 222}])
    try:
        apply_filters(df, {"header_line": 0}, [rule_a, rule_b], False)
    except SystemExit:
        return
    raise AssertionError("Collision test: ocakavana chyba pri viac filtrov.")


def test_missing_columns():
    rule = {
        "name": "TEST_MISSING_COLS",
        "assignments": {"MCC": [231], "MNC": [1]},
        "condition_groups": [[("Frequency", ("eq", 333))]],
    }
    df = pd.DataFrame([{"MCC": 0, "MNC": 0}])
    filtered = apply_filters(df, {"header_line": 0}, [rule], False)
    if len(filtered) != 1:
        raise AssertionError("Missing columns test: ocakavany 1 riadok.")
    out_row = filtered.iloc[0]
    if out_row["MCC"] != 0 or out_row["MNC"] != 0:
        raise AssertionError("Missing columns test: riadok sa nemal upravit.")


def test_header_line():
    rule = {
        "name": "TEST_HEADER_LINE",
        "assignments": {"MCC": [231], "MNC": [1]},
        "condition_groups": [[("Frequency", ("eq", 444))]],
    }
    df = pd.DataFrame(
        [
            {"MCC": 0, "MNC": 0, "Frequency": 444},
            {"MCC": 0, "MNC": 0, "Frequency": 0},
        ]
    )
    filtered = apply_filters(df, {"header_line": 10}, [rule], False)
    if set(filtered["original_excel_row"]) != {11, 12}:
        raise AssertionError("Header line test: zle original_excel_row.")


def test_non_int_index():
    rule = {
        "name": "TEST_NON_INT_INDEX",
        "assignments": {"MCC": [231], "MNC": [1]},
        "condition_groups": [[("Frequency", ("eq", 555))]],
    }
    df = pd.DataFrame(
        [
            {"MCC": 0, "MNC": 0, "Frequency": 555},
            {"MCC": 0, "MNC": 0, "Frequency": 0},
        ],
        index=["row_a", "row_b"],
    )
    filtered = apply_filters(df, {"header_line": 5}, [rule], False)
    if set(filtered["original_excel_row"]) != {6, 7}:
        raise AssertionError("Non-int index test: zle original_excel_row.")


def main():
    root = Path(__file__).resolve().parents[1]
    filter_paths = list((root / "filters").glob("*.txt")) + list((root / "filtre_5G").glob("*.txt"))
    if not filter_paths:
        print("Nenajdene filtre na testovanie.", file=sys.stderr)
        return 1

    for path in sorted(filter_paths):
        rule = load_rule_from_file(path)
        assert_filter_rule(rule)
        print(f"OK: {path.name}")

    test_duplicate_assignments()
    print("OK: duplicate assignments")
    test_multiple_filter_collision()
    print("OK: collision check")
    test_missing_columns()
    print("OK: missing columns")
    test_header_line()
    print("OK: header line")
    test_non_int_index()
    print("OK: non-int index")

    print("Vsetky testy filtrov uspesne dokoncene.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
