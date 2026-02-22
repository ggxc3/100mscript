# Electron Frontend (Production Direction)

Electron desktop UI pre `100mscript` nad Zig backendom.

## Architektúra

- `src/renderer/*` - UI (bez priameho Node prístupu)
- `src/preload.cjs` - bezpečný bridge (`contextBridge`)
- `src/main.cjs` - Electron main process + IPC
- `src/backend/zigClient.cjs` - spúšťanie Zig backendu a NDJSON stream
- `src/backend/uiTools.cjs` - lokálne UI helpery (auto-filtre, path helpery)

## Zig integrácia

- hlavné spracovanie: `100mscript_engine run --config <json>`
- auto-detekcia stĺpcov: `100mscript_engine inspect --csv <path>`
- priebehové eventy: NDJSON (`status`, `error`, `result`)

## Packaging

`npm run prepare:runtime` pripraví staging adresár `build_runtime/` s:
- `zig_backend/bin/*`
- `zig_backend/vendor/proj/*`
- `filters/`
- `filtre_5G/`

`electron-builder` potom kopíruje staging do packaged app resources:
- `resources/100mscript_runtime/`

## Príkazy

```bash
npm install
npm run check
npm run dev
npm run dist:win
```
