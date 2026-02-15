#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import traceback

from app_backend import ProcessingConfig, ProcessingError, run_processing
from filters import load_filter_rules
from prompts import (
    ask_for_custom_operators,
    ask_for_include_empty_zones,
    ask_for_keep_original_rows,
    ask_for_rsrp_threshold,
    ask_for_sinr_threshold,
    ask_for_zone_mode,
    ask_for_zone_size,
    get_column_mapping_for_file,
    parse_arguments,
)


def _wait_for_exit():
    try:
        input("Stlačte Enter pre ukončenie...")
    except EOFError:
        pass


def main():
    args = parse_arguments()

    file_path = args.file
    if not file_path:
        file_path = input("Zadajte cestu k CSV súboru: ")

    column_mapping = get_column_mapping_for_file(file_path)

    filter_rules = load_filter_rules()
    keep_original_rows = False
    if filter_rules:
        keep_original_rows = ask_for_keep_original_rows()

    zone_mode = ask_for_zone_mode()
    use_zone_center = zone_mode == "center"
    zone_size_m = ask_for_zone_size()
    if zone_mode == "segments":
        print(f"Použijú sa presné {zone_size_m} m úseky po trase.")
    else:
        print(f"Použijú sa {'súradnice stredu zóny' if use_zone_center else 'pôvodné súradnice'}.")
        print(f"Veľkosť zóny: {zone_size_m} m")

    rsrp_threshold = ask_for_rsrp_threshold()
    sinr_threshold = ask_for_sinr_threshold()
    include_empty_zones = ask_for_include_empty_zones(zone_mode)

    add_operators = False
    custom_operators = []
    if include_empty_zones:
        add_operators, custom_operators = ask_for_custom_operators()

    config = ProcessingConfig(
        file_path=file_path,
        column_mapping=column_mapping,
        keep_original_rows=keep_original_rows,
        zone_mode=zone_mode,
        zone_size_m=zone_size_m,
        rsrp_threshold=rsrp_threshold,
        sinr_threshold=sinr_threshold,
        include_empty_zones=include_empty_zones,
        add_custom_operators=add_operators,
        custom_operators=custom_operators,
        filter_rules=filter_rules,
        progress_enabled=True,
    )

    try:
        result = run_processing(config)
    except ProcessingError as exc:
        print(f"Chyba: {exc}")
        return

    print("\nSúhrn spracovania:")
    if zone_mode == "segments":
        print(f"Počet unikátnych úsekov: {result.unique_zones}")
        print(f"Počet unikátnych operátorov: {result.unique_operators}")
        print(f"Celkový počet úsekov (úsek+operátor): {result.total_zone_rows}")
    else:
        print(f"Počet unikátnych zón: {result.unique_zones}")
        print(f"Počet unikátnych operátorov: {result.unique_operators}")
        print(f"Celkový počet zón (zóna+operátor): {result.total_zone_rows}")
        if result.min_x is not None and result.max_x is not None and result.range_x_m is not None:
            range_x_km = result.range_x_m / 1000
            range_y_km = result.range_y_m / 1000 if result.range_y_m is not None else 0
            print("\nGeografický rozsah merania:")
            print(
                f"X: {result.min_x} až {result.max_x} metrov "
                f"(rozsah: {result.range_x_m:.2f} m = {range_x_km:.2f} km)"
            )
            print(
                f"Y: {result.min_y} až {result.max_y} metrov "
                f"(rozsah: {result.range_y_m:.2f} m = {range_y_km:.2f} km)"
            )
            if result.theoretical_total_zones is not None and result.theoretical_total_zones > 0:
                print(
                    f"\nTeoretrický počet zón pre celý geografický rozsah: "
                    f"{result.theoretical_total_zones:.0f}"
                )
                print(
                    "Teoretické pokrytie geografického rozsahu: "
                    f"{result.coverage_percent:.2f}%"
                )

    print("\nSpracovanie úspešne dokončené!")
    print(f"Výsledky zón uložené do súboru: {result.zones_file}")
    print(f"Štatistiky uložené do súboru: {result.stats_file}")


if __name__ == "__main__":
    try:
        main()
    except SystemExit as exc:
        if exc.code not in (0, None):
            _wait_for_exit()
        raise
    except BaseException:
        traceback.print_exc()
        _wait_for_exit()
        raise
