# Repository Guidelines

## Project Structure & Module Organization
- Core logic lives in `zig_backend/` (Zig backend engine). Desktop frontend lives in `electron_app/` (Electron). Supporting docs are in `README.md`, `DOKUMENTACIA.md`, and `zony_dokumentacia.txt`.
- Input/output examples are at the repo root (`data.csv`, `data_zones.csv`, `data_stats.csv`).
- Pre-filters are loaded from `filters/` and `filtre_5G/` (one `.txt` file per operator/filter set).
- Test fixtures live in `test_data/scenarios/`.

## Build, Test, and Development Commands
- Install Electron dependencies: `npm --prefix electron_app install`.
- Run locally (desktop app): `npm --prefix electron_app run dev`.
- Build backend: `cd zig_backend && zig build`.
- Validate project: `npm run check` (repo root) or `npm --prefix electron_app run check` + `zig build test`.
- Outputs are written next to the input file as `<input>_zones.csv` and `<input>_stats.csv`.

## Coding Style & Naming Conventions
- Zig backend: keep modules small and explicit, prefer deterministic output ordering.
- Electron app: CommonJS (`.cjs`) in `main/preload/backend`, plain JS in renderer, secure IPC via preload only.
- Keep Slovak user-facing strings consistent with existing UI copy.

## Testing Guidelines
- Tests are scenario-driven CSVs in `test_data/scenarios/` (name files like `test_*.csv`).
- Validate both `_zones.csv` and `_stats.csv` outputs when changing aggregation or filtering.
- Run `zig build test` after Zig backend changes.
- Run `npm --prefix electron_app run check` after Electron changes.

## Commit & Pull Request Guidelines
- Recent commits use descriptive, sentence-style messages (e.g., “Update data processing…”). Follow that pattern; no strict prefixes observed.
- PRs should explain behavior changes, mention any updated data outputs (`data_stats.csv`, `data_zones.csv`), and include a sample command/output snippet when relevant.

## Configuration & Runtime Notes
- Filters are auto-loaded from `filters/` and `filtre_5G/`; add new `.txt` files there to extend behavior.
- Zig projection runtime can use `ZIG_PROJ_DYLIB` and `ZIG_PROJ_DATA_DIR` overrides if needed.
