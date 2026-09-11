# Audit kontroly operátora a BW – 11. 9. 2026

Na dodaných vstupoch `data/2100` a ôsmich dodaných filtroch sa po opravách nenašiel žiadny rozdiel medzi exportovanými výsledkami a nezávislou kontrolou. Audit sa týka aktuálnych pravidiel: kontrolujú sa stred a dva krajné body; neprítomnosť zhodného filtra znamená `yes`.

Export bol následne zjednotený do jedného stĺpca `Operator_sedi`; audit a príklady nižšie používajú tento formát. Pravidlo `yes` pri chýbajúcej zhode filtra zostáva zachované.

## Rozsah a výsledky

- Prečítaných všetkých **566 566 5G** a **419 998 LTE** vstupných meraní.
- Nezávisle potvrdených **105 403 opráv MNC** podľa PLMN.
- **21 kompletných spracovaní**, **42 CSV**, **74 060 výsledných riadkov** a **222 180 kontrolných frekvencií**, bez rozdielu v `yes/no`.
- Priestorové režimy: úseky po trase, štvorce so stredom, štvorce s prvým bodom; veľkosť 100 m.
- BW LTE/5G v MHz: `0/0`, `0.1/0.5`, `2.5/5`, `5/5`, `10/20`, `20/10`; navyše vypnuté filtre pri `5/5`.
- Pôvod vybraných meraní overený vo **4 765 rôznych pôvodných riadkoch**. Súhlasí MCC, opravené MNC, PCI, frekvencia, RSRP a GPS.
- Zmena BW alebo vypnutie filtrov nemení počet, poradie ani hodnoty meraní; mení sa iba príznak operátora.
- Oba exporty majú prázdny prvý riadok, hlavičku na druhom riadku a jediný stĺpec `Operator_sedi`. Platí aj pre export technológie bez meraní.

Každý výsledný bod bol porovnaný s pôvodným Go filtrovacím mechanizmom, ktorý reálne vykoná všetky priradenia na kópii riadku. Druhá kontrola je samostatný Python skript s vlastným načítaním TXT/CSV a aritmetikou `Decimal`; nepoužíva produkčný parser ani porovnávač. Tento skript navyše vyhodnotil pôvodné merania s platnou frekvenciou a operátorom vo všetkých scenároch (14 254 674 logických kontrol bodov; rovnaké kombinácie vstupných hodnôt zdieľajú jeden výpočet).

## Regresné a hraničné testy

- **105 000 porovnaní** proti pôvodnému mechanizmu: 5 000 deterministicky generovaných zostáv pravidiel, sedem BW vrátane 1 Hz, desatinné hodnoty, rovnosti, rozsahy, AND/OR skupiny, priority a viacnásobné priradenia MCC/MNC.
- **4 752 kontrol hraníc všetkých ôsmich dodaných filtrov**: presná hranica a bezprostredne susedná reprezentovateľná hodnota pod a nad ňou; viac MCC/MNC.
- Všetkých osem kombinácií zhody/nezhody troch bodov osobitne pre LTE a 5G.
- Kontrola samostatného BW a filtrov LTE/5G, opravy MNC pred filtrami, nemennosti zdroja, ignorovania vnútra intervalu a najvyššieho jednotlivého RSRP.
- V prehliadači overené názvy BW, oddelené desatinné hodnoty, nula, odmietnutie záporného vstupu, prijatie prázdneho BW ako nuly a odoslanie samostatných/vypnutých filtrov. Wails most bol pri kontrole UI simulovaný; výpočty nad reálnymi súbormi bežali v Go backende.

## Nájdené a opravené chyby

1. Vlastný názov mapovaného MNC, napr. `NetworkCode`, v priradení filtra sa nemusel započítať do zmeny operátora. Filter teraz používa nezávislé mapovanie svojej technológie aj pri vlastných názvoch stĺpcov.
2. Priradenia `MNC` a `mnc` v jednom filtri sa mohli navzájom prepísať podľa poradia Go mapy. Teraz sa zachovajú všetky alternatívy; ak aspoň jedna mení operátora, výsledok je `no`.
3. Pri chýbajúcom `EARFCN` alebo `NR-ARFCN` mohol resolver použiť fyzickú frekvenciu v Hz. Teraz chýbajúci kanálový stĺpec vyvolá chybu; existujúci kanál sa porovnáva ako číslo kanála a BW ho nemení.

Tieto chyby sa nereprodukovali na dodaných ôsmich filtroch, ktoré používajú štandardné MCC/MNC a fyzickú `Frequency`. Každá má samostatný regresný test.

## Príklad z reálnych dát pri BW 5 MHz

| Technológia | MNC | Stred (MHz) | Dolný bod | Horný bod | Operator_sedi |
|---|---:|---:|---:|---:|---|
| 5G | 1 | 2120,45 | 2115,45 | 2125,45 | yes |
| 5G | 2 | 2131,25 | 2126,25 | 2136,25 | no |
| 5G | 6 | 2155,35 | 2150,35 | 2160,35 | yes |
| LTE | 1 | 2112,5 | 2107,5 | 2117,5 | yes |

Pri druhom riadku dolný bod spĺňa filter Orange (`MNC=1`), preto je celková kontrola BW `no`. LTE filtre majú odlišné podmienky od 5G filtrov; výsledky medzi technológiami nemožno odvodzovať z rovnakého frekvenčného rozsahu.

Rozsah `[2110, 2130)` obsahuje 2110 MHz a neobsahuje 2130 MHz. Pri prázdnom alebo nulovom BW kontroluje `Operator_sedi` iba stred. Pri kladnom BW zahŕňa aj oba krajné body. Druhý samostatný príznak sa neexportuje. BW znamená odchýlku na každú stranu, nie šírku delenú dvoma; nesúvisí s pôvodným LTE stĺpcom `BW`.

## Opakovanie auditu

Príkazy sú v sekcii frekvenčného režimu v [README](../README.md). Veľké testy vyžadujú dva pôvodné lokálne CSV súbory; bez explicitného `RUN_LARGE_REAL_DATA_TESTS=1` sa preskočia. Bežné regresné testy sú súčasťou `go test ./...` a CI.

Skript `scripts/audit_frequency_outputs.py` zlyhá pri prvom rozdiele. JSON protokol obsahuje počty podľa scenára, vysvetlenia všetkých unikátnych kombinácií frekvencie/operátora/BW a SHA-256 použitých filtrov. Audit overuje dodané dáta a opísané scenáre; bez konkrétneho problematického riadku a jeho vlastných filtrov nemožno potvrdiť nastavenia iného používateľského spracovania.
