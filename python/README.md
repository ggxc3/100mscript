# 100mscript - Python verzia

Táto aplikácia slúži na spracovanie CSV súborov s meraniami mobilného signálu a ich rozdelenie do zón.

## Hlavná funkcionalita

1. Načítanie CSV súboru s meraniami mobilného signálu
2. Transformácia geografických súradníc (WGS84) na S-JTSK súradnice (metre)
3. Rozdelenie meraní do zón s definovanou veľkosťou
4. Výpočet štatistík pre každú zónu a operátora
5. Uloženie výsledkov do nových CSV súborov

## Nastavenie formátu súradníc

Pri spustení programu sa vás aplikácia opýta, aký formát súradníc chcete použiť vo výslednom súbore:

1. Štvorcové zóny (súradnice stredu zóny) - výstupný súbor bude obsahovať súradnice stredu každej 100m zóny
2. Štvorcové zóny (prvý bod v zóne) - výstupný súbor bude obsahovať pôvodné súradnice prvého bodu v zóne
3. 100m úseky podľa poradia meraní - výstupný súbor bude obsahovať súradnice prvého bodu úseku

### EXE súbor

K dispozícii je jeden EXE súbor, ktorý vám umožní vybrať si formát súradníc počas behu programu:

```
100mscript-vX.X.X.exe [cesta_k_csv_suboru]
```

Ak nie je zadaná cesta k súboru, aplikácia vás vyzve na jej zadanie.

## Funkcie programu

Program vykonáva tieto operácie:

1. Načíta CSV súbor s meraniami
2. Rozdelí merania do štvorcových zón (100m x 100m) alebo do 100m úsekov podľa poradia meraní
3. Pre každú zónu a kombináciu MNC+MCC vypočíta:
   - Priemerné RSRP
   - Frekvenciu s najvyšším priemerným RSRP v zóne (pre daného operátora)
   - Počet meraní
4. Uloží výsledky do dvoch súborov:
   - `<original>_zones.csv` - detaily pre každú zónu
   - `<original>_stats.csv` - štatistiky pokrytia pre každého operátora

## Predspracovanie filtrov

Ak sa v adresári, z ktorého spúšťate skript, nachádza priečinok `filters/` alebo `filtre_5G/`,
program načíta všetky `.txt` filtre a aplikuje ich na vstupný CSV súbor pred spracovaním zón.
Program sa vás spýta, či má pôvodný riadok zostať zachovaný a pridať sa nový s aplikovaným
filtrom, alebo sa má pôvodný riadok nahradiť iba filtrom.

Formát filtra:
- prvý výraz po `<Query>` určuje hodnoty, ktoré sa prepíšu (assignment)
- ďalšie výrazy v zátvorkách sú podmienky, kedy sa prepis uplatní (OR medzi skupinami)
- v podmienkach je podporený aj rozsah `start-end` (inkluzívne)

Ak sa v assignmente opakuje kľúč (napr. `MNC` viac krát), riadok sa duplikuje pre každú
kombináciu hodnôt. Ak jeden riadok vyhovuje viac ako jednému filtru, program vypíše chybu
s číslom riadku a spracovanie ukončí.

## Mapovanie stĺpcov

Pri spustení programu môžete použiť predvolené mapovanie stĺpcov alebo zadať vlastné:

- Latitude (zemepisná šírka): D
- Longitude (zemepisná dĺžka): E
- Frequency (frekvencia): K
- MNC (Mobile Network Code): N
- MCC (Mobile Country Code): M
- RSRP: W
- SINR: V 