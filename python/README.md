# 100mscript - Python verzia

Táto aplikácia slúži na spracovanie CSV súborov s meraniami mobilného signálu a ich rozdelenie do zón.

## Hlavná funkcionalita

1. Načítanie CSV súboru s meraniami mobilného signálu
2. Transformácia geografických súradníc (WGS84) na S-JTSK súradnice (metre)
3. Rozdelenie meraní do zón s definovanou veľkosťou
4. Výpočet štatistík pre každú zónu a operátora
5. Uloženie výsledkov do nových CSV súborov

## Parametre nastavenia

### USE_ZONE_CENTER

V kóde je definovaný parameter `USE_ZONE_CENTER`, ktorý ovplyvňuje, aké súradnice budú použité vo výsledných dátach:

- `USE_ZONE_CENTER = False` - Vo výstupnom súbore sa použijú pôvodné súradnice prvého bodu v zóne
- `USE_ZONE_CENTER = True` - Vo výstupnom súbore sa použijú súradnice stredu zóny

### EXE súbory

Pre vaše pohodlie sú k dispozícii dva EXE súbory:

1. **100mscript-corner-vX.X.X.exe** - Verzia s nastavením `USE_ZONE_CENTER = False`
2. **100mscript-center-vX.X.X.exe** - Verzia s nastavením `USE_ZONE_CENTER = True`

## Použitie

```
100mscript-corner-vX.X.X.exe [cesta_k_csv_suboru]
```

alebo 

```
100mscript-center-vX.X.X.exe [cesta_k_csv_suboru]
```

Ak nie je zadaná cesta k súboru, aplikácia vás vyzve na jej zadanie.

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