# Testovacie dáta pre 100mscript

Tento priečinok obsahuje testovacie dáta a scenáre pre overenie funkčnosti programu na spracovanie a združovanie meraní do zón.

## Štruktúra priečinkov

- `scenarios/` - obsahuje rôzne testovacie scenáre vo formáte CSV

## Testovacie scenáre

Priečinok `scenarios/` obsahuje nasledujúce testovacie súbory:

### 1. test_scenarios.csv
Základný testovací súbor s rôznymi hodnotami MNC (stĺpec N) a rovnakými hodnotami MCC (stĺpec M).
Tento test overuje správne zoskupovanie do zón podľa MNC.

**Očakávaný výsledok:** Tri zóny, každá s unikátnou hodnotou MNC.

### 2. test_mcc.csv
Testovací súbor s rôznymi hodnotami MCC (stĺpec M) a rovnakými hodnotami MNC (stĺpec N).
Tento test overuje, či program správne zohľadňuje MCC pri vytváraní zón.

**Očakávaný výsledok:** 6 zón, pretože aj keď MNC je rovnaké, MCC sa líši.

### 3. test_nearby.csv
Testovací súbor s geograficky blízkymi bodmi, ktoré majú rovnaké MNC a MCC.
Tento test overuje, či program správne združuje geograficky blízke body s rovnakým MNC a MCC do jednej zóny.

**Očakávaný výsledok:** Dve zóny - jedna pre body s MNC=1, MCC=231 a druhá pre body s MNC=2, MCC=231.

### 4. test_nearby_diff_mcc.csv
Testovací súbor s geograficky blízkymi bodmi, ktoré majú rovnaké MNC, ale rôzne MCC.
Tento test overuje, či program správne oddeľuje body s rôznymi MCC, aj keď sú geograficky blízko.

**Očakávaný výsledok:** Dve zóny - jedna pre body s MCC=231 a druhá pre body s MCC=232.

## Ako spúšťať testy

Testy môžete spúšťať pomocou príkazu:

```bash
deno run --allow-read --allow-write main.ts test_data/scenarios/[nazov_suboru].csv
```

Napríklad:

```bash
deno run --allow-read --allow-write main.ts test_data/scenarios/test_scenarios.csv
```

Pre každý test bude vygenerovaný výstupný súbor s príponou `_zones.csv` v rovnakom adresári ako vstupný súbor.

### Spustenie všetkých testov naraz

Všetky testy môžete spustiť naraz pomocou testovacieho skriptu:

```bash
./test_data/test_script.sh
```

Tento skript automaticky spustí všetky testy definované v skripte a zobrazí výsledky.

## Overenie výsledkov

Po spustení testu skontrolujte vygenerovaný súbor `[nazov_suboru]_zones.csv` a overte, či:

1. Body sú správne zoskupené do zón podľa geografickej blízkosti
2. Body sú separované do rôznych zón podľa MNC a MCC
3. Priemerné hodnoty RSRP a najčastejšie frekvencie sú správne vypočítané

## Pridávanie ďalších testov

Nové testovacie scenáre môžete vytvoriť podobne ako existujúce, s upravenými hodnotami zemepisnej šírky/dĺžky, MNC, MCC, RSRP a frekvencie podľa potreby.

Pri vytváraní testov myslite na:
- Geografickú blízkosť bodov (na testovanie zoskupovania)
- Rôzne kombinácie MNC a MCC (na testovanie separácie)
- Rôzne hodnoty RSRP a frekvencie (na testovanie výpočtu priemerov)

## Inštrukcie pre chatbota

Ak používate AI chatbota na testovanie aplikácie, nižšie sú inštrukcie pre kompletnú testovaciu procedúru.

### Postup testovania pre chatbota

1. **Príprava**:
   - Skontrolujte existenciu testovacích súborov v priečinku `test_data/scenarios/`
   - Ak nie sú k dispozícii, vytvorte ich podľa špecifikácie v tomto README

2. **Spustenie testov**:
   - Spustite bash skript `./test_data/test_script.sh` pre automatické vykonanie všetkých testov
   - Ubezpečte sa, že skript má nastavené práva na spustenie (`chmod +x test_data/test_script.sh`)
   - Skript používa `echo "a"` na automatickú odpoveď pre použitie predvolených hodnôt stĺpcov

3. **Kontrola výsledkov**:
   - Po každom teste sa vytvorí výstupný súbor s príponou `_zones.csv`
   - Skontrolujte obsah každého výstupného súboru pomocou príkazu `cat test_data/scenarios/[nazov_suboru]_zones.csv`
   - Overte nasledujúce aspekty v každom výstupnom súbore:

     a. **test_scenarios.csv_zones.csv**:
     - Mali by existovať presne 3 zóny (3 riadky) s rôznymi hodnotami MNC (1, 2, 3)
     - Všetky by mali mať rovnaké MCC (231)
     
     b. **test_mcc.csv_zones.csv**:
     - Každý bod by mal byť v samostatnej zóne (alebo zoskupený s bodmi s rovnakým MCC a MNC)
     - Zóny by mali byť zoradené najprv podľa MCC, potom podľa MNC
     
     c. **test_nearby.csv_zones.csv**:
     - Malo by tam byť len 2 zóny: jedna pre MNC=1, druhá pre MNC=2
     - Všetkých 5 meraní s MNC=1 by malo byť zoskupených do jednej zóny
     
     d. **test_nearby_diff_mcc.csv_zones.csv**:
     - Mali by byť vytvorené 2 zóny: jedna pre MCC=231 a jedna pre MCC=232
     - Každá zóna by mala obsahovať len merania s príslušným MCC

4. **Vyhodnotenie**:
   - Porovnajte skutočné výsledky s očakávanými výsledkami uvedenými v tomto README
   - Ak sa zistia rozdiely, analyzujte príčinu (môže ísť o chybu v kóde alebo nesprávne očakávania)

5. **Reportovanie**:
   - Poskytne súhrnný výsledok testovania
   - Pre každý test uvedte, či prešiel alebo zlyhal, a prípadne detaily o zistených problémoch 