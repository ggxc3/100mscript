#!/usr/bin/env python3
# -*- coding: utf-8 -*-

from __future__ import annotations

import queue
import threading
import tkinter as tk
import traceback
from datetime import datetime
from pathlib import Path
from tkinter import filedialog, messagebox, ttk

from app_backend import ProcessingConfig, run_processing
from filters import discover_filter_paths
from prompts import (
    DEFAULT_COLUMN_LETTERS,
    col_letter_to_name,
    parse_custom_operators_text,
    suggest_column_letters_from_file,
)


ZONE_MODES = {
    "Štvorcové zóny (stred)": "center",
    "Štvorcové zóny (prvý bod v zóne)": "original",
    "Úseky po trase": "segments",
}

DEFAULT_COLUMNS = dict(DEFAULT_COLUMN_LETTERS)


class DesktopApp:
    def __init__(self, root: tk.Tk):
        self.root = root
        self.root.title("100mscript Desktop")
        self.root.geometry("1180x820")
        self.root.minsize(1040, 740)

        self.colors = {
            "bg": "#f4f6fb",
            "surface": "#ffffff",
            "surface_alt": "#eaf0f8",
            "header": "#0f172a",
            "border": "#d6dfeb",
            "text": "#1a2438",
            "muted": "#5d6b82",
            "accent": "#0b7a75",
            "accent_active": "#095f5b",
            "accent_soft": "#d8f2f0",
            "success": "#1f8a5b",
            "warning": "#b86a00",
            "danger": "#b42318",
            "log_bg": "#0b1220",
            "log_fg": "#d8e1ff",
        }
        self.fonts = {
            "title": ("Trebuchet MS", 22, "bold"),
            "subtitle": ("Trebuchet MS", 10),
            "section": ("Trebuchet MS", 11, "bold"),
            "body": ("Verdana", 10),
            "small": ("Verdana", 9),
            "button": ("Trebuchet MS", 10, "bold"),
            "log": ("Consolas", 10),
        }

        self.queue = queue.Queue()
        self.running = False

        self.status_var = tk.StringVar(value="Pripravené")
        self.status_chip_var = tk.StringVar(value="READY")

        self.root.configure(bg=self.colors["bg"])
        self._configure_ttk_styles()
        self._build_ui()
        self.root.after(120, self._process_queue)

    def _configure_ttk_styles(self):
        style = ttk.Style()
        style.theme_use("clam")

        style.configure(
            "App.TCombobox",
            fieldbackground=self.colors["surface"],
            background=self.colors["surface"],
            foreground=self.colors["text"],
            bordercolor=self.colors["border"],
            lightcolor=self.colors["border"],
            darkcolor=self.colors["border"],
            arrowcolor=self.colors["text"],
            padding=(8, 6),
        )
        style.map(
            "App.TCombobox",
            fieldbackground=[("readonly", self.colors["surface"]), ("disabled", self.colors["surface_alt"])],
            foreground=[("disabled", self.colors["muted"])],
        )

        style.configure(
            "App.Horizontal.TProgressbar",
            troughcolor=self.colors["surface_alt"],
            background=self.colors["accent"],
            bordercolor=self.colors["surface_alt"],
            lightcolor=self.colors["accent"],
            darkcolor=self.colors["accent"],
        )

        style.configure(
            "AppPrimary.TButton",
            font=self.fonts["button"],
            background=self.colors["accent"],
            foreground="#ffffff",
            bordercolor=self.colors["accent"],
            darkcolor=self.colors["accent"],
            lightcolor=self.colors["accent"],
            relief="flat",
            padding=(14, 10),
        )
        style.map(
            "AppPrimary.TButton",
            background=[
                ("pressed", self.colors["accent_active"]),
                ("active", self.colors["accent_active"]),
                ("disabled", "#a6b7b5"),
            ],
            foreground=[("disabled", "#eef2f7")],
        )

        style.configure(
            "AppSecondary.TButton",
            font=self.fonts["button"],
            background=self.colors["surface_alt"],
            foreground=self.colors["text"],
            bordercolor=self.colors["border"],
            darkcolor=self.colors["surface_alt"],
            lightcolor=self.colors["surface_alt"],
            relief="flat",
            padding=(12, 9),
        )
        style.map(
            "AppSecondary.TButton",
            background=[
                ("pressed", "#dce5f2"),
                ("active", "#dce5f2"),
                ("disabled", "#eef2f7"),
            ],
            foreground=[("disabled", "#98a5bc")],
        )

        style.configure(
            "AppDanger.TButton",
            font=self.fonts["button"],
            background="#f8d9d6",
            foreground="#8f241b",
            bordercolor="#f1b8b2",
            darkcolor="#f8d9d6",
            lightcolor="#f8d9d6",
            relief="flat",
            padding=(12, 9),
        )
        style.map(
            "AppDanger.TButton",
            background=[
                ("pressed", "#f3c3be"),
                ("active", "#f3c3be"),
                ("disabled", "#f3e5e3"),
            ],
            foreground=[("disabled", "#bc8d88")],
        )

    def _make_card(self, parent):
        card = tk.Frame(
            parent,
            bg=self.colors["surface"],
            highlightthickness=1,
            highlightbackground=self.colors["border"],
            bd=0,
            padx=16,
            pady=14,
        )
        return card

    def _label(self, parent, text, kind="body"):
        fg = self.colors["text"] if kind != "muted" else self.colors["muted"]
        font = self.fonts["body"] if kind not in ("section", "muted") else self.fonts["section"] if kind == "section" else self.fonts["small"]
        return tk.Label(parent, text=text, bg=parent["bg"], fg=fg, font=font)

    def _make_entry(self, parent, text_var, width=None):
        entry = tk.Entry(
            parent,
            textvariable=text_var,
            font=self.fonts["body"],
            bg=self.colors["surface"],
            fg=self.colors["text"],
            insertbackground=self.colors["text"],
            relief="flat",
            highlightthickness=1,
            highlightbackground=self.colors["border"],
            highlightcolor=self.colors["accent"],
            bd=0,
        )
        if width:
            entry.configure(width=width)
        return entry

    def _make_button(self, parent, text, command, variant="secondary"):
        if variant == "primary":
            style = "AppPrimary.TButton"
        elif variant == "danger":
            style = "AppDanger.TButton"
        else:
            style = "AppSecondary.TButton"
        return ttk.Button(parent, text=text, command=command, style=style)

    def _make_checkbutton(self, parent, text, var, command=None):
        return tk.Checkbutton(
            parent,
            text=text,
            variable=var,
            command=command,
            font=self.fonts["body"],
            bg=parent["bg"],
            fg=self.colors["text"],
            activebackground=parent["bg"],
            activeforeground=self.colors["text"],
            selectcolor=self.colors["surface"],
            highlightthickness=0,
            bd=0,
            anchor="w",
        )

    def _build_ui(self):
        wrapper = tk.Frame(self.root, bg=self.colors["bg"], padx=18, pady=16)
        wrapper.pack(fill="both", expand=True)

        self._build_header(wrapper)

        body = tk.Frame(wrapper, bg=self.colors["bg"])
        body.pack(fill="both", expand=True, pady=(12, 0))
        body.grid_columnconfigure(0, weight=3, minsize=680)
        body.grid_columnconfigure(1, weight=2, minsize=380)
        body.grid_rowconfigure(0, weight=1)

        left_wrap = tk.Frame(body, bg=self.colors["bg"])
        left_wrap.grid(row=0, column=0, sticky="nsew", padx=(0, 10))
        left_wrap.grid_rowconfigure(0, weight=1)
        left_wrap.grid_columnconfigure(0, weight=1)

        left_canvas = tk.Canvas(
            left_wrap,
            bg=self.colors["bg"],
            highlightthickness=0,
            bd=0,
            relief="flat",
        )
        left_scrollbar = ttk.Scrollbar(left_wrap, orient="vertical", command=left_canvas.yview)
        left_canvas.configure(yscrollcommand=left_scrollbar.set)

        left_canvas.grid(row=0, column=0, sticky="nsew")
        left_scrollbar.grid(row=0, column=1, sticky="ns")

        left = tk.Frame(left_canvas, bg=self.colors["bg"])
        left_window = left_canvas.create_window((0, 0), window=left, anchor="nw")

        left.bind(
            "<Configure>",
            lambda _e: left_canvas.configure(scrollregion=left_canvas.bbox("all")),
        )
        left_canvas.bind(
            "<Configure>",
            lambda e: left_canvas.itemconfigure(left_window, width=e.width),
        )
        self.left_scroll_container = left_wrap
        self.left_scroll_canvas = left_canvas
        self.root.bind_all("<MouseWheel>", self._on_left_panel_mousewheel, add="+")
        self.root.bind_all("<Button-4>", self._on_left_panel_mousewheel, add="+")
        self.root.bind_all("<Button-5>", self._on_left_panel_mousewheel, add="+")

        right = tk.Frame(body, bg=self.colors["bg"])
        right.grid(row=0, column=1, sticky="nsew")

        self._build_input_card(left)
        self._build_options_card(left)
        self._build_activity_card(right)

    def _is_descendant_of(self, widget: tk.Widget | None, ancestor: tk.Widget | None) -> bool:
        while widget is not None:
            if widget == ancestor:
                return True
            widget = widget.master
        return False

    def _on_left_panel_mousewheel(self, event):
        container = getattr(self, "left_scroll_container", None)
        canvas = getattr(self, "left_scroll_canvas", None)
        if container is None or canvas is None:
            return

        pointer_widget = self.root.winfo_containing(event.x_root, event.y_root)
        if not self._is_descendant_of(pointer_widget, container):
            return

        if getattr(event, "num", None) == 4:
            step = -1
        elif getattr(event, "num", None) == 5:
            step = 1
        else:
            delta = int(getattr(event, "delta", 0))
            if delta == 0:
                return
            step = -1 if delta > 0 else 1

        canvas.yview_scroll(step, "units")
        return "break"

    def _build_header(self, parent):
        header = tk.Frame(parent, bg=self.colors["header"], padx=20, pady=16)
        header.pack(fill="x")

        title_col = tk.Frame(header, bg=self.colors["header"])
        title_col.pack(side="left", fill="x", expand=True)

        tk.Label(
            title_col,
            text="100mscript Desktop",
            bg=self.colors["header"],
            fg="#ffffff",
            font=self.fonts["title"],
            anchor="w",
        ).pack(anchor="w")

        tk.Label(
            title_col,
            text="CSV spracovanie, filtre a štatistiky v modernej desktop aplikácii",
            bg=self.colors["header"],
            fg="#c7d2fe",
            font=self.fonts["subtitle"],
            anchor="w",
        ).pack(anchor="w", pady=(2, 0))

        chip_wrap = tk.Frame(header, bg=self.colors["header"])
        chip_wrap.pack(side="right", anchor="e")
        self.status_chip = tk.Label(
            chip_wrap,
            textvariable=self.status_chip_var,
            bg=self.colors["warning"],
            fg="#ffffff",
            font=self.fonts["button"],
            padx=14,
            pady=8,
        )
        self.status_chip.pack(anchor="e")

    def _build_input_card(self, parent):
        card = self._make_card(parent)
        card.pack(fill="x")

        self._label(card, "Vstupné dáta", kind="section").grid(row=0, column=0, columnspan=3, sticky="w")
        self._label(card, "CSV súbor", kind="muted").grid(row=1, column=0, sticky="w", pady=(10, 4))

        self.csv_path_var = tk.StringVar()
        self._make_entry(card, self.csv_path_var).grid(row=2, column=0, columnspan=2, sticky="ew")
        self._make_button(card, "Vybrať CSV", self._pick_csv).grid(row=2, column=2, padx=(8, 0), sticky="ew")

        self.use_auto_filters_var = tk.BooleanVar(value=True)
        self._make_checkbutton(
            card,
            "Použiť automatické filtre z priečinkov filters/ a filtre_5G/",
            self.use_auto_filters_var,
        ).grid(row=3, column=0, columnspan=3, sticky="w", pady=(10, 4))

        self._label(card, "Dodatočné filtre (.txt)", kind="muted").grid(row=4, column=0, columnspan=3, sticky="w", pady=(4, 4))

        filters_wrap = tk.Frame(card, bg=card["bg"])
        filters_wrap.grid(row=5, column=0, columnspan=3, sticky="ew")
        filters_wrap.grid_columnconfigure(0, weight=1)

        self.filters_listbox = tk.Listbox(
            filters_wrap,
            height=5,
            bg=self.colors["surface"],
            fg=self.colors["text"],
            selectbackground=self.colors["accent"],
            selectforeground="#ffffff",
            font=self.fonts["small"],
            relief="flat",
            highlightthickness=1,
            highlightbackground=self.colors["border"],
            bd=0,
        )
        self.filters_listbox.grid(row=0, column=0, sticky="ew")

        buttons_col = tk.Frame(filters_wrap, bg=filters_wrap["bg"])
        buttons_col.grid(row=0, column=1, sticky="n", padx=(8, 0))
        self._make_button(buttons_col, "Pridať", self._add_filter_files, variant="secondary").pack(fill="x")
        self._make_button(buttons_col, "Odstrániť", self._remove_selected_filter, variant="danger").pack(fill="x", pady=5)
        self._make_button(buttons_col, "Vyčistiť", self._clear_filter_files, variant="secondary").pack(fill="x")

        card.grid_columnconfigure(0, weight=1)
        card.grid_columnconfigure(1, weight=1)

    def _build_options_card(self, parent):
        card = self._make_card(parent)
        card.pack(fill="both", expand=True, pady=(10, 0))

        self._label(card, "Nastavenia spracovania", kind="section").grid(row=0, column=0, columnspan=4, sticky="w")

        self._label(card, "Režim", kind="muted").grid(row=1, column=0, sticky="w", pady=(10, 4))
        self._label(card, "Veľkosť zóny/úseku (m)", kind="muted").grid(row=1, column=1, sticky="w", padx=(10, 0), pady=(10, 4))
        self._label(card, "RSRP hranica", kind="muted").grid(row=1, column=2, sticky="w", padx=(10, 0), pady=(10, 4))
        self._label(card, "SINR hranica", kind="muted").grid(row=1, column=3, sticky="w", padx=(10, 0), pady=(10, 4))

        self.zone_mode_var = tk.StringVar(value=list(ZONE_MODES.keys())[0])
        self.zone_mode_combo = ttk.Combobox(
            card,
            textvariable=self.zone_mode_var,
            values=list(ZONE_MODES.keys()),
            state="readonly",
            style="App.TCombobox",
        )
        self.zone_mode_combo.grid(row=2, column=0, sticky="ew")
        self.zone_mode_combo.bind("<<ComboboxSelected>>", lambda _e: self._refresh_operator_fields())

        self.zone_size_var = tk.StringVar(value="100")
        self._make_entry(card, self.zone_size_var).grid(row=2, column=1, sticky="ew", padx=(10, 0))

        self.rsrp_threshold_var = tk.StringVar(value="-110")
        self._make_entry(card, self.rsrp_threshold_var).grid(row=2, column=2, sticky="ew", padx=(10, 0))

        self.sinr_threshold_var = tk.StringVar(value="-5")
        self._make_entry(card, self.sinr_threshold_var).grid(row=2, column=3, sticky="ew", padx=(10, 0))

        self.keep_original_rows_var = tk.BooleanVar(value=False)
        self._make_checkbutton(
            card,
            "Pri filtroch ponechať aj originálny riadok",
            self.keep_original_rows_var,
        ).grid(row=3, column=0, columnspan=2, sticky="w", pady=(10, 0))

        self.include_empty_var = tk.BooleanVar(value=False)
        self._make_checkbutton(
            card,
            "Generovať prázdne zóny/úseky",
            self.include_empty_var,
            command=self._refresh_operator_fields,
        ).grid(row=3, column=3, sticky="w", padx=(10, 0), pady=(10, 0))

        self.add_custom_operators_var = tk.BooleanVar(value=False)
        self.add_custom_operators_check = self._make_checkbutton(
            card,
            "Pridať vlastných operátorov",
            self.add_custom_operators_var,
        )
        self.add_custom_operators_check.grid(row=4, column=0, columnspan=4, sticky="w", pady=(10, 0))

        self._label(card, "Vlastní operátori", kind="muted").grid(row=5, column=0, sticky="w", pady=(10, 4))
        self.custom_operators_var = tk.StringVar(value="")
        self.custom_operators_entry = self._make_entry(card, self.custom_operators_var)
        self.custom_operators_entry.grid(row=6, column=0, columnspan=4, sticky="ew")

        self._label(
            card,
            "Formát: 231:01 alebo 231:01:10, viac hodnôt oddeľ medzerou",
            kind="muted",
        ).grid(row=7, column=0, columnspan=4, sticky="w", pady=(4, 8))

        self._label(card, "Mapovanie stĺpcov (písmená)", kind="section").grid(row=8, column=0, columnspan=4, sticky="w", pady=(4, 8))

        columns_holder = tk.Frame(card, bg=card["bg"])
        columns_holder.grid(row=9, column=0, columnspan=4, sticky="ew")

        self.column_vars = {}
        ordered_keys = ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"]
        for i, key in enumerate(ordered_keys):
            row = i // 4
            col = (i % 4) * 2
            tk.Label(
                columns_holder,
                text=key,
                bg=columns_holder["bg"],
                fg=self.colors["text"],
                font=self.fonts["small"],
            ).grid(row=row, column=col, sticky="w", padx=(0, 6), pady=6)
            var = tk.StringVar(value=DEFAULT_COLUMNS[key])
            self._make_entry(columns_holder, var, width=5).grid(row=row, column=col + 1, sticky="w", padx=(0, 12), pady=6)
            self.column_vars[key] = var

        for idx in range(4):
            card.grid_columnconfigure(idx, weight=1)
        self._refresh_operator_fields()

    def _build_activity_card(self, parent):
        card = self._make_card(parent)
        card.pack(fill="both", expand=True)

        self._label(card, "Spustenie a priebeh", kind="section").pack(anchor="w")

        actions = tk.Frame(card, bg=card["bg"])
        actions.pack(fill="x", pady=(10, 8))

        self.run_button = self._make_button(actions, "Spustiť spracovanie", self._run, variant="primary")
        self.run_button.pack(side="left")
        self._make_button(actions, "Vyčistiť log", self._clear_log, variant="secondary").pack(side="left", padx=(8, 0))

        self.status_label = tk.Label(
            card,
            textvariable=self.status_var,
            bg=card["bg"],
            fg=self.colors["muted"],
            font=self.fonts["body"],
            anchor="w",
        )
        self.status_label.pack(fill="x", pady=(2, 8))

        self.progress = ttk.Progressbar(card, mode="indeterminate", style="App.Horizontal.TProgressbar")
        self.progress.pack(fill="x", pady=(0, 10))

        log_frame = tk.Frame(
            card,
            bg=self.colors["log_bg"],
            highlightthickness=1,
            highlightbackground="#1f2a44",
            bd=0,
        )
        log_frame.pack(fill="both", expand=True)

        self.log_text = tk.Text(
            log_frame,
            height=18,
            wrap="word",
            bg=self.colors["log_bg"],
            fg=self.colors["log_fg"],
            insertbackground=self.colors["log_fg"],
            relief="flat",
            bd=0,
            highlightthickness=0,
            font=self.fonts["log"],
            padx=10,
            pady=10,
        )
        self.log_text.pack(fill="both", expand=True)
        self.log_text.configure(state="disabled")

    def _set_status(self, message, state="idle"):
        self.status_var.set(message)
        if state == "running":
            self.status_chip_var.set("RUNNING")
            self.status_chip.configure(bg=self.colors["accent"])
            self.status_label.configure(fg=self.colors["warning"])
        elif state == "success":
            self.status_chip_var.set("DONE")
            self.status_chip.configure(bg=self.colors["success"])
            self.status_label.configure(fg=self.colors["success"])
        elif state == "error":
            self.status_chip_var.set("ERROR")
            self.status_chip.configure(bg=self.colors["danger"])
            self.status_label.configure(fg=self.colors["danger"])
        else:
            self.status_chip_var.set("READY")
            self.status_chip.configure(bg=self.colors["warning"])
            self.status_label.configure(fg=self.colors["muted"])

    def _append_log(self, message: str):
        ts = datetime.now().strftime("%H:%M:%S")
        line = f"[{ts}] {message}\n"
        self.log_text.configure(state="normal")
        self.log_text.insert(tk.END, line)
        self.log_text.see(tk.END)
        self.log_text.configure(state="disabled")

    def _clear_log(self):
        self.log_text.configure(state="normal")
        self.log_text.delete("1.0", tk.END)
        self.log_text.configure(state="disabled")

    def _pick_csv(self):
        path = filedialog.askopenfilename(
            title="Vyber CSV súbor",
            filetypes=[("CSV súbory", "*.csv"), ("Všetky súbory", "*.*")],
        )
        if path:
            self.csv_path_var.set(path)
            self._autofill_columns_from_csv(path)

    def _autofill_columns_from_csv(self, file_path: str):
        suggested, detected = suggest_column_letters_from_file(file_path, DEFAULT_COLUMNS)
        for key, var in self.column_vars.items():
            var.set(suggested.get(key, DEFAULT_COLUMNS[key]))

        if detected:
            ordered = ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"]
            parts = []
            for key in ordered:
                if key in detected:
                    parts.append(f"{key}={detected[key]['letter']} ({detected[key]['header']})")
            self._append_log("Auto-detekované stĺpce: " + ", ".join(parts))
        else:
            self._append_log("Auto-detekcia stĺpcov nenašla známe názvy, ostávajú predvolené hodnoty.")

    def _add_filter_files(self):
        files = filedialog.askopenfilenames(
            title="Vyber filter súbory",
            filetypes=[("Text súbory", "*.txt"), ("Všetky súbory", "*.*")],
        )
        if not files:
            return

        existing = set(self.filters_listbox.get(0, tk.END))
        for file_path in files:
            if file_path not in existing:
                self.filters_listbox.insert(tk.END, file_path)

    def _remove_selected_filter(self):
        selected = list(self.filters_listbox.curselection())
        for idx in reversed(selected):
            self.filters_listbox.delete(idx)

    def _clear_filter_files(self):
        self.filters_listbox.delete(0, tk.END)

    def _refresh_operator_fields(self):
        allow_custom = self.include_empty_var.get()
        state = "normal" if allow_custom else "disabled"

        self.custom_operators_entry.configure(state=state)
        self.add_custom_operators_check.configure(state=state)
        if not allow_custom:
            self.add_custom_operators_var.set(False)
            self.custom_operators_var.set("")

    def _resolve_column_mapping(self):
        mapping = {}
        for key, var in self.column_vars.items():
            raw_value = var.get().strip()
            if not raw_value:
                raise ValueError(f"Chýba písmeno stĺpca pre '{key}'.")
            mapping[key] = col_letter_to_name(raw_value)
        return mapping

    def _resolve_filter_paths(self):
        selected = list(self.filters_listbox.get(0, tk.END))
        selected_paths = [str(Path(path)) for path in selected]

        if self.use_auto_filters_var.get() and not selected_paths:
            return None

        all_paths = []
        if self.use_auto_filters_var.get():
            all_paths.extend(discover_filter_paths())
        all_paths.extend(selected_paths)

        deduped = []
        seen = set()
        for path in all_paths:
            if path in seen:
                continue
            seen.add(path)
            deduped.append(path)
        return deduped

    def _build_config(self):
        csv_path = self.csv_path_var.get().strip()
        if not csv_path:
            raise ValueError("Vyber vstupný CSV súbor.")

        zone_mode = ZONE_MODES.get(self.zone_mode_var.get(), "center")
        zone_size = float(self.zone_size_var.get().replace(",", ".").strip())
        if zone_size <= 0:
            raise ValueError("Veľkosť zóny/úseku musí byť kladná.")

        rsrp_threshold = float(self.rsrp_threshold_var.get().replace(",", ".").strip())
        sinr_threshold = float(self.sinr_threshold_var.get().replace(",", ".").strip())

        custom_operators = []
        if self.add_custom_operators_var.get() and self.custom_operators_var.get().strip():
            custom_operators = parse_custom_operators_text(self.custom_operators_var.get())

        return ProcessingConfig(
            file_path=csv_path,
            column_mapping=self._resolve_column_mapping(),
            keep_original_rows=self.keep_original_rows_var.get(),
            zone_mode=zone_mode,
            zone_size_m=zone_size,
            rsrp_threshold=rsrp_threshold,
            sinr_threshold=sinr_threshold,
            include_empty_zones=self.include_empty_var.get(),
            add_custom_operators=self.add_custom_operators_var.get(),
            custom_operators=custom_operators,
            filter_paths=self._resolve_filter_paths(),
            progress_enabled=False,
        )

    def _set_running_state(self, running: bool):
        self.running = running
        if running:
            self.run_button.configure(state="disabled")
            self.progress.start(10)
        else:
            self.run_button.configure(state="normal")
            self.progress.stop()

    def _run(self):
        if self.running:
            return

        try:
            config = self._build_config()
        except Exception as exc:
            messagebox.showerror("Neplatné nastavenie", str(exc))
            return

        self._set_running_state(True)
        self._set_status("Spracovanie prebieha...", state="running")
        self._append_log("Spúšťam spracovanie")

        thread = threading.Thread(target=self._worker, args=(config,), daemon=True)
        thread.start()

    def _worker(self, config: ProcessingConfig):
        try:
            result = run_processing(config, status_callback=lambda msg: self.queue.put(("status", msg)))
            self.queue.put(("done", result))
        except Exception as exc:
            self.queue.put(("error", f"{exc}\n\n{traceback.format_exc()}"))

    def _process_queue(self):
        try:
            while True:
                event, payload = self.queue.get_nowait()
                if event == "status":
                    self._set_status(payload, state="running")
                    self._append_log(payload)
                elif event == "done":
                    self._set_running_state(False)
                    self._set_status("Spracovanie úspešne dokončené", state="success")
                    self._append_log(f"Výstup zón: {payload.zones_file}")
                    self._append_log(f"Výstup štatistík: {payload.stats_file}")
                    self._append_log("Hotovo")
                    messagebox.showinfo(
                        "Dokončené",
                        f"Hotovo.\n\nZóny: {payload.zones_file}\nŠtatistiky: {payload.stats_file}",
                    )
                elif event == "error":
                    self._set_running_state(False)
                    self._set_status("Chyba pri spracovaní", state="error")
                    self._append_log(f"Chyba: {payload}")
                    messagebox.showerror("Chyba pri spracovaní", payload)
        except queue.Empty:
            pass

        self.root.after(120, self._process_queue)


if __name__ == "__main__":
    root = tk.Tk()
    app = DesktopApp(root)
    root.mainloop()
