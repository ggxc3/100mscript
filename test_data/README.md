# Testovacie dáta pre 100mscript

Tento priečinok obsahuje testovacie scenáre (CSV) pre overenie spracovania zón a štatistík.

## Štruktúra

- `scenarios/` – testovacie CSV súbory
- `test_script.sh` – legacy skript (aktuálne odkazuje na `deno run main.ts`)

## Ako spúšťať testy

Program je interaktívny, preto je najspoľahlivejšie spúšťať testy manuálne:

```bash
python3 python/main.py test_data/scenarios/test_scenarios.csv
```

Počas behu odpovedajte na otázky (režim zón/úsekov, RSRP hranica, mapovanie stĺpcov, prípadne filtre).

### Automatizované spustenie (voliteľné)

Ak potrebujete neinteraktívny beh, môžete do procesu poslať odpovede cez `printf`/`echo`, napríklad:

```bash
printf "n\n1\na\na\n" | python3 python/main.py test_data/scenarios/test_scenarios.csv
```

Vzor vyššie je len príklad – počet a poradie otázok závisí od toho, či existujú filtre a či zvolíte generovanie prázdnych zón.

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

Po spustení testu skontrolujte vytvorené súbory:
- `<nazov>_zones.csv`
- `<nazov>_stats.csv`

Zamerajte sa na:
- správne zoskupovanie podľa MCC/MNC,
- správny výber frekvencie (najvyšší priemer RSRP),
- konzistentnosť súradníc zón/úsekov podľa zvoleného režimu.

## Poznámka k `test_script.sh`

Skript momentálne používa `deno run main.ts`, čo nezodpovedá aktuálnemu Python workflow.
Ak ho chcete používať, je potrebné ho aktualizovať na `python3 python/main.py` a doplniť
odpovede na nové interaktívne otázky.
