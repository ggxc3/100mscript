#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import pandas as pd
import numpy as np
import os
from pyproj import Transformer, Geod
import argparse
from collections import defaultdict
from tqdm import tqdm
import re

# Konštanty
ZONE_SIZE_METERS = 100  # Veľkosť zóny v metroch

def parse_arguments():
    """Spracovanie argumentov príkazového riadka."""
    parser = argparse.ArgumentParser(description='Spracovanie CSV súboru s meraniami do zón.')
    parser.add_argument('file', nargs='?', help='Cesta k CSV súboru')
    return parser.parse_args()

def ask_for_zone_center():
    """Opýta sa používateľa, či sa majú použiť súradnice stredu zóny alebo pôvodné súradnice."""
    print("\nNastavenie súradníc v zónach:")
    print("1 - Použiť súradnice stredu zóny")
    print("2 - Použiť pôvodné súradnice (prvý bod v zóne)")
    
    while True:
        choice = input("Vyberte možnosť [1/2]: ").strip()
        if choice == "1":
            return True
        elif choice == "2":
            return False
        else:
            print("Neplatná voľba. Prosím zadajte 1 alebo 2.")

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

def process_data(df, column_mapping, header_line=0):
    """Spracuje dataframe a rozdelí ho do zón."""
    # Vytvoríme transformátor z WGS84 (latitute, longitude) na S-JTSK (metre) - optimálna projekcia pre Slovensko
    transformer = Transformer.from_crs("EPSG:4326", "EPSG:5514", always_xy=True)
    
    # Získame názvy stĺpcov z dataframe
    column_names = list(df.columns)
    
    # Filtrujeme riadky s chýbajúcimi RSRP hodnotami
    rsrp_col = column_names[column_mapping['rsrp']]
    df_filtered = df.copy()
    
    # Zachovávame originálne indexy riadkov z pôvodného súboru
    # Pridáme header_line + 1 aby sme správne vypočítali Excel riadok
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
    
    # Výpočet zóny pre každé meranie
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

def calculate_zone_stats(df, column_mapping, column_names):
    """Vypočíta štatistiky pre každú zónu."""
    print("Počítam štatistiky pre zóny...")
    
    # Pripravíme SINR stĺpec pre výpočet priemeru, ak existuje
    sinr_col = None
    if 'sinr' in column_mapping:
        sinr_col = column_names[column_mapping['sinr']]
        # Konvertujeme SINR hodnoty na float a ignorujeme chýbajúce hodnoty
        df[sinr_col] = df[sinr_col].apply(
            lambda x: float(str(x).replace(',', '.')) if pd.notna(x) and str(x).strip() else np.nan
        )
    
    # Agregačný slovník pre rôzne stĺpce
    agg_dict = {
        column_names[column_mapping['rsrp']]: ['mean', 'count'],
        column_names[column_mapping['frequency']]: lambda x: x.value_counts().index[0] if len(x) > 0 else '',
        'original_excel_row': lambda x: list(x)  # Zachováme zoznam pôvodných excel riadkov
    }
    
    # Pridáme SINR do agregácie, ak existuje
    if sinr_col:
        agg_dict[sinr_col] = lambda x: x.dropna().mean() if len(x.dropna()) > 0 else np.nan
    
    # Agregácia dát podľa zón a operátorov
    zone_stats = df.groupby(['zona_key', 'operator_key', 'zona_x', 'zona_y', 
                            column_names[column_mapping['mcc']], column_names[column_mapping['mnc']]]).agg(agg_dict).reset_index()
    
    # Upravíme názvy stĺpcov
    new_columns = ['zona_key', 'operator_key', 'zona_x', 'zona_y', 'mcc', 'mnc',
                'rsrp_avg', 'pocet_merani', 'najcastejsia_frekvencia', 'original_excel_rows']
    
    if sinr_col:
        new_columns.append('sinr_avg')
    
    zone_stats.columns = new_columns
    
    # Konvertujeme zona_x a zona_y späť na latitude/longitude (stred zóny)
    transformer = Transformer.from_crs("EPSG:5514", "EPSG:4326", always_xy=True)
    
    # Pridáme stred zóny
    zone_stats['zona_stred_x'] = zone_stats['zona_x'] + ZONE_SIZE_METERS/2
    zone_stats['zona_stred_y'] = zone_stats['zona_y'] + ZONE_SIZE_METERS/2
    
    # Transformujeme späť na WGS84 s progress barom
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
    
    # Klasifikácia RSRP
    zone_stats['rsrp_kategoria'] = np.where(zone_stats['rsrp_avg'] >= -110, 'RSRP_GOOD', 'RSRP_BAD')
    
    return zone_stats

def save_zone_results(zone_stats, original_file, df, column_mapping, column_names, file_info, use_zone_center):
    """Uloží výsledky zón do CSV súboru, zachovávajúc pôvodný formát riadkov."""
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
    
    # Pridáme nový stĺpec pre zoznam riadkov do hlavičky
    header_line += ";Riadky_v_zone"
    
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
        
        # Nájdeme vzorový riadok z tejto zóny
        sample_rows = df[df['zona_operator_key'] == zona_operator_key]
        
        if len(sample_rows) == 0:
            continue  # Preskočíme, ak nemáme vzorový riadok
        
        # Vezmeme prvý riadok ako základ
        sample_row = sample_rows.iloc[0]
        
        # Vytvoríme kópiu vzorového riadku - už filtrovaného dataframu
        # Nepoužívame original_row_index, ktorý by mohol byť mimo rozsahu
        base_row = sample_row.copy()
        
        # Aktualizujeme hodnoty v riadku - RSRP a najčastejšia frekvencia
        rsrp_col = column_names[column_mapping['rsrp']]
        freq_col = column_names[column_mapping['frequency']]
        lat_col = column_names[column_mapping['latitude']]
        lon_col = column_names[column_mapping['longitude']]
        
        # Aktualizujeme hodnoty - používame bodku namiesto čiarky pre desatinné hodnoty
        base_row[rsrp_col] = f"{zone_row['rsrp_avg']:.2f}"
        base_row[freq_col] = zone_row['najcastejsia_frekvencia']
        
        # Aktualizujeme SINR, ak je k dispozícii
        if has_sinr and not pd.isna(zone_row['sinr_avg']):
            sinr_col = column_names[column_mapping['sinr']]
            base_row[sinr_col] = f"{zone_row['sinr_avg']:.2f}"
        
        # Aktualizujeme súradnice na stred zóny alebo ponecháme pôvodné podľa nastavenia
        if use_zone_center:
            # Použijeme súradnice stredu zóny
            base_row[lat_col] = f"{zone_row['latitude']:.6f}"
            base_row[lon_col] = f"{zone_row['longitude']:.6f}"
        
        # Získame zoznam riadkov z Excelu, ktoré patria do tejto zóny
        excel_rows = zone_row['original_excel_rows']
        excel_rows_str = ','.join(map(str, excel_rows)) if excel_rows else ""
        
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
        
        csv_row = ';'.join(row_values)
        
        # Pridáme informáciu o zóne a zoznam riadkov
        csv_row += f";{excel_rows_str}"
        
        # Pridáme poznámku o počte meraní
        csv_row += f" # Meraní: {zone_row['pocet_merani']}"
        
        output_lines.append(csv_row)
    
    # Vytvoríme chýbajúce zóny pre všetkých operátorov
    generate_empty_zones = input("Chcete vytvoriť prázdne zóny pre chýbajúce kombinácie zón a operátorov? (a/n): ").lower() == 'a'
    
    if generate_empty_zones:
        print("Generujem prázdne zóny...")
        
        # Získame všetky unikátne zóny a operátorov
        unique_zones = sorted_zone_stats['zona_key'].unique()
        unique_operators = sorted_zone_stats[['mcc', 'mnc']].drop_duplicates().values
        
        # Pre každú kombináciu zóny a operátora skontrolujeme, či existuje
        total_combinations = len(unique_zones) * len(unique_operators)
        combinations_processed = 0
        added_empty_zones = 0
        
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
                        
                        csv_row = ';'.join(row_values)
                        
                        # Pre prázdne zóny pridáme prázdny stĺpec pre zoznam riadkov
                        csv_row += ";"
                        
                        # Pridáme informáciu o prázdnej zóne
                        csv_row += " # Prázdna zóna - automaticky vygenerovaná"
                        
                        output_lines.append(csv_row)
                        added_empty_zones += 1
    
    # Zapíšeme výsledky do súboru
    with open(output_file, 'w', encoding='utf-8') as f:
        f.write('\n'.join(output_lines))
    
    if generate_empty_zones:
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
                            
                            csv_row = ';'.join(row_values)
                            
                            # Pridáme prázdny stĺpec pre zoznam riadkov
                            csv_row += ";"
                            
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

def save_stats(zone_stats, original_file, include_empty_zones=False):
    """Uloží štatistiky pre každého operátora do CSV súboru."""
    stats_file = original_file.replace('.csv', '_stats.csv')
    
    # Získame všetky unikátne zóny
    all_zones = set(zone_stats['zona_key'])
    total_unique_zones = len(all_zones)
    
    # Pripravíme dataframe pre štatistiky
    stats_data = []
    
    # Získame všetkých unikátnych operátorov
    operators = zone_stats[['mcc', 'mnc']].drop_duplicates()
    
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
            'RSRP >= -110': rsrp_good,
            'RSRP < -110': rsrp_bad
        })
    
    # Vytvoríme dataframe a uložíme
    stats_df = pd.DataFrame(stats_data)
    stats_df.to_csv(stats_file, sep=';', index=False, encoding='utf-8')
    print(f"Štatistiky uložené do súboru: {stats_file}")

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
    
    # Opýtame sa používateľa, či chce použiť súradnice stredu zóny
    use_zone_center = ask_for_zone_center()
    print(f"Použijú sa {'súradnice stredu zóny' if use_zone_center else 'pôvodné súradnice'}.")
    
    # Získame mapovanie stĺpcov
    column_mapping = get_column_mapping()
    
    # Získame číslo riadku hlavičky z info o súbore
    header_line = file_info.get('header_line', 0) if file_info else 0
    print(f"Hlavička súboru sa nachádza na riadku {header_line + 1}")
    
    # Spracujeme dáta s odovzdaním informácie o riadku hlavičky
    processed_df, column_names = process_data(df, column_mapping, header_line)
    
    # Vypočítame štatistiky zón
    zone_stats = calculate_zone_stats(processed_df, column_mapping, column_names)
    
    # Uložíme výsledky zachovávajúc pôvodný formát
    output_file = file_path.replace('.csv', '_zones.csv')
    include_empty_zones, processed_zones, unique_zones = save_zone_results(zone_stats, file_path, processed_df, column_mapping, column_names, file_info, use_zone_center)
    
    # Pridáme vlastných operátorov iba ak používateľ chce generovať prázdne zóny
    custom_operators_added = False
    if include_empty_zones:
        zone_stats, custom_operators_added = add_custom_operators(
            zone_stats, processed_df, column_mapping, column_names, 
            output_file, use_zone_center, processed_zones, unique_zones
        )
    
    # Uložíme štatistiky - zohľadňujeme voľbu používateľa o prázdnych zónach
    save_stats(zone_stats, file_path, include_empty_zones)
    
    # Vypíšeme informácie o zónach a rozsahu merania
    print("\nSúhrn spracovania:")
    
    # Počet unikátnych zón a operátorov
    unique_zones = zone_stats['zona_key'].nunique()
    unique_operators = zone_stats[['mcc', 'mnc']].drop_duplicates().shape[0]
    total_zones = len(zone_stats)
    
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