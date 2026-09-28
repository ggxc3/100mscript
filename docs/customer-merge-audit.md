# Spájanie štyroch častí trasy – 28. 9. 2026

## Potvrdená príčina

Problém sa reprodukoval na ôsmich CSV z `data/new_bad_merge`: štyri LTE a štyri 5G NR, samostatné spojenie pre každú technológiu. Ide o časti lip_po, po-pp, pp_sl a sl_lip, spolu 1 962 765 vstupných riadkov (~593 MiB).

Všetky súbory sa načítali. Chyba vznikala pri geometrickom zarovnaní trás: krátke stretnutie pri križovatke poskytlo dostatok blízkych GPS bodov na odhad spoločnej vzdialenosti, ale algoritmus neoveril polohu zvyšku údajne spoločného úseku. Rôzne cesty tak dostali rovnaké ID úsekov. Následný správny výber maxima RSRP v každej skupine potlačil merania z nesprávne zlúčených častí.

Približne 190 km trasy sa tak natlačilo do rozsahu asi 79 km. Body v jednom údajnom 100 m úseku mali pred opravou ohraničujúci obdĺžnik s uhlopriečkou až 56,2 km. Nešlo o timeout ani chýbajúce hlavičky; predchádzajúca optimalizácia riešila iný problém.

## Oprava

Každé navrhované prekrytie sa overuje pozdĺž spoločnej vzdialenosti pomocou interpolovaných GPS bodov. Aspoň 90 % porovnaní musí byť do 150 m a žiadne nad 300 m. Tolerancie vychádzajú z existujúcej tolerancie zarovnania 75 m. Horný limit zabraňuje tomu, aby dlhá spoločná časť zakryla krátku, výrazne odlišnú odbočku.

Nové priradenie musí byť geometricky zlučiteľné so všetkými už priradenými trasami, aj pri napájaní cez koncový bod. Kontrola používa najviac 2 000 vzoriek na dvojicu trás. Oprava je v spoločnom výpočte úsekov, teda platí aj pre pôvodný režim. Skutočné prekrytie a opačný smer jazdy zostávajú podporované.

## Výsledky

Nastavenie auditu: úseky 100 m, bez časových výrezov, filtre vypnuté, BW 0; frekvencia LTE `Frequency`, 5G `SSRef`. Počty sú výsledné dátové riadky, bez prázdneho riadka a hlavičky. Porovnanie s commitom `3ddb1af`.

| Výsledok | Pred opravou | Po oprave |
|---|---:|---:|
| LTE riadky | 777 | 1 852 |
| LTE obsadené úseky | 773 | 1 847 |
| 5G riadky | 3 146 | 7 387 |
| 5G obsadené úseky | 791 | 1 877 |
| Najväčšia uhlopriečka obálky meraní jedného LTE úseku | 56 177,5 m | 99,5 m |
| Najväčšia uhlopriečka obálky meraní jedného 5G úseku | 56 176,4 m | 99,3 m |

Každý zo štyroch vstupov je po oprave zastúpený vo výsledku:

| Časť trasy | LTE riadky | 5G riadky |
|---|---:|---:|
| lip_po | 312 | 1 248 |
| po-pp | 743 | 3 036 |
| pp_sl | 448 | 1 795 |
| sl_lip | 349 | 1 308 |

Samostatná kontrola pôvodných CSV v Pythone potvrdila zhodu počtov platných meraní podľa operátora a frekvencie so súčtom `Pocet_merani` v exportoch: 621 938 LTE a 763 111 5G meraní. Overila aj pôvodný súbor, číslo riadku, GPS, PCI, MCC, opravené MNC, RSRP a frekvenciu všetkých 9 239 výsledných riadkov. Menej platných než vstupných riadkov je očakávané pri chýbajúcom RSRP alebo kľúčoch skupiny. Pri 5G sa vykonalo 207 204 opráv MNC podľa PLMN.

Spracovanie a export pri jednom meraní trvalo približne 1,93 s pre LTE a 3,97 s pre 5G. Časy sú orientačné pre vývojový počítač, nie garancia výkonu na inom zariadení.

## Regresné overenie

- Nový test zákazníckych dát preukázateľne zlyhal na pôvodnom kóde a prešiel po oprave. Kontroluje geografický rozsah každého úseku, zastúpenie každej časti a počet úsekov; výstup verejného `RunProcessing` porovnáva s celým exportom kontrolovaného výpočtu.
- Syntetické testy: spoločných 100 m s následnou odbočkou, dlhé prekrytie s krátkym odlišným koncom, kolmé pokračovanie a uzavretý okruh v 24 poradiach súborov aj s obráteným smerom jednej časti.
- Pôvodné dáta `2100`, celé aj rozdelené na 11 súborov, vo všetkých troch priestorových režimoch prešli existujúcimi kontrolami. Výsledné kontrolné súčty skupín sa oproti predchádzajúcemu auditu nezmenili.

Lokálne opravené exporty a JSON protokoly sú v `data/new_bad_merge/audit/`, porovnávacie staré výsledky v `audit_before_fix/`. Zákaznícke dáta ani exporty nie sú súčasťou Gitu.

```sh
RUN_LARGE_REAL_DATA_TESTS=1 CUSTOMER_MERGE_AUDIT_DIR="$PWD/data/new_bad_merge/audit" \
  go test ./internal/backend -run '^TestFrequencyMode_RealCustomerMerge$' -count=1 -v -timeout 10m
RUN_LARGE_REAL_DATA_TESTS=1 \
  go test ./internal/backend -run '^TestFrequencyMode_Real2100SplitFiles$' -count=1 -v -timeout 10m
```

Prvý test vyžaduje lokálne zákaznícke súbory v `data/new_bad_merge`; cestu možno zmeniť cez `CUSTOMER_MERGE_INPUT_DIR`. Bez `RUN_LARGE_REAL_DATA_TESTS=1` sa veľké testy preskočia; syntetické testy bežia aj v CI.
