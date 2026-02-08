# Dokumentácia CSV Zónového Analyzátora (Python)

## Obsah
1. [Úvod](#úvod)
2. [Základné koncepty](#základné-koncepty)
3. [Spracovanie dát – krok za krokom](#spracovanie-dát--krok-za-krokom)
4. [Zóny a úseky](#zóny-a-úseky)
5. [Agregácie a výber frekvencie](#agregácie-a-výber-frekvencie)
6. [Výstupy](#výstupy)
7. [Filtre](#filtre)
8. [Obmedzenia a poznámky](#obmedzenia-a-poznámky)

## Úvod

CSV Zónový Analyzátor je Python skript, ktorý spracováva CSV merania mobilného signálu. Merania agreguje do štvorcových zón alebo do presných úsekov po trase s voliteľnou veľkosťou (predvolene 100 m) a vypočíta štatistiky pokrytia podľa operátorov (MCC+MNC).

## Základné koncepty

- **Meranie**: jeden riadok vstupného CSV so súradnicami, frekvenciou, MCC/MNC a RSRP.
- **Operátor**: kombinácia MCC + MNC, kľúč `operator_key = "<MCC>_<MNC>"`.
- **Zóna**: štvorcová bunka v metrovej projekcii (S-JTSK) s veľkosťou `S × S` m (predvolene 100 m).
- **Úsek**: segment po trase s dĺžkou `S` m (predvolene 100 m), definovaný kumulatívnou vzdialenosťou.

## Spracovanie dát – krok za krokom

1. **Načítanie CSV**
   - hľadá hlavičku automaticky (nemusí byť na prvom riadku),
   - používa bodkočiarku `;` ako oddeľovač,
   - skúša viac kódovaní (utf-8, cp1250, latin2, ...).

2. **Predspracovanie filtrov (voliteľné)**
   - ak existuje `filters/` alebo `filtre_5G/`, načítajú sa `.txt` filtre,
   - filtre môžu prepisovať stĺpce alebo duplikovať riadky.

3. **Interaktívne voľby**
   - režim zón/úsekov,
   - veľkosť zóny/úseku (predvolene 100 m),
   - hranica RSRP (predvolene -110 dBm),
   - mapovanie stĺpcov podľa písmen.

4. **Transformácia súradníc**
   - WGS84 (EPSG:4326) → S-JTSK (EPSG:5514) kvôli presným vzdialenostiam v metroch.

5. **Agregácia a výstupy**
   - pre každú zónu/úsek a operátora sa vypočíta priemer RSRP,
   - vyberie sa frekvencia s najvyšším priemerom RSRP,
   - vytvoria sa výstupné CSV súbory `_zones.csv` a `_stats.csv`.

## Zóny a úseky

### Zóny (štvorce S×S m)

Po transformácii do metrov sa pre každý bod vypočíta ľavý dolný roh zóny:

```
zona_x = floor(x / S) * S
zona_y = floor(y / S) * S
zona_key = "<zona_x>_<zona_y>"
```

Výstupné súradnice môžu byť:
- **stred zóny** (zona_x + S/2, zona_y + S/2), alebo
- **pôvodné súradnice vzorového merania** (prvý nájdený riadok pre vybranú frekvenciu).

### Úseky (S m po trase)

Pri režime úsekov sa merania spracúvajú **v poradí riadkov**. Vzdialenosť medzi bodmi sa sčíta a každých `S` m začína nový segment:

```
segment_id = floor(cumulative_distance / S)
segment_key = "segment_<id>"
```

Ak presný bod na hranici segmentu neexistuje, začiatok segmentu sa interpoluje medzi dvoma bodmi. Na výstup sa používajú súradnice začiatku segmentu.

## Agregácie a výber frekvencie

Agregácia prebieha po kľúčoch: `zona_key`, `operator_key`, `frequency`.

Pre každú kombináciu sa počíta:
- `rsrp_avg` – priemerné RSRP,
- `pocet_merani` – počet meraní,
- `original_excel_rows` – zoznam pôvodných riadkov.

**Výber frekvencie:**
- vyberie sa frekvencia s najvyšším `rsrp_avg`,
- pri zhode rozhoduje vyšší počet meraní,
- potom nižšia numerická frekvencia a napokon textové poradie.

Poznámka: stĺpec `najcastejsia_frekvencia` v `_zones.csv` reprezentuje **vybranú frekvenciu podľa RSRP**, nie najčastejšiu podľa počtu.

## Výstupy

### `<vstup>_zones.csv`

- zachováva pôvodnú hlavičku,
- pridáva stĺpce `Riadky_v_zone` a `Frekvencie_v_zone`,
- na konci riadku pridáva komentár `# Meraní: X`,
- prázdne zóny/úseky dostanú RSRP `-174` a poznámku o automatickom generovaní.

### `<vstup>_stats.csv`

- sumarizuje pokrytie podľa operátora,
- názvy stĺpcov závisia od zvolených hraníc (napr. `RSRP >= -110 a SINR >= -5`),
- zóna/úsek je vyhovujúca iba keď súčasne platí `RSRP >= hranica_RSRP` a `SINR >= hranica_SINR`.

Ak používateľ zvolí generovanie prázdnych zón/úsekov, chýbajúce kombinácie sa započítajú ako zlé pokrytie.

## Filtre

- súbory `.txt` v `filters/` alebo `filtre_5G/`,
- `assignment` prepíše hodnoty, podmienky v zátvorkách sú AND a skupiny OR,
- podporované sú rozsahy `start-end`,
- ak riadok vyhovuje viacerým filtrom, spracovanie skončí chybou,
- podľa voľby používateľa sa pôvodný riadok môže ponechať alebo nahradiť.

Voliteľne:
- `FILTERS_DEBUG_OUTPUT=1` uloží `<vstup>_filters.csv` po filtrovaní.

## Obmedzenia a poznámky

- Režim úsekov predpokladá, že dáta sú v poradí trasy.
- Skript očakáva bodkočiarkové CSV; iné oddeľovače nie sú podporované.
- Výstupy sú zapisované v UTF-8 a používajú bodku ako desatinný oddeľovač.
