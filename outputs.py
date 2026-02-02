# -*- coding: utf-8 -*-

import re

import numpy as np
import pandas as pd
from pyproj import Transformer
from tqdm import tqdm

from constants import ZONE_SIZE_METERS


def save_zone_results(zone_stats, original_file, df, column_mapping, column_names, file_info, use_zone_center, zone_mode="zones", output_suffix="", segment_meta=None):
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
    header_line = ';'.join(orig_header_cols) + ";Riadky_v_zone;Frekvencie_v_zone"

    # Spočítame očakávaný počet stĺpcov
    expected_columns = len(orig_header_cols)
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

    # Vytvorím progress bar pre zápis zón
    for i in tqdm(range(len(sorted_zone_stats)), desc="Zápis zón"):
        zone_row = sorted_zone_stats.iloc[i]
        zona_operator_key = f"{zone_row['zona_key']}_{zone_row['operator_key']}"

        # Overíme, či sme už túto zónu+operátora spracovali
        if zona_operator_key in processed_zones:
            continue

        # Označíme túto zónu ako spracovanú
        processed_zones[zona_operator_key] = True

        # Nájdeme vzorový riadok z tejto zóny a vybranej frekvencie
        rsrp_col = column_names[column_mapping['rsrp']]
        freq_col = column_names[column_mapping['frequency']]
        lat_col = column_names[column_mapping['latitude']]
        lon_col = column_names[column_mapping['longitude']]
        selected_frequency = zone_row['najcastejsia_frekvencia']
        if pd.isna(selected_frequency):
            sample_rows = df[(df['zona_operator_key'] == zona_operator_key) & (df[freq_col].isna())]
        else:
            sample_rows = df[(df['zona_operator_key'] == zona_operator_key) & (df[freq_col] == selected_frequency)]

        if len(sample_rows) == 0:
            continue  # Preskočíme, ak nemáme vzorový riadok

        # Vezmeme prvý riadok ako základ
        sample_row = sample_rows.iloc[0]

        # Vytvoríme kópiu vzorového riadku - už filtrovaného dataframu
        # Nepoužívame original_row_index, ktorý by mohol byť mimo rozsahu
        base_row = sample_row.copy()

        # Aktualizujeme hodnoty - používame bodku namiesto čiarky pre desatinné hodnoty
        base_row[rsrp_col] = f"{zone_row['rsrp_avg']:.2f}"
        base_row[freq_col] = zone_row['najcastejsia_frekvencia']

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
        for j, val in enumerate(base_row[column_names]):
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
    if zone_mode == "segments":
        generate_empty_zones = input(
            "Chcete vytvoriť prázdne úseky pre chýbajúce kombinácie úsekov a operátorov? (a/n): "
        ).lower() == 'a'
    else:
        generate_empty_zones = input(
            "Chcete vytvoriť prázdne zóny pre chýbajúce kombinácie zón a operátorov? (a/n): "
        ).lower() == 'a'

    if generate_empty_zones:
        if zone_mode == "segments":
            print("Generujem prázdne úseky...")
        else:
            print("Generujem prázdne zóny...")

        # Získame všetky unikátne zóny/úseky a operátorov
        unique_zones = sorted_zone_stats['zona_key'].unique()
        operator_columns = ['mcc', 'mnc']
        if 'pci' in sorted_zone_stats.columns:
            operator_columns.append('pci')
        unique_operators = sorted_zone_stats[operator_columns].drop_duplicates().values
        added_empty_zones = 0

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
            for zona_key in tqdm(unique_zones, desc="Generovanie prázdnych úsekov"):
                for operator_values in unique_operators:
                    mcc, mnc = operator_values[0], operator_values[1]
                    pci = operator_values[2] if len(operator_values) > 2 else ""
                    operator_key = f"{mcc}_{mnc}_{pci}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    if zona_operator_key not in processed_zones:
                        sample_operator_rows = df[
                            (df[mcc_col] == mcc) &
                            (df[mnc_col] == mnc)
                        ]
                        if pci_col is not None:
                            sample_operator_rows = sample_operator_rows[sample_operator_rows[pci_col] == pci]
                        if len(sample_operator_rows) == 0:
                            sample_operator_rows = df
                        sample_row = sample_operator_rows.iloc[0]
                        base_row = sample_row.copy()

                        rsrp_col = column_names[column_mapping['rsrp']]
                        lat_col = column_names[column_mapping['latitude']]
                        lon_col = column_names[column_mapping['longitude']]

                        if isinstance(segment_coords, dict):
                            coords = segment_coords.get(zona_key)
                            if coords:
                                lat, lon = coords
                                base_row[lat_col] = f"{lat:.6f}"
                                base_row[lon_col] = f"{lon:.6f}"
                        elif segment_coords is not None and zona_key in segment_coords.index:
                            lat = segment_coords.loc[zona_key, 'latitude']
                            lon = segment_coords.loc[zona_key, 'longitude']
                            base_row[lat_col] = f"{lat:.6f}"
                            base_row[lon_col] = f"{lon:.6f}"

                        base_row[rsrp_col] = "-174"

                        try:
                            base_row[mcc_col] = str(int(float(mcc)))
                        except:
                            base_row[mcc_col] = mcc

                        try:
                            base_row[mnc_col] = str(int(float(mnc)))
                        except:
                            base_row[mnc_col] = mnc

                        if pci_col is not None:
                            try:
                                base_row[pci_col] = str(int(float(pci)))
                            except:
                                base_row[pci_col] = pci

                        row_values = []
                        for j, val in enumerate(base_row[column_names]):
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

                        csv_row = ';'.join(row_values)
                        csv_row += ";;"
                        csv_row += " # Prázdny úsek - automaticky vygenerovaný"

                        output_lines.append(csv_row)
                        added_empty_zones += 1
        else:
            # Progress bar pre generovanie prázdnych zón
            for zona_key in tqdm(unique_zones, desc="Generovanie prázdnych zón"):
                for operator_values in unique_operators:
                    mcc, mnc = operator_values[0], operator_values[1]
                    pci = operator_values[2] if len(operator_values) > 2 else ""
                    operator_key = f"{mcc}_{mnc}_{pci}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    # Ak táto kombinácia neexistuje, vytvoríme ju
                    if zona_operator_key not in processed_zones:
                        # Nájdeme vzorový riadok s týmto operátorom
                        sample_operator_rows = df[
                            (df[mcc_col] == mcc) &
                            (df[mnc_col] == mnc)
                        ]
                        if pci_col is not None:
                            sample_operator_rows = sample_operator_rows[sample_operator_rows[pci_col] == pci]

                        # Ak nemáme vzorový riadok pre tohto operátora, vezmeme ľubovoľný riadok
                        if len(sample_operator_rows) == 0:
                            sample_operator_rows = df

                        # Vezmeme prvý riadok od operátora ako základ
                        sample_row = sample_operator_rows.iloc[0]
                        base_row = sample_row.copy()

                        # Aktualizujeme základné hodnoty
                        rsrp_col = column_names[column_mapping['rsrp']]
                        lat_col = column_names[column_mapping['latitude']]
                        lon_col = column_names[column_mapping['longitude']]

                        # Rozdelíme zona_key na súradnice
                        zona_coords = zona_key.split('_')
                        if len(zona_coords) == 2:
                            zona_x, zona_y = float(zona_coords[0]), float(zona_coords[1])

                            # Získame stred zóny
                            zona_center_x = zona_x + ZONE_SIZE_METERS/2
                            zona_center_y = zona_y + ZONE_SIZE_METERS/2

                            # Aktualizujeme súradnice na stred zóny alebo ponecháme pôvodné podľa nastavenia
                            if use_zone_center:
                                # Pre prázdne zóny pri use_zone_center=True nemusíme robiť nič extra,
                                # keďže nižšie sa vždy nastavujú súradnice stredu zóny
                                pass
                            # V prípade False necháme pôvodné súradnice (t.j. neaktualizujeme súradnice vôbec)

                            # Pre prázdne zóny vždy používame stredové súradnice
                            # Transformujeme späť na WGS84
                            transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)

                            # Vždy používame súradnice stredu zóny pre prázdne zóny
                            lon, lat = transformer.transform(zona_center_x, zona_center_y)

                            # Aktualizujeme hodnoty - používame bodku namiesto čiarky
                            base_row[lat_col] = f"{lat:.6f}"
                            base_row[lon_col] = f"{lon:.6f}"
                            base_row[rsrp_col] = "-174"  # Extrémne nízka hodnota pre prázdne zóny

                            # Upravíme MCC a MNC na celé čísla
                            try:
                                base_row[mcc_col] = str(int(float(mcc)))
                            except:
                                base_row[mcc_col] = mcc

                            try:
                                base_row[mnc_col] = str(int(float(mnc)))
                            except:
                                base_row[mnc_col] = mnc

                            if pci_col is not None:
                                try:
                                    base_row[pci_col] = str(int(float(pci)))
                                except:
                                    base_row[pci_col] = pci

                            # Vytvoríme riadok pre CSV s ošetrením NaN hodnôt
                            row_values = []
                            for j, val in enumerate(base_row[column_names]):
                                if pd.isna(val):
                                    row_values.append("")
                                # Kontrola, či index zodpovedá MCC alebo MNC
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

                            # Pridáme prázdne stĺpce pre zoznam riadkov a frekvencií
                            csv_row += ";;"

                            # Pridáme informáciu o prázdnej zóne
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


def add_custom_operators(zone_stats, df, column_mapping, column_names, output_file, use_zone_center, processed_zones, unique_zones):
    """Pridá vlastných operátorov do zón a štatistík."""
    add_operators = input("Chcete pridať vlastných operátorov, ktorí neboli nájdení v dátach? (a/n): ").lower() == 'a'

    if not add_operators:
        return zone_stats, False

    # Získame existujúcich operátorov
    has_pci = 'pci' in zone_stats.columns
    if has_pci:
        existing_operators = set([
            f"{mcc}_{mnc}_{pci}"
            for mcc, mnc, pci in zip(zone_stats['mcc'], zone_stats['mnc'], zone_stats['pci'])
        ])
    else:
        existing_operators = set([f"{mcc}_{mnc}" for mcc, mnc in zip(zone_stats['mcc'], zone_stats['mnc'])])

    custom_operators = []
    continue_adding = True

    # Regex vzor: MCC a MNC musia obsahovať iba čísla a nesmú byť prázdne
    mcc_pattern = re.compile(r'^\d+$')
    mnc_pattern = re.compile(r'^\d+$')
    pci_pattern = re.compile(r'^\d+$')

    while continue_adding:
        # Zadáme nových operátorov v jednom riadku oddelených dvojbodkou
        try:
            operators_input = input(
                "Zadajte MCC:MNC:PCI operátorov oddelené medzerou (napr. '231:01:123 231:02:45'), "
                "PCI je voliteľné. Alebo 'koniec' pre ukončenie: "
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
                operator_key = f"{mcc}_{mnc}_{pci}" if has_pci else f"{mcc}_{mnc}"
                if operator_key in existing_operators:
                    if has_pci:
                        print(f"Operátor s MCC={mcc}, MNC={mnc}, PCI={pci} už existuje v dátach!")
                    else:
                        print(f"Operátor s MCC={mcc} a MNC={mnc} už existuje v dátach!")
                    continue

                # Pridáme operátora do zoznamu
                custom_operators.append((mcc, mnc, pci))
                existing_operators.add(operator_key)
                if has_pci:
                    print(f"Operátor s MCC={mcc}, MNC={mnc}, PCI={pci} bol pridaný.")
                else:
                    print(f"Operátor s MCC={mcc} a MNC={mnc} bol pridaný.")
                added_in_batch = True

            # Opýtame sa, či chce pridať ďalšie, iba ak bol pridaný aspoň jeden operátor
            if added_in_batch and input("Chcete pridať ďalších operátorov? (a/n): ").lower() != 'a':
                continue_adding = False

        except Exception as e:
            print(f"Chyba pri zadávaní operátorov: {e}")

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

    # Pridáme nové riadky do súboru - prázdne zóny pre nových operátorov
    new_zones_added = 0

    # Ak máme nejaké zóny, môžeme pridať nových operátorov
    if len(unique_zones) > 0:
        # Vzorový riadok - vezmeme prvý riadok z dataframe
        if len(df) > 0:
            sample_row = df.iloc[0].copy()

            # Vytvoríme riadky pre nových operátorov
            rsrp_col = column_names[column_mapping['rsrp']]
            lat_col = column_names[column_mapping['latitude']]
            lon_col = column_names[column_mapping['longitude']]
            mcc_col = column_names[column_mapping['mcc']]
            mnc_col = column_names[column_mapping['mnc']]
            pci_col = column_names[column_mapping['pci']] if 'pci' in column_mapping else None

            # Premenná pre sledovanie, či sme už pridali prvý riadok
            first_custom_operator_line = True

            # Pre každú kombináciu zóny a nového operátora vytvoríme záznam
            print("Generujem zóny pre nových operátorov...")
            for zona_key in tqdm(unique_zones, desc="Generovanie zón pre nových operátorov"):
                for mcc, mnc, pci in custom_operators:
                    operator_key = f"{mcc}_{mnc}_{pci}" if has_pci else f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"

                    # Ak táto kombinácia neexistuje, vytvoríme ju
                    if zona_operator_key not in processed_zones:
                        base_row = sample_row.copy()

                        # Rozdelíme zona_key na súradnice
                        zona_coords = zona_key.split('_')
                        if len(zona_coords) == 2:
                            zona_x, zona_y = float(zona_coords[0]), float(zona_coords[1])

                            # Získame stred zóny
                            zona_center_x = zona_x + ZONE_SIZE_METERS/2
                            zona_center_y = zona_y + ZONE_SIZE_METERS/2

                            # Pre prázdne zóny vždy používame stredové súradnice
                            # Transformujeme späť na WGS84
                            transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)

                            # Vždy používame súradnice stredu zóny pre prázdne zóny
                            lon, lat = transformer.transform(zona_center_x, zona_center_y)

                            # Aktualizujeme hodnoty - používame bodku namiesto čiarky
                            base_row[lat_col] = f"{lat:.6f}"
                            base_row[lon_col] = f"{lon:.6f}"
                            base_row[rsrp_col] = "-174"  # Extrémne nízka hodnota pre prázdne zóny

                            # Nastavíme MCC a MNC
                            base_row[mcc_col] = mcc
                            base_row[mnc_col] = mnc
                            if pci_col is not None:
                                base_row[pci_col] = pci

                            # Vytvoríme riadok pre CSV s ošetrením NaN hodnôt
                            row_values = []
                            for j, val in enumerate(base_row[column_names]):
                                if pd.isna(val):
                                    row_values.append("")
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

                            # Pridáme prázdne stĺpce pre zoznam riadkov a frekvencií
                            csv_row += ";;"

                            # Pridáme informáciu o prázdnej zóne
                            csv_row += " # Prázdna zóna - vlastný operátor"

                            # Ak je to prvý riadok vlastného operátora, pridáme prázdny riadok pred ním
                            if first_custom_operator_line:
                                output_lines.append("\n" + csv_row + "\n")
                                first_custom_operator_line = False
                            else:
                                output_lines.append(csv_row + "\n")

                            new_zones_added += 1

                            # Označíme túto zónu ako spracovanú
                            processed_zones[zona_operator_key] = True

    # Zapíšeme výsledky späť do súboru
    with open(output_file, 'w', encoding='utf-8') as f:
        f.writelines(output_lines)

    if new_zones_added > 0:
        print(f"Pridaných {new_zones_added} prázdnych zón pre vlastných operátorov.")

    # Pridáme vlastných operátorov do dataframe so štatistikami
    for mcc, mnc, pci in custom_operators:
        # Vytvoríme nový riadok pre tento operátor
        new_row = pd.DataFrame({
            'zona_key': [unique_zones[0] if len(unique_zones) > 0 else '0_0'],
            'operator_key': [f"{mcc}_{mnc}_{pci}" if has_pci else f"{mcc}_{mnc}"],
            'zona_x': [0],
            'zona_y': [0],
            'mcc': [mcc],
            'mnc': [mnc],
            **({'pci': [pci]} if has_pci else {}),
            'rsrp_avg': [-174],
            'pocet_merani': [0],
            'najcastejsia_frekvencia': [''],
            'vsetky_frekvencie': [[]],  # Prázdny zoznam pre všetky frekvencie
            'original_excel_rows': [[]],  # Prázdny zoznam pre originálne excell riadky
            'zona_stred_x': [0],
            'zona_stred_y': [0],
            'longitude': [0],
            'latitude': [0],
            'rsrp_kategoria': ['RSRP_BAD']
        })

        # Pridáme SINR stĺpec, ak existuje
        if 'sinr_avg' in zone_stats.columns:
            new_row['sinr_avg'] = np.nan

        # Spojíme s existujúcim dataframe
        zone_stats = pd.concat([zone_stats, new_row], ignore_index=True)

    return zone_stats, True


def save_stats(zone_stats, original_file, include_empty_zones=False, rsrp_threshold=-110, output_suffix="", zone_mode="zones", segment_meta=None):
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
    if 'pci' in zone_stats.columns:
        operator_columns.append('pci')
    operators = zone_stats[operator_columns].drop_duplicates()

    # Vytvoríme dynamické názvy stĺpcov na základe RSRP hranice
    rsrp_good_column = f"RSRP >= {rsrp_threshold}"
    rsrp_bad_column = f"RSRP < {rsrp_threshold}"

    print("Vytváram štatistiky...")
    for _, op in tqdm(list(operators.iterrows()), desc="Štatistiky operátorov"):
        mcc, mnc = op['mcc'], op['mnc']
        pci = op['pci'] if 'pci' in op else None

        # Filtrujeme zóny pre daného operátora
        op_zones = zone_stats[(zone_stats['mcc'] == mcc) & (zone_stats['mnc'] == mnc)]
        if 'pci' in zone_stats.columns:
            op_zones = op_zones[op_zones['pci'] == pci]

        # Počítame RSRP kategórie
        rsrp_good = len(op_zones[op_zones['rsrp_kategoria'] == 'RSRP_GOOD'])
        rsrp_bad = len(op_zones[op_zones['rsrp_kategoria'] == 'RSRP_BAD'])

        # Počet chýbajúcich zón a ich započítanie iba ak používateľ zvolil generovanie prázdnych zón
        if include_empty_zones:
            existing_zones = set(op_zones['zona_key'])
            missing_zones = total_unique_zones - len(existing_zones)

            # Všetky chýbajúce zóny pridáme ako zlý signál
            rsrp_bad += missing_zones

        # Konvertujeme MCC a MNC na celé čísla
        try:
            mcc_int = int(float(mcc))
        except:
            mcc_int = mcc

        try:
            mnc_int = int(float(mnc))
        except:
            mnc_int = mnc

        pci_int = None
        if 'pci' in zone_stats.columns:
            try:
                pci_int = int(float(pci))
            except:
                pci_int = pci

        stats_row = {
            'MNC': mnc_int,
            'MCC': mcc_int,
        }
        if 'pci' in zone_stats.columns:
            stats_row['PCI'] = pci_int
        stats_row[rsrp_good_column] = rsrp_good
        stats_row[rsrp_bad_column] = rsrp_bad
        stats_data.append(stats_row)

    # Vytvoríme dataframe a uložíme
    stats_df = pd.DataFrame(stats_data)
    stats_df.to_csv(stats_file, sep=';', index=False, encoding='utf-8')
    print(f"Štatistiky uložené do súboru: {stats_file}")
    print(f"Použitá RSRP hranica: {rsrp_threshold} dBm")
