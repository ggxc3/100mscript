# Repository Guidelines

## Project Structure & Module Organization
- Core logic lives in Go under `internal/backend/`, with Wails desktop integration in `main.go` and `app.go`. Frontend sources are in `frontend/`. Supporting docs are in `README.md` and `docs/TECHNICKA_DOKUMENTACIA_AUDIT_VYPOCTOV.md`.
- Input/output examples are in `data/` (`data/data.csv`, `data/data_zones.csv`, `data/data_stats.csv`).
- Pre-filters are loaded from `filters/` and `filtre_5G/` (one `.txt` file per operator/filter set).

## Build, Test, and Development Commands
- Install frontend dependencies: `cd frontend && npm install`.
- Run desktop app (dev): `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev`.
- Build desktop app (Windows target): `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -platform windows/amd64 -clean`.
- Compile-check / tests: `go test ./...`.
- Outputs are written next to the input file as `<input>_zones.csv` and `<input>_stats.csv`.
- Optional debug/runtime env vars (if still supported by backend): `FILTERS_DEBUG_OUTPUT=1`, `OUTPUT_SUFFIX=_dev`.

## Coding Style & Naming Conventions
- Go code follows `gofmt`; prefer small focused helpers and explicit error returns.
- Frontend TypeScript should stay simple and consistent with existing Wails frontend patterns.
- Keep Slovak user-facing strings consistent across the desktop UI.
- Prefer explicit helper functions for parsing and filtering logic; avoid hidden side effects.

## Testing Guidelines
- Run `go test ./...` before commits.
- Validate both `_zones.csv` and `_stats.csv` outputs when changing aggregation or filtering logic.

## Commit & Pull Request Guidelines
- Recent commits use descriptive, sentence-style messages (e.g., “Update data processing…”). Follow that pattern; no strict prefixes observed.
- PRs should explain behavior changes, mention any updated data outputs (`data/data_stats.csv`, `data/data_zones.csv`), and include a sample command/output snippet when relevant.

## Configuration & Runtime Notes
- Filters are auto-loaded from `filters/` and `filtre_5G/`; add new `.txt` files there to extend behavior.
- Environment variables: `FILTERS_DEBUG_OUTPUT` for filter dumps and `OUTPUT_SUFFIX` for output naming.

## Cursor Cloud specific instructions

- **System deps (pre-installed in snapshot):** `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`, `pkg-config`, `build-essential` are required for compiling Wails on Linux.
- **webkit2gtk version caveat:** Ubuntu 24.04 ships `webkit2gtk-4.1` (not 4.0). You **must** pass `-tags webkit2_41` to all Wails commands. For example:
  - Dev: `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev -tags webkit2_41`
  - Tests: `go test -tags webkit2_41 ./...`
  - Build: `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -tags webkit2_41 -platform linux/amd64 -clean`
- **Frontend build before Go compile:** The Go binary embeds `frontend/dist/` via `//go:embed`. Run `cd frontend && npm run build` before `go build` or `go test` if `frontend/dist/` is missing.
- **Display:** The VM has `DISPLAY=:1` running via Xvfb. Wails dev renders in this virtual display; use `http://localhost:34115` in Chrome to interact with the app via the browser dev server.
- **Sample data:** `data/` is gitignored. To test end-to-end through the UI, create a semicolon-delimited CSV with columns: `latitude;longitude;frequency;pci;mcc;mnc;rsrp;sinr` (and optionally `Date;Time` for time selector).
