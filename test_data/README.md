# Testovacie dáta pre 100mscript

Tento priečinok obsahuje testovacie scenáre (CSV) pre overenie spracovania zón a štatistík.

## Štruktúra

- `scenarios/` – testovacie CSV súbory

## Ako spúšťať testy

Testovacie CSV scenáre slúžia ako fixture dáta pre overovanie Zig backendu a desktop appky.

```bash
cd /Users/jakubvysocan/Documents/Personal/Jakub/ostatne/100mscript/zig_backend
zig build test
```

Desktop smoke / UI test (manuálne):

```bash
cd /Users/jakubvysocan/Documents/Personal/Jakub/ostatne/100mscript
npm --prefix electron_app run dev
```

## Testovacie scenáre

### 1. `test_scenarios.csv`
Základný test so zoskupovaním podľa MNC a výberom frekvencie podľa najvyššieho priemeru RSRP.

### 2. `test_mcc.csv`
Overuje, že MCC ovplyvňuje rozdelenie zón (rovnaké MNC, rôzne MCC).

### 3. `test_nearby.csv`
Blízke body s rovnakým MCC/MNC by mali skončiť v jednej zóne.

### 4. `test_nearby_diff_mcc.csv`
Blízke body s rôznym MCC musia byť v oddelených zónach.

### 5. `test_frequency_selection.csv`
Overuje výber frekvencie s najvyšším priemerom RSRP v rámci zóny+operátora.

## Overenie výsledkov

Po spustení spracovania skontrolujte vytvorené súbory:
- `<nazov>_zones.csv`
- `<nazov>_stats.csv`

Zamerajte sa na:
- správne zoskupovanie podľa MCC/MNC,
- správny výber frekvencie (najvyšší priemer RSRP),
- konzistentnosť súradníc zón/úsekov podľa zvoleného režimu.
