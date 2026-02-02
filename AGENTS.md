# Repository Guidelines

## Project Structure & Module Organization
- Core logic lives in `python/main.py`. Supporting docs are in `python/README.md`, `DOKUMENTACIA.md`, and `python/zony_dokumentacia.txt`.
- Input/output examples are at the repo root (`data.csv`, `data_zones.csv`, `data_stats.csv`).
- Pre-filters are loaded from `filters/` and `filtre_5G/` (one `.txt` file per operator/filter set).
- Test fixtures live in `test_data/scenarios/`; the legacy test runner is `test_data/test_script.sh`.

## Build, Test, and Development Commands
- Install dependencies (Python): `pip install -r requirements.txt` (or `pip install -r python/requirements.txt`).
- Run locally: `python3 python/main.py path/to/input.csv` (interactive prompts for column mapping and options).
- Outputs are written next to the input file as `<input>_zones.csv` and `<input>_stats.csv`.
- Optional debug: `FILTERS_DEBUG_OUTPUT=1 python3 python/main.py …` to emit `<input>_filters.csv`; `OUTPUT_SUFFIX=_dev` appends a suffix to output filenames.

## Coding Style & Naming Conventions
- Python uses 4-space indentation, `snake_case` functions/variables, and UPPER_SNAKE_CASE constants.
- Keep Slovak user prompts consistent with existing strings in `python/main.py`.
- Prefer explicit helper functions for parsing and filtering logic; avoid hidden side effects.

## Testing Guidelines
- Tests are scenario-driven CSVs in `test_data/scenarios/` (name files like `test_*.csv`).
- `test_data/test_script.sh` currently calls `deno run main.ts` (legacy). Update it to `python3 python/main.py` before relying on it, or run scenarios manually.
- Validate both `_zones.csv` and `_stats.csv` outputs when changing aggregation or filtering.

## Commit & Pull Request Guidelines
- Recent commits use descriptive, sentence-style messages (e.g., “Update data processing…”). Follow that pattern; no strict prefixes observed.
- PRs should explain behavior changes, mention any updated data outputs (`data_stats.csv`, `data_zones.csv`), and include a sample command/output snippet when relevant.

## Configuration & Runtime Notes
- Filters are auto-loaded from `filters/` and `filtre_5G/`; add new `.txt` files there to extend behavior.
- Environment variables: `FILTERS_DEBUG_OUTPUT` for filter dumps and `OUTPUT_SUFFIX` for output naming.
