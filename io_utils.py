# -*- coding: utf-8 -*-

import os
import sys

import pandas as pd


def _split_semicolon_columns(line):
    """Rozdelí riadok podľa ';' a odstráni koncové prázdne položky."""
    columns = line.rstrip("\r\n").split(";")
    while columns and columns[-1] == "":
        columns.pop()
    return columns


def _has_tabular_followup(lines, start_index, expected_columns, min_columns=6):
    """Overí, že po kandidátnom riadku nasledujú ďalšie riadky podobné tabuľke."""
    seen_candidates = 0
    tabular_rows = 0

    for line in lines[start_index + 1:]:
        if not line.strip():
            continue
        seen_candidates += 1
        cols_count = len(_split_semicolon_columns(line))
        if cols_count >= max(min_columns, expected_columns - 1):
            tabular_rows += 1
            if tabular_rows >= 2:
                return True
        if seen_candidates >= 25:
            break

    return False


def _find_tabular_header(lines, min_columns=6):
    """
    Nájde začiatok reálnej CSV tabuľky.
    Preskočí technický úvod exportu (metadata blok), ktorý nie je dátová tabuľka.
    """
    first_candidate = None

    for i, line in enumerate(lines):
        cols = _split_semicolon_columns(line)
        cols_count = len(cols)
        if cols_count < min_columns:
            continue
        if first_candidate is None:
            first_candidate = (i, line.strip())
        if _has_tabular_followup(lines, i, cols_count, min_columns=min_columns):
            return i, line.strip()

    return first_candidate if first_candidate is not None else (-1, None)


def _make_unique_column_names(columns):
    """Vytvorí unikátne názvy stĺpcov (pandas neakceptuje duplicity pri names=...)."""
    unique_columns = []
    seen = {}
    for index, raw_name in enumerate(columns, start=1):
        base_name = str(raw_name).strip() if raw_name is not None else ""
        if not base_name:
            base_name = f"column_{index}"
        occurrence = seen.get(base_name, 0)
        seen[base_name] = occurrence + 1
        unique_columns.append(base_name if occurrence == 0 else f"{base_name}_{occurrence + 1}")
    return unique_columns


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
                found_header_line, found_header = _find_tabular_header(lines)
                if found_header_line != -1:
                    header_line = found_header_line
                    encoding_to_use = enc
                    original_header = found_header
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
                    found_header_line, found_header = _find_tabular_header(lines)
                    if found_header_line != -1:
                        header_line = found_header_line
                        original_header = found_header
                    break
            except Exception:
                continue

    if header_line == -1:
        header_line = 0  # Ak nenájdeme hlavičku, predpokladáme že je to prvý riadok

    # Teraz načítame súbor robustne tak, aby prežil nekonzistentný počet stĺpcov.
    try:
        encoding_selected = encoding_to_use or 'utf-8'
        header_cols = _split_semicolon_columns(original_header) if original_header else []
        if not header_cols:
            with open(file_path, 'r', encoding=encoding_selected, errors='ignore') as f:
                lines = f.readlines()
            if 0 <= header_line < len(lines):
                header_cols = _split_semicolon_columns(lines[header_line])

        max_fields = len(header_cols)
        with open(file_path, 'r', encoding=encoding_selected, errors='ignore') as f:
            for line_number, line in enumerate(f):
                if line_number <= header_line or not line.strip():
                    continue
                max_fields = max(max_fields, len(_split_semicolon_columns(line)))

        if max_fields <= 0:
            max_fields = max(len(header_cols), 1)

        if len(header_cols) < max_fields:
            missing_count = max_fields - len(header_cols)
            header_cols.extend([f"extra_col_{i}" for i in range(1, missing_count + 1)])

        column_names = _make_unique_column_names(header_cols)

        df = pd.read_csv(
            file_path,
            sep=';',
            skiprows=header_line + 1,
            header=None,
            names=column_names,
            usecols=range(len(column_names)),
            encoding=encoding_selected,
            engine='python',
            encoding_errors='ignore',
            on_bad_lines='skip',
        )

        # Vrátime DataFrame a informácie o súbore
        return df, {
            'encoding': encoding_selected,
            'header_line': header_line,
            'original_header': original_header
        }
    except Exception as e:
        print(f"Chyba pri načítaní súboru: {e}")
        return None, None


def get_app_base_dir():
    if getattr(sys, "frozen", False):
        return os.path.dirname(sys.executable)
    return os.path.dirname(os.path.abspath(__file__))


def get_output_suffix():
    suffix = os.getenv("OUTPUT_SUFFIX", "").strip()
    if suffix and not suffix.startswith("_"):
        suffix = "_" + suffix
    return suffix
