# -*- coding: utf-8 -*-

import os
import sys

import pandas as pd


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
        encoding_selected = encoding_to_use or 'utf-8'
        df = pd.read_csv(file_path, sep=';', header=header_line, encoding=encoding_selected)

        # Ak je počet stĺpcov odlišný od hlavičky alebo sú prítomné "Unnamed" stĺpce,
        # znovu načítame dáta s explicitnými názvami stĺpcov.
        header_cols = original_header.split(';') if original_header else None
        if header_cols and (
            len(df.columns) != len(header_cols)
            or any(str(col).startswith('Unnamed') for col in df.columns)
        ):
            df = pd.read_csv(
                file_path,
                sep=';',
                skiprows=header_line + 1,
                header=None,
                names=header_cols,
                usecols=range(len(header_cols)),
                encoding=encoding_selected,
                engine='python',
                encoding_errors='ignore'
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
