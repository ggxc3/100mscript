# -*- coding: utf-8 -*-

import argparse

from constants import ZONE_SIZE_METERS


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


def get_column_mapping():
    """Získa mapovanie stĺpcov podľa predvolených hodnôt alebo od používateľa."""
    default_columns = {
        'latitude': 'D',
        'longitude': 'E',
        'frequency': 'K',
        'pci': 'L',
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
