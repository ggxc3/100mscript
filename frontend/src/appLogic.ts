import { backend, main } from "../wailsjs/go/models";

export type ColumnKey =
  | "latitude"
  | "longitude"
  | "frequency"
  | "pci"
  | "mcc"
  | "mnc"
  | "rsrp"
  | "sinr";

export type Tone = "idle" | "running" | "success" | "error";
export type DropdownField = "startDate" | "startTime" | "endDate" | "endTime";
export type CsvLoadStage = "preview" | "time";

export type TimeValueOption = {
  timeLabel: string;
  timestampMS: number;
};

export type TimeDateOption = {
  dateLabel: string;
  times: TimeValueOption[];
};

export type TimeWindowDraft = {
  id: string;
  startDateInput: string;
  startTimeInput: string;
  endDateInput: string;
  endTimeInput: string;
  startTimestampMS: number | null;
  endTimestampMS: number | null;
  excludedRows: number;
};

export type UIState = {
  preview: main.CSVPreview | null;
  previewError: string | null;
  previewLoading: boolean;
  columnMapping: Partial<Record<ColumnKey, number>>;
  inputCsvPaths: string[];
  mobileLtePaths: string[];
  customFilterPaths: string[];
  running: boolean;
  statusText: string;
  statusTone: Tone;
  logs: string[];
  result: backend.ProcessingResult | null;
  excludedOriginalRows: number[];
  timeSelectorData: backend.TimeSelectorData | null;
  timeDateOptions: TimeDateOption[];
  timeWindows: TimeWindowDraft[];
  timeSelectorLoading: boolean;
  timeSelectorError: string;
  csvLoadDialog: {
    visible: boolean;
    stage: CsvLoadStage;
    showTimeStep: boolean;
    fileCount: number;
  };
  activeDropdown: { windowId: string; field: DropdownField; query: string } | null;
  processingPhases: PhaseRow[];
};

export type ReadinessItem = {
  id: string;
  label: string;
  ok: boolean;
  detail: string;
};

export type PhaseStatus = "pending" | "active" | "done" | "error";

export type PhaseRow = {
  id: string;
  label: string;
  status: PhaseStatus;
};

export const PROCESSING_PHASE_LABELS: Record<string, string> = {
  load_csv: "Načítanie a zlúčenie CSV",
  prepare_rows: "Príprava riadkov a časové výnimky",
  apply_filters: "Aplikácia filtrov",
  mobile_sync: "Synchronizácia 5G / LTE",
  compute_zones: "Výpočet zón",
  zone_stats: "Štatistiky zón",
  export_files: "Zápis výstupných súborov",
};

export function buildProcessingPhaseRows(cfg: backend.ProcessingConfig): PhaseRow[] {
  const rows: PhaseRow[] = [
    { id: "load_csv", label: PROCESSING_PHASE_LABELS.load_csv, status: "pending" },
    { id: "prepare_rows", label: PROCESSING_PHASE_LABELS.prepare_rows, status: "pending" },
    { id: "apply_filters", label: PROCESSING_PHASE_LABELS.apply_filters, status: "pending" },
  ];
  if (cfg.mobile_mode_enabled) {
    rows.push({ id: "mobile_sync", label: PROCESSING_PHASE_LABELS.mobile_sync, status: "pending" });
  }
  rows.push(
    { id: "compute_zones", label: PROCESSING_PHASE_LABELS.compute_zones, status: "pending" },
    { id: "zone_stats", label: PROCESSING_PHASE_LABELS.zone_stats, status: "pending" },
    { id: "export_files", label: PROCESSING_PHASE_LABELS.export_files, status: "pending" }
  );
  return rows;
}

export const HIGH_EXCLUSION_RATIO = 0.9;

export const COLUMN_FIELDS: Array<{ key: ColumnKey; label: string }> = [
  { key: "latitude", label: "Latitude" },
  { key: "longitude", label: "Longitude" },
  { key: "frequency", label: "Frequency" },
  { key: "pci", label: "PCI" },
  { key: "mcc", label: "MCC" },
  { key: "mnc", label: "MNC" },
  { key: "rsrp", label: "RSRP" },
  { key: "sinr", label: "SINR" },
];

export const ZONE_MODES = [
  { value: "segments", label: "Úseky po trase" },
  { value: "center", label: "Štvorcové zóny (stred)" },
  { value: "original", label: "Štvorcové zóny (prvý bod v zóne)" },
];

/** Hodnoty z backendu: 5g | lte | unknown */
export function inputRadioTechUiLabel(tech: string | undefined | null): string {
  switch (tech) {
    case "5g":
      return "5G (NR)";
    case "lte":
      return "LTE";
    default:
      return "neznámy typ";
  }
}

export function pathsMatchPreview(paths: string[], preview: main.CSVPreview | null): boolean {
  if (!preview || paths.length === 0) {
    return false;
  }
  const fromPreview =
    preview.filePaths && preview.filePaths.length > 0 ? preview.filePaths : preview.filePath ? [preview.filePath] : [];
  if (paths.length !== fromPreview.length) {
    return false;
  }
  return paths.every((p, i) => p === fromPreview[i]);
}

export function mappingComplete(ui: UIState): boolean {
  if (!ui.preview) {
    return false;
  }
  return COLUMN_FIELDS.every((f) => {
    const idx = ui.columnMapping[f.key];
    return typeof idx === "number" && !Number.isNaN(idx) && idx >= 0 && idx < ui.preview!.columns.length;
  });
}

export function computeReadinessItems(
  paths: string[],
  state: UIState,
  mobileEnabled: boolean,
  mobileLtePaths: string[]
): ReadinessItem[] {
  const items: ReadinessItem[] = [];
  items.push({
    id: "csv",
    label: "Vstupné CSV",
    ok: paths.length > 0,
    detail: paths.length === 0 ? "Pridaj aspoň jeden súbor." : `${paths.length} v zozname`,
  });
  const previewSync = pathsMatchPreview(paths, state.preview);
  const csvDataLoading = state.previewLoading || state.timeSelectorLoading;
  items.push({
    id: "preview",
    label: "Náhľad a časové dáta",
    ok: previewSync && !csvDataLoading,
    detail: csvDataLoading
      ? "Načítavam náhľad a časové údaje…"
      : state.previewError
        ? state.previewError
        : previewSync
          ? "Súbory zodpovedajú zoznamu."
          : paths.length > 0
            ? "Načítaj náhľad alebo uprav zoznam súborov."
            : "—",
  });
  const mapOk = mappingComplete(state);
  items.push({
    id: "mapping",
    label: "Mapovanie stĺpcov",
    ok: mapOk,
    detail: mapOk ? "Všetky povinné polia." : "Vyber stĺpec pre každé pole.",
  });
  const lteCount = mobileLtePaths.filter((p) => p.trim().length > 0).length;
  const mobileOk = !mobileEnabled || lteCount > 0;
  items.push({
    id: "mobile",
    label: "Mobile režim",
    ok: mobileOk,
    detail: mobileEnabled
      ? mobileOk
        ? `LTE: ${lteCount} ${lteCount === 1 ? "súbor" : "súbory"} v zozname.`
        : "Pridaj aspoň jeden LTE CSV."
      : "Vypnutý.",
  });
  return items;
}

export function pluralizeFiles(count: number): string {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (count === 1) {
    return "súbor";
  }
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return "súbory";
  }
  return "súborov";
}

export function phaseStatusIcon(status: PhaseStatus): string {
  switch (status) {
    case "done":
      return "✓";
    case "active":
      return "›";
    case "error":
      return "✕";
    default:
      return "○";
  }
}

export function cssEscape(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
    return CSS.escape(value);
  }
  return value.replaceAll('"', '\\"');
}

export function filterDropdownOptions(options: string[], query: string): string[] {
  const trimmed = query.trim().toLocaleLowerCase("sk-SK");
  if (!trimmed) {
    return options;
  }
  return options.filter((option) => option.toLocaleLowerCase("sk-SK").includes(trimmed));
}

export function buildDefaultTimeWindow(dateOptions: TimeDateOption[], index: number): TimeWindowDraft {
  const flat = flattenTimeDateOptions(dateOptions);
  const safeIndex = Math.min(index, Math.max(flat.length - 1, 0));
  const startOption = flat[safeIndex] ?? null;
  const endOption = flat[Math.min(safeIndex + 30, Math.max(flat.length - 1, 0))] ?? startOption;
  return {
    id: `window-${Date.now()}-${index}-${Math.random().toString(36).slice(2, 8)}`,
    startDateInput: startOption?.dateLabel ?? "",
    startTimeInput: startOption?.timeLabel ?? "",
    endDateInput: endOption?.dateLabel ?? "",
    endTimeInput: endOption?.timeLabel ?? "",
    startTimestampMS: startOption?.timestampMS ?? null,
    endTimestampMS: endOption?.timestampMS ?? null,
    excludedRows: 0,
  };
}

export function isCompleteTimeWindow(window: TimeWindowDraft): boolean {
  return window.startTimestampMS !== null && window.endTimestampMS !== null && window.startTimestampMS <= window.endTimestampMS;
}

/** Vypočíta počty vylúčených riadkov pre každé okno a zjednotený zoznam originálnych riadkov. */
export function recomputeTimeWindowExclusions(
  timeSelectorData: backend.TimeSelectorData | null,
  timeWindows: TimeWindowDraft[]
): { timeWindows: TimeWindowDraft[]; excludedOriginalRows: number[] } {
  if (!timeSelectorData) {
    return {
      timeWindows: timeWindows.map((window) => ({ ...window, excludedRows: 0 })),
      excludedOriginalRows: [],
    };
  }

  const timedRows = timeSelectorData.rows ?? [];
  const excludedRows = new Set<number>();
  const nextWindows = timeWindows.map((window) => {
    const startMS = window.startTimestampMS;
    const endMS = window.endTimestampMS;
    let matchingRows = 0;

    if (startMS !== null && endMS !== null && startMS <= endMS) {
      for (const row of timedRows) {
        if (row.timestamp_ms >= startMS && row.timestamp_ms <= endMS) {
          matchingRows++;
          excludedRows.add(row.original_row);
        }
      }
    }

    return {
      ...window,
      excludedRows: matchingRows,
    };
  });

  return {
    timeWindows: nextWindows,
    excludedOriginalRows: sortUniqueNumbers(Array.from(excludedRows)),
  };
}

export function describeTimeWindow(window: TimeWindowDraft): string {
  if (!window.startDateInput || !window.startTimeInput || !window.endDateInput || !window.endTimeInput) {
    return "Vyber dátum aj čas v oboch dropdownoch.";
  }
  if (window.startTimestampMS === null || window.endTimestampMS === null) {
    return "Vyber presnú kombináciu dátumu a času z ponuky.";
  }
  if (window.startTimestampMS > window.endTimestampMS) {
    return "Koniec musí byť po začiatku.";
  }
  if (window.excludedRows === 0) {
    return "V tomto okne nie sú žiadne body.";
  }
  return `${window.excludedRows} bodov bude vynechaných.`;
}

export function buildTimeDateOptions(rows: backend.TimeSelectorRow[]): TimeDateOption[] {
  const grouped = new Map<string, Map<string, number>>();
  for (const row of rows) {
    const dateLabel = formatDateLabel(row.timestamp_ms);
    const timeLabel = formatTimeLabel(row.timestamp_ms);
    let times = grouped.get(dateLabel);
    if (!times) {
      times = new Map<string, number>();
      grouped.set(dateLabel, times);
    }
    if (!times.has(timeLabel)) {
      times.set(timeLabel, row.timestamp_ms);
    }
  }

  return Array.from(grouped.entries())
    .map(([dateLabel, times]) => ({
      dateLabel,
      times: Array.from(times.entries())
        .map(([timeLabel, timestampMS]) => ({ timeLabel, timestampMS }))
        .sort((a, b) => a.timestampMS - b.timestampMS),
    }))
    .sort((a, b) => {
      const left = a.times[0]?.timestampMS ?? 0;
      const right = b.times[0]?.timestampMS ?? 0;
      return left - right;
    });
}

export function getTimeOptionsForDate(dateOptions: TimeDateOption[], dateLabel: string): TimeValueOption[] {
  const trimmed = dateLabel.trim();
  if (!trimmed) {
    return [];
  }
  return dateOptions.find((option) => option.dateLabel === trimmed)?.times ?? [];
}

export function flattenTimeDateOptions(
  dateOptions: TimeDateOption[]
): Array<{ dateLabel: string; timeLabel: string; timestampMS: number }> {
  return dateOptions.flatMap((dateOption) =>
    dateOption.times.map((timeOption) => ({
      dateLabel: dateOption.dateLabel,
      timeLabel: timeOption.timeLabel,
      timestampMS: timeOption.timestampMS,
    }))
  );
}

export function updateTimeWindowField(
  window: TimeWindowDraft,
  field: string,
  value: string,
  dateOptions: TimeDateOption[]
): TimeWindowDraft {
  const next = { ...window };
  const trimmed = value.trim();

  switch (field) {
    case "startDate":
      next.startDateInput = trimmed;
      if (!getTimeOptionsForDate(dateOptions, trimmed).some((option) => option.timeLabel === next.startTimeInput)) {
        next.startTimeInput = "";
      }
      next.startTimestampMS = resolveTimeWindowTimestamp(next.startDateInput, next.startTimeInput, dateOptions);
      return next;
    case "startTime":
      next.startTimeInput = trimmed;
      next.startTimestampMS = resolveTimeWindowTimestamp(next.startDateInput, next.startTimeInput, dateOptions);
      return next;
    case "endDate":
      next.endDateInput = trimmed;
      if (!getTimeOptionsForDate(dateOptions, trimmed).some((option) => option.timeLabel === next.endTimeInput)) {
        next.endTimeInput = "";
      }
      next.endTimestampMS = resolveTimeWindowTimestamp(next.endDateInput, next.endTimeInput, dateOptions);
      return next;
    case "endTime":
      next.endTimeInput = trimmed;
      next.endTimestampMS = resolveTimeWindowTimestamp(next.endDateInput, next.endTimeInput, dateOptions);
      return next;
    default:
      return next;
  }
}

export function resolveTimeWindowTimestamp(dateLabel: string, timeLabel: string, dateOptions: TimeDateOption[]): number | null {
  const trimmedDate = dateLabel.trim();
  const trimmedTime = timeLabel.trim();
  if (!trimmedDate || !trimmedTime) {
    return null;
  }
  const dateOption = dateOptions.find((option) => option.dateLabel === trimmedDate);
  const timeOption = dateOption?.times.find((option) => option.timeLabel === trimmedTime);
  return timeOption ? timeOption.timestampMS : null;
}

export function formatDateLabel(timestampMS: number): string {
  return formatDateTimeParts(timestampMS).dateLabel;
}

export function formatTimeLabel(timestampMS: number): string {
  return formatDateTimeParts(timestampMS).timeLabel;
}

export function formatDateTimeLabel(timestampMS: number): string {
  const parts = formatDateTimeParts(timestampMS);
  return `${parts.dateLabel} ${parts.timeLabel}`;
}

export function formatDateTimeParts(timestampMS: number): { dateLabel: string; timeLabel: string } {
  const formatter = new Intl.DateTimeFormat("sk-SK", {
    timeZone: "Europe/Bratislava",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3,
    hour12: false,
  });
  const formatted = formatter.formatToParts(new Date(timestampMS));
  const values = new Map<string, string>();
  for (const part of formatted) {
    values.set(part.type, (values.get(part.type) ?? "") + part.value);
  }
  const dateLabel = `${values.get("day") ?? "00"}.${values.get("month") ?? "00"}.${values.get("year") ?? "0000"}`;
  const fraction = values.get("fractionalSecond") ?? "000";
  const timeLabel =
    `${values.get("hour") ?? "00"}:${values.get("minute") ?? "00"}:${values.get("second") ?? "00"}.${fraction}`;
  return { dateLabel, timeLabel };
}

export function labelForTimeStrategy(strategy?: string): string {
  switch (strategy) {
    case "utc_numeric":
      return "UTC číslo";
    case "utc_datetime":
      return "UTC dátum/čas";
    case "date_time_with_utc_fallback":
      return "Date + Time (UTC fallback)";
    case "date_time":
      return "Date + Time";
    default:
      return strategy || "neznáme";
  }
}

export function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function renderDropdownField(params: {
  window: TimeWindowDraft;
  field: DropdownField;
  label: string;
  value: string;
  placeholder: string;
  options: string[];
  state: UIState;
  disabled?: boolean;
}): string {
  const { window, field, label, value, placeholder, options, state, disabled = false } = params;
  const isOpen = state.activeDropdown?.windowId === window.id && state.activeDropdown.field === field;
  const query = isOpen ? state.activeDropdown?.query ?? "" : "";
  const buttonLabel = value || placeholder;
  const emptyText = query ? "Žiadna zhoda pre vyhľadávanie." : "Žiadne dostupné možnosti.";

  return `
    <div class="dropdown-field${disabled ? " is-disabled" : ""}">
      <span>${label}</span>
      <button
        class="dropdown-trigger${isOpen ? " is-open" : ""}"
        type="button"
        data-open-dropdown="true"
        data-window-id="${escapeHtml(window.id)}"
        data-field="${field}"
        ${disabled ? "disabled" : ""}
      >
        <span class="${value ? "dropdown-value" : "dropdown-placeholder"}">${escapeHtml(buttonLabel)}</span>
        <span class="dropdown-caret">▾</span>
      </button>
      ${
        isOpen && !disabled
          ? `
            <div class="dropdown-panel">
              <input
                class="dropdown-search"
                type="text"
                value="${escapeHtml(query)}"
                placeholder="Hľadaj..."
                data-window-id="${escapeHtml(window.id)}"
                data-dropdown-search="${field}"
                autocomplete="off"
              />
              <div class="dropdown-options">
                ${
                  options.length > 0
                    ? options
                        .map(
                          (option) => `
                            <button
                              class="dropdown-option${option === value ? " is-selected" : ""}"
                              type="button"
                              data-select-option="true"
                              data-window-id="${escapeHtml(window.id)}"
                              data-field="${field}"
                              data-option-value="${escapeHtml(option)}"
                            >
                              ${escapeHtml(option)}
                            </button>`
                        )
                        .join("")
                    : `<div class="dropdown-empty">${escapeHtml(emptyText)}</div>`
                }
              </div>
            </div>`
          : ""
      }
    </div>
  `;
}

export function dedupePaths(paths: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const path of paths) {
    const trimmed = path.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    out.push(trimmed);
  }
  return out;
}

export function parseNumberInput(input: HTMLInputElement, label: string): number {
  const normalized = input.value.trim().replace(",", ".");
  if (!normalized) {
    throw new Error(`Chýba hodnota: ${label}`);
  }
  const value = Number(normalized);
  if (!Number.isFinite(value)) {
    throw new Error(`Neplatná číselná hodnota: ${label}`);
  }
  return value;
}

export function parseIntegerInput(input: HTMLInputElement, label: string): number {
  const value = parseNumberInput(input, label);
  if (!Number.isInteger(value) || value < 0) {
    throw new Error(`${label} musí byť celé číslo >= 0.`);
  }
  return value;
}

export function parseCustomOperatorsText(text: string): backend.CustomOperator[] {
  const trimmed = text.trim();
  if (!trimmed) {
    return [];
  }

  const out: backend.CustomOperator[] = [];
  for (const token of trimmed.split(/\s+/)) {
    const parts = token.split(":");
    if (parts.length !== 2 && parts.length !== 3) {
      throw new Error(`Neplatný operátor '${token}'. Použi formát MCC:MNC alebo MCC:MNC:PCI.`);
    }
    const [mcc, mnc, pci = ""] = parts.map((value) => value.trim());
    if (!mcc || !mnc) {
      throw new Error(`Neplatný operátor '${token}'. MCC a MNC musia byť vyplnené.`);
    }
    out.push({ mcc, mnc, pci } as backend.CustomOperator);
  }
  return out;
}

export function formatPercent(value?: number): string {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "n/a";
  }
  return `${value.toFixed(2)} %`;
}

export function sortUniqueNumbers(values: number[]): number[] {
  const out = Array.from(new Set(values.filter((value) => Number.isFinite(value))));
  out.sort((a, b) => a - b);
  return out;
}

export function fileParentDir(filePath: string): string {
  const i = Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\"));
  return i <= 0 ? "" : filePath.slice(0, i);
}

export function fileBasename(filePath: string): string {
  const i = Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\"));
  return i < 0 ? filePath : filePath.slice(i + 1);
}

export function timestamp(): string {
  return new Date().toLocaleTimeString("sk-SK", { hour12: false });
}

export function formatTimestampForFilename(): string {
  const d = new Date();
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}`;
}
