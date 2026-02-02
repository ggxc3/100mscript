# 100mscript – Python verzia

Táto aplikácia spracúva CSV súbory s meraniami mobilného signálu a agreguje ich do 100 m zón (štvorce) alebo 100 m úsekov po trase. Pre každú zónu/úsek a operátora vypočíta priemerné RSRP, vyberie „najlepšiu“ frekvenciu (podľa najvyššieho priemeru RSRP) a vytvorí štatistiky pokrytia.

## Hlavné funkcionality

- prevod GPS súradníc (WGS84) do S-JTSK (EPSG:5514) a práca v metroch,
- režimy výstupu súradníc:
  1. **Stred 100 m zóny**,
  2. **Pôvodné súradnice vzorového merania** (prvý nájdený riadok pre vybranú frekvenciu),
  3. **100 m úseky po trase** (začiatok úseku, interpolovaný, ak chýba meraný bod),
- voliteľné predfiltre z `filters/` a `filtre_5G/`,
- voliteľné generovanie prázdnych zón/úsekov (RSRP = -174),
- štatistiky pokrytia s nastaviteľnou RSRP hranicou.

## Spustenie

```bash
python3 python/main.py cesta/k/suboru.csv
```

Program je interaktívny a vyžiada si:
- potvrdenie použitia filtrov (ak existujú),
- režim zón/úsekov (1/2/3),
- hranicu RSRP pre štatistiky (predvolene -110 dBm),
- mapovanie stĺpcov podľa písmen (predvolené A–Z mapovanie).

## Vstupné CSV

- oddeľovač `;` (bodkočiarka),
- hlavička môže byť aj na inom riadku — skript sa ju pokúsi nájsť automaticky,
- minimálne stĺpce: latitude, longitude, frequency, MCC, MNC, RSRP,
- SINR stĺpec je podporovaný (ak ho mapujete, vypočíta sa priemer `sinr_avg`).

## Výstupy

- `<vstup>_zones.csv` – agregované zóny/úseky (jeden riadok na kombináciu zóna+operátor).
- `<vstup>_stats.csv` – štatistiky podľa operátorov, s názvami stĺpcov podľa zvolenej RSRP hranice.

### Poznámky k `_zones.csv`

- zachováva pôvodnú hlavičku a pridá `Riadky_v_zone;Frekvencie_v_zone`,
- na konci riadku pridáva komentár `# Meraní: X`,
- pri prázdnych zónach/úsekoch pridá poznámku o automatickom generovaní.

## Filtre

Ak existuje `filters/` alebo `filtre_5G/`, všetky `.txt` filtre sa načítajú a použijú pred spracovaním.

Správanie filtrov:
- assignmenty v `<Query>` menia hodnoty stĺpcov,
- podmienky v zátvorkách sú AND, skupiny sú OR,
- rozsahy sa zapisujú ako `start-end`,
- ak riadok vyhovuje viac filtrom, spracovanie skončí chybou,
- podľa voľby používateľa sa pôvodný riadok môže ponechať alebo nahradiť filtrovaným.

Debug:
- `FILTERS_DEBUG_OUTPUT=1` uloží `<vstup>_filters.csv`.

## Premenné prostredia

- `OUTPUT_SUFFIX` – suffix pre výstupy (napr. `_dev` → `<vstup>_dev_zones.csv`).

## Poznámky k režimu úsekov

Režim 100 m úsekov počíta kumulatívnu vzdialenosť medzi po sebe idúcimi bodmi **v poradí riadkov**. Ak body nie sú v poradí trasy, úseky budú skreslené.
