# Zig Backend (Migration Scaffold)

Tento priečinok obsahuje nový backend engine v Zigu pre desktop aplikáciu.

## Stav

- `desktop_app.py` už vie volať Zig backend cez `desktop_backend_adapter.py`
- portované moduly: JSON protokol, config parser, CSV header probe, filter parser/loading
- pri behu vracia `NOT_IMPLEMENTED`, desktop app v `DESKTOP_BACKEND=auto` režime fallbackne na Python backend

## Build

```bash
cd zig_backend
zig build
```

Ak Zig nemá prístup do `~/.cache/zig` (sandbox), použite lokálny cache:

```bash
ZIG_GLOBAL_CACHE_DIR=.zig-global-cache zig build
```

Výstup:

- `zig_backend/zig-out/bin/100mscript_engine` (Linux/macOS)
- `zig_backend/zig-out/bin/100mscript_engine.exe` (Windows)

## Protokol (interný)

Spustenie:

```bash
100mscript_engine run --config /path/to/config.json
```

Stdout používa newline-delimited JSON eventy:

- `{"type":"status","message":"..."}`
- `{"type":"result", ...}` (bude pridané pri implementácii jadra)
- `{"type":"error","code":"...","message":"..."}`

Desktop app používa:

- `DESKTOP_BACKEND=auto|python|zig`
- `ZIG_BACKEND_BIN=/custom/path/to/100mscript_engine`
