# 100mscript (Electron + Zig)

Desktop aplikácia na spracovanie CSV meraní mobilného signálu do zón/úsekov a výpočet štatistík pokrytia.

Aktuálna architektúra:
- `Electron` frontend (`electron_app/`)
- `Zig` backend engine (`zig_backend/`)
- presná projekcia cez natívny `libproj` (bez Python runtime)

Python desktop/backend bol z projektu odstránený. Projekt je teraz runtime orientovaný na `Electron + Zig`.

## Funkcionalita

- spracovanie CSV s automatickým nájdením hlavičky
- auto-detekcia mapovania stĺpcov (beží cez Zig `inspect --csv`)
- filtre z `filters/` a `filtre_5G/`
- mobile sync (LTE -> 5G NR)
- režimy:
  - štvorcové zóny (stred)
  - štvorcové zóny (prvý bod v zóne)
  - úseky po trase
- výstupy:
  - `_zones.csv`
  - `_stats.csv`

## Vývojové spustenie

Požiadavky:
- `Zig` (0.15.x)
- `Node.js` + `npm`

Inštalácia Electron dependencies:

```bash
npm --prefix electron_app install
```

Build backendu:

```bash
cd /Users/jakubvysocan/Documents/Personal/Jakub/ostatne/100mscript/zig_backend
zig build
```

Spustenie desktop appky:

```bash
cd /Users/jakubvysocan/Documents/Personal/Jakub/ostatne/100mscript
npm --prefix electron_app run dev
```

Alternatívne cez root skript:

```bash
npm run dev
```

## Build / Check

Syntax check Electron vrstvy + Zig unit testy:

```bash
npm run check
```

Len Zig build:

```bash
npm run build:zig
```

## Windows Packaging (Electron + Zig)

Electron build bundluje:
- Zig backend binary
- PROJ runtime assety (`zig_backend/vendor/proj`)
- filtre (`filters/`, `filtre_5G/`)

Packaging príkaz:

```bash
npm --prefix electron_app run dist:win
```

Runtime assety sa stagingujú do `electron_app/build_runtime/` a do packaged app sa kopírujú do:
- `resources/100mscript_runtime/`

## GitHub Actions

Workflow `.github/workflows/build-exe.yml` je Windows-first pipeline pre:
- Zig build + test
- Electron checks
- Electron Windows packaging (NSIS)

## Poznámky k dátam

- `filters/` a `filtre_5G/` zostávajú súčasťou runtime projektu.
- `test_data/` obsahuje CSV scenáre/fixture dáta.
