# -*- coding: utf-8 -*-

import numpy as np
import pandas as pd
from pyproj import Transformer
from tqdm import tqdm

from constants import ZONE_SIZE_METERS


def _to_string_series(series):
    """Bezpečne prevedie pandas Series na string pre skladanie kľúčov."""
    return series.astype("string").fillna("")


def process_data(
    df,
    column_mapping,
    header_line=0,
    zone_mode="zones",
    zone_size_m=ZONE_SIZE_METERS,
    progress_enabled=True
):
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
    for i in tqdm(
        range(0, total_rows, chunk_size),
        total=num_chunks,
        desc="Transformácia súradníc",
        disable=not progress_enabled
    ):
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
    segment_meta = None
    if zone_mode == "segments":
        print("Počítam úseky...")
        total_rows = len(df_filtered)
        x_values = df_filtered['x_meters'].to_numpy()
        y_values = df_filtered['y_meters'].to_numpy()

        segment_ids = np.zeros(total_rows, dtype=int)
        segment_start_xs = np.zeros(total_rows)
        segment_start_ys = np.zeros(total_rows)
        segment_meta = {}
        epsilon = 1e-9

        if total_rows > 0:
            cumulative_distance = 0.0
            prev_x = x_values[0]
            prev_y = y_values[0]
            segment_meta[0] = (prev_x, prev_y)
            segment_ids[0] = 0
            segment_start_xs[0] = prev_x
            segment_start_ys[0] = prev_y

            for i in range(1, total_rows):
                x = x_values[i]
                y = y_values[i]

                step_distance = ((x - prev_x) ** 2 + (y - prev_y) ** 2) ** 0.5
                if step_distance > 0:
                    prev_cumulative = cumulative_distance
                    cumulative_distance += step_distance
                    prev_segment = int((prev_cumulative + epsilon) // zone_size_m)
                    new_segment = int((cumulative_distance + epsilon) // zone_size_m)

                    if new_segment > prev_segment:
                        for segment_id in range(prev_segment + 1, new_segment + 1):
                            boundary_distance = segment_id * zone_size_m
                            offset = boundary_distance - prev_cumulative
                            fraction = offset / step_distance
                            if fraction < 0.0:
                                fraction = 0.0
                            elif fraction > 1.0:
                                fraction = 1.0
                            start_x = prev_x + (x - prev_x) * fraction
                            start_y = prev_y + (y - prev_y) * fraction
                            segment_meta[segment_id] = (start_x, start_y)

                current_segment = int((cumulative_distance + epsilon) // zone_size_m)
                segment_ids[i] = current_segment
                prev_x = x
                prev_y = y

            for i in range(total_rows):
                start_x, start_y = segment_meta.get(segment_ids[i], (x_values[0], y_values[0]))
                segment_start_xs[i] = start_x
                segment_start_ys[i] = start_y

        df_filtered['segment_id'] = segment_ids
        df_filtered['zona_x'] = segment_start_xs
        df_filtered['zona_y'] = segment_start_ys
        df_filtered['zona_key'] = [f"segment_{segment_id}" for segment_id in segment_ids]
    else:
        print("Počítam zóny...")
        df_filtered['zona_x'] = (df_filtered['x_meters'] // zone_size_m) * zone_size_m
        df_filtered['zona_y'] = (df_filtered['y_meters'] // zone_size_m) * zone_size_m

        # Vytvoríme kľúč zóny a operátora
        df_filtered['zona_key'] = _to_string_series(df_filtered['zona_x']).str.cat(
            _to_string_series(df_filtered['zona_y']),
            sep="_"
        )
    mcc_col = column_names[column_mapping['mcc']]
    mnc_col = column_names[column_mapping['mnc']]
    pci_col = column_names[column_mapping['pci']]
    # Operátor je unikátny podľa MCC+MNC. PCI sa vyberá spolu s frekvenciou.
    df_filtered['operator_key'] = _to_string_series(df_filtered[mcc_col]).str.cat(
        _to_string_series(df_filtered[mnc_col]),
        sep="_"
    )

    # Vytvoríme kombinovaný kľúč zóna+operátor
    df_filtered['zona_operator_key'] = _to_string_series(df_filtered['zona_key']).str.cat(
        _to_string_series(df_filtered['operator_key']),
        sep="_"
    )

    # Zachováme originálny riadok pre neskoršie použitie
    df_filtered['original_row_index'] = df_filtered.index

    return df_filtered, column_names, segment_meta


def calculate_zone_stats(
    df,
    column_mapping,
    column_names,
    rsrp_threshold=-110,
    sinr_threshold=-5,
    zone_mode="zones",
    zone_size_m=ZONE_SIZE_METERS,
    progress_enabled=True
):
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
    pci_col = column_names[column_mapping['pci']]

    # Agregačný slovník pre rôzne stĺpce (po kombinácii frekvencia+PCI v rámci zóny + operátora)
    agg_kwargs = {
        'rsrp_avg': (rsrp_col, 'mean'),
        'pocet_merani': (rsrp_col, 'count'),
        'original_excel_rows': ('original_excel_row', lambda x: list(x))
    }

    # Pridáme SINR do agregácie, ak existuje
    if sinr_col:
        agg_kwargs['sinr_avg'] = (sinr_col, lambda x: x.dropna().mean() if len(x.dropna()) > 0 else np.nan)

    # Agregácia dát podľa zón, operátorov a kombinácie frekvencia+PCI
    zone_freq_stats = df.groupby(
        ['zona_key', 'operator_key', 'zona_x', 'zona_y', mcc_col, mnc_col, pci_col, freq_col]
    ).agg(**agg_kwargs).reset_index()

    # Výber najlepšej kombinácie frekvencia+PCI podľa priemernej RSRP (s deterministickým tie-break)
    zone_freq_stats['frequency_sort_numeric'] = pd.to_numeric(zone_freq_stats[freq_col], errors='coerce')
    zone_freq_stats['frequency_sort_text'] = zone_freq_stats[freq_col].astype(str)
    zone_freq_stats['pci_sort_numeric'] = pd.to_numeric(zone_freq_stats[pci_col], errors='coerce')
    zone_freq_stats['pci_sort_text'] = zone_freq_stats[pci_col].astype(str)
    zone_freq_stats = zone_freq_stats.sort_values(
        ['rsrp_avg', 'pocet_merani', 'frequency_sort_numeric', 'frequency_sort_text', 'pci_sort_numeric', 'pci_sort_text'],
        ascending=[False, False, True, True, True, True]
    )

    zone_stats = zone_freq_stats.groupby(
        ['zona_key', 'operator_key', 'zona_x', 'zona_y', mcc_col, mnc_col],
        as_index=False
    ).first()

    # Po výbere necháme len jednu frekvenciu
    zone_stats['najcastejsia_frekvencia'] = zone_stats[freq_col]
    zone_stats['vsetky_frekvencie'] = zone_stats[freq_col].apply(lambda value: [value])
    zone_stats = zone_stats.rename(columns={mcc_col: 'mcc', mnc_col: 'mnc', pci_col: 'pci'})
    zone_stats = zone_stats.drop(columns=[freq_col, 'frequency_sort_numeric', 'frequency_sort_text', 'pci_sort_numeric', 'pci_sort_text'])

    new_columns = [
        'zona_key', 'operator_key', 'zona_x', 'zona_y', 'mcc', 'mnc', 'pci',
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
        zone_stats['zona_stred_x'] = zone_stats['zona_x'] + zone_size_m / 2
        zone_stats['zona_stred_y'] = zone_stats['zona_y'] + zone_size_m / 2

    # Transformujeme späť na WGS84 s progress barom
    if zone_mode == "segments":
        print("Transformujem súradnice úsekov...")
    else:
        print("Transformujem súradnice zón...")
    zone_stats['longitude'] = 0.0
    zone_stats['latitude'] = 0.0

    for i in tqdm(range(len(zone_stats)), desc="Transformácia zón", disable=not progress_enabled):
        lon, lat = transformer.transform(
            zone_stats.iloc[i]['zona_stred_x'],
            zone_stats.iloc[i]['zona_stred_y']
        )
        zone_stats.loc[zone_stats.index[i], 'longitude'] = lon
        zone_stats.loc[zone_stats.index[i], 'latitude'] = lat

    # Klasifikácia pokrytia podľa prahov RSRP + SINR.
    if 'sinr_avg' in zone_stats.columns:
        coverage_good = (
            (zone_stats['rsrp_avg'] >= rsrp_threshold)
            & (zone_stats['sinr_avg'] >= sinr_threshold)
        )
    else:
        print("Upozornenie: SINR stĺpec nie je dostupný, klasifikácia použije iba RSRP.")
        coverage_good = zone_stats['rsrp_avg'] >= rsrp_threshold

    zone_stats['rsrp_kategoria'] = np.where(coverage_good, 'RSRP_GOOD', 'RSRP_BAD')

    return zone_stats
