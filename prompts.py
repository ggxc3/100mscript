# -*- coding: utf-8 -*-

import argparse
import os
import re

from constants import ZONE_SIZE_METERS
from io_utils import load_csv_file


DEFAULT_COLUMN_LETTERS = {
    "latitude": "D",
    "longitude": "E",
    "frequency": "K",
    "pci": "L",
    "mcc": "M",
    "mnc": "N",
    "rsrp": "W",
    "sinr": "V",
}

_COLUMN_HEADER_CANDIDATES = {
    "latitude": ["Latitude", "Lat"],
    "longitude": ["Longitude", "Lon", "Lng"],
    "frequency": ["SSRef", "Frequency"],
    "pci": ["PCI", "NPCI", "Physical Cell ID"],
    "mcc": ["MCC"],
    "mnc": ["MNC"],
    "rsrp": ["SSS-RSRP", "RSRP", "NR-SS-RSRP"],
    "sinr": ["SSS-SINR", "SINR", "NR-SS-SINR"],
}


def parse_arguments():
    """Spracovanie argumentov príkazového riadka."""
    parser = argparse.ArgumentParser(description='Spracovanie CSV súboru s meraniami do zón.')
    parser.add_argument('file', nargs='?', help='Cesta k CSV súboru')
    return parser.parse_args()


def ask_for_mobile_mode():
    """Opýta sa používateľa, či chce zapnúť Mobile režim (5G + LTE synchronizácia)."""
    print("\nVoliteľný Mobile režim:")
    print("Ak je zapnutý, načíta sa aj LTE súbor a 5G dáta sa obohatia podľa stĺpca '5G NR'.")

    while True:
        choice = input("Chcete zapnúť Mobile režim? (a/n): ").strip().lower()
        if choice == "a":
            return True
        if choice == "n":
            return False
        print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")


def ask_for_lte_file_path():
    """Interaktívne načíta cestu k LTE CSV súboru pre Mobile režim."""
    while True:
        lte_file_path = input("Zadajte cestu k LTE CSV súboru pre Mobile režim: ").strip()
        if not lte_file_path:
            print("Cesta nemôže byť prázdna.")
            continue
        if not os.path.isfile(lte_file_path):
            print("Súbor neexistuje. Skontrolujte cestu a skúste znova.")
            continue
        return lte_file_path


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


def ask_for_sinr_threshold():
    """Opýta sa používateľa na hranicu SINR pre štatistiky."""
    print("\nNastavenie hranice SINR pre štatistiky:")
    print("Predvolená hodnota: -5 dB")

    while True:
        choice = input("Chcete použiť predvolenú hodnotu -5 dB? (a/n): ").strip().lower()
        if choice == "a":
            return -5
        elif choice == "n":
            while True:
                try:
                    threshold = input("Zadajte vlastnú hranicu SINR (napr. 0 alebo -3): ").strip()
                    threshold_value = float(threshold.replace(',', '.'))
                    print(f"Použije sa hranica SINR: {threshold_value} dB")
                    return threshold_value
                except ValueError:
                    print("Neplatná hodnota. Prosím zadajte číslo (napr. 0 alebo -3).")
        else:
            print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")


def ask_for_zone_mode():
    """Opýta sa používateľa na režim spracovania zón/úsekov."""
    print("\nNastavenie súradníc a režimu:")
    print("1 - Štvorcové zóny (súradnice stredu zóny)")
    print("2 - Štvorcové zóny (prvý bod v zóne)")
    print("3 - Úseky po trase (presný začiatok každých zvolených m)")

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


def ask_for_zone_size(default_zone_size=ZONE_SIZE_METERS):
    """Opýta sa používateľa na veľkosť zóny/úseku v metroch."""
    print("\nNastavenie veľkosti zóny/úseku:")
    print(f"Predvolená hodnota: {default_zone_size} m")

    while True:
        choice = input(f"Chcete použiť predvolenú hodnotu {default_zone_size} m? (a/n): ").strip().lower()
        if choice == "a":
            return default_zone_size
        elif choice == "n":
            while True:
                try:
                    size_input = input("Zadajte veľkosť zóny/úseku v metroch (napr. 50 alebo 200): ").strip()
                    size_value = float(size_input.replace(',', '.'))
                    if size_value <= 0:
                        raise ValueError("Veľkosť musí byť kladná.")
                    if size_value.is_integer():
                        size_value = int(size_value)
                    print(f"Použije sa veľkosť zóny/úseku: {size_value} m")
                    return size_value
                except ValueError:
                    print("Neplatná hodnota. Prosím zadajte kladné číslo (napr. 50).")
        else:
            print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")


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


def ask_for_include_empty_zones(zone_mode="zones"):
    """Opýta sa používateľa, či sa majú generovať prázdne zóny/úseky."""
    if zone_mode == "segments":
        prompt = "Chcete vytvoriť prázdne úseky pre chýbajúce kombinácie úsekov a operátorov? (a/n): "
    else:
        prompt = "Chcete vytvoriť prázdne zóny pre chýbajúce kombinácie zón a operátorov? (a/n): "

    while True:
        choice = input(prompt).strip().lower()
        if choice == "a":
            return True
        if choice == "n":
            return False
        print("Neplatná voľba. Prosím zadajte 'a' alebo 'n'.")


def parse_custom_operators_text(text):
    """Spracuje text MCC:MNC[:PCI] a vráti zoznam trojíc (mcc, mnc, pci)."""
    mcc_pattern = re.compile(r'^\d+$')
    mnc_pattern = re.compile(r'^\d+$')
    pci_pattern = re.compile(r'^\d+$')

    operators = []
    for raw_item in text.replace(',', ' ').split():
        parts = raw_item.split(':')
        if len(parts) not in (2, 3):
            raise ValueError(
                f"Neplatný formát operátora '{raw_item}'. Použite MCC:MNC alebo MCC:MNC:PCI."
            )
        mcc = parts[0].strip()
        mnc = parts[1].strip()
        pci = parts[2].strip() if len(parts) == 3 else ""
        if not mcc_pattern.match(mcc):
            raise ValueError(f"Neplatné MCC '{mcc}'. MCC musí obsahovať iba čísla.")
        if not mnc_pattern.match(mnc):
            raise ValueError(f"Neplatné MNC '{mnc}'. MNC musí obsahovať iba čísla.")
        if pci and not pci_pattern.match(pci):
            raise ValueError(f"Neplatné PCI '{pci}'. PCI musí obsahovať iba čísla.")
        operators.append((mcc, mnc, pci))
    return operators


def ask_for_custom_operators():
    """Interaktívne načíta vlastných operátorov."""
    add_operators = input(
        "Chcete pridať vlastných operátorov, ktorí neboli nájdení v dátach? (a/n): "
    ).strip().lower() == "a"
    if not add_operators:
        return False, []

    custom_operators = []
    while True:
        operators_input = input(
            "Zadajte MCC:MNC operátorov oddelené medzerou (napr. '231:01 231:02'), "
            "PCI je voliteľné (MCC:MNC:PCI). Alebo 'koniec' pre ukončenie: "
        ).strip()
        if not operators_input or operators_input.lower() in ['koniec', 'quit', 'q', 'exit']:
            break
        try:
            custom_operators.extend(parse_custom_operators_text(operators_input))
        except ValueError as exc:
            print(exc)
            continue
        if input("Chcete pridať ďalších operátorov? (a/n): ").strip().lower() != 'a':
            break
    return True, custom_operators


def get_column_mapping():
    """Získa mapovanie stĺpcov podľa predvolených hodnôt alebo od používateľa."""
    return get_column_mapping_for_file(None)


def _normalize_header_name(name):
    value = str(name).strip().lower()
    return re.sub(r"[^a-z0-9]+", "", value)


def col_index_to_letter(col_index):
    """Konvertuje 0-based index stĺpca na Excel písmeno (A, B, ..., AA, AB...)."""
    if col_index is None or col_index < 0:
        return "A"
    value = int(col_index) + 1
    letters = []
    while value > 0:
        value, remainder = divmod(value - 1, 26)
        letters.append(chr(ord("A") + remainder))
    return "".join(reversed(letters))


def suggest_column_letters_from_headers(column_names, base_columns=None):
    """Podľa názvov hlavičky navrhne písmená stĺpcov."""
    suggested = dict(base_columns or DEFAULT_COLUMN_LETTERS)
    if not column_names:
        return suggested, {}

    detected = {}
    normalized_candidates = {
        mapping_key: {_normalize_header_name(candidate) for candidate in candidates}
        for mapping_key, candidates in _COLUMN_HEADER_CANDIDATES.items()
    }

    # Pre každý typ stĺpca berieme prvý match podľa poradia v CSV (zľava doprava).
    for idx, raw_name in enumerate(column_names):
        normalized_name = _normalize_header_name(raw_name)
        if not normalized_name:
            continue

        for mapping_key, candidates in normalized_candidates.items():
            if mapping_key in detected:
                continue
            if normalized_name in candidates:
                letter = col_index_to_letter(idx)
                suggested[mapping_key] = letter
                detected[mapping_key] = {"letter": letter, "header": str(raw_name)}

    return suggested, detected


def suggest_column_letters_from_file(file_path, base_columns=None):
    """Načíta hlavičku CSV a navrhne mapovanie stĺpcov podľa názvov."""
    suggested = dict(base_columns or DEFAULT_COLUMN_LETTERS)
    if not file_path:
        return suggested, {}

    df, _ = load_csv_file(file_path)
    if df is None:
        return suggested, {}

    return suggest_column_letters_from_headers(list(df.columns), suggested)


def _letters_to_mapping(columns_by_letter):
    mapping = {}
    for key, letter in columns_by_letter.items():
        mapping[key] = col_letter_to_name(letter)
    return mapping


def get_column_mapping_for_file(file_path=None):
    """Získa mapovanie stĺpcov, pri zadanom súbore skúsi auto-detekciu podľa hlavičky."""
    suggested_columns, detected_columns = suggest_column_letters_from_file(
        file_path,
        DEFAULT_COLUMN_LETTERS,
    )

    if detected_columns:
        print("Nájdené stĺpce podľa hlavičky CSV:")
    else:
        print("Predvolené hodnoty stĺpcov:")

    for name in DEFAULT_COLUMN_LETTERS:
        letter = suggested_columns[name]
        if name in detected_columns:
            header_name = detected_columns[name]["header"]
            print(f"  {name}: {letter} ({header_name})")
        else:
            print(f"  {name}: {letter}")

    use_default = input("Chcete použiť tieto predvyplnené hodnoty stĺpcov? (a/n): ").strip().lower() == "a"

    if use_default:
        return _letters_to_mapping(suggested_columns)

    columns = {}
    for name in DEFAULT_COLUMN_LETTERS:
        default_letter = suggested_columns[name]
        col = input(f"Zadajte písmeno stĺpca pre {name} [{default_letter}]: ").strip() or default_letter
        columns[name] = col
    return _letters_to_mapping(columns)


def col_letter_to_name(letter):
    """Konvertuje písmeno stĺpca (napr. 'A', 'B'...) na názov stĺpca v pandas DataFrame."""
    if letter is None:
        return 0

    text = str(letter).strip().upper()
    if not text:
        return 0

    letters_only = "".join(ch for ch in text if "A" <= ch <= "Z")
    if not letters_only:
        return 0

    col_index = 0
    for ch in letters_only:
        col_index = col_index * 26 + (ord(ch) - ord("A") + 1)

    return col_index - 1
