#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import traceback

from constants import ZONE_SIZE_METERS
from filters import apply_filters, load_filter_rules, maybe_dump_filtered_df
from io_utils import get_output_suffix, load_csv_file
from outputs import add_custom_operators, save_stats, save_zone_results
from processing import calculate_zone_stats, process_data
from prompts import (
    ask_for_keep_original_rows,
    ask_for_rsrp_threshold,
    ask_for_zone_mode,
    get_column_mapping,
    parse_arguments,
)


def _wait_for_exit():
    try:
        input("Stlačte Enter pre ukončenie...")
    except EOFError:
        pass


def main():
    args = parse_arguments()

    # Získame cestu k súboru
    file_path = args.file
    if not file_path:
        file_path = input("Zadajte cestu k CSV súboru: ")

    # Načítame súbor
    print(f"Načítavam súbor {file_path}...")
    df, file_info = load_csv_file(file_path)
    if df is None:
        return

    # Aplikujeme filtre pred spracovanim zon, ak existuju
    filter_rules = load_filter_rules()
    if filter_rules:
        keep_original_rows = ask_for_keep_original_rows()
        df = apply_filters(df, file_info, filter_rules, keep_original_rows)
        maybe_dump_filtered_df(df, file_path)
    output_suffix = get_output_suffix()

    # Opýtame sa používateľa na režim spracovania zón
    zone_mode = ask_for_zone_mode()
    use_zone_center = zone_mode == "center"
    if zone_mode == "segments":
        print("Použijú sa presné 100m úseky po trase.")
    else:
        print(f"Použijú sa {'súradnice stredu zóny' if use_zone_center else 'pôvodné súradnice'}.")

    # Opýtame sa používateľa na hranicu RSRP pre štatistiky
    rsrp_threshold = ask_for_rsrp_threshold()

    # Získame mapovanie stĺpcov
    column_mapping = get_column_mapping()

    # Získame číslo riadku hlavičky z info o súbore
    header_line = file_info.get('header_line', 0) if file_info else 0
    print(f"Hlavička súboru sa nachádza na riadku {header_line + 1}")

    # Spracujeme dáta s odovzdaním informácie o riadku hlavičky
    processed_df, column_names, segment_meta = process_data(df, column_mapping, header_line, zone_mode)

    # Vypočítame štatistiky zón s použitím zadanej RSRP hranice
    zone_stats = calculate_zone_stats(processed_df, column_mapping, column_names, rsrp_threshold, zone_mode)

    # Uložíme výsledky zachovávajúc pôvodný formát
    if output_suffix:
        output_file = file_path.replace('.csv', f'{output_suffix}_zones.csv')
    else:
        output_file = file_path.replace('.csv', '_zones.csv')
    include_empty_zones, processed_zones, unique_zones = save_zone_results(
        zone_stats,
        file_path,
        processed_df,
        column_mapping,
        column_names,
        file_info,
        use_zone_center,
        zone_mode,
        output_suffix,
        segment_meta
    )

    # Pridáme vlastných operátorov iba ak používateľ chce generovať prázdne zóny
    custom_operators_added = False
    if include_empty_zones and zone_mode != "segments":
        zone_stats, custom_operators_added = add_custom_operators(
            zone_stats, processed_df, column_mapping, column_names,
            output_file, use_zone_center, processed_zones, unique_zones
        )

    # Uložíme štatistiky - zohľadňujeme voľbu používateľa o prázdnych zónach a RSRP hranicu
    save_stats(zone_stats, file_path, include_empty_zones, rsrp_threshold, output_suffix, zone_mode, segment_meta)

    # Vypíšeme informácie o zónach/úsekoch a rozsahu merania
    print("\nSúhrn spracovania:")

    # Počet unikátnych zón/úsekov a operátorov
    unique_zones = zone_stats['zona_key'].nunique()
    operator_columns = ['mcc', 'mnc']
    if 'pci' in zone_stats.columns:
        operator_columns.append('pci')
    unique_operators = zone_stats[operator_columns].drop_duplicates().shape[0]
    total_zones = len(zone_stats)

    if zone_mode == "segments":
        print(f"Počet unikátnych úsekov: {unique_zones}")
        print(f"Počet unikátnych operátorov: {unique_operators}")
        print(f"Celkový počet úsekov (úsek+operátor): {total_zones}")
    else:
        print(f"Počet unikátnych zón: {unique_zones}")
        print(f"Počet unikátnych operátorov: {unique_operators}")
        print(f"Celkový počet zón (zóna+operátor): {total_zones}")

        # Geografický rozsah merania
        min_x = zone_stats['zona_x'].min()
        max_x = zone_stats['zona_x'].max()
        min_y = zone_stats['zona_y'].min()
        max_y = zone_stats['zona_y'].max()

        # Výpočet geografického rozsahu v metroch a kilometroch
        range_x_m = max_x - min_x + ZONE_SIZE_METERS
        range_y_m = max_y - min_y + ZONE_SIZE_METERS
        range_x_km = range_x_m / 1000
        range_y_km = range_y_m / 1000

        print(f"\nGeografický rozsah merania:")
        print(f"X: {min_x} až {max_x} metrov (rozsah: {range_x_m:.2f} m = {range_x_km:.2f} km)")
        print(f"Y: {min_y} až {max_y} metrov (rozsah: {range_y_m:.2f} m = {range_y_km:.2f} km)")

        # Teoretický počet zón pre geografický rozsah
        theoretical_zones_x = range_x_m / ZONE_SIZE_METERS
        theoretical_zones_y = range_y_m / ZONE_SIZE_METERS
        theoretical_total_zones = theoretical_zones_x * theoretical_zones_y

        print(f"\nTeoretrický počet zón pre celý geografický rozsah: {theoretical_total_zones:.0f}")
        print(f"Teoretické pokrytie geografického rozsahu: {(unique_zones / theoretical_total_zones * 100):.2f}%")

    print("\nSpracovanie úspešne dokončené!")


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
