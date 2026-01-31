#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import pandas as pd
import numpy as np
import os
from pyproj import Transformer, Geod
import argparse
from collections import defaultdict
from itertools import product
from tqdm import tqdm
import re

# Konštanty
ZONE_SIZE_METERS = 100  # Veľkosť zóny v metroch

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
            if row_value < condition[1] or row_value > condition[2]:
                return False
    return True

def _row_matches_filter(row, filter_rule):
    for group in filter_rule["condition_groups"]:
        if _row_matches_group(row, group):
            return True
    return False

def _load_filter_rules():
    filter_rules = []
    base_dir = os.getcwd()
    filter_dirs = [os.path.join(base_dir, "filters"), os.path.join(base_dir, "filtre_5G")]
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
        filter_rules = _load_filter_rules()
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
        matching_filters = [rule for rule in filter_rules if _row_matches_filter(row, rule)]

        if len(matching_filters) > 1:
            print(f"CHYBA: Riadok {row_number} vyhovuje viac filtrom. Spracovanie sa zastavi.")
            raise SystemExit(1)

        if len(matching_filters) == 1:
            rule = matching_filters[0]
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
    if not result_df.empty:
        extra_columns = [col for col in result_df.columns if col not in base_columns]
        result_df = result_df[base_columns + extra_columns]
    return result_df

def _maybe_dump_filtered_df(df, original_file):
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

def parse_arguments():
    """Spracovanie argumentov príkazového riadka."""
    parser = argparse.ArgumentParser(description='Spracovanie CSV súboru s meraniami do zón.')
    parser.add_argument('file', nargs='?', help='Cesta k CSV súboru')
    return parser.parse_args()

def ask_for_rsrp_threshold():
    """Opýta sa používateľa na hranicu RSRP pre štatistiky."""
    print("\nNastavenie hranice RSRP pre štatistiky:")
    print("Predvolená hodnota: -110 dBm")
    
    while True:
        choice = input("Chcete použiť predvolenú hodnotu -110 dBm? (a/n): ").strip().lower()
        if choice == "a":
            return -110
        elif choice == "n":
            while True:
                try:
                    threshold = input("Zadajte vlastnú hranicu RSRP (napr. -105): ").strip()
                    threshold_value = float(threshold)
                    print(f"Použije sa hranica RSRP: {threshold_value} dBm")
                    return threshold_value
                except ValueError:
                    print("Neplatná hodnota. Prosím zadajte číslo (napr. -105).")
        else:
            print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")

def ask_for_zone_mode():
    """Opýta sa používateľa na režim spracovania zón/úsekov."""
    print("\nNastavenie súradníc a režimu:")
    print("1 - Štvorcové zóny (súradnice stredu zóny)")
    print("2 - Štvorcové zóny (prvý bod v zóne)")
    print("3 - 100m úseky podľa poradia meraní (prvý bod úseku)")
    
    while True:
        choice = input("Vyberte možnosť [1/2/3]: ").strip()
        if choice == "1":
            return "center"
        elif choice == "2":
            return "original"
        elif choice == "3":
            return "segments"
        else:
            print("Neplatná voľba. Prosím zadajte 1, 2 alebo 3.")

def ask_for_keep_original_rows():
    """Opýta sa používateľa, či sa majú ponechať pôvodné riadky po filtrovaní."""
    print("\nNastavenie filtrov:")
    while True:
        choice = input("Chcete ponechať pôvodný riadok a pridať nový s filtrom? (a/n): ").strip().lower()
        if choice == "a":
            return True
        elif choice == "n":
            return False
        else:
            print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")

def load_csv_file(file_path):
    """Načíta CSV súbor a vráti DataFrame a informácie o súbore."""
    # Zoznam kódovaní, ktoré skúsime
    encodings = ['utf-8', 'latin1', 'latin2', 'cp1250', 'windows-1250', 'iso-8859-2']
    
    # Najprv sa pokúsime nájsť hlavičku s rôznymi kódovaniami
    header_line = -1
    encoding_to_use = None
    original_header = None
    
    for enc in encodings:
        try:
            with open(file_path, 'r', encoding=enc) as f:
                lines = f.readlines()
                for i, line in enumerate(lines):
                    if ';' in line and len(line.split(';')) > 5:  # Hľadáme CSV riadok
                        header_line = i
                        encoding_to_use = enc
                        original_header = line.strip()
                        break
                if header_line != -1:
                    break
        except Exception:
            continue
    
    if header_line == -1 or encoding_to_use is None:
        # Skúsime predvolené kódovania bez pýtania
        for enc in ['utf-8', 'latin1', 'cp1250']:
            try:
                with open(file_path, 'r', encoding=enc) as f:
                    lines = f.readlines()
                    # Ak sme načítali súbor bez chyby, použijeme toto kódovanie
                    encoding_to_use = enc
                    # Pokúsime sa nájsť hlavičku
                    for i, line in enumerate(lines):
                        if ';' in line and len(line.split(';')) > 5:  # Hľadáme CSV riadok
                            header_line = i
                            original_header = line.strip()
                            break
                    break
            except Exception:
                continue
    
    if header_line == -1:
        header_line = 0  # Ak nenájdeme hlavičku, predpokladáme že je to prvý riadok
    
    # Teraz načítame súbor s identifikovaným kódovaním
    try:
        df = pd.read_csv(file_path, sep=';', header=header_line, encoding=encoding_to_use or 'utf-8')
        # Vrátime DataFrame a informácie o súbore
        return df, {
            'encoding': encoding_to_use or 'utf-8',
            'header_line': header_line,
            'original_header': original_header
        }
    except Exception as e:
        print(f"Chyba pri načítaní súboru: {e}")
        return None, None

def process_data(df, column_mapping, header_line=0, zone_mode="zones"):
    """Spracuje dataframe a rozdelí ho do zón alebo úsekov."""
    # Vytvoríme transformátor z WGS84 (latitute, longitude) na S-JTSK (metre) - optimálna projekcia pre Slovensko
    transformer = Transformer.from_crs("EPSG:4326", "EPSG:5514", always_xy=True)
    
    # Získame názvy stĺpcov z dataframe
    column_names = list(df.columns)
    
    # Filtrujeme riadky s chýbajúcimi RSRP hodnotami
    rsrp_col = column_names[column_mapping['rsrp']]
    df_filtered = df.copy()
    
    # Zachovávame originálne indexy riadkov z pôvodného súboru
    # Pridáme header_line + 1 aby sme správne vypočítali Excel riadok
    if 'original_excel_row' not in df_filtered.columns:
        df_filtered['original_excel_row'] = df_filtered.index + header_line + 1
    
    # Konvertujeme RSRP hodnoty na float a označujeme chýbajúce hodnoty ako NaN
    df_filtered[rsrp_col] = df_filtered[rsrp_col].apply(
        lambda x: float(str(x).replace(',', '.')) if pd.notna(x) and str(x).strip() else np.nan
    )
    
    # Odstránime riadky s chýbajúcimi RSRP hodnotami
    missing_rsrp_count = df_filtered[rsrp_col].isna().sum()
    if missing_rsrp_count > 0:
        print(f"Odstraňujem {missing_rsrp_count} riadkov s chýbajúcimi RSRP hodnotami.")
        df_filtered = df_filtered.dropna(subset=[rsrp_col])
    
    # Vytvoríme progress bar
    print("Spracovávam merania...")
    total_rows = len(df_filtered)
    
    # Budeme spracovávať dáta po častiach pre rýchlejšie spracovanie
    chunk_size = 1000
    num_chunks = (total_rows + chunk_size - 1) // chunk_size
    
    # Inicializujeme nové stĺpce
    df_filtered['x_meters'] = 0.0
    df_filtered['y_meters'] = 0.0
    
    # Spracovávame dáta po častiach s progress barom
    for i in tqdm(range(0, total_rows, chunk_size), total=num_chunks, desc="Transformácia súradníc"):
        end_idx = min(i + chunk_size, total_rows)
        chunk = df_filtered.iloc[i:end_idx]
        
        # Transformujeme súradnice pre túto časť
        x_meters, y_meters = zip(*[
            transformer.transform(
                float(str(row[column_names[column_mapping['longitude']]]).replace(',', '.')),
                float(str(row[column_names[column_mapping['latitude']]]).replace(',', '.'))
            ) for _, row in chunk.iterrows()
        ])
        
        # Uložíme výsledky
        df_filtered.loc[df_filtered.index[i:end_idx], 'x_meters'] = x_meters
        df_filtered.loc[df_filtered.index[i:end_idx], 'y_meters'] = y_meters
    
    # Výpočet zóny/úseku pre každé meranie
    if zone_mode == "segments":
        print("Počítam úseky...")
        total_rows = len(df_filtered)
        x_values = df_filtered['x_meters'].to_numpy()
        y_values = df_filtered['y_meters'].to_numpy()
        
        segment_ids = np.zeros(total_rows, dtype=int)
        segment_start_xs = np.zeros(total_rows)
        segment_start_ys = np.zeros(total_rows)
        
        if total_rows > 0:
            current_segment = 0
            segment_start_x = x_values[0]
            segment_start_y = y_values[0]
            prev_x = x_values[0]
            prev_y = y_values[0]
            cumulative_distance = 0.0
            
            for i in range(total_rows):
                x = x_values[i]
                y = y_values[i]
                
                if i != 0:
                    step_distance = ((x - prev_x) ** 2 + (y - prev_y) ** 2) ** 0.5
                    if cumulative_distance + step_distance > ZONE_SIZE_METERS:
                        current_segment += 1
                        segment_start_x = x
                        segment_start_y = y
                        cumulative_distance = 0.0
                    else:
                        cumulative_distance += step_distance
                    prev_x = x
                    prev_y = y
                
                segment_ids[i] = current_segment
                segment_start_xs[i] = segment_start_x
                segment_start_ys[i] = segment_start_y
        
        df_filtered['segment_id'] = segment_ids
        df_filtered['zona_x'] = segment_start_xs
        df_filtered['zona_y'] = segment_start_ys
        df_filtered['zona_key'] = [f"segment_{segment_id}" for segment_id in segment_ids]
    else:
        print("Počítam zóny...")
        df_filtered['zona_x'] = (df_filtered['x_meters'] // ZONE_SIZE_METERS) * ZONE_SIZE_METERS
        df_filtered['zona_y'] = (df_filtered['y_meters'] // ZONE_SIZE_METERS) * ZONE_SIZE_METERS
        
        # Vytvoríme kľúč zóny a operátora
        df_filtered['zona_key'] = df_filtered['zona_x'].astype(str) + '_' + df_filtered['zona_y'].astype(str)
    df_filtered['operator_key'] = df_filtered[column_names[column_mapping['mcc']]].astype(str) + '_' + df_filtered[column_names[column_mapping['mnc']]].astype(str)
    
    # Vytvoríme kombinovaný kľúč zóna+operátor
    df_filtered['zona_operator_key'] = df_filtered['zona_key'] + '_' + df_filtered['operator_key']
    
    # Zachováme originálny riadok pre neskoršie použitie
    df_filtered['original_row_index'] = df_filtered.index
    
    return df_filtered, column_names

def calculate_zone_stats(df, column_mapping, column_names, rsrp_threshold=-110, zone_mode="zones"):
    """Vypočíta štatistiky pre každú zónu alebo úsek."""
    if zone_mode == "segments":
        print("Počítam štatistiky pre úseky...")
    else:
        print("Počítam štatistiky pre zóny...")
    
    # Pripravíme SINR stĺpec pre výpočet priemeru, ak existuje
    sinr_col = None
    if 'sinr' in column_mapping:
        sinr_col = column_names[column_mapping['sinr']]
        # Konvertujeme SINR hodnoty na float a ignorujeme chýbajúce hodnoty
        df[sinr_col] = df[sinr_col].apply(
            lambda x: float(str(x).replace(',', '.')) if pd.notna(x) and str(x).strip() else np.nan
        )
    
    rsrp_col = column_names[column_mapping['rsrp']]
    freq_col = column_names[column_mapping['frequency']]
    mcc_col = column_names[column_mapping['mcc']]
    mnc_col = column_names[column_mapping['mnc']]
    
    # Agregačný slovník pre rôzne stĺpce (po frekvencii v rámci zóny + operátora)
    agg_kwargs = {
        'rsrp_avg': (rsrp_col, 'mean'),
        'pocet_merani': (rsrp_col, 'count'),
        'original_excel_rows': ('original_excel_row', lambda x: list(x))
    }
    
    # Pridáme SINR do agregácie, ak existuje
    if sinr_col:
        agg_kwargs['sinr_avg'] = (sinr_col, lambda x: x.dropna().mean() if len(x.dropna()) > 0 else np.nan)
    
    # Agregácia dát podľa zón, operátorov a frekvencií
    zone_freq_stats = df.groupby(
        ['zona_key', 'operator_key', 'zona_x', 'zona_y', mcc_col, mnc_col, freq_col]
    ).agg(**agg_kwargs).reset_index()
    
    # Výber najlepšej frekvencie podľa priemernej RSRP (s deterministickým tie-break)
    zone_freq_stats['frequency_sort_numeric'] = pd.to_numeric(zone_freq_stats[freq_col], errors='coerce')
    zone_freq_stats['frequency_sort_text'] = zone_freq_stats[freq_col].astype(str)
    zone_freq_stats = zone_freq_stats.sort_values(
        ['rsrp_avg', 'pocet_merani', 'frequency_sort_numeric', 'frequency_sort_text'],
        ascending=[False, False, True, True]
    )
    
    zone_stats = zone_freq_stats.groupby(
        ['zona_key', 'operator_key', 'zona_x', 'zona_y', mcc_col, mnc_col],
        as_index=False
    ).first()
    
    # Po výbere necháme len jednu frekvenciu
    zone_stats['najcastejsia_frekvencia'] = zone_stats[freq_col]
    zone_stats['vsetky_frekvencie'] = zone_stats[freq_col].apply(lambda value: [value])
    zone_stats = zone_stats.rename(columns={mcc_col: 'mcc', mnc_col: 'mnc'})
    zone_stats = zone_stats.drop(columns=[freq_col, 'frequency_sort_numeric', 'frequency_sort_text'])
    
    new_columns = [
        'zona_key', 'operator_key', 'zona_x', 'zona_y', 'mcc', 'mnc',
        'rsrp_avg', 'pocet_merani', 'najcastejsia_frekvencia', 'vsetky_frekvencie', 'original_excel_rows'
    ]
    
    if sinr_col:
        new_columns.append('sinr_avg')
    
    zone_stats = zone_stats[new_columns]
    
    # Konvertujeme zona_x a zona_y späť na latitude/longitude (stred zóny alebo začiatok úseku)
    transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
    
    # Pridáme stred zóny alebo štart úseku
    if zone_mode == "segments":
        zone_stats['zona_stred_x'] = zone_stats['zona_x']
        zone_stats['zona_stred_y'] = zone_stats['zona_y']
    else:
        zone_stats['zona_stred_x'] = zone_stats['zona_x'] + ZONE_SIZE_METERS/2
        zone_stats['zona_stred_y'] = zone_stats['zona_y'] + ZONE_SIZE_METERS/2
    
    # Transformujeme späť na WGS84 s progress barom
    if zone_mode == "segments":
        print("Transformujem súradnice úsekov...")
    else:
        print("Transformujem súradnice zón...")
    zone_stats['longitude'] = 0.0
    zone_stats['latitude'] = 0.0
    
    for i in tqdm(range(len(zone_stats)), desc="Transformácia zón"):
        lon, lat = transformer.transform(
            zone_stats.iloc[i]['zona_stred_x'], 
            zone_stats.iloc[i]['zona_stred_y']
        )
        zone_stats.loc[zone_stats.index[i], 'longitude'] = lon
        zone_stats.loc[zone_stats.index[i], 'latitude'] = lat
    
    # Klasifikácia RSRP s použitím zadanej hranice
    zone_stats['rsrp_kategoria'] = np.where(zone_stats['rsrp_avg'] >= rsrp_threshold, 'RSRP_GOOD', 'RSRP_BAD')
    
    return zone_stats

def save_zone_results(zone_stats, original_file, df, column_mapping, column_names, file_info, use_zone_center, zone_mode="zones"):
    """Uloží výsledky zón alebo úsekov do CSV súboru, zachovávajúc pôvodný formát riadkov."""
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
    
    # Vytvoríme nový obsah pre výstupný súbor - začíname prázdnym riadkom
    output_lines = ['']  # Prázdny riadok na začiatku
    if header_line:
        output_lines.append(header_line)
    
    # Pre každú zónu vytvoríme riadok založený na prvom meraní v danej zóne
    processed_zones = {}  # Slúži na sledovanie už spracovaných zón
    
    # Zoradíme zóny podľa operátora (MCC, MNC)
    sorted_zone_stats = zone_stats.sort_values(['mcc', 'mnc'])
    
    # Získame všetky unikátne zóny bez ohľadu na to, či budeme generovať prázdne zóny
    unique_zones = sorted_zone_stats['zona_key'].unique()
    
    print("Zapisujem výsledky zón...")
    
    # Kontrolujeme, či máme SINR stĺpec
    has_sinr = 'sinr' in column_mapping and 'sinr_avg' in sorted_zone_stats.columns
    
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
            elif j == column_mapping['mcc'] or j == column_mapping['mnc']:
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
        unique_operators = sorted_zone_stats[['mcc', 'mnc']].drop_duplicates().values
        added_empty_zones = 0
        
        if zone_mode == "segments":
            segment_coords = zone_stats.groupby('zona_key')[['longitude', 'latitude']].first()
            for zona_key in tqdm(unique_zones, desc="Generovanie prázdnych úsekov"):
                for mcc, mnc in unique_operators:
                    operator_key = f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"
                    
                    if zona_operator_key not in processed_zones:
                        sample_operator_rows = df[
                            (df[column_names[column_mapping['mcc']]] == mcc) &
                            (df[column_names[column_mapping['mnc']]] == mnc)
                        ]
                        if len(sample_operator_rows) == 0:
                            sample_operator_rows = df
                        sample_row = sample_operator_rows.iloc[0]
                        base_row = sample_row.copy()
                        
                        rsrp_col = column_names[column_mapping['rsrp']]
                        lat_col = column_names[column_mapping['latitude']]
                        lon_col = column_names[column_mapping['longitude']]
                        mcc_col = column_names[column_mapping['mcc']]
                        mnc_col = column_names[column_mapping['mnc']]
                        
                        if zona_key in segment_coords.index:
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
                        
                        row_values = []
                        for j, val in enumerate(base_row[column_names]):
                            if pd.isna(val):
                                row_values.append("")
                            elif j == column_mapping['mcc'] or j == column_mapping['mnc']:
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
                for mcc, mnc in unique_operators:
                    operator_key = f"{mcc}_{mnc}"
                    zona_operator_key = f"{zona_key}_{operator_key}"
                    
                    # Ak táto kombinácia neexistuje, vytvoríme ju
                    if zona_operator_key not in processed_zones:
                        # Nájdeme vzorový riadok s týmto operátorom
                        sample_operator_rows = df[
                            (df[column_names[column_mapping['mcc']]] == mcc) & 
                            (df[column_names[column_mapping['mnc']]] == mnc)
                        ]
                        
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
                        mcc_col = column_names[column_mapping['mcc']]
                        mnc_col = column_names[column_mapping['mnc']]
                        
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
                            
                            # Vytvoríme riadok pre CSV s ošetrením NaN hodnôt
                            row_values = []
                            for j, val in enumerate(base_row[column_names]):
                                if pd.isna(val):
                                    row_values.append("")
                                # Kontrola, či index zodpovedá MCC alebo MNC
                                elif j == column_mapping['mcc'] or j == column_mapping['mnc']:
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
    existing_operators = set([f"{mcc}_{mnc}" for mcc, mnc in zip(zone_stats['mcc'], zone_stats['mnc'])])
    
    custom_operators = []
    continue_adding = True
    
    # Regex vzor: MCC a MNC musia obsahovať iba čísla a nesmú byť prázdne
    mcc_pattern = re.compile(r'^\d+$')
    mnc_pattern = re.compile(r'^\d+$')
    
    while continue_adding:
        # Zadáme nových operátorov v jednom riadku oddelených dvojbodkou
        try:
            operators_input = input("Zadajte MCC:MNC operátorov oddelené medzerou (napr. '231:01 231:02'), alebo 'koniec' pre ukončenie: ").strip()
            
            # Kontrola ukončenia
            if not operators_input or operators_input.lower() in ['koniec', 'quit', 'q', 'exit']:
                continue_adding = False
                continue
            
            operator_pairs = operators_input.split()
            added_in_batch = False
            
            for operator_pair in operator_pairs:
                parts = operator_pair.split(':')
                if len(parts) != 2:
                    print(f"Neplatný formát operátora '{operator_pair}'. Použite formát MCC:MNC.")
                    continue
                    
                mcc, mnc = parts
                
                # Validácia MCC a MNC pomocou regex
                if not mcc_pattern.match(mcc):
                    print(f"Neplatné MCC '{mcc}'. MCC musí obsahovať iba čísla a nesmie byť prázdne.")
                    continue
                    
                if not mnc_pattern.match(mnc):
                    print(f"Neplatné MNC '{mnc}'. MNC musí obsahovať iba čísla a nesmie byť prázdne.")
                    continue
                
                # Skontrolujeme, či tento operátor už existuje
                operator_key = f"{mcc}_{mnc}"
                if operator_key in existing_operators:
                    print(f"Operátor s MCC={mcc} a MNC={mnc} už existuje v dátach!")
                    continue
                    
                # Pridáme operátora do zoznamu
                custom_operators.append((mcc, mnc))
                existing_operators.add(operator_key)
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
            
            # Premenná pre sledovanie, či sme už pridali prvý riadok
            first_custom_operator_line = True
            
            # Pre každú kombináciu zóny a nového operátora vytvoríme záznam
            print("Generujem zóny pre nových operátorov...")
            for zona_key in tqdm(unique_zones, desc="Generovanie zón pre nových operátorov"):
                for mcc, mnc in custom_operators:
                    operator_key = f"{mcc}_{mnc}"
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
    for mcc, mnc in custom_operators:
        # Vytvoríme nový riadok pre tento operátor
        new_row = pd.DataFrame({
            'zona_key': [unique_zones[0] if len(unique_zones) > 0 else '0_0'],
            'operator_key': [f"{mcc}_{mnc}"],
            'zona_x': [0],
            'zona_y': [0],
            'mcc': [mcc],
            'mnc': [mnc],
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

def save_stats(zone_stats, original_file, include_empty_zones=False, rsrp_threshold=-110):
    """Uloží štatistiky pre každého operátora do CSV súboru."""
    stats_file = original_file.replace('.csv', '_stats.csv')
    
    # Získame všetky unikátne zóny
    all_zones = set(zone_stats['zona_key'])
    total_unique_zones = len(all_zones)
    
    # Pripravíme dataframe pre štatistiky
    stats_data = []
    
    # Získame všetkých unikátnych operátorov
    operators = zone_stats[['mcc', 'mnc']].drop_duplicates()
    
    # Vytvoríme dynamické názvy stĺpcov na základe RSRP hranice
    rsrp_good_column = f"RSRP >= {rsrp_threshold}"
    rsrp_bad_column = f"RSRP < {rsrp_threshold}"
    
    print("Vytváram štatistiky...")
    for _, op in tqdm(list(operators.iterrows()), desc="Štatistiky operátorov"):
        mcc, mnc = op['mcc'], op['mnc']
        
        # Filtrujeme zóny pre daného operátora
        op_zones = zone_stats[(zone_stats['mcc'] == mcc) & (zone_stats['mnc'] == mnc)]
        
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
        
        stats_data.append({
            'MNC': mnc_int,
            'MCC': mcc_int,
            rsrp_good_column: rsrp_good,
            rsrp_bad_column: rsrp_bad
        })
    
    # Vytvoríme dataframe a uložíme
    stats_df = pd.DataFrame(stats_data)
    stats_df.to_csv(stats_file, sep=';', index=False, encoding='utf-8')
    print(f"Štatistiky uložené do súboru: {stats_file}")
    print(f"Použitá RSRP hranica: {rsrp_threshold} dBm")

def get_column_mapping():
    """Získa mapovanie stĺpcov podľa predvolených hodnôt alebo od používateľa."""
    default_columns = {
        'latitude': 'D',
        'longitude': 'E',
        'frequency': 'K',
        'mnc': 'N',
        'mcc': 'M',
        'rsrp': 'W',
        'sinr': 'V'
    }
    
    print("Predvolené hodnoty stĺpcov:")
    for name, col in default_columns.items():
        print(f"  {name}: {col}")
    
    use_default = input("Chcete použiť predvolené hodnoty stĺpcov? (a/n): ").lower() == 'a'
    
    if use_default:
        # Použijeme predvolené stĺpce
        columns = {}
        for name, col in default_columns.items():
            columns[name] = col_letter_to_name(col)
        return columns
    else:
        # Používateľ zadá vlastné hodnoty
        columns = {}
        for name, default in default_columns.items():
            col = input(f"Zadajte písmeno stĺpca pre {name} [{default}]: ") or default
            columns[name] = col_letter_to_name(col)
        return columns

def col_letter_to_name(letter):
    """Konvertuje písmeno stĺpca (napr. 'A', 'B'...) na názov stĺpca v pandas DataFrame."""
    letter = letter.upper()
    col_index = ord(letter) - ord('A')
    
    if col_index < 0 or col_index > 25:
        return 0  # Vrátime index 0 (prvý stĺpec)
    
    # Vrátime index stĺpca (napr. 'A' -> 0, 'B' -> 1)
    return col_index

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
    filter_rules = _load_filter_rules()
    if filter_rules:
        keep_original_rows = ask_for_keep_original_rows()
        df = apply_filters(df, file_info, filter_rules, keep_original_rows)
        _maybe_dump_filtered_df(df, file_path)
    
    # Opýtame sa používateľa na režim spracovania zón
    zone_mode = ask_for_zone_mode()
    use_zone_center = zone_mode == "center"
    if zone_mode == "segments":
        print("Použijú sa 100m úseky podľa poradia meraní.")
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
    processed_df, column_names = process_data(df, column_mapping, header_line, zone_mode)
    
    # Vypočítame štatistiky zón s použitím zadanej RSRP hranice
    zone_stats = calculate_zone_stats(processed_df, column_mapping, column_names, rsrp_threshold, zone_mode)
    
    # Uložíme výsledky zachovávajúc pôvodný formát
    output_file = file_path.replace('.csv', '_zones.csv')
    include_empty_zones, processed_zones, unique_zones = save_zone_results(
        zone_stats,
        file_path,
        processed_df,
        column_mapping,
        column_names,
        file_info,
        use_zone_center,
        zone_mode
    )
    
    # Pridáme vlastných operátorov iba ak používateľ chce generovať prázdne zóny
    custom_operators_added = False
    if include_empty_zones and zone_mode != "segments":
        zone_stats, custom_operators_added = add_custom_operators(
            zone_stats, processed_df, column_mapping, column_names, 
            output_file, use_zone_center, processed_zones, unique_zones
        )
    
    # Uložíme štatistiky - zohľadňujeme voľbu používateľa o prázdnych zónach a RSRP hranicu
    save_stats(zone_stats, file_path, include_empty_zones, rsrp_threshold)
    
    # Vypíšeme informácie o zónach/úsekoch a rozsahu merania
    print("\nSúhrn spracovania:")
    
    # Počet unikátnych zón/úsekov a operátorov
    unique_zones = zone_stats['zona_key'].nunique()
    unique_operators = zone_stats[['mcc', 'mnc']].drop_duplicates().shape[0]
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
    main() 