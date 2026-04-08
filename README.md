# 100mscript

Desktop aplikácia na spracovanie CSV meraní mobilného signálu (LTE / 5G NR) do priestorových zón alebo úsekov po trase, s automatickým generovaním štatistických výstupov pre operátorov.

Program načíta namerané dáta z jedného alebo viacerých CSV súborov, aplikuje voliteľné filtre na duplikáciu zdieľaných frekvencií, voliteľne synchronizuje 5G NR pokrytie z LTE merania, transformuje GPS súradnice do metrického systému (S-JTSK), rozdelí body do zón alebo úsekov, agreguje hodnoty signálu po frekvenciách a operátoroch, klasifikuje kvalitu pokrytia a exportuje dva výstupné CSV súbory.

---

## Technológie

| Vrstva | Technológia |
|---|---|
| Backend a výpočty | Go 1.24 |
| Desktop shell | Wails v2 |
| Frontend (UI) | TypeScript, vanilla DOM |
| Cieľový OS pre build | Windows (amd64), vývoj na macOS/Linux |
| Geodetická projekcia | Natívna Go implementácia (Krovák / EPSG:5514) |
| Kódovania CSV | UTF-8, Latin-1, Latin-2, CP1250, Windows-1250, ISO-8859-2 |

---

## Spustenie a build

**Požiadavky:** Go 1.22+, Node.js 20+, Wails CLI (voliteľné).

**Frontend závislosti:**

```bash
cd frontend && npm install
```

**Vývojový režim:**

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev
```

**Windows produkčný build:**

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -platform windows/amd64 -clean
```

**Testy:**

```bash
go test ./...
```

---

## Vstupné a výstupné súbory

**Vstup:** Jeden alebo viac CSV súborov oddelených bodkočiarkou (`;`). Pri viacerých súboroch musia mať identickú hlavičku (názvy stĺpcov v rovnakom poradí). Po zlúčení sa riadky zoradia podľa časovej značky, ak je dostupná.

**Výstupy:**

| Súbor | Obsah |
|---|---|
| `<input>_zones.csv` | Jeden riadok za každú kombináciu zóna + operátor s priemerným RSRP, vybranou frekvenciou, PCI a GPS súradnicami stredu zóny |
| `<input>_stats.csv` | Sumárna tabuľka za každého operátora (MCC/MNC) s počtom GOOD a BAD zón |

V mobile režime sa do názvu výstupov automaticky pridáva `_mobile`. Výstupné cesty je možné manuálne prepísať v UI.

---

## Hlavný dátový tok (pipeline)

Celé spracovanie prebieha v jednom sekvenčnom pipeline v tomto poradí:

1. **Načítanie CSV** -- Detekcia kódovania, nájdenie hlavičky, normalizácia stĺpcov. Pri viacerých súboroch zlúčenie riadkov a zoradenie podľa času.
2. **Príprava riadkov** -- Doplnenie stĺpca `original_excel_row` pre trasovateľnosť. Voliteľné vylúčenie riadkov podľa časových okien alebo podľa čísla riadku.
3. **Aplikácia filtrov** -- Načítanie pravidiel z `.txt` súborov, priradenie k riadkom podľa podmienok, duplikácia riadkov podľa assignment kombinácií.
4. **Mobile sync** *(voliteľný)* -- Načítanie LTE CSV, synchronizácia stĺpca `5G NR` do 5G datasetu na základe časovej zhody.
5. **Transformácia súradníc** -- Prevod WGS84 (GPS) na S-JTSK (EPSG:5514) v metroch.
6. **Výpočet zón / úsekov** -- Priradenie každého bodu do zóny podľa zvoleného režimu.
7. **Agregácia štatistík** -- Zoskupenie podľa zóna + operátor + frekvencia + PCI, výpočet priemerov, výber najlepšej frekvencie.
8. **Export zones CSV** -- Zápis jedného riadku za zóna + operátor, voliteľné doplnenie prázdnych zón a custom operátorov.
9. **Export stats CSV** -- Zápis sumárnych GOOD/BAD počtov za operátora.

---

## Načítanie CSV

### Detekcia kódovania

Program skúša dekódovať súbor v tomto poradí: UTF-8 (striktná validácia), Latin-1, Latin-2, CP1250, Windows-1250, ISO-8859-2. Použije sa prvý dekóder, pri ktorom sa nájde tabulárna hlavička.

### Hľadanie hlavičky

Za hlavičku sa považuje prvý riadok s minimálne 6 stĺpcami (oddelených bodkočiarkou), za ktorým nasledujú aspoň 2 dátové riadky s podobným počtom stĺpcov (z najbližších 25 neprázdnych riadkov).

### Normalizácia schémy

- Trailing prázdne stĺpce po `;` sa orezávajú.
- Ak dátové riadky majú viac polí ako hlavička, doplnia sa stĺpce `extra_col_1`, `extra_col_2` atď.
- Duplicitné názvy stĺpcov sa disambiguujú: `name`, `name_2`, `name_3`.
- Riadky sa zarovnajú na jednotnú šírku (doplnia sa prázdne hodnoty alebo sa orezávajú).

### Zlúčenie viacerých súborov

Všetky súbory musia mať zhodné názvy stĺpcov v rovnakom poradí (po normalizácii). Riadky sa spoja do jedného datasetu. Ak súbory obsahujú časové údaje (UTC alebo Date + Time), riadky sa po zlúčení zoradia chronologicky.

### Detekcia typu rádiového prístupu

Na základe názvov stĺpcov program automaticky rozpozná typ merania:
- **5G (NR)** -- ak hlavička obsahuje stĺpec s textom `NR-ARFCN`
- **LTE** -- ak hlavička obsahuje stĺpec s textom `EARFCN`
- **Neznámy** -- inak

Táto informácia sa zobrazuje v UI a slúži na varovanie, ak je v mobile režime vstupný súbor LTE namiesto 5G.

---

## Mapovanie stĺpcov

Používateľ mapuje logické kľúče na indexy stĺpcov z hlavičky CSV:

| Kľúč | Povinný | Typické názvy stĺpcov |
|---|---|---|
| `latitude` | áno | Latitude, Lat |
| `longitude` | áno | Longitude, Lon, Lng |
| `frequency` | áno | Frequency, NR-ARFCN, EARFCN |
| `pci` | áno | PCI |
| `mcc` | áno | MCC |
| `mnc` | áno | MNC |
| `rsrp` | áno | SSS-RSRP, RSRP, NR-SS-RSRP |
| `sinr` | nie | SSS-SINR, SINR, NR-SS-SINR |

Program sa pokúsi automaticky predvyplniť mapovanie na základe hlavičky. Používateľ môže každé pole manuálne upraviť.

Parsovanie číselných hodnôt vo všetkých stĺpcoch: orezanie medzier, nahradenie čiarky bodkou, prevod na float64.

---

## Filtre

Filtre slúžia na duplikáciu riadkov pre zdieľané frekvencie medzi operátormi. Načítavajú sa z `.txt` súborov. Program automaticky hľadá filtre v priečinkoch `filters/` a `filtre_5G/` vedľa spustiteľného súboru. Dodatočné filtre je možné pridať manuálne cez UI.

### Formát filtračného súboru

Súbor obsahuje blok `<Query>...</Query>` (alebo celý text súboru, ak tag chýba). Text sa delí na dve časti oddelené prvou bodkočiarkou:

- **Pred bodkočiarkou** -- assignments (priradenia hodnôt)
- **Za bodkočiarkou** -- conditions (podmienky)

### Assignments (priradenia)

Syntax: `"NázovStĺpca" = hodnota`

Každý assignment definuje, aké hodnoty sa majú zapísať do daného stĺpca. Jedno pole môže mať viac assignment hodnôt -- vytvoria sa všetky kombinácie (kartézsky súčin).

### Conditions (podmienky)

Podmienky sú zoskupené v zátvorkách `(...)`. Každá skupina je kombinácia AND podmienok. Viacero skupín funguje ako OR.

Typy podmienok:
- **Rovnosť:** `"Pole" = hodnota` -- presná zhoda floatov
- **Rozsah:** `"Pole" = od-do` -- interval `[od, do)` (dolná hranica vrátane, horná bez). Ak od == do, správa sa ako rovnosť. Ak od > do, hranice sa automaticky prehodia.

### Vyhodnotenie

Pre každý riadok sa nájdu všetky pravidlá, ktorých aspoň jedna skupina podmienok sedí. Z nich sa vyberie jedno pravidlo: prednosť má pravidlo s väčším počtom podmienok v najlepšej zhodenej skupine, pri zhode rozhoduje abecedný názov súboru.

Pre vybrané pravidlo sa vygenerujú všetky kombinácie assignment hodnôt. Každá kombinácia vytvorí nový riadok so zmenenými hodnotami v assignment stĺpcoch. Voliteľne sa ponechá aj originálny riadok.

### Mapovanie názvov polí z filtra na stĺpce CSV

Hľadanie stĺpca prebieha v tomto poradí: presná zhoda názvu, case-insensitive zhoda, špeciálna logika pre `Frequency` (preferuje stĺpec `SSRef`, ak existuje), fallback cez aliasy a používateľské mapovanie.

---

## Mobile režim (synchronizácia 5G NR z LTE)

Mobile režim slúži na doplnenie informácie o 5G NR pokrytí do 5G datasetu na základe časovej synchronizácie s LTE meraním. Vstupný (hlavný) CSV je 5G meranie, LTE CSV je súbor s rovnakou trasou meraný v rovnakom čase.

### Postup synchronizácie

1. **Načítanie LTE CSV** -- rovnaká logika ako pre hlavný súbor, voliteľná aplikácia filtrov.
2. **Zostrojenie časovej osi** -- pre oba datasety sa z riadkov extrahuje čas v milisekundách.
3. **Vybudovanie lookup tabuliek** -- LTE riadky sa zoskupia podľa MCC+MNC a zoradia podľa času. Pre každú skupinu sa prebuduje prefixový súčet YES a NO skóre pre efektívne dotazovanie okien.
4. **Pre každý 5G riadok** -- v LTE dátach sa nájde okno `[čas - tolerancia, čas + tolerancia]` a vyhodnotí sa skóre.
5. **Zápis výsledku** -- do stĺpca `5G NR` v 5G datasete sa zapíše `yes`, `no`, alebo zostane prázdny.

### Prevod času na milisekundy

Stratégie v poradí priority:
1. **UTC ako číslo** -- ak sú hodnoty v stĺpci UTC parsovateľné ako float, určí sa faktor: ak medián absolútnych hodnôt >= 1e11, ide o milisekundy (faktor 1), inak o sekundy (faktor 1000).
2. **UTC ako datetime string** -- skúšajú sa formáty: `YYYY-MM-DD HH:MM:SS.nnn`, `D.M.YYYY HH:MM:SS`, `DD.MM.YYYY HH:MM:SS`, RFC3339 a ďalšie.
3. **Date + Time stĺpce** -- kombinácia stĺpcov Date a Time.

Časová zóna: `Europe/Bratislava` (ak dostupná), inak lokálna.

### Normalizácia 5G NR hodnoty

Hodnoty `yes`, `true`, `1`, `y`, `t`, `a`, `ano`, `áno` sa normalizujú na `yes`. Hodnoty `no`, `false`, `0`, `n`, `f` na `no`. Všetko ostatné zostáva prázdne.

### Vyhodnotenie okna

Pre daný 5G riadok sa v LTE lookup tabuľke binárnym vyhľadávaním nájdu všetky LTE riadky v časovom okne:
- Ak okno obsahuje aspoň jedno `yes`, výsledok je `yes`.
- Inak ak obsahuje aspoň jedno `no`, výsledok je `no`.
- Inak je výsledok prázdny (žiadna zhoda).
- Ak okno obsahuje zároveň `yes` aj `no`, eviduje sa ako konflikt, ale výsledok ostáva `yes`.

Tolerancia je konfigurovateľná (predvolene 1000 ms). Záporná tolerancia sa automaticky nastaví na 0.

Hľadanie prebieha najprv podľa presnej zhody MCC+MNC. Ak 5G riadok nemá MCC/MNC, použije sa globálny LTE lookup cez všetkých operátorov.

### Povinné podmienky

- LTE súbor musí obsahovať stĺpce MCC, MNC a 5G NR (alebo alias 5GNR, NR).
- LTE súbor musí obsahovať aspoň jeden riadok s `5G NR = yes`.
- Oba súbory musia mať parsovateľný čas.

---

## Geodetická transformácia (WGS84 na S-JTSK)

Všetky GPS súradnice (WGS84, EPSG:4326) sa pred výpočtom zón transformujú do metrického súradnicového systému S-JTSK (EPSG:5514). Implementácia je celá v Go bez externých knižníc.

### Forward transformácia (WGS84 -> S-JTSK)

1. **Stupne na radiány**
2. **Geodetické na ECEF** (Earth-Centered Earth-Fixed) na elipsoide WGS84
3. **Inverzná Helmertova transformácia** (7-parametrová, position-vector konvencia) -- prevod z WGS84 geocentrických na Bessel 1841 geocentrické
4. **ECEF na geodetické** na elipsoide Bessel 1841 (iteratívny výpočet)
5. **Krovákova projekcia** -- prevod na rovinné súradnice
6. **Prehodenie osí** na EPSG:5514 konvenciu (East, North)

### Inverzná transformácia (S-JTSK -> WGS84)

Používa sa na spätný prevod stredov zón do GPS pre export:
1. Aproximácia cez inverznú Krovákovu projekciu + Helmert forward + ECEF na WGS84
2. Newton-Raphson spresňovanie (max 8 iterácií) s Jakobiánom cez konečné diferencie

### Helmertove parametre (S-JTSK to WGS 84, operácia č. 4)

- Translácie: tx=485.0 m, ty=169.5 m, tz=483.8 m
- Rotácie: rx=7.786", ry=4.398", rz=4.103" (v uhlových sekundách)
- Mierka: 0 ppm

---

## Výpočet zón a úsekov

Program podporuje tri režimy rozdelenia bodov do priestorových jednotiek:

### Režim "Úseky po trase" (segments)

Body sa rozdeľujú do úsekov po trase podľa kumulatívnej vzdialenosti. Každý úsek má dĺžku definovanú veľkosťou zóny (predvolene 100 m).

**Výpočet:**
- Kumulatívna vzdialenosť sa počíta postupne: `cumDist += hypot(x_i - x_{i-1}, y_i - y_{i-1})`
- ID úseku: `floor((cumDist + 1e-9) / veľkosťZóny)` -- epsilon 1e-9 chráni pred hraničnými chybami float aritmetiky
- Ak krok prekročí hranicu jedného alebo viac úsekov, pre každú hranicu sa interpoluje začiatok nového úseku: `P_start = P_prev + (P_curr - P_prev) * fraction`, kde `fraction = (boundary - prevCumDist) / stepDist`, clipované do [0, 1]

Kľúč zóny: `segment_<id>`. Súradnice zóny: interpolovaný začiatok úseku.

### Režim "Štvorcové zóny -- stred" (center)

Body sa mapujú do štvorcovej mriežky:
- `zónaX = floor(x / veľkosťZóny) * veľkosťZóny`
- `zónaY = floor(y / veľkosťZóny) * veľkosťZóny`

Kľúč zóny: `<zónaX>_<zónaY>`. Do exportu sa zapisujú súradnice stredu zóny (zónaX + veľkosť/2, zónaY + veľkosť/2) pretransformované späť do WGS84.

### Režim "Štvorcové zóny -- prvý bod" (original)

Rovnaká mriežková logika ako center, ale do exportu sa zapisujú pôvodné GPS súradnice z prvého nameraného bodu v zóne.

### Veľkosť zóny

Predvolená: 100 m. Ak je zadaná hodnota <= 0, automaticky sa použije 100.

---

## Agregácia a výber frekvencie

### Zoskupenie (grouping)

Riadky sa zoskupia podľa kombinácie: **zóna + operátor (MCC_MNC) + frekvencia + PCI**. Riadky s prázdnou hodnotou v ktoromkoľvek z týchto polí sa vylúčia.

Pre každú skupinu sa vypočíta:
- **RSRPAvg** = súčet RSRP / počet meraní
- **SINRAvg** = súčet SINR / počet platných SINR hodnôt (iba ak aspoň jedna existuje)
- **NRValue** = `yes` ak aspoň jedno meranie má NR=yes, inak `no` ak aspoň jedno je `no`, inak prázdne

### Výber najlepšej frekvencie

Pre každú kombináciu zóna + operátor sa vyberie jedna frekvencia + PCI. Skupiny sa zoradia:

1. RSRPAvg zostupne (lepší signál prvý)
2. Počet meraní zostupne (viac dát prvý)
3. Frekvencia numericky vzostupne (tie-break)
4. Frekvencia lexikograficky vzostupne
5. PCI numericky vzostupne
6. PCI lexikograficky vzostupne

Z takto zoradeného zoznamu sa vyberie prvá skupina, ktorá súčasne spĺňa:
- `RSRPAvg >= RSRPThreshold`
- `SINRAvg >= SINRThreshold`

Ak žiadna skupina nespĺňa obe prahy, vyberie sa úplne prvá (najlepšia podľa RSRP).

### Klasifikácia GOOD / BAD

Pre export štatistík:

**Bez SINR:**
- GOOD ak `RSRPAvg >= RSRPThreshold`

**So SINR:**
- GOOD ak `RSRPAvg >= RSRPThreshold` **a** `SINRAvg >= SINRThreshold`

**V mobile režime navyše:**
- GOOD len ak súčasne `5G NR = yes`

Hranica (>=) patrí do GOOD.

---

## Výstupný súbor zones (_zones.csv)

### Štruktúra

- Prvý riadok je prázdny
- Druhý riadok je hlavička: pôvodné stĺpce z CSV + (voliteľne 5G NR stĺpec) + `Riadky_v_zone` + `Frekvencie_v_zone`
- Nasledujúce riadky: po jednom za každú kombináciu zóna + operátor

### Formátovanie hodnôt v export riadku

| Pole | Formát |
|---|---|
| RSRP | `%.2f` (priemer) |
| SINR | `%.2f` (priemer, ak existuje) |
| Lat/Lon (segments, center) | `%.6f` (prepočítané zo stredu zóny) |
| Lat/Lon (original) | pôvodná hodnota z prvého bodu |
| 5G NR | `1` (yes), `0` (no), alebo pôvodná hodnota |
| MCC, MNC, PCI | celočíselný formát (bez `.0`) |

### Prázdne zóny

Ak je zapnutá voľba "Generovať prázdne zóny", pre každú kombináciu zóna + operátor, kde chýba meranie, sa doplní riadok:
- RSRP = -174
- 5G NR = 0
- Súradnice sa prepočítajú zo stredu príslušnej zóny
- Na konci riadku je komentár `# Prázdna zóna - automaticky vygenerovaná` (alebo `# Prázdny úsek...` pre segmenty)

### Custom operátori

Ak sú zapnuté prázdne zóny aj custom operátori, pre každého zadaného operátora (MCC:MNC alebo MCC:MNC:PCI), ktorý sa v dátach nenachádza, sa pre každú zónu doplní prázdny riadok s rovnakými hodnotami. Zároveň sa pre nich vytvorí placeholder v štatistikách.

---

## Výstupný súbor stats (_stats.csv)

### Štruktúra

Hlavička: `MNC;MCC;<GOOD stĺpec>;<BAD stĺpec>`

Názvy GOOD/BAD stĺpcov sa generujú dynamicky podľa nastavených prahov a režimu:
- Bez SINR: `RSRP >= -110` / `RSRP < -110`
- So SINR: `RSRP >= -110 a SINR >= -5` / `RSRP < -110 alebo SINR < -5`
- V mobile režime sa pridáva: `a 5G NR = yes` / `alebo 5G NR != yes`

### Výpočet

Pre každého operátora (MCC + MNC) sa spočíta počet GOOD a BAD zón podľa pravidiel klasifikácie.

Ak sú zapnuté prázdne zóny: chýbajúce zóny pre operátora sa pripočítajú k BAD. Výpočet: `missingZones = celkovýPočetZón - počtZónOperátora`, a `BAD += missingZones`.

Ak je výsledný dataset prázdny, stats súbor obsahuje len prázdny riadok.

---

## Časové úseky (Time Windows)

Voliteľná funkcia v UI umožňuje definovať časové okná, ktoré vylúčia merania z výpočtu. Používateľ zadá začiatok a koniec (dátum + čas) a program pred spracovaním vyfiltruje všetky riadky, ktorých časová značka padne do niektorého z definovaných okien.

Podporované formáty dátumu a času sú rovnaké ako pri mobile sync. Pri viacerých oknách sa riadok vylúči, ak padne do ktoréhokoľvek z nich.

---

## Predvolené hodnoty konfigurácie

| Parameter | Predvolená hodnota |
|---|---|
| Režim zón | segments (úseky po trase) |
| Veľkosť zóny / úseku | 100 m |
| RSRP hranica | -110 dBm |
| SINR hranica | -5 dB |
| Tolerancia mobile sync | 1000 ms |
| NR stĺpec | `5G NR` |
| Generovať prázdne zóny | vypnuté |
| Ponechať originálne riadky pri filtroch | vypnuté |
| Mobile režim | vypnutý |

---

## Filtračné súbory dodávané s programom

Program obsahuje pripravené filtre pre slovenských operátorov:

**Priečinok `filters/` (LTE filtre):**
- Orange (231-01) so zdieľanými frekvenciami SWAN
- Slovak Telekom (231-02) so zdieľanými frekvenciami O2
- SWAN (231-03) so zdieľanými frekvenciami Orange
- O2 (231-06) so zdieľanými frekvenciami Slovak Telekom

**Priečinok `filtre_5G/` (5G filtre):**
- Orange (231-01) -- frekvenčné rozsahy
- Slovak Telekom (231-02) -- frekvenčné rozsahy
- SWAN (231-03) -- frekvenčné rozsahy
- O2 (231-06) -- frekvenčné rozsahy

Tieto filtre zabezpečujú, že pri meraní zdieľaných frekvencií sa riadok správne zduplikuje pre oboch operátorov.

---

## Štruktúra projektu

```
100mscript/
  main.go              -- vstupný bod, konfigurácia Wails okna
  app.go               -- Wails API vrstva (file dialógy, preview, spustenie)
  wails.json           -- Wails konfigurácia projektu
  internal/backend/    -- celá výpočtová logika
    types.go           -- definície ProcessingConfig, ProcessingResult
    runner.go          -- vstupný bod RunProcessing
    runner_native.go   -- hlavný pipeline (orchestrácia krokov)
    csv_loader.go      -- načítanie CSV, detekcia kódovania, hlavička
    csv_merge.go       -- zlúčenie viacerých CSV, triedenie podľa času
    filters.go         -- parser filtračných pravidiel
    filter_apply.go    -- aplikácia filtrov na CSV dáta
    mobile_sync_native.go -- synchronizácia 5G NR z LTE
    projection_native.go  -- geodetická transformácia WGS84 <-> S-JTSK
    processing_native.go  -- výpočet zón, agregácia, výber frekvencie
    outputs_native.go     -- export _zones.csv a _stats.csv
    input_kind.go         -- detekcia 5G vs LTE
    mapping.go            -- automatické mapovanie stĺpcov
    original_rows.go      -- správa original_excel_row
    time_selector.go      -- časové okná a triedenie podľa času
    processing_events.go  -- progress eventy pre UI
  frontend/src/
    main.ts            -- celé UI (formulár, mapovanie, spustenie, výsledky)
    app.css             -- štýly
  filters/             -- LTE filtre pre slovenských operátorov
  filtre_5G/           -- 5G filtre pre slovenských operátorov
  data/                -- testovacie vstupné a výstupné dáta
  build/               -- Wails build konfigurácia
```

---

## Sumarizačné metriky (výsledok spracovania)

Po spracovaní program zobrazí:

| Metrika | Popis |
|---|---|
| Unikátne zóny | Počet rôznych zón v export dátach |
| Unikátni operátori | Počet rôznych MCC_MNC kombinácií |
| Celkový počet riadkov zón | Počet riadkov v zones výstupe |
| Pokrytie (%) | Iba pre grid režimy (nie segmenty): pomer obsadených zón k teoretickému maximu na základe bounding boxu |

Pokrytie sa počíta: `rangeX = (maxX - minX + veľkosťZóny)`, `rangeY = (maxY - minY + veľkosťZóny)`, `teoretickýMax = (rangeX / veľkosťZóny) * (rangeY / veľkosťZóny)`, `pokrytie = (unikátneZóny / teoretickýMax) * 100`.

---

## UI (používateľské rozhranie)

Aplikácia má jedno okno rozdelené na ľavý a pravý panel.

**Ľavý panel:**
- **Vstupné dáta** -- správa zoznamu CSV súborov (pridať, odstrániť, vyčistiť), automatické načítanie náhľadu po pridaní. Voľba mobile režimu s LTE súbormi. Správa filtrov (automatické + manuálne).
- **Nastavenia spracovania** -- výber režimu zón, veľkosť, prahy RSRP/SINR, voľba ponechania originálnych riadkov, generovanie prázdnych zón, custom operátori.
- **Mapovanie stĺpcov** -- dropdown pre každý povinný a voliteľný stĺpec, predvyplnený z hlavičky.
- **Časové úseky** -- definícia časových okien pre vylúčenie meraní.

**Pravý panel (sticky):**
- **Kontrola pripravenosti** -- prehľad, či sú splnené všetky podmienky pre spustenie.
- **Priebeh spracovania** -- krokový indikátor fázy pipeline s progress barom.
- **Výstupné cesty** -- voliteľné prepísanie výstupných súborov.
- **Tlačidlo Spustiť** -- spustí spracovanie.
- **Výsledky** -- po dokončení zobrazí štatistiky a odkazy na výstupné súbory.

---

## Autor

Jakub Vysocan ([jakubvysocan@gmail.com](mailto:jakubvysocan@gmail.com))
