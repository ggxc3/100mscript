# -*- coding: utf-8 -*-

import re

import numpy as np
import pandas as pd
from pyproj import Transformer
from tqdm import tqdm

from constants import ZONE_SIZE_METERS


def save_zone_results(
    zone_stats,
    original_file,
    df,
    column_mapping,
    column_names,
    file_info,
    use_zone_center,
    zone_mode="zones",
    output_suffix="",
    segment_meta=None,
    zone_size_m=ZONE_SIZE_METERS,
    generate_empty_zones=False,
    progress_enabled=True
):
    """Uloží výsledky zón alebo úsekov do CSV súboru, zachovávajúc pôvodný formát riadkov."""
    if output_suffix:
        output_file = original_file.replace('.csv', f'{output_suffix}_zones.csv')
    else:
        output_file = original_file.replace('.csv', '_zones.csv')

    # Použijeme originálnu hlavičku z načítaného súboru
    header_line = file_info.get('original_header') if file_info else None

    # Ak nemáme originálnu hlavičku, pokúsime sa ju získať zo súboru
    if not header_line:
        # Načítame pôvodný súbor pre hlavičku - skúsime všetky kódovania
        encodings = ['utf-8', 'latin1', 'latin2', 'cp1250', 'windows-1250', 'iso-8859-2']

        for enc in encodings:
            try:
                with open(original_file, 'r', encoding=enc) as f:
                    # Prečítame prvý riadok ako hlavičku
                    header_line = f.readline().strip()
                    if header_line and ';' in header_line:
                        break
            except Exception:
                continue

    # Ak sa nepodarilo nájsť hlavičku, použijeme názvy stĺpcov
    if not header_line or ';' not in header_line:
        header_line = ';'.join(column_names)

    # Pridáme nové stĺpce pre zoznam riadkov a frekvencií do hlavičky
    orig_header_cols = header_line.split(';')
    while orig_header_cols and orig_header_cols[-1] == "":
        orig_header_cols.pop()
    extra_output_cols = []
    if "5G NR" in column_names and "5G NR" not in orig_header_cols:
        extra_output_cols.append("5G NR")

    export_header_cols = orig_header_cols + extra_output_cols
    header_line = ';'.join(export_header_cols) + ";Riadky_v_zone;Frekvencie_v_zone"
    export_header_to_index = {name: idx for idx, name in enumerate(export_header_cols)}

    # Spočítame očakávaný počet stĺpcov
    expected_columns = len(export_header_cols)
    print(f"Počet stĺpcov v pôvodnej hlavičke: {expected_columns}")
    mcc_col = column_names[column_mapping['mcc']]
    mnc_col = column_names[column_mapping['mnc']]
    pci_col = column_names[column_mapping['pci']] if 'pci' in column_mapping else None

    # Vytvoríme nový obsah pre výstupný súbor - začíname prázdnym riadkom
    output_lines = ['']  # Prázdny riadok na začiatku
    if header_line:
        output_lines.append(header_line)

    # Pre každú zónu vytvoríme riadok založený na prvom meraní v danej zóne
    processed_zones = {}  # Slúži na sledovanie už spracovaných zón

    # Zoradíme zóny podľa operátora (MCC, MNC, PCI)
    sort_columns = ['mcc', 'mnc']
    if 'pci' in zone_stats.columns:
        sort_columns.append('pci')
    sorted_zone_stats = zone_stats.sort_values(sort_columns)

    # Získame všetky unikátne zóny bez ohľadu na to, či budeme generovať prázdne zóny
    unique_zones = sorted_zone_stats['zona_key'].unique()

    print("Zapisujem výsledky zón...")

    # Kontrolujeme, či máme SINR stĺpec
    has_sinr = 'sinr' in column_mapping and 'sinr_avg' in sorted_zone_stats.columns
    pci_index = column_mapping.get('pci') if isinstance(column_mapping, dict) else None
    rsrp_col = column_names[column_mapping['rsrp']]
    freq_col = column_names[column_mapping['frequency']]
    lat_col = column_names[column_mapping['latitude']]
    lon_col = column_names[column_mapping['longitude']]
    lat_index = export_header_to_index.get(lat_col, column_mapping['latitude'])
    lon_index = export_header_to_index.get(lon_col, column_mapping['longitude'])
    rsrp_index = export_header_to_index.get(rsrp_col, column_mapping['rsrp'])

    # Predpočítame prvý riadok pre každú kombináciu zóna+operátor+frekvencia+PCI
    # (stráca sa O(n^2) filtrovanie v hlavnej slučke)
    missing_freq_key = "__MISSING_FREQ__"
    freq_keys = df[freq_col].astype(object)
    freq_keys[pd.isna(freq_keys)] = missing_freq_key
    missing_pci_key = "__MISSING_PCI__"
    if pci_col is not None:
        pci_keys = df[pci_col].astype(object)
        pci_keys[pd.isna(pci_keys)] = missing_pci_key
    else:
        pci_keys = pd.Series([missing_pci_key] * len(df), index=df.index)
    sample_lookup = pd.DataFrame({
        "zona_operator_key": df["zona_operator_key"],
        "freq_key": freq_keys,
        "pci_key": pci_keys,
        "row_index": df.index,
    })
    sample_lookup = sample_lookup.drop_duplicates(
        subset=["zona_operator_key", "freq_key", "pci_key"],
        keep="first"
    )
    sample_row_index_by_key = dict(
        zip(
            zip(sample_lookup["zona_operator_key"], sample_lookup["freq_key"], sample_lookup["pci_key"]),
            sample_lookup["row_index"]
        )
    )

    # Vytvorím progress bar pre zápis zón
    for i in tqdm(range(len(sorted_zone_stats)), desc="Zápis zón", disable=not progress_enabled):
        zone_row = sorted_zone_stats.iloc[i]
        zona_operator_key = f"{zone_row['zona_key']}_{zone_row['operator_key']}"

        # Overíme, či sme už túto zónu+operátora spracovali
        if zona_operator_key in processed_zones:
            continue

        # Označíme túto zónu ako spracovanú
        processed_zones[zona_operator_key] = True

        # Nájdeme vzorový riadok z tejto zóny a vybranej kombinácie frekvencia+PCI
        selected_frequency = zone_row['najcastejsia_frekvencia']
        selected_freq_key = missing_freq_key if pd.isna(selected_frequency) else selected_frequency
        if pci_col is not None and 'pci' in zone_row:
            selected_pci = zone_row['pci']
            selected_pci_key = missing_pci_key if pd.isna(selected_pci) else selected_pci
        else:
            selected_pci_key = missing_pci_key
        row_index = sample_row_index_by_key.get((zona_operator_key, selected_freq_key, selected_pci_key))
        if row_index is None:
            continue  # Preskočíme, ak nemáme vzorový riadok
        # Vezmeme prvý riadok ako základ
        sample_row = df.loc[row_index]

        # Vytvoríme kópiu vzorového riadku - už filtrovaného dataframu
        # Nepoužívame original_row_index, ktorý by mohol byť mimo rozsahu
        base_row = sample_row.copy()

        # Aktualizujeme hodnoty - používame bodku namiesto čiarky pre desatinné hodnoty
        base_row[rsrp_col] = f"{zone_row['rsrp_avg']:.2f}"
        base_row[freq_col] = zone_row['najcastejsia_frekvencia']
        if pci_col is not None and 'pci' in zone_row:
            base_row[pci_col] = zone_row['pci']

        # Aktualizujeme SINR, ak je k dispozícii
        if has_sinr and not pd.isna(zone_row['sinr_avg']):
            sinr_col = column_names[column_mapping['sinr']]
            base_row[sinr_col] = f"{zone_row['sinr_avg']:.2f}"

        # Aktualizujeme súradnice podľa zvoleného režimu
        if zone_mode == "segments":
            base_row[lat_col] = f"{zone_row['latitude']:.6f}"
            base_row[lon_col] = f"{zone_row['longitude']:.6f}"
        elif use_zone_center:
            # Použijeme súradnice stredu zóny
            base_row[lat_col] = f"{zone_row['latitude']:.6f}"
            base_row[lon_col] = f"{zone_row['longitude']:.6f}"

        # Získame zoznam riadkov z Excelu, ktoré patria do tejto zóny
        excel_rows = zone_row['original_excel_rows']
        excel_rows_str = ','.join(map(str, excel_rows)) if excel_rows else ""

        # Získame zoznam frekvencií, ktoré patria do tejto zóny
        all_frequencies = zone_row['vsetky_frekvencie']
        # Odstránime duplicity a zoradíme frekvencie
        unique_frequencies = sorted(set(all_frequencies))
        frequencies_str = ','.join(map(str, unique_frequencies)) if unique_frequencies else ""

        # Vytvoríme riadok pre CSV
        row_values = []
        for j, header_col in enumerate(export_header_cols):
            val = base_row.get(header_col, "")
            # Ak je hodnota NaN, nahraďme ju prázdnym reťazcom
            if pd.isna(val):
                row_values.append("")
            # Ak je to MCC alebo MNC, zaokrúhlime na celé číslo
            elif j == column_mapping['mcc'] or j == column_mapping['mnc'] or (pci_index is not None and j == pci_index):
                try:
                    row_values.append(str(int(float(val))))
                except:
                    row_values.append(str(val))
            else:
                row_values.append(str(val))

        # Zabezpečíme, že máme presne toľko stĺpcov, koľko má hlavička
        while len(row_values) < expected_columns:
            row_values.append("")

        # Ak máme viac stĺpcov, odrežeme nadbytočné
        if len(row_values) > expected_columns:
            row_values = row_values[:expected_columns]

        # Vytvoríme základný CSV riadok
        csv_row = ';'.join(row_values)

        # Pridáme informáciu o zóne a zoznam riadkov a frekvencií
        csv_row += f";{excel_rows_str};{frequencies_str}"

        # Pridáme poznámku o počte meraní
        csv_row += f" # Meraní: {zone_row['pocet_merani']}"

        output_lines.append(csv_row)

    # Vytvoríme chýbajúce zóny pre všetkých operátorov
    if generate_empty_zones:
        if zone_mode == "segments":
            print("Generujem prázdne úseky...")
        else:
            print("Generujem prázdne zóny...")

        # Získame všetky unikátne zóny/úseky a operátorov
        unique_zones = sorted_zone_stats['zona_key'].unique()
        operator_columns = ['mcc', 'mnc']
        unique_operators = sorted_zone_stats[operator_columns].drop_duplicates().values
        added_empty_zones = 0

        # Predpočítame prvý riadok pre každú kombináciu operátora (rovnaké správanie ako filtrovanie)
        operator_key_columns = [mcc_col, mnc_col]

        def _normalize_operator_value(value):
            if isinstance(value, (int, float, np.integer, np.floating)):
                try:
                    return float(value)
                except Exception:
                    return value
            return value

        operator_first_row = {}
        for row_index, *values in df[operator_key_columns].itertuples(index=True, name=None):
            key = tuple(_normalize_operator_value(v) for v in values)
            if key not in operator_first_row:
                operator_first_row[key] = row_index

        rsrp_col = column_names[column_mapping['rsrp']]

        operator_row_templates = {}
        for operator_values in unique_operators:
            mcc, mnc = operator_values[0], operator_values[1]
            operator_key = f"{mcc}_{mnc}"

            operator_key_values = (mcc, mnc)
            lookup_key = tuple(_normalize_operator_value(v) for v in operator_key_values)
            row_index = operator_first_row.get(lookup_key)
            if row_index is None:
                sample_row = df.iloc[0]
            else:
                sample_row = df.loc[row_index]
            base_row = sample_row.copy()

            try:
                base_row[mcc_col] = str(int(float(mcc)))
            except:
                base_row[mcc_col] = mcc

            try:
                base_row[mnc_col] = str(int(float(mnc)))
            except:
                base_row[mnc_col] = mnc

            base_row[rsrp_col] = "-174"

            row_values = []
            for j, header_col in enumerate(export_header_cols):
                val = base_row.get(header_col, "")
                if pd.isna(val):
                    row_values.append("")
                elif j == column_mapping['mcc'] or j == column_mapping['mnc'] or (pci_index is not None and j == pci_index):
                    try:
                        row_values.append(str(int(float(val))))
                    except:
                        row_values.append(str(val))
                else:
                    row_values.append(str(val))

            while len(row_values) < expected_columns:
                row_values.append("")

            if len(row_values) > expected_columns:
                row_values = row_values[:expected_columns]

            operator_row_templates[operator_key] = row_values

        if zone_mode == "segments":
            segment_coords = None
            if segment_meta:
                transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
                segment_coords = {}
                for segment_id, (segment_x, segment_y) in segment_meta.items():
                    lon, lat = transformer.transform(segment_x, segment_y)
                    segment_coords[f"segment_{segment_id}"] = (lat, lon)
                unique_zones = [f"segment_{segment_id}" for segment_id in sorted(segment_meta.keys())]
            else:
                segment_coords = zone_stats.groupby('zona_key')[['longitude', 'latitude']].first()
            segment_latlon = {}
            if isinstance(segment_coords, dict):
                for zona_key, coords in segment_coords.items():
                    if coords:
                        lat, lon = coords
                        segment_latlon[zona_key] = (f"{lat:.6f}", f"{lon:.6f}")
            elif segment_coords is not None:
                for zona_key, coords in segment_coords.iterrows():
                    segment_latlon[zona_key] = (f"{coords['latitude']:.6f}", f"{coords['longitude']:.6f}")
            for zona_key in tqdm(unique_zones, desc="Generovanie prázdnych úsekov", disable=not progress_enabled):
                coords = segment_latlon.get(zona_key)
                for operator_values in unique_operators:
                    mcc, mnc = operator_values[0], operator_values[1]
                    operator_key = f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    if zona_operator_key not in processed_zones:
                        row_values = operator_row_templates[operator_key].copy()
                        if coords:
                            lat_str, lon_str = coords
                            if lat_index < expected_columns:
                                row_values[lat_index] = lat_str
                            if lon_index < expected_columns:
                                row_values[lon_index] = lon_str
                        if rsrp_index < expected_columns:
                            row_values[rsrp_index] = "-174"

                        csv_row = ';'.join(row_values)
                        csv_row += ";;"
                        csv_row += " # Prázdny úsek - automaticky vygenerovaný"

                        output_lines.append(csv_row)
                        added_empty_zones += 1
        else:
            zone_latlon = {}
            transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
            for zona_key in unique_zones:
                zona_coords = zona_key.split('_')
                if len(zona_coords) != 2:
                    continue
                zona_x, zona_y = float(zona_coords[0]), float(zona_coords[1])
                zona_center_x = zona_x + zone_size_m / 2
                zona_center_y = zona_y + zone_size_m / 2
                lon, lat = transformer.transform(zona_center_x, zona_center_y)
                zone_latlon[zona_key] = (f"{lat:.6f}", f"{lon:.6f}")

            # Progress bar pre generovanie prázdnych zón
            for zona_key in tqdm(unique_zones, desc="Generovanie prázdnych zón", disable=not progress_enabled):
                coords = zone_latlon.get(zona_key)
                if coords is None:
                    continue
                for operator_values in unique_operators:
                    mcc, mnc = operator_values[0], operator_values[1]
                    operator_key = f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    # Ak táto kombinácia neexistuje, vytvoríme ju
                    if zona_operator_key not in processed_zones:
                        lat_str, lon_str = coords
                        row_values = operator_row_templates[operator_key].copy()
                        if lat_index < expected_columns:
                            row_values[lat_index] = lat_str
                        if lon_index < expected_columns:
                            row_values[lon_index] = lon_str
                        if rsrp_index < expected_columns:
                            row_values[rsrp_index] = "-174"

                        csv_row = ';'.join(row_values)
                        csv_row += ";;"
                        csv_row += " # Prázdna zóna - automaticky vygenerovaná"

                        output_lines.append(csv_row)
                        added_empty_zones += 1

    # Zapíšeme výsledky do súboru
    with open(output_file, 'w', encoding='utf-8') as f:
        f.write('\n'.join(output_lines))

    if generate_empty_zones:
        if zone_mode == "segments":
            print(f"Pridaných {added_empty_zones} prázdnych úsekov")
        else:
            print(f"Pridaných {added_empty_zones} prázdnych zón")
    print(f"Výsledky zón uložené do súboru: {output_file}")

    return generate_empty_zones, processed_zones, unique_zones  # Vrátime informáciu, či boli generované prázdne zóny a zoznam spracovaných zón


def add_custom_operators(
    zone_stats,
    df,
    column_mapping,
    column_names,
    output_file,
    use_zone_center,
    processed_zones,
    unique_zones,
    zone_size_m=ZONE_SIZE_METERS,
    zone_mode="zones",
    segment_meta=None,
    add_operators=None,
    custom_operators=None,
    progress_enabled=True
):
    """Pridá vlastných operátorov do zón a štatistík."""
    if add_operators is None:
        add_operators = input(
            "Chcete pridať vlastných operátorov, ktorí neboli nájdení v dátach? (a/n): "
        ).lower() == 'a'

    if not add_operators:
        return zone_stats, False

    # Získame existujúcich operátorov
    has_pci = 'pci' in zone_stats.columns
    existing_operators = set([
        f"{mcc}_{mnc}"
        for mcc, mnc in zip(zone_stats['mcc'], zone_stats['mnc'])
    ])
    existing_operators_at_start = set(existing_operators)

    if custom_operators is None:
        custom_operators = []
    continue_adding = True

    # Regex vzor: MCC a MNC musia obsahovať iba čísla a nesmú byť prázdne
    mcc_pattern = re.compile(r'^\d+$')
    mnc_pattern = re.compile(r'^\d+$')
    pci_pattern = re.compile(r'^\d+$')

    while continue_adding and not custom_operators:
        # Zadáme nových operátorov v jednom riadku oddelených dvojbodkou
        try:
            operators_input = input(
                "Zadajte MCC:MNC operátorov oddelené medzerou (napr. '231:01 231:02'), "
                "PCI je voliteľné (MCC:MNC:PCI). Alebo 'koniec' pre ukončenie: "
            ).strip()

            # Kontrola ukončenia
            if not operators_input or operators_input.lower() in ['koniec', 'quit', 'q', 'exit']:
                continue_adding = False
                continue

            operator_pairs = operators_input.split()
            added_in_batch = False

            for operator_pair in operator_pairs:
                parts = operator_pair.split(':')
                if len(parts) not in (2, 3):
                    print(f"Neplatný formát operátora '{operator_pair}'. Použite formát MCC:MNC alebo MCC:MNC:PCI.")
                    continue

                mcc, mnc = parts[0], parts[1]
                pci = parts[2] if len(parts) == 3 else ""

                # Validácia MCC a MNC pomocou regex
                if not mcc_pattern.match(mcc):
                    print(f"Neplatné MCC '{mcc}'. MCC musí obsahovať iba čísla a nesmie byť prázdne.")
                    continue

                if not mnc_pattern.match(mnc):
                    print(f"Neplatné MNC '{mnc}'. MNC musí obsahovať iba čísla a nesmie byť prázdne.")
                    continue

                if pci and not pci_pattern.match(pci):
                    print(f"Neplatné PCI '{pci}'. PCI musí obsahovať iba čísla alebo zostať prázdne.")
                    continue

                # Skontrolujeme, či tento operátor už existuje
                operator_key = f"{mcc}_{mnc}"
                if operator_key in existing_operators:
                    print(f"Operátor s MCC={mcc} a MNC={mnc} už existuje v dátach!")
                    continue

                # Pridáme operátora do zoznamu
                custom_operators.append((mcc, mnc, pci))
                existing_operators.add(operator_key)
                if pci:
                    print(f"Operátor s MCC={mcc}, MNC={mnc}, PCI={pci} bol pridaný.")
                else:
                    print(f"Operátor s MCC={mcc} a MNC={mnc} bol pridaný.")
                added_in_batch = True

            # Opýtame sa, či chce pridať ďalšie, iba ak bol pridaný aspoň jeden operátor
            if added_in_batch and input("Chcete pridať ďalších operátorov? (a/n): ").lower() != 'a':
                continue_adding = False

        except Exception as e:
            print(f"Chyba pri zadávaní operátorov: {e}")

    deduped_custom_operators = []
    seen_custom_operator_keys = set()
    for operator in custom_operators:
        if not isinstance(operator, (list, tuple)):
            continue
        if len(operator) < 2:
            continue
        mcc = str(operator[0]).strip()
        mnc = str(operator[1]).strip()
        pci = str(operator[2]).strip() if len(operator) >= 3 else ""
        operator_key = f"{mcc}_{mnc}"
        if operator_key in existing_operators_at_start:
            print(f"Operátor s MCC={mcc} a MNC={mnc} už existuje v dátach, preskakujem.")
            continue
        if operator_key in seen_custom_operator_keys:
            continue
        deduped_custom_operators.append((mcc, mnc, pci))
        seen_custom_operator_keys.add(operator_key)
        existing_operators.add(operator_key)

    custom_operators = deduped_custom_operators
    if not custom_operators:
        return zone_stats, False

    print(f"Pridávam {len(custom_operators)} vlastných operátorov...")

    # Načítame existujúci súbor so zónami
    try:
        with open(output_file, 'r', encoding='utf-8') as f:
            output_lines = f.readlines()
    except:
        # Ak súbor ešte neexistuje, vytvoríme prázdny zoznam
        output_lines = []

    # Vypočítame expected_columns z počtu stĺpcov v column_names
    expected_columns = len(column_names)

    # Pridáme nové riadky do súboru - prázdne zóny/úseky pre nových operátorov
    new_zones_added = 0

    # Ak máme nejaké zóny/úseky, môžeme pridať nových operátorov
    if len(unique_zones) > 0:
        if len(df) > 0:
            sample_row = df.iloc[0].copy()

            rsrp_col = column_names[column_mapping['rsrp']]
            lat_col = column_names[column_mapping['latitude']]
            lon_col = column_names[column_mapping['longitude']]
            mcc_col = column_names[column_mapping['mcc']]
            mnc_col = column_names[column_mapping['mnc']]
            pci_col = column_names[column_mapping['pci']] if 'pci' in column_mapping else None

            first_custom_operator_line = True
            zone_transformer = None

            zone_latlon = {}
            if zone_mode == "segments":
                if segment_meta:
                    segment_transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
                    for segment_id, (segment_x, segment_y) in segment_meta.items():
                        lon, lat = segment_transformer.transform(segment_x, segment_y)
                        zone_latlon[f"segment_{segment_id}"] = (lat, lon)

                fallback_segment_coords = zone_stats.groupby('zona_key')[['latitude', 'longitude']].first()
                for zona_key, coords in fallback_segment_coords.iterrows():
                    if pd.notna(coords['latitude']) and pd.notna(coords['longitude']):
                        zone_latlon.setdefault(zona_key, (coords['latitude'], coords['longitude']))
            else:
                zone_transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
                for zona_key in unique_zones:
                    zona_coords = zona_key.split('_')
                    if len(zona_coords) != 2:
                        continue
                    try:
                        zona_x, zona_y = float(zona_coords[0]), float(zona_coords[1])
                    except ValueError:
                        continue
                    zona_center_x = zona_x + zone_size_m / 2
                    zona_center_y = zona_y + zone_size_m / 2
                    lon, lat = zone_transformer.transform(zona_center_x, zona_center_y)
                    zone_latlon[zona_key] = (lat, lon)

            item_label = "úseky" if zone_mode == "segments" else "zóny"
            desc_label = "Generovanie úsekov pre nových operátorov" if zone_mode == "segments" else "Generovanie zón pre nových operátorov"
            print(f"Generujem {item_label} pre nových operátorov...")
            for zona_key in tqdm(
                unique_zones,
                desc=desc_label,
                disable=not progress_enabled
            ):
                for mcc, mnc, pci in custom_operators:
                    operator_key = f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    if zona_operator_key not in processed_zones:
                        base_row = sample_row.copy()

                        coords = zone_latlon.get(zona_key)
                        if coords is None and zone_mode != "segments" and zone_transformer is not None:
                            zona_coords = zona_key.split('_')
                            if len(zona_coords) == 2:
                                try:
                                    zona_x, zona_y = float(zona_coords[0]), float(zona_coords[1])
                                    zona_center_x = zona_x + zone_size_m / 2
                                    zona_center_y = zona_y + zone_size_m / 2
                                    lon, lat = zone_transformer.transform(zona_center_x, zona_center_y)
                                    coords = (lat, lon)
                                    zone_latlon[zona_key] = coords
                                except ValueError:
                                    coords = None
                        if coords is None:
                            continue

                        lat, lon = coords
                        base_row[lat_col] = f"{lat:.6f}"
                        base_row[lon_col] = f"{lon:.6f}"
                        base_row[rsrp_col] = "-174"

                        base_row[mcc_col] = mcc
                        base_row[mnc_col] = mnc
                        if pci_col is not None:
                            base_row[pci_col] = pci

                        row_values = []
                        for _, val in enumerate(base_row[column_names]):
                            if pd.isna(val):
                                row_values.append("")
                            else:
                                row_values.append(str(val))

                        while len(row_values) < expected_columns:
                            row_values.append("")

                        if len(row_values) > expected_columns:
                            row_values = row_values[:expected_columns]

                        csv_row = ';'.join(row_values)
                        csv_row += ";;"
                        if zone_mode == "segments":
                            csv_row += " # Prázdny úsek - vlastný operátor"
                        else:
                            csv_row += " # Prázdna zóna - vlastný operátor"

                        if first_custom_operator_line:
                            output_lines.append("\n" + csv_row + "\n")
                            first_custom_operator_line = False
                        else:
                            output_lines.append(csv_row + "\n")

                        new_zones_added += 1
                        processed_zones[zona_operator_key] = True

    # Zapíšeme výsledky späť do súboru
    with open(output_file, 'w', encoding='utf-8') as f:
        f.writelines(output_lines)

    if new_zones_added > 0:
        if zone_mode == "segments":
            print(f"Pridaných {new_zones_added} prázdnych úsekov pre vlastných operátorov.")
        else:
            print(f"Pridaných {new_zones_added} prázdnych zón pre vlastných operátorov.")

    # Pridáme vlastných operátorov do dataframe so štatistikami
    if len(unique_zones) > 0:
        default_zona_key = unique_zones[0]
    elif zone_mode == "segments":
        default_zona_key = "segment_0"
    else:
        default_zona_key = "0_0"

    default_zona_x = 0
    default_zona_y = 0
    default_longitude = 0
    default_latitude = 0
    if not zone_stats.empty:
        first_zone_row = zone_stats.iloc[0]
        default_zona_x = first_zone_row.get('zona_x', 0)
        default_zona_y = first_zone_row.get('zona_y', 0)
        default_longitude = first_zone_row.get('longitude', 0)
        default_latitude = first_zone_row.get('latitude', 0)

    for mcc, mnc, pci in custom_operators:
        new_row = pd.DataFrame({
            'zona_key': [default_zona_key],
            'operator_key': [f"{mcc}_{mnc}"],
            'zona_x': [default_zona_x],
            'zona_y': [default_zona_y],
            'mcc': [mcc],
            'mnc': [mnc],
            **({'pci': [pci]} if has_pci else {}),
            'rsrp_avg': [-174],
            'pocet_merani': [0],
            'najcastejsia_frekvencia': [''],
            'vsetky_frekvencie': [[]],
            'original_excel_rows': [[]],
            'zona_stred_x': [default_zona_x],
            'zona_stred_y': [default_zona_y],
            'longitude': [default_longitude],
            'latitude': [default_latitude],
            'rsrp_kategoria': ['RSRP_BAD']
        })

        # Pridáme SINR stĺpec, ak existuje
        if 'sinr_avg' in zone_stats.columns:
            new_row['sinr_avg'] = np.nan

        # Spojíme s existujúcim dataframe
        zone_stats = pd.concat([zone_stats, new_row], ignore_index=True)

    return zone_stats, True


def save_stats(
    zone_stats,
    original_file,
    include_empty_zones=False,
    rsrp_threshold=-110,
    sinr_threshold=-5,
    output_suffix="",
    zone_mode="zones",
    segment_meta=None,
    progress_enabled=True
):
    """Uloží štatistiky pre každého operátora do CSV súboru."""
    if output_suffix:
        stats_file = original_file.replace('.csv', f'{output_suffix}_stats.csv')
    else:
        stats_file = original_file.replace('.csv', '_stats.csv')

    # Získame všetky unikátne zóny
    if zone_mode == "segments" and segment_meta:
        all_zones = set([f"segment_{segment_id}" for segment_id in segment_meta.keys()])
    else:
        all_zones = set(zone_stats['zona_key'])
    total_unique_zones = len(all_zones)

    # Pripravíme dataframe pre štatistiky
    stats_data = []

    # Získame všetkých unikátnych operátorov
    operator_columns = ['mcc', 'mnc']
    operators = zone_stats[operator_columns].drop_duplicates()

    has_sinr = 'sinr_avg' in zone_stats.columns
    if has_sinr:
        good_column = f"RSRP >= {rsrp_threshold} a SINR >= {sinr_threshold}"
        bad_column = f"RSRP < {rsrp_threshold} alebo SINR < {sinr_threshold}"
    else:
        good_column = f"RSRP >= {rsrp_threshold}"
        bad_column = f"RSRP < {rsrp_threshold}"

    print("Vytváram štatistiky...")
    for _, op in tqdm(list(operators.iterrows()), desc="Štatistiky operátorov", disable=not progress_enabled):
        mcc, mnc = op['mcc'], op['mnc']

        # Filtrujeme zóny pre daného operátora
        op_zones = zone_stats[(zone_stats['mcc'] == mcc) & (zone_stats['mnc'] == mnc)]

        # Počítame RSRP kategórie
        good_count = len(op_zones[op_zones['rsrp_kategoria'] == 'RSRP_GOOD'])
        bad_count = len(op_zones[op_zones['rsrp_kategoria'] == 'RSRP_BAD'])

        # Počet chýbajúcich zón a ich započítanie iba ak používateľ zvolil generovanie prázdnych zón
        if include_empty_zones:
            existing_zones = set(op_zones['zona_key'])
            missing_zones = total_unique_zones - len(existing_zones)

            # Všetky chýbajúce zóny pridáme ako zlý signál
            bad_count += missing_zones

        # Konvertujeme MCC a MNC na celé čísla
        try:
            mcc_int = int(float(mcc))
        except:
            mcc_int = mcc

        try:
            mnc_int = int(float(mnc))
        except:
            mnc_int = mnc

        stats_row = {
            'MNC': mnc_int,
            'MCC': mcc_int,
        }
        stats_row[good_column] = good_count
        stats_row[bad_column] = bad_count
        stats_data.append(stats_row)

    # Vytvoríme dataframe a uložíme
    stats_df = pd.DataFrame(stats_data)
    stats_df.to_csv(stats_file, sep=';', index=False, encoding='utf-8')
    print(f"Štatistiky uložené do súboru: {stats_file}")
    print(f"Použitá RSRP hranica: {rsrp_threshold} dBm")
    if has_sinr:
        print(f"Použitá SINR hranica: {sinr_threshold} dB")
