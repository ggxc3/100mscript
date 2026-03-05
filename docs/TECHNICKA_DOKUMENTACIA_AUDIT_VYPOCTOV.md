# 100mscript - technicka auditna dokumentacia vypoctov (Go backend)

## 0. Meta a rozsah

- **Jazyk dokumentacie:** slovencina
- **Primarny zdroj pravdy:** aktualny Go kod v `internal/backend/` + orchestrace `app.go`, `main.go`, `frontend/src/main.ts`
- **Auditovany commit:** `f9e9739`
- **Auditovany branch:** `main`
- **Datum auditu:** 2026-03-05
- **Rozsah:** kompletne end-to-end spracovanie od nacitania CSV po export `_zones.csv` a `_stats.csv`, vratane filtrov, mobile synchronizacie, transformacii suradnic, agregacii, klasifikacie GOOD/BAD, generovania prazdnych zon/usekov a custom operatorov.

Tento dokument je pisany tak, aby sa dali jednotlive kroky prepocitat rucne (vedecka kalkulacka + tabulka).

---

## 1. End-to-end architektura

## 1.1 Vrstvy

1. **UI (Wails frontend, TypeScript)**
   - Zber konfiguracie, mapovanie stlpcov, validacia vstupov pre UX.
   - Subor: `frontend/src/main.ts`.

2. **Wails bridge (Go app vrstva)**
   - API medzi UI a backendom.
   - Subor: `app.go`.

3. **Backend pipeline (Go, `internal/backend`)**
   - `LoadCSVFile` -> (volitelne) `ApplyFiltersCSV` -> (volitelne) `syncMobileNRFromLTECSVNative` -> `ProcessDataNative` -> `CalculateZoneStatsNative` -> `SaveZoneResultsNative` + `SaveStatsNative` -> `ProcessingResult`.
   - Hlavna orchestrace: `runProcessingNative` v `internal/backend/runner_native.go`.

## 1.2 Datovy tok (presne poradie)

1. Nacitaj hlavny CSV (`LoadCSVFile`).
2. Zisti/načitaj filtre (`loadRulesForConfig`).
3. Ak su filtre, aplikuj ich (`ApplyFiltersCSV`).
4. Ak je mobile rezim zapnuty:
   - nacitaj LTE CSV,
   - volitelne aplikuj rovnake filtre aj na LTE,
   - zosynchronizuj `5G NR` do 5G datasetu (`syncMobileNRFromLTECSVNative`).
5. Transformuj WGS84 -> S-JTSK + vypocitaj zonu/usek pre kazdy riadok (`ProcessDataNative`).
6. Agreguj statistiky po zona+operator+frekvencia, vyber jednu frekvenciu pre zona+operator (`CalculateZoneStatsNative`).
7. Exportuj `_zones.csv` (`SaveZoneResultsNative`).
8. Exportuj `_stats.csv` (`SaveStatsNative`).
9. Spocitaj sumarizacne metriky (`ProcessingResult`).

---

## 2. Konfiguracia a vstupy

## 2.1 `ProcessingConfig` (presne polia)

Definicia: `internal/backend/types.go`.

Klucove polia pre vypocty:

- `FilePath` - cesta k vstupnemu CSV.
- `ColumnMapping` - mapovanie logickych klucov na indexy stlpcov:
  - povinne: `latitude`, `longitude`, `frequency`, `pci`, `mcc`, `mnc`, `rsrp`
  - volitelne: `sinr`
- `ZoneMode` - `segments` | `center` | `original`
- `ZoneSizeM` - velkost zony/useku v metroch
- `RSRPThreshold`, `SINRThreshold` - prahy GOOD/BAD
- `IncludeEmptyZones`
- `AddCustomOperators`, `CustomOperators`
- `FilterPaths`
- `OutputSuffix`
- `MobileModeEnabled`, `MobileLTEFilePath`, `MobileTimeToleranceMS`, `MobileNRColumnName`

Predvolene hodnoty (`DefaultProcessingConfig`):

- `ZoneMode="segments"`
- `ZoneSizeM=100`
- `RSRPThreshold=-110`
- `SINRThreshold=-5`
- `MobileTimeToleranceMS=1000`
- `MobileRequireNRYES=false` (legacy, realne sa nepouziva na filtrovanie)
- `MobileNRColumnName="5G NR"`

## 2.2 Validacie

- Backend vrati chybu, ak chyba povinne mapovanie stlpcov.
- UI navyse validuje cisla (kladna velkost zony, integer tolerancia), ale backend je definitivna autorita.

---

## 3. CSV loader (dekodovanie, hlavicka, riadky)

Kde v kode:

- `internal/backend/csv_loader.go`
- Hlavna funkcia: `LoadCSVFile`

## 3.1 Dekodovanie suboru

Dekodery v poradi:

1. `utf-8` (strict validacia)
2. `latin1`
3. `latin2`
4. `cp1250`
5. `windows-1250`
6. `iso-8859-2`

Pravidlo:

- Zoberie sa prvy dekoder, pri ktorom sa najde tabulkova hlavicka (>= 6 stlpcov a nasledne tabulkove riadky).
- Ak sa hlavicka neda najst, zachova sa prvy dekodovatelny text a fallbackne sa na `headerLine=0`.

## 3.2 Hladanie hlavicky

Funkcie:

- `splitSemicolonColumns`
- `hasTabularFollowup`
- `findTabularHeader`

Logika:

- Kandidat hlavicky je riadok s min. `minColumns=6` stlpcami.
- Za hlavickou sa kontroluje max 25 ne-prazdnych riadkov.
- Ak aspon 2 riadky maju pocet stlpcov >= `max(minColumns, expectedColumns-1)`, kandidat sa berie ako hlavicka.

## 3.3 Normalizacia schema stlpcov

- Trailing prazdne stlpce po `;` sa odrezavaju.
- Ak data maju viac poli ako hlavicka, doplnia sa `extra_col_1`, `extra_col_2`, ...
- Duplicity nazvov stlpcov sa disambiguuju: `name`, `name_2`, `name_3`.
- Riadky sa doplnaju prazdnymi hodnotami na `maxFields` alebo skracaju na `maxFields`.

## 3.4 Hranične stavy a neplatne vstupy

- Neexistujuci subor -> chyba `os.ReadFile`.
- Nekompatibilne kodovanie -> chyba `unable to decode CSV...`.
- Prazdny/ne-tabulkovy subor -> fallback hlavicka na riadok 0, ale moze vzniknut minimalna schema.

---

## 4. Filtre - parser a aplikacia

Kde v kode:

- Parser pravidiel: `internal/backend/filters.go`
- Aplikacia na CSV: `internal/backend/filter_apply.go`

## 4.1 Syntax pravidla

Zo suboru sa vezme `<Query>...</Query>` alebo cely text.

Rozdelenie:

- cast pred prvym `;` = `assignments`
- cast za prvym `;` = `conditions`

### 4.1.1 Assignments

Regex: `"field" = value`

- Povolene cisla s `.` alebo `,`.
- Rozsah (`a-b`) v assignment casti je **zakazany** -> chyba.
- Duplicitne assignment hodnoty pre rovnake pole sa deduplikuju.

### 4.1.2 Conditions

Podmienka moze byt:

- `eq`: `value`
- `range`: `start-end` (ak start > end, prehodi sa)

Dolezite:

- `range` je implementovane ako:
  - ak `low == high`: `val == low`
  - inak: `low <= val < high` (horny interval je otvoreny)

## 4.2 Mapovanie nazvov poli filtra na realne CSV stlpce

Funkcia `resolveColumnName`:

1. exact match,
2. case-insensitive match,
3. specialita: ak field = `frequency` a existuje stlpec `SSRef`, preferuje sa `SSRef`,
4. fallback cez alias map (`lat`, `lon`, `earfcn`, ... ) + `columnMapping`.

## 4.3 Vyhodnotenie podmienok

Funkcia `rowMatchesGroup`:

- Vsetky podmienky v skupine su `AND`.
- Skupiny su `OR`.
- Ak sa hodnota neda parsovat na float -> podmienka neplati.

## 4.4 Vyber pravidla pri viacnasobnom matchi

Na jeden riadok sa zo vsetkych rules vyberie **jedno** pravidlo:

1. vacsia `bestGroupSize` (viac podmienok) ma prioritu,
2. pri zhode lexikograficky mensie `rule.Name`.

To znamena: kod **nehadze chybu** pri viac matchoch; deterministicky vyberie jedno pravidlo.

## 4.5 Generovanie vystupnych riadkov po filtroch

Pre vybrane pravidlo:

- volitelne sa ponecha original riadok (`keepOriginalOnMatch=true`),
- vytvoria sa vsetky kombinacie assignment hodnot (`kartézsky sucin`),
- pre kazdu kombinaciu sa vytvori novy riadok.

Vzorec poctu duplikatov:

- Nech `A_i` je pocet hodnot pre i-te assignment pole.
- Pocet generovanych assignment riadkov = `∏ A_i`.
- Celkom pre match riadok = `∏ A_i + (keepOriginalOnMatch ? 1 : 0)`.

Kazdy vystupny riadok dostane/doplni `original_excel_row`.

## 4.6 Rucny priklad filtra

Pravidlo:

- assignment: `"MCC"=231`, `"MNC"=1` a `"MNC"=3`
- condition skupina: `("Frequency"=3500000000-3600000000 AND "MCC"=231)`

Vstupny riadok:

- `Frequency=3550000000`, `MCC=231`, `MNC=2`

Vyhodnotenie:

1. `3500000000 <= 3550000000 < 3600000000` -> true
2. `MCC==231` -> true
3. skupina true -> pravidlo match
4. assignment kombinacie pre `MNC` su 2 (`1`, `3`)
5. Vystup: 2 riadky (plus original, ak zapnute)

---

## 5. Mobile sync (`5G NR`) z LTE CSV

Kde v kode:

- `internal/backend/mobile_sync_native.go`
- Hlavna funkcia: `syncMobileNRFromLTECSVNative`

## 5.1 Predpoklady

Povinne LTE stlpce: `MCC`, `MNC`, `5G NR` (alebo alias `5GNR`, `NR`).

Povinne 5G mapovanie: `mcc`, `mnc` indexy musia byt validne.

Ak chyba pouzitelny cas v niektorom datasete -> chyba.

## 5.2 Casova os - prevod na milisekundy

Funkcia `buildTimeMillisSeriesNative` ma strategiu:

1. Skus `UTC` ako cislo.
2. Ak su cisla, urci faktor:
   - median absolutnych hodnot `>= 1e11` -> uz su to ms, faktor `1`
   - inak -> sekundy, faktor `1000`
3. `ms = mathRound(value * factor)`
4. Ak UTC-cislo zlyha, skus parse UTC ako datetime string.
5. Ak zlyha, skus `Date + Time`.

Podporovane layouty datetime:

- `2006-01-02 15:04:05.999999999`
- `2006-01-02 15:04:05`
- `2.1.2006 15:04:05`
- `02.01.2006 15:04:05`
- RFC3339Nano, RFC3339

Casova zona: `time.Local`.

## 5.3 Score `5G NR`

Normalizacia `normalizeNRValueNative`:

- `yes` aj varianty (`true`, `1`, `ano`, `áno`, ... ) -> `yes`
- `no` aj varianty (`false`, `0`, ... ) -> `no`
- inak prazdne

Skore:

- `yes -> 2`
- `no -> 1`
- prazdne -> `0`

## 5.4 Oknove rozhodovanie

Pre kazdy 5G riadok sa hlada LTE okno:

- interval `[timeMS - tolerance, timeMS + tolerance]`
- najprv podla presneho `MCC+MNC`
- ak 5G riadok nema MCC/MNC, fallback na globalny LTE lookup

Funkcia `resolveWindowScore`:

- Ak v okne je aspon jedno `yes`, vysledok je `yes`.
- Inak ak je aspon jedno `no`, vysledok je `no`.
- Inak prazdne.
- Konflikt (`yes` aj `no`) sa zaznamena v stats, ale vysledok ostava `yes`.

## 5.5 Hraničné stavy

- `timeToleranceMS < 0` -> clip na `0`.
- Bez LTE riadkov s `yes` -> chyba.
- Ak `nrColumnName` v 5G neexistuje, vytvori sa novy stlpec.

## 5.6 Rucny priklad mobile okna

LTE (po prevode na ms):

- `1000 -> yes`
- `1500 -> no`
- `3000 -> no`

`tolerance=300`.

5G casy:

1. `1300`: okno `[1000,1600]` -> obsahuje yes+no -> vysledok `yes`, `conflict=true`
2. `2800`: okno `[2500,3100]` -> obsahuje `3000 no` -> `no`
3. `5000`: okno prazdne -> prazdne

---

## 6. Geodeticka transformacia (WGS84 -> EPSG:5514)

Kde v kode:

- `internal/backend/projection_native.go`

Pipeline `forwardOne`:

1. stupne -> radiany (`degToRad`)
2. geodeticke -> ECEF na WGS84 (`geodeticToECEF`)
3. inverzna Helmert transformacia "S-JTSK to WGS84 (4)" (`applyHelmertSJTSKtoWGS84_4(..., inverse=true)`)
4. ECEF -> geodeticke na Bessel 1841 (`ecefToGeodetic`)
5. Krovak forward (`krovakEN.forward`)
6. prehodenie osi na EPSG:5514 (`return pyK, pxK`)

## 6.1 Zakladne vzorce

### 6.1.1 Geodetic -> ECEF

Premenne:

- `lon`, `lat` [rad]
- `h` [m]
- `a` [m] velka polos
- `e2` [-] excentricita^2

Vzorce:

- `N = a / sqrt(1 - e2 * sin(lat)^2)`
- `x = (N + h) * cos(lat) * cos(lon)`
- `y = (N + h) * cos(lat) * sin(lon)`
- `z = (N * (1 - e2) + h) * sin(lat)`

### 6.1.2 Helmert 7-param

Konstanty:

- translacia [m]: `tx=485.0`, `ty=169.5`, `tz=483.8`
- rotacie [arcsec]: `rx=7.786`, `ry=4.398`, `rz=4.103`
- mierka [ppm]: `s=0`

Konverzia rotacii:

- `r = arcsec * pi/(180*3600)`

Maticovy tvar (position-vector):

- `v2 = t + A * v1`
- pre inverzny smer sa pocita `v1 = A^-1 * (v2 - t)`

### 6.1.3 ECEF -> geodetic

- inicializacia cez Bowring-like odhad (`theta`)
- 3 iteracie Newton-like refinemenu
- stop pri `|newLat - lat| < 1e-14`

### 6.1.4 Krovak

Implementacia je priamo v `krovakEN.forward` a `krovakEN.inverse`.

Dolezite numericke detaily:

- `asinz` saturuje argument do `[-1,1]`
- inverse iteruje max `15` krokov, stop pri `1e-10`

## 6.2 Inverzna transformacia (`Inverse`)

- Najprv `inverseApproxOne` (Krovak inverse + Helmert forward + ECEF->WGS84).
- Potom Newton refine (max 8 iteracii):
  - Jacobi cez konecne diferencie so `stepDeg=1e-6`
  - stop ked `hypot(rx,ry) < 1e-6` alebo `|dLon|+|dLat| < 1e-12`
  - ak `|det(J)| < 1e-12`, iteracia sa prerusi.

## 6.3 Chyby a hraničné stavy

- NaN lon/lat -> chyba.
- Degenerovany determinant pri Helmert inverse -> vrati povodne `(x,y,z)` (fallback).
- `ctx.Done()` okamzite prerusi batch transformaciu.

Poznamka pre manualny audit: tato cast je numericky najnarocnejsia; pre rucny prepocet je nutna vedecka kalkulacka a presne poradie operacii z funkcii uvedenych vyssie.

---

## 7. Spracovanie riadkov, zony a useky (`ProcessDataNative`)

Kde v kode:

- `internal/backend/processing_native.go`, funkcia `ProcessDataNative`

## 7.1 Predspracovanie riadkov

Pre kazdy CSV riadok:

1. urci `originalExcelRow`:
   - default `i + headerLine + 1`
   - ak existuje stlpec `original_excel_row` a je parse-ovatelny int, ma prednost
2. `RSRP` sa musi dat parsovat na float, inak sa riadok potichu zahodi
3. `Latitude/Longitude` musia byt validne float, inak sa vracia chyba
4. `SINR` je volitelny (`HasSINR=true` len ak parse uspeje)

Decimalny parser vsade:

- trim
- `,` -> `.`
- `strconv.ParseFloat(...,64)`

## 7.2 Prevod suradnic

- Batch `transformer.Forward` pre `(lon,lat)` -> `(x,y)` v metroch.

## 7.3 `ZoneSizeM`

- Ak `ZoneSizeM <= 0`, backend fallbackne na `100`.

## 7.4 Rezim `segments`

### 7.4.1 Definicie

- `cumulativeDistance` [m]
- `stepDistance = hypot(x_i - x_{i-1}, y_i - y_{i-1})`
- `segmentID_i = floor((cumulativeDistance + epsilon)/ZoneSizeM)`
- `epsilon = 1e-9`

### 7.4.2 Hranice segmentov

Ak krok prekroci jednu alebo viac hranic segmentov:

- `boundaryDistance = segID * ZoneSizeM`
- `offset = boundaryDistance - prevCumulative`
- `fraction = offset / stepDistance`
- `fraction` sa clipuje do `[0,1]`
- zaciatok segmentu:
  - `x_b = prevX + (x - prevX) * fraction`
  - `y_b = prevY + (y - prevY) * fraction`

Uklada sa do `SegmentMeta[segID]`.

### 7.4.3 Vystupny kluc

- `zonaKey = "segment_<id>"`
- `zonaX,zonaY` = start segmentu z `SegmentMeta`

## 7.5 Rezim `center` a `original` (grid zony)

Bod sa mapuje:

- `zonaX = floor(x / ZoneSizeM) * ZoneSizeM`
- `zonaY = floor(y / ZoneSizeM) * ZoneSizeM`
- `zonaKey = "<zonaX>_<zonaY>"`

Pozor pri zapornych hodnotach: `floor` ide k mensiemu cislu (napr. `floor(-0.1)=-1`).

## 7.6 Operator kluce

- `operatorKey = MCC + "_" + MNC`
- `zonaOperatorKey = zonaKey + "_" + operatorKey`

---

## 8. Agregacia a vyber frekvencie (`CalculateZoneStatsNative`)

Kde v kode:

- `internal/backend/processing_native.go`, funkcia `CalculateZoneStatsNative`

## 8.1 Grouping kluc (uroven 1)

Agreguje sa podla:

- `ZonaKey`, `OperatorKey`, `ZonaX`, `ZonaY`, `MCC`, `MNC`, `PCI`, `Freq`

Riadok sa vyradi, ak je blank v ktoromkolvek z: `MCC`, `MNC`, `PCI`, `Frequency`.

## 8.2 Vypocet agregatov pre skupinu

Pre kazdu skupinu:

- `RSRPAvg = RSRPSum / Count`
- `SINRAvg = SINRSum / SINRCount` (len ak `SINRCount>0`)
- `NRValue`:
  - `yes`, ak `NRYesCount > 0`
  - inak `no`, ak `NRNoCount > 0`
  - inak prazdne

## 8.3 Ranking skupin (vyber najlepsiej frekvencie)

`zoneFreqStats` sa zoradi:

1. `RSRPAvg` zostupne
2. `Count` zostupne
3. `Freq` numericky vzostupne (`compareNumericStringAsc`)
4. `Freq` lexikograficky vzostupne
5. `PCI` numericky vzostupne
6. `PCI` lexikograficky vzostupne

Potom sa berie **prva** skupina pre kazdy kluc:

- `ZonaKey`, `OperatorKey`, `ZonaX`, `ZonaY`, `MCC`, `MNC`

Tym padom pre jednu zonu+operator zostane jedna vybrana frekvencia + PCI.

## 8.4 Zona stred pre geolokaciu

- `segments`: stred = `zonaX,zonaY` (start useku)
- inak: stred = `(zonaX + ZoneSizeM/2, zonaY + ZoneSizeM/2)`

Tento stred sa transformuje `Inverse` na lon/lat.

## 8.5 GOOD/BAD klasifikacia pre `ZoneStat`

Ak ma statistika SINR (`HasSINRAvg=true`):

- GOOD ak `RSRPAvg >= RSRPThreshold` **a** `SINRAvg >= SINRThreshold`
- inak BAD

Ak nema SINR:

- GOOD ak `RSRPAvg >= RSRPThreshold`
- inak BAD

Vysledok ide do `RSRPKategoria`.

## 8.6 Rucny priklad agregacie

Vstup pre jednu zonu/operator:

- r1: freq=3500, pci=10, rsrp=-100, sinr=5, nr=yes, row=10
- r2: freq=3500, pci=10, rsrp=-110, sinr=1, nr=no, row=11
- r3: freq=3600, pci=20, rsrp=-105, sinr=3, nr=yes, row=12

Skupiny:

1. `(3500,10)`:
   - `RSRPAvg=(-100-110)/2=-105`
   - `Count=2`
   - `SINRAvg=(5+1)/2=3`
   - `NRValue=yes` (existuje aspon jedno yes)
2. `(3600,20)`:
   - `RSRPAvg=-105`
   - `Count=1`
   - `SINRAvg=3`
   - `NRValue=yes`

Ranking:

- `RSRPAvg` remiza, rozhoduje `Count`: vyhra `(3500,10)`.

---

## 9. Export `_zones.csv` (`SaveZoneResultsNative`)

Kde v kode:

- `internal/backend/outputs_native.go`

## 9.1 Header a format

- Prvy riadok exportu je prazdny string (`""`), potom header.
- Header = povodna hlavicka + (volitelne NR stlpec) + `Riadky_v_zone;Frekvencie_v_zone`.

## 9.2 Hodnoty zapisovane do riadku

Pre vybrany `ZoneStat`:

- `RSRP` sa zapisuje `%.2f`
- `SINR` sa zapisuje `%.2f` (len ak existuje)
- `lat/lon`:
  - `segments` alebo `center`: `%.6f` z vypocitaneho stredu/useku
  - `original`: nechava sa povodny sample row (normalizovany parser->string)

`NR` export:

- `yes -> 1`
- `no -> 0`
- inak povodny trim text

`MCC/MNC/PCI`:

- ked su numericke, formatuju sa ako int-like string bez `.0`.

## 9.3 Prázdne zóny/úseky (`IncludeEmptyZones`)

Pre kazdu chybajucu kombinaciu `zona x operator` sa doplni riadok:

- `RSRP=-174`
- `NR=0` (ak NR stlpec existuje)
- komentar `# Prázdna zóna - automaticky vygenerovaná` / `# Prázdny úsek...`

### 9.3.1 Ako sa ziskaju all zones

- `segments`: z `SegmentMeta` (zoradene ID)
- inak: poradie unikatnych `zonaKey` zo stats

### 9.3.2 Lat/Lon prazdnych zon

- `segments`: z `SegmentMeta[id]` + `Inverse`
- grid: parse `zonaKey` -> center `(zx+S/2, zy+S/2)` + `Inverse`

## 9.4 Custom operators

Ak `IncludeEmptyZones && AddCustomOperators`:

1. Deduplikuju sa custom operatori podla `MCC_MNC`.
2. Preskocia sa operatori uz pritomni v povodnych stats.
3. Pre kazdu zonu sa doplni prazdny riadok s tymto operatorom (`RSRP=-174`, `NR=0`).
4. Do `ZoneStats` sa prida placeholder (jeden na operator), aby `_stats.csv` vedel dopoctat `bad` cez chybajuce zony.

---

## 10. Export `_stats.csv` (`SaveStatsNative`)

Kde v kode:

- `internal/backend/outputs_native.go`

## 10.1 Grouping

Bucket kluc je len:

- `(MCC, MNC)`

Kazdy `ZoneStat` prispieva `good` alebo `bad`.

## 10.2 Pravidlo GOOD/BAD v stats

Funkcia `isZoneStatGoodForStats`:

1. `good = (RSRPAvg >= RSRPThreshold)`
2. ak ma SINR: `good = good && (SINRAvg >= SINRThreshold)`
3. ak mobile mode: `good = good && (normalizeNRValueNative(NRValue) == "yes")`

## 10.3 IncludeEmptyZones v stats

Pre kazdy operator:

- `missingZones = totalUniqueZones - len(existingZonesOperatora)`
- ak `missingZones > 0`: `bad += missingZones`

`totalUniqueZones`:

- primarne z `allZones` vratenych z exportu zones,
- fallback z unikatnych `zonaKey` v `zoneStats`.

## 10.4 Vystupna schema

Header:

- `MNC;MCC;<GOOD column>;<BAD column>`

Nazov stlpcov sa sklada dynamicky z thresholdov a mobile rezimu.

Ak `zoneStats` prazdne -> subor obsahuje len `"\n"`.

## 10.5 Rucny priklad stats

Predpoklad:

- `totalUniqueZones = 5`
- operator `231_01`: ma v stats zony `{A,B}`
- z tychto dvoch: `good=1`, `bad=1`

Pri `IncludeEmptyZones=true`:

- `missing=5-2=3`
- final `bad=1+3=4`
- zapis: `good=1`, `bad=4`

---

## 11. `ProcessingResult` sumarizacie

Kde v kode:

- `internal/backend/runner_native.go`

## 11.1 Zakladne pocty

- `UniqueZones` = pocet unikatnych `zonaKey` v final `zoneStats`
- `UniqueOperators` = pocet unikatnych `MCC_MNC`
- `TotalZoneRows` = `len(zoneStats)`

## 11.2 Coverage metriky (len pre non-segments)

Pocitaju sa iba ak `ZoneMode != "segments" && len(zoneStats)>0`.

- `minX,maxX,minY,maxY` z `ZonaX,ZonaY`
- `rangeXM = maxX - minX + ZoneSizeM`
- `rangeYM = maxY - minY + ZoneSizeM`
- `theoreticalTotalZones = (rangeXM/ZoneSizeM) * (rangeYM/ZoneSizeM)`
- `coveragePercent = (UniqueZones / theoreticalTotalZones) * 100` ak menovatel > 0

V segment rezime su tieto pointer polia `nil`.

---

## 12. Zaokruhlovanie, floating-point, integer detaily

## 12.1 Systematicky prehlad

1. **`parseNumberString` / `parseNumber`**
   - `,` -> `.`
   - `float64`
2. **Filter equality**
   - porovnanie float `==` (bez epsilon)
3. **Filter range**
   - `[low, high)`
4. **Segment ID**
   - `floor((distance + 1e-9)/ZoneSize)` (ochrana proti hraničnej binarnej chybe)
5. **Interpolacia hranice segmentu**
   - `fraction` clip `[0,1]`
6. **`mathRound` (mobile time)**
   - polovicne hodnoty sa zaokruhluju od nuly (implementacne cez `int64(v±0.5)`)
7. **CSV export RSRP/SINR**
   - fixne `%.2f`
8. **CSV export lat/lon v center/segments**
   - fixne `%.6f`
9. **Stats percento v UI**
   - frontend `toFixed(2)` pre zobrazenie
10. **`compareNumericStringAsc`**
   - ak oboje parse-ovatelne -> numericke porovnanie
   - ak len jedno parse-ovatelne, numericke ide skor
   - ak ani jedno -> remiza (nasleduje lexikografia v sort callbacku)

## 12.2 Epsilon/saturacia/clipping checklist

- `epsilon=1e-9` pri segmente: ano
- clipping `fraction [0,1]`: ano
- clipping `asin` argumentu: ano (`asinz`)
- determinant guardy:
  - Helmert inverse: `det==0`
  - Newton inverse transform: `|det|<1e-12`

---

## 13. Rucne auditovatelne priklady krok za krokom

## 13.1 Priklad A - grid zona (`ZoneMode=center`)

Predpoklad po transformacii:

- `x=1234.56 m`, `y=5678.90 m`, `S=100 m`

Kroky:

1. `x/S = 12.3456`
2. `floor = 12`
3. `zonaX = 12*100 = 1200`
4. `y/S = 56.789`
5. `floor = 56`
6. `zonaY = 56*100 = 5600`
7. `zonaKey = "1200_5600"`
8. stred zony:
   - `centerX=1200+50=1250`
   - `centerY=5600+50=5650`

Toto je presne logika `ProcessDataNative` + `CalculateZoneStatsNative` pre non-segments.

## 13.2 Priklad B - segmentacia s interpolaciou hranic

Body (po transformacii), `S=100`:

- `P0=(0,0)`
- `P1=(130,0)`
- `P2=(250,0)`

Inicializacia:

- `cumulative=0`, `segment_0` start `(0,0)`

Krok P0->P1:

1. `step=130`
2. `prevCumulative=0`, `cumulative=130`
3. `prevSeg=floor((0+1e-9)/100)=0`
4. `newSeg=floor((130+1e-9)/100)=1`
5. prekrocena hranica segID=1:
   - `boundary=100`
   - `offset=100-0=100`
   - `fraction=100/130=0.769230...`
   - start `segment_1 = (0 + (130-0)*0.769230, 0) = (100,0)`
6. riadok P1 patri do `segment_1`

Krok P1->P2:

1. `step=120`
2. `prevCumulative=130`, `cumulative=250`
3. `prevSeg=1`, `newSeg=2`
4. hranica segID=2:
   - `boundary=200`
   - `offset=200-130=70`
   - `fraction=70/120=0.583333...`
   - start `segment_2=(130 + (250-130)*0.583333, 0)=(200,0)`
5. riadok P2 patri do `segment_2`

## 13.3 Priklad C - vyber frekvencie pri remize RSRP

Skupina 1: `freq=3500`, `count=2`, `RSRPAvg=-105`

Skupina 2: `freq=3600`, `count=1`, `RSRPAvg=-105`

Sort poradie:

1. RSRP remiza
2. count: 2 > 1 -> vyhrava `3500`

## 13.4 Priklad D - mobile window score

Data z kapitoly 5.6:

- 5G `t=1300` -> okno obsahuje yes aj no -> vysledok `yes`
- 5G `t=2800` -> len no -> `no`
- 5G `t=5000` -> nic -> prazdne

Presne podla `resolveWindowScore`.

## 13.5 Priklad E - stats s prazdnymi zonami

`totalUniqueZones=5`, operator ma existujuce 2 zony (`good=1`, `bad=1`):

- `missing=3`
- final `bad=4`

---

## 14. Mapovanie "kde v kode sa to pocita"

## 14.1 Orchestrace

- `internal/backend/runner.go` -> `RunProcessing`
- `internal/backend/runner_native.go` -> `runProcessingNative`, `loadRulesForConfig`

## 14.2 CSV nacitanie

- `internal/backend/csv_loader.go`
  - `LoadCSVFile`
  - `decodeWithCandidates`
  - `findTabularHeader`, `hasTabularFollowup`
  - `makeUniqueColumnNames`

## 14.3 Filtre

- `internal/backend/filters.go`
  - `LoadFilterRuleFromFile`
  - `parseAssignments`
  - `parseConditionGroups`
- `internal/backend/filter_apply.go`
  - `ApplyFiltersCSV`
  - `rowMatchesGroup`
  - `buildAssignmentCombinations`

## 14.4 Mobile sync

- `internal/backend/mobile_sync_native.go`
  - `syncMobileNRFromLTECSVNative`
  - `buildTimeMillisSeriesNative`
  - `parseDateTimeToMillis`
  - `resolveWindowScore`

## 14.5 Projekcia a geodezia

- `internal/backend/projection_native.go`
  - `PyProjTransformer.Forward/Inverse`
  - `forwardOne`, `inverseOne`, `inverseApproxOne`
  - `geodeticToECEF`, `ecefToGeodetic`
  - `applyHelmertSJTSKtoWGS84_4`
  - `krovakEN.forward`, `krovakEN.inverse`

## 14.6 Zony/useky a agregacia

- `internal/backend/processing_native.go`
  - `ProcessDataNative`
  - `CalculateZoneStatsNative`
  - `compareNumericStringAsc`

## 14.7 Exporty

- `internal/backend/outputs_native.go`
  - `SaveZoneResultsNative`
  - `appendEmptyZonesNative`
  - `appendCustomOperatorsNative`
  - `SaveStatsNative`
  - `isZoneStatGoodForStats`

## 14.8 UI/API vrstva

- `app.go`
  - `RunProcessingWithConfig`
  - `LoadCSVPreview`
- `frontend/src/main.ts`
  - `buildProcessingConfig`
  - `parseCustomOperatorsText`
  - `runProcessing`

---

## 15. Overenie spravnosti (manualny audit + test scenare)

## 15.1 Minimalny auditny checklist

1. Overit mapovanie stlpcov a required keys.
2. Overit pocet nacitanych riadkov pred/po filtroch.
3. Rucne prepocitat aspon 3 riadky segmentacie alebo floor-zonovania.
4. Rucne prepocitat aspon 1 agregaciu po frekvencii (RSRPAvg, Count, SINRAvg).
5. Overit ranking a vyber frekvencie pri remize.
6. Overit GOOD/BAD logiku na hranicnych hodnotach (`== threshold`).
7. Overit `IncludeEmptyZones` dopocet bad v `_stats.csv`.
8. V mobile rezime overit aspon 1 konflikt yes/no v okne.
9. Overit, ze NR v `_zones.csv` je `1/0`.
10. Overit, ze `ProcessingResult` coverage je len pre non-segments.

## 15.2 Odporucane testovacie scenare

1. **Bez SINR stlpca**
   - Ocekavanie: klasifikacia len podla RSRP.
2. **So SINR stlpcom, ale prazdny SINR v casti riadkov**
   - Ocekavanie: priemer SINR len z validnych SINR.
3. **Filter s rozsahom low==high**
   - Ocekavanie: sprava sa ako rovnost.
4. **Filter s low>high**
   - Ocekavanie: parser prehodi hranice.
5. **Segment rezim s nulovou vzdialenostou medzi po sebe iducimi bodmi**
   - Ocekavanie: bez noveho segmentu.
6. **Negative zone coordinates**
   - Ocekavanie: `floor` semantika pre zaporne hodnoty.
7. **Mobile UTC v sekundach vs milisekundach**
   - Ocekavanie: detekcia faktora podla medianu.
8. **Mobile bez LTE YES**
   - Ocekavanie: hard error.

## 15.3 Existujuci automaticky test

- `internal/backend/outputs_native_test.go`
  - overuje, ze `5G NR` sa exportuje binarne (`1/0`) a nie textovo (`yes/no`).

Poznamka: v tomto prostredi nebol dostupny prikaz `go`, preto nebolo mozne test spustit lokalne v ramci tohto auditu.

---

## 16. Otvorene body (co nie je explicitne dokazatelne iba z kodu)

1. **Numericka parita s externym pyproj**
   - Kod deklaruje paritu komentarom, ale v repozitari nie je plnohodnotny golden test proti pyproj pre sirsi dataset.
2. **Historicke env premenne `FILTERS_DEBUG_OUTPUT` a `OUTPUT_SUFFIX`**
   - V aktualnom kóde nie je priame citanie `os.Getenv` pre tieto premenne.
   - `OutputSuffix` existuje ako konfiguracne pole, ale nie je viazane na env.
3. **Referencia na sample data `data/` v starsich textoch**
   - V aktualnom worktree priecinok `data/` neexistuje, preto auditne priklady v tomto dokumente su konstruovane priamo z implementacnych pravidiel.

---

## 17. Strucna matica "vypocet -> vzorec -> hranične stavy"

| Oblast | Vzorec / pravidlo | Hranične stavy |
|---|---|---|
| Grid zona | `floor(x/S)*S`, `floor(y/S)*S` | `S<=0 -> 100`; zaporne hodnoty cez `floor` |
| Segment ID | `floor((cum+1e-9)/S)` | `step=0`; prechody cez viac hranic naraz |
| Interpolacia hranice | `P = P_prev + (P_cur-P_prev)*fraction` | `fraction` clip `[0,1]` |
| RSRP priemer | `sum/count` | `count>0` garantovane groupingom |
| SINR priemer | `sum/count` len z validnych SINR | ak `count=0`, SINR sa nepouzije |
| GOOD/BAD | `RSRP>=thr` a volitelne `SINR>=thr` a volitelne `NR=yes` | presne `>=` (hranica patri do GOOD) |
| Coverage | `(unique/theoretical)*100` | len non-segments; menovatel >0 |
| Filter range | `low<=x<high` | `low==high` -> `x==low` |
| Mobile okno | interval `t±tol`, priorita `yes>no` | `tol<0 -> 0`; konflikt yes+no |
| UTC konverzia | `ms=round(v*factor)` | `factor=1` ak median>=1e11 inak 1000 |

---

## 18. Zaver

Dokument pokryva kompletny aktualny vypoctovy tok v Go implementacii. Vsetky klucove vypocty maju uvedene vzorce, poradie operacii, jednotky, hranične stavy a mapovanie na funkcie. Pre manualny audit odporucam postupovat podla kapitoly 15 a pri geodetickej casti pouzit vedecku kalkulacku alebo export medzivysledkov priamo z funkcii v `projection_native.go`.
