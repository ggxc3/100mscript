# Repository Guidelines

## Project Structure & Module Organization
- Core logic lives in Go under `internal/backend/`, with Wails desktop integration in `main.go` and `app.go`. Frontend sources are in `frontend/`. Supporting docs are in `README.md`, `DOKUMENTACIA.md`, and `zony_dokumentacia.txt`.
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
