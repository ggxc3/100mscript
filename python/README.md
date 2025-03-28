# Nástroj na spracovanie zón

Tento nástroj slúži na spracovanie CSV súborov s meraniami signálov (latitude, longitude, RSRP, SINR, atď.) do zón.

## Inštalácia

Pred použitím nainštalujte potrebné knižnice:

```bash
pip install -r requirements.txt
```

## Použitie

Program môžete spustiť dvoma spôsobmi:

1. Zadaním cesty k súboru ako parameter:

```bash
python main.py cesta/k/vasmu/suboru.csv
```

2. Bez parametrov - program vás požiada o zadanie cesty:

```bash
python main.py
```

## Funkcie programu

Program vykonáva tieto operácie:

1. Načíta CSV súbor s meraniami
2. Rozdelí merania do zón podľa súradníc (100m x 100m)
3. Pre každú zónu a kombináciu MNC+MCC vypočíta:
   - Priemerné RSRP
   - Najčastejšie používanú frekvenciu
   - Počet meraní
4. Uloží výsledky do dvoch súborov:
   - `<original>_zones.csv` - detaily pre každú zónu
   - `<original>_stats.csv` - štatistiky pokrytia pre každého operátora

## Mapovanie stĺpcov

Pri spustení programu môžete použiť predvolené mapovanie stĺpcov alebo zadať vlastné:

- Latitude (zemepisná šírka): D
- Longitude (zemepisná dĺžka): E
- Frequency (frekvencia): K
- MNC (Mobile Network Code): N
- MCC (Mobile Country Code): M
- RSRP: W
- SINR: V 