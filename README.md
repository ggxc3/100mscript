# CSV Zónový Analyzátor (100mscript)

Tento projekt spracováva CSV súbory s meraniami mobilného signálu. Dáta prepočíta do 100 m zón (štvorce) alebo 100 m úsekov po trase a pre každú zónu/úsek + operátora vypočíta priemerné RSRP, vyberie najlepšiu frekvenciu a vytvorí štatistiky pokrytia.

## Požiadavky

- Python 3.9+
- Knižnice: `pandas`, `numpy`, `pyproj`, `tqdm` (viď `requirements.txt` alebo `python/requirements.txt`)

Inštalácia:

```bash
pip install -r requirements.txt
```

## Spustenie

```bash
python3 python/main.py cesta/k/suboru.csv
```

Program je interaktívny a postupne sa pýta na:
- použitie filtrov (ak existuje `filters/` alebo `filtre_5G/`),
- režim zón/úsekov (stred zóny, pôvodné súradnice, alebo 100 m úseky),
- hranicu RSRP pre štatistiky (predvolene -110 dBm),
- mapovanie stĺpcov (predvolené písmená alebo vlastné).

## Vstupné dáta

- CSV musí byť oddelené bodkočiarkou `;`.
- Hlavička nemusí byť na prvom riadku — skript sa ju pokúsi automaticky nájsť.
- Očakávané stĺpce: latitude, longitude, frequency, MCC, MNC, RSRP (voliteľne SINR).

## Výstupy

Skript vytvorí dva súbory vedľa vstupu:
- `<vstup>_zones.csv` — jedna zóna/úsek na riadok.
- `<vstup>_stats.csv` — štatistiky pokrytia pre každého operátora.

Poznámky k výstupu zón:
- zachováva pôvodnú hlavičku a pridá stĺpce `Riadky_v_zone;Frekvencie_v_zone`,
- na konci riadku pridá komentár `# Meraní: X`,
- pri prázdnych zónach/úsekoch použije RSRP `-174` a poznámku o automatickom generovaní.

## Filtre

Ak existujú priečinky `filters/` alebo `filtre_5G/`, všetky `.txt` filtre sa načítajú a aplikujú pred spracovaním zón. Filtre môžu prepísať hodnoty stĺpcov, prípadne duplikovať riadky pre viaceré kombinácie. Ak jeden riadok vyhovuje viac filtrom, spracovanie sa ukončí chybou.

Voliteľné premenné prostredia:
- `FILTERS_DEBUG_OUTPUT=1` vytvorí `<vstup>_filters.csv` po filtrovaní,
- `OUTPUT_SUFFIX=_nieco` pridá suffix k výstupom (napr. `_dev_zones.csv`).

## Testovanie

Testovacie scenáre sú v `test_data/scenarios/`. Manuálne spustenie:

```bash
python3 python/main.py test_data/scenarios/test_scenarios.csv
```

Súbor `test_data/test_script.sh` je legacy (používa `deno run main.ts`) a neodráža aktuálny Python workflow.
