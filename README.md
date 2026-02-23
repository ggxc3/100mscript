# 100mscript (Go + Wails)

Desktop aplikácia na spracovanie CSV meraní mobilného signálu do zón/úsekov a generovanie výstupov `_zones.csv` a `_stats.csv`.

Projekt je po migrácii `Go-only`:
- backend: Go (natívny runtime, bez Python bridge),
- desktop shell: Wails,
- cieľový build: Windows.

## Spustenie (vývoj)

Požiadavky:
- Go 1.22+
- Node.js 20+
- Wails CLI (voliteľné, dá sa použiť aj `go run .../wails`)

Frontend:

```bash
cd frontend
npm install
npm run build
```

Desktop app (Wails):

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev
```

Windows build:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -platform windows/amd64 -clean
```

## Dáta a filtre

- Vstupné CSV súbory sú oddelené bodkočiarkou `;`.
- Filtre sa načítavajú z `filters/` a `filtre_5G/` (`.txt` súbory).
- Výstupy sa zapisujú vedľa vstupného súboru ako `<input>_zones.csv` a `<input>_stats.csv`.

## Poznámka

Historické Python implementácie, parity testy a migration pomocné skripty boli z repozitára odstránené po dokončení natívneho Go portu.
