import "./style.css";
import "./app.css";

import {
  DefaultOutputPaths,
  DeletePreset,
  DiscoverAutoFilterPaths,
  GetAppInfo,
  ListPresets,
  LoadPreset,
  OpenContainingFolder,
  PickFilterFiles,
  PickInputCSVFile,
  PickInputCSVPaths,
  PickMobileLTECSVFile,
  PickMobileLTECSVPaths,
  PickOutputCSVFile,
  RunProcessingWithConfig,
  SavePreset,
  StartLoadCSVPreview,
} from "../wailsjs/go/main/App";
import { backend, main } from "../wailsjs/go/models";
import { ClipboardSetText, EventsOn } from "../wailsjs/runtime/runtime";

type ColumnKey =
  | "latitude"
  | "longitude"
  | "frequency"
  | "pci"
  | "mcc"
  | "mnc"
  | "rsrp"
  | "sinr";

type Tone = "idle" | "running" | "success" | "error";
type DropdownField = "startDate" | "startTime" | "endDate" | "endTime";
type TimeWindowDraft = {
  id: string;
  startDateInput: string;
  startTimeInput: string;
  endDateInput: string;
  endTimeInput: string;
  startTimestampMS: number | null;
  endTimestampMS: number | null;
  applied: boolean;
  excludedRows: number;
};

type UIState = {
  preview: main.CSVPreview | null;
  /** Posledná chyba načítania náhľadu (hlavičky); pri úspechu null. */
  previewError: string | null;
  previewLoading: boolean;
  columnMapping: Partial<Record<ColumnKey, number>>;
  inputCsvPaths: string[];
  /** LTE CSV súbory pre Mobile režim (zlúčia sa v tom istom poradí ako pri vstupnom CSV). */
  mobileLtePaths: string[];
  customFilterPaths: string[];
  running: boolean;
  statusText: string;
  statusTone: Tone;
  logs: string[];
  result: backend.ProcessingResult | null;
  excludedOriginalRows: number[];
  timeWindows: TimeWindowDraft[];
  activeDropdown: { windowId: string; field: DropdownField; query: string } | null;
  processingPhases: PhaseRow[];
};

type ReadinessItem = {
  id: string;
  label: string;
  ok: boolean;
  detail: string;
};

type PhaseStatus = "pending" | "active" | "done" | "error";

type PhaseRow = {
  id: string;
  label: string;
  status: PhaseStatus;
};

type CSVPreviewLoadEvent = {
  requestId?: number;
  preview?: main.CSVPreview;
  error?: string;
};

type PresetUIState = {
  input_csv_paths: string[];
  mobile_lte_paths: string[];
  custom_filter_paths: string[];
  use_auto_filters: boolean;
  use_additional_filters: boolean;
  enable_time_selector: boolean;
  custom_operators_text: string;
  column_mapping: Record<string, number>;
  time_windows: Array<{ start: string; end: string }>;
};

type ProcessingPreset = {
  schemaVersion: number;
  id: string;
  name: string;
  inputRadioTech: string;
  createdAt: string;
  updatedAt: string;
  config: backend.ProcessingConfig;
  uiState: PresetUIState;
};

const PROCESSING_PHASE_LABELS: Record<string, string> = {
  load_csv: "Načítanie a zlúčenie CSV",
  prepare_rows: "Príprava riadkov a časové výnimky",
  apply_filters: "Aplikácia filtrov",
  mobile_sync: "Synchronizácia 5G / LTE",
  compute_zones: "Výpočet zón",
  zone_stats: "Štatistiky zón",
  export_files: "Zápis výstupných súborov",
};

function buildProcessingPhaseRows(cfg: backend.ProcessingConfig): PhaseRow[] {
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

const PREVIEW_AUTOLOAD_MS = 420;
const COLUMN_FIELDS: Array<{ key: ColumnKey; label: string }> = [
  { key: "latitude", label: "Latitude" },
  { key: "longitude", label: "Longitude" },
  { key: "frequency", label: "Frequency" },
  { key: "pci", label: "PCI" },
  { key: "mcc", label: "MCC" },
  { key: "mnc", label: "MNC" },
  { key: "rsrp", label: "RSRP" },
  { key: "sinr", label: "SINR" },
];

/** Hodnoty z backendu: 5g | lte | unknown */
function inputRadioTechUiLabel(tech: string | undefined | null): string {
  switch (tech) {
    case "5g":
      return "5G (NR)";
    case "lte":
      return "LTE";
    default:
      return "neznámy typ";
  }
}

function pathsMatchPreview(paths: string[], preview: main.CSVPreview | null): boolean {
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

function mappingComplete(ui: UIState): boolean {
  if (!ui.preview) {
    return false;
  }
  return COLUMN_FIELDS.every((f) => {
    const idx = ui.columnMapping[f.key];
    return typeof idx === "number" && !Number.isNaN(idx) && idx >= 0 && idx < ui.preview!.columns.length;
  });
}

function computeReadinessItems(
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
  const csvDataLoading = state.previewLoading;
  items.push({
    id: "preview",
    label: "Náhľad CSV",
    ok: previewSync && !csvDataLoading,
    detail: csvDataLoading
      ? "Načítavam náhľad CSV…"
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

const ZONE_MODES = [
  { value: "segments", label: "Úseky po trase" },
  { value: "center", label: "Štvorcové zóny (stred)" },
  { value: "original", label: "Štvorcové zóny (prvý bod v zóne)" },
];

const app = document.querySelector<HTMLDivElement>("#app");
if (!app) {
  throw new Error("App root not found");
}

mountMainView(app);

function mountMainView(root: HTMLDivElement): void {
  const state: UIState = {
    preview: null,
    previewError: null,
    previewLoading: false,
    columnMapping: {},
    inputCsvPaths: [],
    mobileLtePaths: [],
    customFilterPaths: [],
    running: false,
    statusText: "Pripravené",
    statusTone: "idle",
    logs: [],
    result: null,
    excludedOriginalRows: [],
    timeWindows: [],
    activeDropdown: null,
    processingPhases: [],
  };

  root.innerHTML = `
    <main class="desktop-shell">
      <header class="topbar">
        <div class="topbar-main">
          <h1>100mscript</h1>
          <p class="lede">Spracovanie CSV meraní do zón a štatistík. Mapovanie stĺpcov a voliteľné časové výnimky.</p>
        </div>
        <div class="topbar-aside">
          <button id="presetsBtn" type="button" class="btn btn-toolbar">Presety</button>
          <button id="aboutBtn" type="button" class="btn btn-toolbar">O aplikácii</button>
          <div class="status-stack">
            <div id="statusChip" class="status-chip idle">PRIPRAVENÉ</div>
            <div id="statusText" class="status-text" hidden aria-live="polite"></div>
            <div id="statusElapsed" class="status-elapsed" aria-live="polite"></div>
          </div>
        </div>
      </header>

      <section class="content-grid">
        <section class="left-column">
          <article class="card section-card">
            <div class="section-head">
              <h2>Vstupné dáta</h2>
            </div>

            <div class="filters-panel">
              <div class="filters-head">
                <strong>Vstupné CSV súbory</strong>
              </div>
              <p class="section-note csv-input-note">
                Pri viacerých súboroch musí byť <strong>rovnaká celá hlavička</strong> (všetky názvy stĺpcov v tom istom poradí). Po zlúčení sa riadky
                zoradia podľa času (UTC alebo Date+Time), aby sedeli s časovými oknami.
              </p>
              <p class="section-note csv-autoload-note">
                Po pridaní súborov sa náhľad <strong>načíta automaticky</strong>. Tlačidlo nižšie použite na obnovenie po zmene súborov na disku.
              </p>
              <div class="inline-actions">
                <button id="addCsvBtn" class="btn secondary" type="button">Pridať súbor</button>
                <button id="addCsvMultiBtn" class="btn secondary" type="button">Pridať viac naraz</button>
                <button id="removeCsvBtn" class="btn danger" type="button">Odstrániť vybrané</button>
                <button id="clearCsvBtn" class="btn ghost" type="button">Vyčistiť</button>
                <button id="loadPreviewBtn" class="btn ghost" type="button">Načítať stĺpce</button>
              </div>
              <select id="csvList" class="listbox" multiple size="5"></select>
              <p id="csvPreviewStatus" class="csv-preview-inline" hidden aria-live="polite"></p>
            </div>

            <label class="check-row">
              <input id="mobileMode" type="checkbox" />
              <span>Mobile režim (synchronizácia 5G NR cez LTE súbor)</span>
            </label>

            <div id="mobileLteInputWarning" class="mobile-mode-input-warning" role="status" aria-live="polite" hidden>
              Zapol si Mobile režim (dopĺňanie 5G NR z LTE súboru), ale vstupné CSV sú rozpoznané ako
              <strong>LTE</strong>, nie 5G (NR). Skontroluj, či je to zámer; hlavný merací súbor má mať
              typicky stĺpce pre 5G (napr. NR-ARFCN).
            </div>

            <div id="mobileFieldsWrap" class="collapsible-block" aria-hidden="true">
              <div class="collapsible-block__inner">
                <div id="mobileFields" class="stack">
                  <div class="filters-panel">
                    <div class="filters-head">
                      <strong>LTE CSV súbory (iba pre Mobile režim)</strong>
                    </div>
                    <p class="section-note csv-input-note">
                      Pri viacerých súboroch musí byť <strong>rovnaká celá hlavička</strong> (všetky názvy stĺpcov v tom istom poradí) ako pri vstupnom CSV. Po zlúčení sa riadky zoradia podľa času (UTC alebo Date+Time).
                    </p>
                    <div class="inline-actions">
                      <button id="addMobileLteBtn" class="btn secondary" type="button">Pridať súbor</button>
                      <button id="addMobileLteMultiBtn" class="btn secondary" type="button">Pridať viac naraz</button>
                      <button id="removeMobileLteBtn" class="btn danger" type="button">Odstrániť vybrané</button>
                      <button id="clearMobileLteBtn" class="btn ghost" type="button">Vyčistiť</button>
                    </div>
                    <select id="mobileLteList" class="listbox" multiple size="5"></select>
                  </div>

                  <label class="field">
                    <span>Tolerancia času (ms)</span>
                    <input id="mobileTolerance" type="number" min="0" step="1" value="1000" />
                  </label>
                </div>
              </div>
            </div>

            <label class="check-row">
              <input id="useAutoFilters" type="checkbox" checked />
              <span>Použiť automatické filtre z priečinkov <code>filters/</code> a <code>filtre_5G/</code></span>
            </label>

            <label class="check-row">
              <input id="useAdditionalFilters" type="checkbox" />
              <span>Nahrať dodatočné filtre (.txt)</span>
            </label>

            <div id="extraFiltersWrap" class="collapsible-block" aria-hidden="true">
              <div class="collapsible-block__inner">
                <div class="filters-panel">
                  <div class="filters-head">
                    <strong>Dodatočné filtre (.txt)</strong>
                    <div class="inline-actions">
                      <button id="addFiltersBtn" class="btn secondary" type="button">Pridať</button>
                      <button id="removeFilterBtn" class="btn danger" type="button">Odstrániť</button>
                      <button id="clearFiltersBtn" class="btn ghost" type="button">Vyčistiť</button>
                    </div>
                  </div>
                  <select id="filtersList" class="listbox" multiple size="5"></select>
                </div>
              </div>
            </div>
          </article>

          <article class="card section-card">
            <div class="section-head">
              <h2>Nastavenia spracovania</h2>
            </div>

            <div class="quad-grid">
              <label class="field">
                <span>Režim</span>
                <select id="zoneMode"></select>
              </label>
              <label class="field">
                <span>Veľkosť zóny/úseku (m)</span>
                <input id="zoneSize" type="number" min="0.1" step="0.1" value="100" />
              </label>
              <label class="field">
                <span>RSRP hranica</span>
                <input id="rsrpThreshold" type="number" step="0.1" value="-110" />
              </label>
              <label class="field">
                <span>SINR hranica</span>
                <input id="sinrThreshold" type="number" step="0.1" value="-5" />
              </label>
            </div>

            <div class="double-grid checks-grid">
              <label class="check-row">
                <input id="keepOriginalRows" type="checkbox" />
                <span>Pri filtroch ponechať aj originálny riadok</span>
              </label>
              <label class="check-row">
                <input id="includeEmptyZones" type="checkbox" />
                <span>Generovať prázdne zóny/úseky</span>
              </label>
            </div>

            <div id="customOperatorsPanel" class="stack disabled-panel">
              <div class="custom-operators-intro">
                <p class="section-note operator-hint">
                  Dostupné len pri zapnutí „Generovať prázdne zóny“. Umožní doplniť operátorov, ktorí nemajú merané body, aby sa im aj tak vytvorili záznamy v štatistikách.
                </p>
                <label class="check-row">
                  <input id="addCustomOperators" type="checkbox" />
                  <span>Pridať vlastných operátorov</span>
                </label>
              </div>
              <label class="field">
                <span>Vlastní operátori</span>
                <input id="customOperatorsText" type="text" placeholder="231:01 231:02:10" />
                <small>Formát: <code>MCC:MNC</code> alebo <code>MCC:MNC:PCI</code>, viac hodnôt oddeľ medzerou.</small>
              </label>
            </div>
          </article>

          <article class="card section-card">
            <div class="section-head">
              <h2>Mapovanie stĺpcov</h2>
            </div>
            <p class="section-note">Najprv načítaj CSV (automaticky po pridaní). Stĺpce sa predvyplnia podľa hlavičky; pred spustením skontroluj každé pole.</p>
            <div id="mappingGrid" class="mapping-grid"></div>
          </article>

          <article class="card section-card time-selector-card">
            <div class="section-head">
              <h2>Časové úseky</h2>
            </div>
            <label class="check-row">
              <input id="enableTimeSelector" type="checkbox" />
              <span>Zapnúť časové úseky a načítať časové dáta</span>
            </label>
            <div id="timeSelectorWrap" class="collapsible-block" aria-hidden="true">
              <div class="collapsible-block__inner">
                <div id="timeSelectorPanel" class="time-selector-panel">
                  <p class="section-note">
                    Voliteľné okná vylúčia merania z výpočtu. Počet vylúčených bodov sa prepočíta pri každej zmene.
                  </p>
                  <div id="timeSelectorInfo" class="time-selector-info muted">Najprv načítaj CSV.</div>
                  <div id="timeSelectorSummary" class="time-selector-summary">
                    <div><span>Aktívne okná</span><strong>0</strong></div>
                    <div><span>Neúplné</span><strong>0</strong></div>
                  </div>
                  <div class="inline-actions time-window-actions">
                    <button id="addTimeWindowBtn" class="btn primary" type="button">Pridať časové okno</button>
                    <button id="clearTimeWindowsBtn" class="btn ghost" type="button">Vymazať všetky</button>
                  </div>
                  <div id="timeWindowList" class="time-window-list">
                    <div class="time-window-empty muted">Zatiaľ nie je definované žiadne časové okno.</div>
                  </div>
                </div>
              </div>
            </div>
          </article>
        </section>

        <section class="right-column">
          <article class="card section-card sticky-panel">
            <div class="section-head">
              <h2>Spustenie a priebeh</h2>
            </div>

            <div id="readinessPanel" class="readiness-panel" aria-label="Kontrola pred spustením"></div>

            <div id="processingPipelineWrap" class="processing-pipeline-wrap" hidden>
              <div class="preview-head pipeline-head">
                <strong>Priebeh spracovania</strong>
              </div>
              <div id="processingPipeline" class="processing-pipeline" role="list" aria-live="polite"></div>
            </div>

            <hr class="panel-divider" aria-hidden="true" />

            <div class="output-paths-panel">
              <p class="section-note output-paths-note">
                Výstupné CSV sú voliteľné. Prázdne polia znamená uloženie vedľa vstupného súboru s rovnakým pravidlom pomenovania ako doteraz (<code>_zones.csv</code>, <code>_stats.csv</code>; pri Mobile režime <code>_mobile</code> v názve).
              </p>
              <label class="field">
                <span>Súbor zón</span>
                <div class="inline-row">
                  <input
                    id="outputZonesPath"
                    class="input-path-show-end"
                    type="text"
                    placeholder=""
                    autocomplete="off"
                    spellcheck="false"
                  />
                  <button id="pickOutputZonesBtn" class="btn secondary" type="button">Vybrať…</button>
                </div>
              </label>
              <label class="field">
                <span>Štatistiky</span>
                <div class="inline-row">
                  <input
                    id="outputStatsPath"
                    class="input-path-show-end"
                    type="text"
                    placeholder=""
                    autocomplete="off"
                    spellcheck="false"
                  />
                  <button id="pickOutputStatsBtn" class="btn secondary" type="button">Vybrať…</button>
                </div>
              </label>
            </div>

            <div id="progressBar" class="progress-bar" aria-hidden="true">
              <div class="progress-fill"></div>
            </div>

            <div class="run-actions">
              <button id="runBtn" class="btn primary" type="button">Spustiť spracovanie</button>
              <button id="openLogBtn" type="button" class="btn btn-toolbar run-actions__log">Technický log</button>
            </div>

            <div class="result-box">
              <div class="preview-head">
                <strong>Výsledok</strong>
              </div>
              <div id="resultContent" class="result-body muted">Zatiaľ nebolo spustené spracovanie.</div>
            </div>
          </article>
        </section>
      </section>

      <div id="aboutOverlay" class="modal-overlay" hidden>
        <div class="modal-dialog" role="dialog" aria-modal="true" aria-labelledby="aboutTitle">
          <h3 id="aboutTitle" class="modal-title">100mscript</h3>
          <p id="aboutBody" class="modal-body"></p>
          <button type="button" id="aboutCloseBtn" class="btn primary">Zavrieť</button>
        </div>
      </div>

      <div id="presetOverlay" class="preset-overlay" hidden>
        <aside class="preset-drawer" role="dialog" aria-modal="true" aria-labelledby="presetTitle">
          <div class="preset-head">
            <h3 id="presetTitle" class="modal-title">Presety</h3>
            <button id="presetCloseBtn" type="button" class="btn ghost small-btn">Zavrieť</button>
          </div>
          <p class="muted">Ulož aktuálne nastavenia alebo načítaj uložený preset.</p>
          <div class="preset-save-row">
            <input id="presetNameInput" type="text" placeholder="Názov presetu" />
            <button id="presetSaveBtn" type="button" class="btn primary">Uložiť</button>
          </div>
          <div id="presetStatus" class="csv-preview-inline" hidden></div>
          <div id="presetList" class="preset-list" aria-live="polite"></div>
        </aside>
      </div>

      <div id="logOverlay" class="modal-overlay" hidden>
        <div
          class="modal-dialog modal-dialog--log"
          role="dialog"
          aria-modal="true"
          aria-labelledby="logModalTitle"
        >
          <h3 id="logModalTitle" class="modal-title">Technický log</h3>
          <p class="log-modal-lede muted">Detailné hlášky pri diagnostike alebo podpore.</p>
          <div class="log-modal-toolbar">
            <button id="clearLogBtn" class="btn ghost small-btn" type="button">Vyčistiť log</button>
            <button id="exportLogBtn" class="btn ghost small-btn" type="button">Exportovať log</button>
          </div>
          <pre id="logOutput" class="log-output log-output--modal">Pripravené.</pre>
          <button type="button" id="logCloseBtn" class="btn primary log-modal-close">Zavrieť</button>
        </div>
      </div>
    </main>
  `;

  const csvList = qs<HTMLSelectElement>("#csvList");
  const addCsvBtn = qs<HTMLButtonElement>("#addCsvBtn");
  const addCsvMultiBtn = qs<HTMLButtonElement>("#addCsvMultiBtn");
  const removeCsvBtn = qs<HTMLButtonElement>("#removeCsvBtn");
  const clearCsvBtn = qs<HTMLButtonElement>("#clearCsvBtn");
  const loadPreviewBtn = qs<HTMLButtonElement>("#loadPreviewBtn");
  const csvPreviewStatus = qs<HTMLParagraphElement>("#csvPreviewStatus");
  const mobileModeCheckbox = qs<HTMLInputElement>("#mobileMode");
  const mobileLteInputWarning = qs<HTMLDivElement>("#mobileLteInputWarning");
  const mobileFieldsWrap = qs<HTMLDivElement>("#mobileFieldsWrap");
  const mobileFields = qs<HTMLDivElement>("#mobileFields");
  const mobileLteList = qs<HTMLSelectElement>("#mobileLteList");
  const addMobileLteBtn = qs<HTMLButtonElement>("#addMobileLteBtn");
  const addMobileLteMultiBtn = qs<HTMLButtonElement>("#addMobileLteMultiBtn");
  const removeMobileLteBtn = qs<HTMLButtonElement>("#removeMobileLteBtn");
  const clearMobileLteBtn = qs<HTMLButtonElement>("#clearMobileLteBtn");
  const mobileToleranceInput = qs<HTMLInputElement>("#mobileTolerance");
  const useAutoFiltersCheckbox = qs<HTMLInputElement>("#useAutoFilters");
  const useAdditionalFiltersCheckbox = qs<HTMLInputElement>("#useAdditionalFilters");
  const extraFiltersWrap = qs<HTMLDivElement>("#extraFiltersWrap");
  const addFiltersBtn = qs<HTMLButtonElement>("#addFiltersBtn");
  const removeFilterBtn = qs<HTMLButtonElement>("#removeFilterBtn");
  const clearFiltersBtn = qs<HTMLButtonElement>("#clearFiltersBtn");
  const filtersList = qs<HTMLSelectElement>("#filtersList");
  const zoneModeSelect = qs<HTMLSelectElement>("#zoneMode");
  const zoneSizeInput = qs<HTMLInputElement>("#zoneSize");
  const rsrpThresholdInput = qs<HTMLInputElement>("#rsrpThreshold");
  const sinrThresholdInput = qs<HTMLInputElement>("#sinrThreshold");
  const keepOriginalRowsCheckbox = qs<HTMLInputElement>("#keepOriginalRows");
  const includeEmptyZonesCheckbox = qs<HTMLInputElement>("#includeEmptyZones");
  const customOperatorsPanel = qs<HTMLDivElement>("#customOperatorsPanel");
  const addCustomOperatorsCheckbox = qs<HTMLInputElement>("#addCustomOperators");
  const customOperatorsTextInput = qs<HTMLInputElement>("#customOperatorsText");
  const enableTimeSelectorCheckbox = qs<HTMLInputElement>("#enableTimeSelector");
  const timeSelectorWrap = qs<HTMLDivElement>("#timeSelectorWrap");
  const timeSelectorInfo = qs<HTMLDivElement>("#timeSelectorInfo");
  const timeSelectorSummary = qs<HTMLDivElement>("#timeSelectorSummary");
  const addTimeWindowBtn = qs<HTMLButtonElement>("#addTimeWindowBtn");
  const clearTimeWindowsBtn = qs<HTMLButtonElement>("#clearTimeWindowsBtn");
  const timeWindowList = qs<HTMLDivElement>("#timeWindowList");
  const mappingGrid = qs<HTMLDivElement>("#mappingGrid");
  const runBtn = qs<HTMLButtonElement>("#runBtn");
  const clearLogBtn = qs<HTMLButtonElement>("#clearLogBtn");
  const exportLogBtn = qs<HTMLButtonElement>("#exportLogBtn");
  const progressBar = qs<HTMLDivElement>("#progressBar");
  const resultContent = qs<HTMLDivElement>("#resultContent");
  const logOutput = qs<HTMLPreElement>("#logOutput");
  const statusChip = qs<HTMLDivElement>("#statusChip");
  const statusText = qs<HTMLDivElement>("#statusText");
  const statusElapsed = qs<HTMLDivElement>("#statusElapsed");
  const outputZonesPathInput = qs<HTMLInputElement>("#outputZonesPath");
  const outputStatsPathInput = qs<HTMLInputElement>("#outputStatsPath");
  const pickOutputZonesBtn = qs<HTMLButtonElement>("#pickOutputZonesBtn");
  const pickOutputStatsBtn = qs<HTMLButtonElement>("#pickOutputStatsBtn");
  const readinessPanel = qs<HTMLDivElement>("#readinessPanel");
  const presetsBtn = qs<HTMLButtonElement>("#presetsBtn");
  const presetOverlay = qs<HTMLDivElement>("#presetOverlay");
  const presetCloseBtn = qs<HTMLButtonElement>("#presetCloseBtn");
  const presetSaveBtn = qs<HTMLButtonElement>("#presetSaveBtn");
  const presetNameInput = qs<HTMLInputElement>("#presetNameInput");
  const presetList = qs<HTMLDivElement>("#presetList");
  const presetStatus = qs<HTMLDivElement>("#presetStatus");
  const aboutBtn = qs<HTMLButtonElement>("#aboutBtn");
  const aboutOverlay = qs<HTMLDivElement>("#aboutOverlay");
  const aboutBody = qs<HTMLParagraphElement>("#aboutBody");
  const aboutCloseBtn = qs<HTMLButtonElement>("#aboutCloseBtn");
  const processingPipelineWrap = qs<HTMLDivElement>("#processingPipelineWrap");
  const processingPipeline = qs<HTMLDivElement>("#processingPipeline");
  const openLogBtn = qs<HTMLButtonElement>("#openLogBtn");
  const logOverlay = qs<HTMLDivElement>("#logOverlay");
  const logCloseBtn = qs<HTMLButtonElement>("#logCloseBtn");

  let presetsCache: ProcessingPreset[] = [];
  let activePresetId: string | null = null;
  let previewAutoloadTimer: ReturnType<typeof setTimeout> | null = null;
  let runElapsedTimer: ReturnType<typeof setInterval> | null = null;
  let pendingLoaderExitId: string | null = null;
  let loaderExitClearTimer: ReturnType<typeof setTimeout> | null = null;
  let previewLoadRequestId = 0;
  let activePreviewPathsKey = "";
  let pendingPreviewReload = false;
  const phaseProgressPercent: Record<string, number> = {};

  function updateModalScrollLock(): void {
    const shouldLock = !aboutOverlay.hidden || !logOverlay.hidden || !presetOverlay.hidden;
    document.documentElement.classList.toggle("scroll-locked", shouldLock);
    document.body.classList.toggle("scroll-locked", shouldLock);
  }

  function waitForUiSettle(): Promise<void> {
    return new Promise((resolve) => {
      window.setTimeout(() => {
        window.requestAnimationFrame(() => resolve());
      }, 0);
    });
  }

  function scheduleLoaderExitCleanup(): void {
    if (loaderExitClearTimer !== null) {
      clearTimeout(loaderExitClearTimer);
    }
    loaderExitClearTimer = window.setTimeout(() => {
      loaderExitClearTimer = null;
      pendingLoaderExitId = null;
      renderProcessingPipeline();
    }, 420);
  }

  function cancelPreviewAutoload(): void {
    if (previewAutoloadTimer !== null) {
      clearTimeout(previewAutoloadTimer);
      previewAutoloadTimer = null;
    }
  }

  function scheduleAutoLoadPreview(): void {
    cancelPreviewAutoload();
    previewAutoloadTimer = setTimeout(() => {
      previewAutoloadTimer = null;
      if (state.inputCsvPaths.length === 0) {
        return;
      }
      void loadPreviewForCurrentCSV().catch(handlePreviewError);
    }, PREVIEW_AUTOLOAD_MS);
  }

  function stopRunElapsedTimer(): void {
    if (runElapsedTimer !== null) {
      clearInterval(runElapsedTimer);
      runElapsedTimer = null;
    }
    statusElapsed.textContent = "";
  }

  function startRunElapsedTimer(): void {
    stopRunElapsedTimer();
    const started = Date.now();
    runElapsedTimer = setInterval(() => {
      const sec = Math.floor((Date.now() - started) / 1000);
      const m = Math.floor(sec / 60);
      const s = sec % 60;
      statusElapsed.textContent = m > 0 ? `Uplynulo ${m}:${s.toString().padStart(2, "0")}` : `Uplynulo ${sec} s`;
    }, 500);
  }

  function allInputsReady(): boolean {
    return computeReadinessItems(getInputCsvPaths(), state, mobileModeCheckbox.checked, state.mobileLtePaths).every(
      (item) => item.ok
    );
  }

  function renderReadiness(): void {
    const items = computeReadinessItems(getInputCsvPaths(), state, mobileModeCheckbox.checked, state.mobileLtePaths);
    readinessPanel.innerHTML = items
      .map(
        (item) => `
        <div class="readiness-row ${item.ok ? "is-ok" : "is-missing"}">
          <span class="readiness-icon" aria-hidden="true">${item.ok ? "✓" : "·"}</span>
          <div class="readiness-copy">
            <div class="readiness-label">${escapeHtml(item.label)}</div>
            <div class="readiness-detail">${escapeHtml(item.detail)}</div>
          </div>
        </div>`
      )
      .join("");
    if (!state.running) {
      runBtn.disabled = !allInputsReady();
    }
  }

  function updateLoadPreviewButtonLabel(): void {
    loadPreviewBtn.textContent = state.preview ? "Obnoviť náhľad" : "Načítať stĺpce";
  }

  function phaseStatusIcon(status: PhaseStatus): string {
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

  function phaseLoadingBlock(p: PhaseRow): string {
    if (p.status === "active") {
      const raw = phaseProgressPercent[p.id];
      const pct = Math.max(0, Math.min(100, Math.round(typeof raw === "number" && !Number.isNaN(raw) ? raw : 0)));
      return `
        <div class="phase-progress-stack phase-progress-stack--enter" aria-label="Priebeh: ${pct} percent">
          <div class="phase-pct-header">
            <span class="phase-pct-value">${pct}%</span>
          </div>
          <div class="phase-pct-track" aria-hidden="true">
            <div class="phase-pct-fill" style="width: ${pct}%"></div>
          </div>
        </div>`;
    }
    if (pendingLoaderExitId === p.id && (p.status === "done" || p.status === "error")) {
      return `
        <div class="phase-loading-wrap phase-loading-wrap--exit" aria-hidden="true">
          <div class="phase-loading-track">
            <div class="phase-loading-bar"></div>
          </div>
        </div>`;
    }
    return "";
  }

  function renderProcessingPipeline(): void {
    const phases = state.processingPhases;
    if (phases.length === 0) {
      processingPipelineWrap.hidden = true;
      processingPipeline.innerHTML = "";
      return;
    }
    processingPipelineWrap.hidden = false;
    processingPipeline.innerHTML = phases
      .map(
        (p) => `
        <div class="phase-row phase-${p.status}" role="listitem">
          <span class="phase-icon" aria-hidden="true">${phaseStatusIcon(p.status)}</span>
          <div class="phase-text-block">
            <span class="phase-label">${escapeHtml(p.label)}</span>
            ${phaseLoadingBlock(p)}
          </div>
        </div>`
      )
      .join("");
  }

  function applyProcessingPhaseEvent(phaseId: string): void {
    const phases = state.processingPhases;
    const prevActive = phases.find((p) => p.status === "active");
    const idx = phases.findIndex((p) => p.id === phaseId);
    if (idx < 0) {
      return;
    }
    if (prevActive && prevActive.id !== phaseId) {
      pendingLoaderExitId = prevActive.id;
      scheduleLoaderExitCleanup();
      delete phaseProgressPercent[prevActive.id];
    }
    state.processingPhases = phases.map((p, i) => ({
      ...p,
      status: i < idx ? "done" : i === idx ? "active" : "pending",
    }));
    renderProcessingPipeline();
    const label = PROCESSING_PHASE_LABELS[phaseId] ?? phaseId;
    setStatus(label, "running");
  }

  function openLogModal(): void {
    logOverlay.hidden = false;
    updateModalScrollLock();
    logOutput.textContent = state.logs.length > 0 ? state.logs.join("\n") : "Pripravené.";
    window.requestAnimationFrame(() => {
      logOutput.scrollTop = logOutput.scrollHeight;
    });
  }

  function closeLogModal(): void {
    logOverlay.hidden = true;
    updateModalScrollLock();
  }

  EventsOn("processing:phase", (...args: unknown[]) => {
    const raw = args[0];
    const phaseId = typeof raw === "string" ? raw : String(raw ?? "");
    if (!phaseId) {
      return;
    }
    applyProcessingPhaseEvent(phaseId);
  });

  EventsOn("processing:progress", (...args: unknown[]) => {
    const phase = typeof args[0] === "string" ? args[0] : String(args[0] ?? "");
    const n = typeof args[1] === "number" ? args[1] : Number(args[1]);
    if (!phase || Number.isNaN(n)) {
      return;
    }
    phaseProgressPercent[phase] = n;
    renderProcessingPipeline();
  });

  EventsOn("csv-preview:loaded", (...args: unknown[]) => {
    const payload = (args[0] ?? {}) as CSVPreviewLoadEvent;
    const requestId = typeof payload.requestId === "number" ? payload.requestId : Number(payload.requestId);
    if (Number.isNaN(requestId) || requestId !== previewLoadRequestId) {
      return;
    }

    const currentPathsKey = getInputCsvPaths().join("\n");
    if (activePreviewPathsKey !== currentPathsKey) {
      state.previewLoading = false;
      renderPreview();
      renderTimeSelector();
      renderReadiness();
      if (pendingPreviewReload && getInputCsvPaths().length > 0) {
        pendingPreviewReload = false;
        window.setTimeout(() => {
          void loadPreviewForCurrentCSV().catch(handlePreviewError);
        }, 0);
      } else if (pendingPreviewReload) {
        pendingPreviewReload = false;
      }
      return;
    }

    state.previewLoading = false;
    if (payload.error) {
      state.preview = null;
      state.previewError = payload.error;
      state.columnMapping = {};
      clearTimeSelectorState();
      renderPreview();
      renderMappingGrid();
      setStatus("Chyba pri načítaní CSV", "error");
      appendLog(`Chyba pri načítaní CSV: ${payload.error}`);
    } else if (payload.preview) {
      state.preview = payload.preview;
      state.previewError = null;
      applySuggestedMapping(payload.preview);
      renderPreview();
      renderMappingGrid();
      renderTimeSelector();
      appendLog(
        `Načítané stĺpce (${payload.preview.columns.length}), vstup: ${inputRadioTechUiLabel(payload.preview.inputRadioTech)}, encoding=${payload.preview.encoding}, headerLine=${payload.preview.headerLine + 1}`
      );
      void refreshOutputPathDefaults();
    } else {
      state.preview = null;
      state.previewError = "Backend nevrátil náhľad CSV.";
      state.columnMapping = {};
      clearTimeSelectorState();
      renderPreview();
      renderMappingGrid();
    }

    renderReadiness();
    if (pendingPreviewReload && getInputCsvPaths().length > 0) {
      pendingPreviewReload = false;
      window.setTimeout(() => {
        void loadPreviewForCurrentCSV().catch(handlePreviewError);
      }, 0);
    } else if (pendingPreviewReload) {
      pendingPreviewReload = false;
    }
  });

  ZONE_MODES.forEach((mode) => {
    const opt = document.createElement("option");
    opt.value = mode.value;
    opt.textContent = mode.label;
    zoneModeSelect.appendChild(opt);
  });
  zoneModeSelect.value = "segments";

  function appendLog(message: string): void {
    state.logs.push(`[${timestamp()}] ${message}`);
    if (state.logs.length > 250) {
      state.logs = state.logs.slice(-250);
    }
    logOutput.textContent = state.logs.join("\n");
    logOutput.scrollTop = logOutput.scrollHeight;
  }

  function setStatus(text: string, tone: Tone): void {
    state.statusText = text;
    state.statusTone = tone;
    const hideSubtitle =
      (tone === "idle" && text === "Pripravené") || tone === "success";
    statusText.hidden = hideSubtitle;
    statusText.textContent = hideSubtitle ? "" : text;
    statusChip.className = `status-chip ${tone}`;
    statusChip.textContent =
      tone === "running" ? "PREBIEHA" : tone === "success" ? "HOTOVÉ" : tone === "error" ? "CHYBA" : "PRIPRAVENÉ";
  }

  function setRunning(running: boolean): void {
    state.running = running;
    if (running) {
      startRunElapsedTimer();
    } else {
      stopRunElapsedTimer();
    }
    runBtn.disabled = running || !allInputsReady();
    csvList.disabled = running;
    addCsvBtn.disabled = running;
    addCsvMultiBtn.disabled = running;
    removeCsvBtn.disabled = running;
    clearCsvBtn.disabled = running;
    loadPreviewBtn.disabled = running;
    const mobileOn = mobileModeCheckbox.checked;
    addMobileLteBtn.disabled = running || !mobileOn;
    addMobileLteMultiBtn.disabled = running || !mobileOn;
    removeMobileLteBtn.disabled = running || !mobileOn;
    clearMobileLteBtn.disabled = running || !mobileOn;
    mobileLteList.disabled = running || !mobileOn;
    const additionalFiltersOn = useAdditionalFiltersCheckbox.checked;
    addFiltersBtn.disabled = running || !additionalFiltersOn;
    removeFilterBtn.disabled = running || !additionalFiltersOn;
    clearFiltersBtn.disabled = running || !additionalFiltersOn;
    filtersList.disabled = running || !additionalFiltersOn;
    addTimeWindowBtn.disabled = running || !enableTimeSelectorCheckbox.checked;
    clearTimeWindowsBtn.disabled = running || !enableTimeSelectorCheckbox.checked || state.timeWindows.length === 0;
    outputZonesPathInput.disabled = running;
    outputStatsPathInput.disabled = running;
    progressBar.classList.toggle("is-running", running);
    progressBar.setAttribute("aria-hidden", running ? "false" : "true");
    renderReadiness();
    void refreshOutputPathDefaults();
  }

  function renderPreview(): void {
    updateLoadPreviewButtonLabel();
    if (state.previewLoading) {
      csvPreviewStatus.hidden = false;
      csvPreviewStatus.className = "csv-preview-inline";
      csvPreviewStatus.innerHTML = `<span class="csv-preview-inline__muted">Načítavam hlavičku CSV…</span>`;
      updateMobileLteInputWarning();
      renderReadiness();
      return;
    }
    if (state.previewError) {
      csvPreviewStatus.hidden = false;
      csvPreviewStatus.className = "csv-preview-inline";
      csvPreviewStatus.innerHTML = `<span class="csv-preview-inline__err">Chyba: ${escapeHtml(state.previewError)}</span>`;
      updateMobileLteInputWarning();
      renderReadiness();
      return;
    }
    if (!state.preview) {
      csvPreviewStatus.hidden = true;
      csvPreviewStatus.className = "csv-preview-inline";
      csvPreviewStatus.textContent = "";
      updateMobileLteInputWarning();
      renderReadiness();
      return;
    }
    csvPreviewStatus.hidden = false;
    csvPreviewStatus.className = "csv-preview-inline";
    const techLabel = inputRadioTechUiLabel(state.preview.inputRadioTech);
    csvPreviewStatus.innerHTML = `<span class="csv-preview-inline__ok">Hlavička CSV načítaná úspešne.</span><span class="csv-preview-inline__muted"> · Vstup: ${escapeHtml(techLabel)}</span>`;
    updateMobileLteInputWarning();
    renderReadiness();
  }

  function renderFilterList(): void {
    filtersList.innerHTML = "";
    for (const path of state.customFilterPaths) {
      const opt = document.createElement("option");
      opt.value = path;
      opt.textContent = path;
      filtersList.appendChild(opt);
    }
  }

  function renderCsvList(): void {
    csvList.innerHTML = "";
    for (const path of state.inputCsvPaths) {
      const opt = document.createElement("option");
      opt.value = path;
      opt.textContent = path;
      csvList.appendChild(opt);
    }
  }

  function renderMobileLteList(): void {
    mobileLteList.innerHTML = "";
    for (const path of state.mobileLtePaths) {
      const opt = document.createElement("option");
      opt.value = path;
      opt.textContent = path;
      mobileLteList.appendChild(opt);
    }
  }

  function getInputCsvPaths(): string[] {
    return [...state.inputCsvPaths];
  }

  function fileParentDir(filePath: string): string {
    const i = Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\"));
    return i <= 0 ? "" : filePath.slice(0, i);
  }

  function fileBasename(filePath: string): string {
    const i = Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\"));
    return i < 0 ? filePath : filePath.slice(i + 1);
  }

  async function refreshOutputPathDefaults(): Promise<void> {
    const paths = getInputCsvPaths();
    const hasPaths = paths.length > 0;
    pickOutputZonesBtn.disabled = state.running || !hasPaths;
    pickOutputStatsBtn.disabled = state.running || !hasPaths;

    if (!hasPaths) {
      outputZonesPathInput.placeholder = "";
      outputStatsPathInput.placeholder = "";
      return;
    }
    try {
      const defaults = (await DefaultOutputPaths(paths[0], mobileModeCheckbox.checked, "")) as main.DefaultOutputPathsResult;
      outputZonesPathInput.placeholder = defaults.zones;
      outputStatsPathInput.placeholder = defaults.stats;
    } catch {
      outputZonesPathInput.placeholder = "";
      outputStatsPathInput.placeholder = "";
    }
  }

  async function pickOutputZonesPath(): Promise<void> {
    const paths = getInputCsvPaths();
    if (paths.length === 0) {
      appendLog("Najprv pridaj vstupný CSV.");
      return;
    }
    const first = paths[0];
    const defaults = (await DefaultOutputPaths(first, mobileModeCheckbox.checked, "")) as main.DefaultOutputPathsResult;
    const picked = await PickOutputCSVFile("Uložiť súbor zón", fileParentDir(first), fileBasename(defaults.zones));
    if (!picked) {
      return;
    }
    outputZonesPathInput.value = picked;
  }

  async function pickOutputStatsPath(): Promise<void> {
    const paths = getInputCsvPaths();
    if (paths.length === 0) {
      appendLog("Najprv pridaj vstupný CSV.");
      return;
    }
    const first = paths[0];
    const defaults = (await DefaultOutputPaths(first, mobileModeCheckbox.checked, "")) as main.DefaultOutputPathsResult;
    const picked = await PickOutputCSVFile("Uložiť súbor štatistík", fileParentDir(first), fileBasename(defaults.stats));
    if (!picked) {
      return;
    }
    outputStatsPathInput.value = picked;
  }

  function clearCsvInputAndPreview(): void {
    cancelPreviewAutoload();
    state.inputCsvPaths = [];
    state.preview = null;
    state.previewError = null;
    state.columnMapping = {};
    clearTimeSelectorState();
    renderCsvList();
    renderPreview();
    renderMappingGrid();
    renderResult();
    void refreshOutputPathDefaults();
  }

  function invalidateCsvPreviewAfterListChange(): void {
    state.preview = null;
    state.previewError = null;
    state.columnMapping = {};
    clearTimeSelectorState();
    renderPreview();
    renderMappingGrid();
    renderResult();
    void refreshOutputPathDefaults();
  }

  function removeSelectedCsvPaths(): void {
    const selected = new Set(Array.from(csvList.selectedOptions).map((opt) => opt.value));
    if (selected.size === 0) {
      return;
    }
    state.inputCsvPaths = state.inputCsvPaths.filter((path) => !selected.has(path));
    if (state.inputCsvPaths.length === 0) {
      clearCsvInputAndPreview();
    } else {
      invalidateCsvPreviewAfterListChange();
      renderCsvList();
      scheduleAutoLoadPreview();
    }
    appendLog(`Odstránené vstupné CSV (${selected.size})`);
  }

  function clearCsvPaths(): void {
    if (state.inputCsvPaths.length === 0) {
      return;
    }
    const count = state.inputCsvPaths.length;
    clearCsvInputAndPreview();
    appendLog(`Vyčistený zoznam vstupných CSV (${count})`);
  }

  async function addOneCsvFromPicker(): Promise<void> {
    const path = await PickInputCSVFile();
    if (!path) {
      return;
    }
    if (state.inputCsvPaths.includes(path)) {
      appendLog(`Súbor už je v zozname: ${path}`);
      return;
    }
    state.inputCsvPaths = [...state.inputCsvPaths, path];
    invalidateCsvPreviewAfterListChange();
    renderCsvList();
    appendLog(`Pridaný CSV: ${path}`);
    scheduleAutoLoadPreview();
  }

  async function addMultipleCsvFromPicker(): Promise<void> {
    const paths = (await PickInputCSVPaths()) as string[];
    if (!paths || paths.length === 0) {
      return;
    }
    let added = 0;
    for (const p of paths) {
      if (state.inputCsvPaths.includes(p)) {
        continue;
      }
      state.inputCsvPaths = [...state.inputCsvPaths, p];
      added++;
    }
    if (added > 0) {
      invalidateCsvPreviewAfterListChange();
    }
    renderCsvList();
    const skipped = paths.length - added;
    appendLog(`Pridané nové CSV súbory: ${added}${skipped > 0 ? ` (preskočené duplicitné: ${skipped})` : ""}`);
    if (added > 0) {
      scheduleAutoLoadPreview();
    }
  }

  function renderResult(): void {
    if (!state.result) {
      resultContent.className = "result-body muted";
      resultContent.innerHTML = "Zatiaľ nebolo spustené spracovanie.";
      return;
    }
    const result = state.result;
    resultContent.className = "result-body";
    resultContent.innerHTML = `
      <div class="result-grid">
        <div><span>Zóny CSV</span><strong>${escapeHtml(result.zones_file ?? "")}</strong></div>
        <div><span>Štatistiky CSV</span><strong>${escapeHtml(result.stats_file ?? "")}</strong></div>
        <div><span>Unikátne zóny</span><strong>${String(result.unique_zones ?? 0)}</strong></div>
        <div><span>Unikátni operátori</span><strong>${String(result.unique_operators ?? 0)}</strong></div>
        <div><span>Riadky zón</span><strong>${String(result.total_zone_rows ?? 0)}</strong></div>
        <div><span>Časové okná</span><strong>${String(state.timeWindows.filter((window) => isCompleteTimeWindow(window)).length)}</strong></div>
        <div><span>Pokrytie</span><strong>${formatPercent(result.coverage_percent)}</strong></div>
      </div>
      <div class="result-actions-bar">
        <button type="button" class="btn secondary small-btn" data-result-action="open-output">Otvoriť výstupný priečinok</button>
        <button type="button" class="btn ghost small-btn" data-result-action="copy-zones">Skopírovať cestu (zóny)</button>
        <button type="button" class="btn ghost small-btn" data-result-action="copy-stats">Skopírovať cestu (štatistiky)</button>
      </div>
    `;
  }

  function recomputeTimeWindowSelection(): void {
    state.timeWindows = state.timeWindows.map((window) => {
      return {
        ...window,
        startTimestampMS: window.applied ? resolveDraftTimestamp(window.startDateInput, window.startTimeInput) : null,
        endTimestampMS: window.applied ? resolveDraftTimestamp(window.endDateInput, window.endTimeInput) : null,
        excludedRows: 0,
      };
    });
  }

  function renderTimeSelectorSummaryCards(): void {
    const activeWindows = state.timeWindows.filter((window) => isCompleteTimeWindow(window)).length;
    const incompleteWindows = state.timeWindows.length - activeWindows;
    timeSelectorSummary.innerHTML = `
      <div><span>Aktívne okná</span><strong>${String(activeWindows)}</strong></div>
      <div><span>Neúplné</span><strong>${String(incompleteWindows)}</strong></div>
    `;
  }

  function updateTimeWindowSectionStatus(section: HTMLElement, window: TimeWindowDraft): void {
    const status = section.querySelector<HTMLDivElement>(".time-window-status");
    const count = section.querySelector<HTMLSpanElement>(".time-window-count");
    if (status) {
      status.textContent = describeTimeWindow(window);
    }
    if (count) {
      const complete = isCompleteTimeWindow(window);
      count.textContent = complete ? "Pripravené" : "Neúplné";
      count.classList.toggle("count-active", complete);
      count.classList.toggle("count-idle", !complete);
    }
  }

  function renderTimeSelector(): void {
    const timeSelectorEnabled = enableTimeSelectorCheckbox.checked;
    renderTimeSelectorSummaryCards();

    if (!timeSelectorEnabled) {
      timeSelectorInfo.className = "time-selector-info muted";
      timeSelectorInfo.textContent = "Zapni časové úseky, ak chceš pri spracovaní vyradiť zadané časové intervaly.";
    } else {
      timeSelectorInfo.className = "time-selector-info";
      timeSelectorInfo.textContent =
        "Zadaj začiatok a koniec intervalu. Všetky merania, ktoré časovo spadnú do týchto okien, backend vyradí počas spracovania.";
    }

    addTimeWindowBtn.disabled = state.running || !timeSelectorEnabled;
    clearTimeWindowsBtn.disabled = state.running || !timeSelectorEnabled || state.timeWindows.length === 0;

    if (!timeSelectorEnabled) {
      const message = "Sekcia je vypnutá. Po zapnutí môžeš pridávať intervaly ručne bez ďalšieho načítania.";
      timeWindowList.innerHTML = `<div class="time-window-empty muted">${escapeHtml(message)}</div>`;
      renderReadiness();
      return;
    }

    if (state.timeWindows.length === 0) {
      timeWindowList.innerHTML = `<div class="time-window-empty muted">Zatiaľ nie je definované žiadne časové okno. Klikni na „Pridať časové okno“.</div>`;
      renderReadiness();
      return;
    }

    timeWindowList.innerHTML = state.timeWindows
      .map((window, index) => {
        const statusText = describeTimeWindow(window);
        return `
          <section class="time-window-item" data-window-section="${escapeHtml(window.id)}">
            <div class="time-window-item-head">
              <div>
                <strong>Okno ${index + 1}</strong>
                <div class="time-window-status">${escapeHtml(statusText)}</div>
              </div>
              <span class="time-window-count ${isCompleteTimeWindow(window) ? "count-active" : "count-idle"}">${isCompleteTimeWindow(window) ? "Pripravené" : "Neúplné"}</span>
            </div>
            <div class="time-window-fields">
              <div class="time-window-column">
                <div class="time-window-column-title">Od</div>
                <div class="time-window-subfields">
                  <label class="field">
                    <span>Dátum</span>
                    <input type="date" value="${escapeHtml(window.startDateInput)}" data-window-id="${escapeHtml(window.id)}" data-time-field="startDate" />
                  </label>
                  <label class="field">
                    <span>Čas</span>
                    <input type="time" step="1" value="${escapeHtml(window.startTimeInput)}" data-window-id="${escapeHtml(window.id)}" data-time-field="startTime" />
                  </label>
                </div>
                <small class="time-window-hint">Zadaj začiatok intervalu.</small>
              </div>
              <div class="time-window-column">
                <div class="time-window-column-title">Do</div>
                <div class="time-window-subfields">
                  <label class="field">
                    <span>Dátum</span>
                    <input type="date" value="${escapeHtml(window.endDateInput)}" data-window-id="${escapeHtml(window.id)}" data-time-field="endDate" />
                  </label>
                  <label class="field">
                    <span>Čas</span>
                    <input type="time" step="1" value="${escapeHtml(window.endTimeInput)}" data-window-id="${escapeHtml(window.id)}" data-time-field="endTime" />
                  </label>
                </div>
                <small class="time-window-hint">Zadaj koniec intervalu.</small>
              </div>
            </div>
            <div class="time-window-foot">
              <span class="muted">Interval sa aplikuje až pri spustení spracovania.</span>
              <div class="inline-actions">
                <button class="btn secondary small-btn" type="button" data-save-window="${escapeHtml(window.id)}">Použiť interval</button>
                <button class="btn danger small-btn" type="button" data-remove-window="${escapeHtml(window.id)}">Odstrániť</button>
              </div>
            </div>
          </section>
        `;
      })
      .join("");
    renderReadiness();
  }

  function clearTimeSelectorState(): void {
    state.excludedOriginalRows = [];
    renderTimeSelector();
    renderResult();
    renderReadiness();
  }

  function renderMappingGrid(): void {
    if (!state.preview) {
      mappingGrid.innerHTML = COLUMN_FIELDS.map(
        ({ key, label }) => `
          <label class="field mapping-field">
            <span>${label}</span>
            <select data-column-key="${key}" disabled>
              <option>Najprv načítaj CSV</option>
            </select>
          </label>`
      ).join("");
      renderReadiness();
      return;
    }

    mappingGrid.innerHTML = COLUMN_FIELDS.map(({ key, label }) => {
      const selected = state.columnMapping[key];
      const options = state.preview!.columns
        .map((col, idx) => {
          const selectedAttr = selected === idx ? " selected" : "";
          return `<option value="${idx}"${selectedAttr}>${idx}: ${escapeHtml(col)}</option>`;
        })
        .join("");
      return `
        <label class="field mapping-field">
          <span>${label}</span>
          <select data-column-key="${key}">
            <option value="">-- vyber stĺpec --</option>
            ${options}
          </select>
        </label>`;
    }).join("");

    mappingGrid.querySelectorAll<HTMLSelectElement>("select[data-column-key]").forEach((select) => {
      select.addEventListener("change", () => {
        const key = select.dataset.columnKey as ColumnKey;
        const raw = select.value;
        if (raw === "") {
          delete state.columnMapping[key];
        } else {
          state.columnMapping[key] = Number(raw);
        }
        renderReadiness();
      });
    });
    renderReadiness();
  }

  function updateMobileLteInputWarning(): void {
    const mobileOn = mobileModeCheckbox.checked;
    const tech = state.preview?.inputRadioTech;
    const isLteInput = tech === "lte";
    mobileLteInputWarning.hidden = !(mobileOn && isLteInput);
  }

  function updateDependentUI(): void {
    const mobileEnabled = mobileModeCheckbox.checked;
    mobileFieldsWrap.classList.toggle("is-open", mobileEnabled);
    mobileFieldsWrap.setAttribute("aria-hidden", mobileEnabled ? "false" : "true");
    mobileFields.querySelectorAll<HTMLInputElement | HTMLButtonElement | HTMLSelectElement>("input,button,select").forEach((el) => {
      el.disabled = !mobileEnabled || state.running;
    });

    const additionalFiltersEnabled = useAdditionalFiltersCheckbox.checked;
    extraFiltersWrap.classList.toggle("is-open", additionalFiltersEnabled);
    extraFiltersWrap.setAttribute("aria-hidden", additionalFiltersEnabled ? "false" : "true");

    const timeSelectorEnabled = enableTimeSelectorCheckbox.checked;
    timeSelectorWrap.classList.toggle("is-open", timeSelectorEnabled);
    timeSelectorWrap.setAttribute("aria-hidden", timeSelectorEnabled ? "false" : "true");

    const allowCustomOperators = includeEmptyZonesCheckbox.checked;
    customOperatorsPanel.classList.toggle("disabled-panel", !allowCustomOperators);
    addCustomOperatorsCheckbox.disabled = !allowCustomOperators || state.running;
    customOperatorsTextInput.disabled = !allowCustomOperators || !addCustomOperatorsCheckbox.checked || state.running;
    if (!allowCustomOperators) {
      addCustomOperatorsCheckbox.checked = false;
      customOperatorsTextInput.value = "";
    }

    updateMobileLteInputWarning();
    setRunning(state.running);
  }

  function applySuggestedMapping(preview: main.CSVPreview): void {
    const next: Partial<Record<ColumnKey, number>> = {};
    for (const field of COLUMN_FIELDS) {
      const suggested = preview.suggestedMapping?.[field.key];
      if (typeof suggested === "number" && suggested >= 0 && suggested < preview.columns.length) {
        next[field.key] = suggested;
      }
    }
    state.columnMapping = next;
  }

  async function loadPreviewForCurrentCSV(): Promise<void> {
    const paths = getInputCsvPaths();
    if (paths.length === 0) {
      throw new Error("Pridaj aspoň jeden vstupný CSV súbor.");
    }
    if (state.previewLoading) {
      pendingPreviewReload = true;
      return;
    }

    const requestId = ++previewLoadRequestId;
    const requestedPathsKey = paths.join("\n");
    activePreviewPathsKey = requestedPathsKey;
    pendingPreviewReload = false;

    state.previewLoading = true;
    state.previewError = null;
    renderPreview();
    renderTimeSelector();
    renderReadiness();
    await waitForUiSettle();

    try {
      appendLog(`Načítavam hlavičku CSV (${paths.length} súborov): ${paths.join(", ")}`);
      if (requestId !== previewLoadRequestId || requestedPathsKey !== getInputCsvPaths().join("\n")) {
        return;
      }
      await StartLoadCSVPreview(requestId, paths);
    } catch (err) {
      if (requestId !== previewLoadRequestId || requestedPathsKey !== getInputCsvPaths().join("\n")) {
        return;
      }
      state.previewLoading = false;
      throw err;
    }
  }

  function buildColumnMapping(): Record<string, number> {
    if (!state.preview) {
      throw new Error("Najprv načítaj stĺpce zo vstupného CSV.");
    }
    const columnMapping: Record<string, number> = {};
    for (const field of COLUMN_FIELDS) {
      const idx = state.columnMapping[field.key];
      if (typeof idx !== "number" || Number.isNaN(idx)) {
        throw new Error(`Chýba mapovanie stĺpca pre '${field.label}'.`);
      }
      columnMapping[field.key] = idx;
    }
    return columnMapping;
  }

  async function buildProcessingConfig(): Promise<backend.ProcessingConfig> {
    const paths = getInputCsvPaths();
    if (paths.length === 0) {
      throw new Error("Pridaj vstupný CSV súbor (aspoň jeden).");
    }
    const filePath = paths[0];
    const input_file_paths = paths.length > 1 ? paths : undefined;

    const column_mapping = buildColumnMapping();
    const mobile_mode_enabled = mobileModeCheckbox.checked;
    const ltePaths = dedupePaths(state.mobileLtePaths.map((p) => p.trim()).filter((p) => p.length > 0));
    if (mobile_mode_enabled && ltePaths.length === 0) {
      throw new Error("Pre Mobile režim pridaj aspoň jeden LTE CSV súbor.");
    }

    const zone_size_m = parseNumberInput(zoneSizeInput, "Veľkosť zóny/úseku");
    if (zone_size_m <= 0) {
      throw new Error("Veľkosť zóny/úseku musí byť kladná.");
    }

    const rsrp_threshold = parseNumberInput(rsrpThresholdInput, "RSRP hranica");
    const sinr_threshold = parseNumberInput(sinrThresholdInput, "SINR hranica");
    const mobile_time_tolerance_ms = parseIntegerInput(mobileToleranceInput, "Tolerancia času");

    let custom_operators: backend.CustomOperator[] = [];
    const include_empty_zones = includeEmptyZonesCheckbox.checked;
    const add_custom_operators = include_empty_zones && addCustomOperatorsCheckbox.checked;
    if (add_custom_operators) {
      custom_operators = parseCustomOperatorsText(customOperatorsTextInput.value);
    }

    const output_zones_file_path = outputZonesPathInput.value.trim();
    const output_stats_file_path = outputStatsPathInput.value.trim();

    let filter_paths: string[] | undefined;
    const useAuto = useAutoFiltersCheckbox.checked;
    const useAdditional = useAdditionalFiltersCheckbox.checked;
    const effectiveCustomPaths = useAdditional ? state.customFilterPaths : [];
    if (!useAuto && effectiveCustomPaths.length === 0) {
      filter_paths = [];
    } else if (useAuto && effectiveCustomPaths.length === 0) {
      filter_paths = undefined;
    } else {
      let merged: string[] = [];
      if (useAuto) {
        const autoPaths = (await DiscoverAutoFilterPaths()) as string[];
        merged = merged.concat(autoPaths);
      }
      merged = merged.concat(effectiveCustomPaths);
      filter_paths = dedupePaths(merged);
    }

    return {
      file_path: filePath,
      input_file_paths,
      column_mapping,
      keep_original_rows: keepOriginalRowsCheckbox.checked,
      excluded_original_rows: [],
      time_windows: buildConfiguredTimeWindows(state.timeWindows),
      zone_mode: zoneModeSelect.value || "segments",
      zone_size_m,
      rsrp_threshold,
      sinr_threshold,
      include_empty_zones,
      add_custom_operators,
      custom_operators,
      filter_paths,
      output_suffix: "",
      ...(output_zones_file_path ? { output_zones_file_path } : {}),
      ...(output_stats_file_path ? { output_stats_file_path } : {}),
      mobile_mode_enabled,
      mobile_lte_file_path: mobile_mode_enabled && ltePaths.length > 0 ? ltePaths[0] : "",
      ...(mobile_mode_enabled && ltePaths.length > 1 ? { mobile_lte_file_paths: ltePaths } : {}),
      mobile_time_tolerance_ms,
      mobile_require_nr_yes: false,
      mobile_nr_column_name: "5G NR",
      progress_enabled: false,
    } as unknown as backend.ProcessingConfig;
  }

  async function runProcessing(): Promise<void> {
    if (state.running) {
      return;
    }
    if (!allInputsReady()) {
      appendLog("Nie je možné spustiť: skontroluj kontrolný zoznam v pravom paneli.");
      setStatus("Doplň položky v kontrolnom zozname", "idle");
      return;
    }

    try {
      setRunning(true);
      pendingLoaderExitId = null;
      if (loaderExitClearTimer !== null) {
        clearTimeout(loaderExitClearTimer);
        loaderExitClearTimer = null;
      }
      for (const k of Object.keys(phaseProgressPercent)) {
        delete phaseProgressPercent[k];
      }
      state.processingPhases = [];
      renderProcessingPipeline();
      setStatus("Kontrolujem vstupy...", "running");
      appendLog("Spúšťam spracovanie");

      const cfg = await buildProcessingConfig();
      state.processingPhases = buildProcessingPhaseRows(cfg);
      renderProcessingPipeline();
      appendLog(
        `Konfigurácia: mode=${cfg.zone_mode}, zone=${cfg.zone_size_m}m, filters=${cfg.filter_paths === undefined ? "auto" : cfg.filter_paths.length}, mobile=${cfg.mobile_mode_enabled}, time_windows=${cfg.time_windows?.length ?? 0}`
      );

      const result = (await RunProcessingWithConfig(cfg)) as backend.ProcessingResult;
      const lastActive = state.processingPhases.find((p) => p.status === "active");
      state.processingPhases = state.processingPhases.map((p) => ({ ...p, status: "done" as PhaseStatus }));
      if (lastActive) {
        pendingLoaderExitId = lastActive.id;
        scheduleLoaderExitCleanup();
      }
      renderProcessingPipeline();
      state.result = result;
      renderResult();

      setStatus("Spracovanie úspešne dokončené", "success");
      appendLog(`Výstup zón: ${result.zones_file}`);
      appendLog(`Výstup štatistík: ${result.stats_file}`);
      appendLog(
        `Hotovo (zóny=${result.unique_zones}, operátori=${result.unique_operators}, riadky=${result.total_zone_rows}, okná=${buildConfiguredTimeWindows(state.timeWindows).length})`
      );
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setStatus("Chyba pri spracovaní", "error");
      appendLog(`Chyba: ${message}`);
      if (state.processingPhases.length > 0) {
        if (state.processingPhases.some((p) => p.status === "active")) {
          const activePhase = state.processingPhases.find((p) => p.status === "active");
          if (activePhase) {
            pendingLoaderExitId = activePhase.id;
            scheduleLoaderExitCleanup();
          }
          state.processingPhases = state.processingPhases.map((p) =>
            p.status === "active" ? { ...p, status: "error" as PhaseStatus } : p
          );
        } else {
          let lastDone = -1;
          state.processingPhases.forEach((p, i) => {
            if (p.status === "done") {
              lastDone = i;
            }
          });
          const failAt = Math.min(Math.max(lastDone + 1, 0), state.processingPhases.length - 1);
          state.processingPhases = state.processingPhases.map((p, i) => ({
            ...p,
            status: i < failAt ? "done" : i === failAt ? ("error" as PhaseStatus) : "pending",
          }));
        }
        renderProcessingPipeline();
      }
    } finally {
      setRunning(false);
    }
  }

  function showPresetStatus(message: string, isError = false): void {
    presetStatus.hidden = false;
    presetStatus.textContent = message;
    presetStatus.classList.toggle("preset-status-error", isError);
  }

  function hidePresetStatus(): void {
    presetStatus.hidden = true;
    presetStatus.textContent = "";
    presetStatus.classList.remove("preset-status-error");
  }

  function parsePresetDateToLocalLabel(value: string): string {
    if (!value) {
      return "";
    }
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) {
      return value;
    }
    return d.toLocaleString("sk-SK", { hour12: false });
  }

  function renderPresetList(): void {
    if (presetsCache.length === 0) {
      presetList.innerHTML = '<p class="muted">Zatiaľ nie je uložený žiadny preset.</p>';
      return;
    }
    presetList.innerHTML = presetsCache
      .map(
        (preset) => `
        <article class="preset-item ${activePresetId === preset.id ? "is-active" : ""}">
          <div class="preset-item__title">${escapeHtml(preset.name)}</div>
          <div class="preset-item__meta">Typ vstupu: ${escapeHtml(inputRadioTechUiLabel(preset.inputRadioTech))}</div>
          <div class="preset-item__meta">Upravené: ${escapeHtml(parsePresetDateToLocalLabel(preset.updatedAt))}</div>
          <div class="preset-item__actions">
            <button type="button" class="btn secondary small-btn" data-preset-action="apply" data-preset-id="${escapeHtml(preset.id)}">Načítať</button>
            <button type="button" class="btn danger small-btn" data-preset-action="delete" data-preset-id="${escapeHtml(preset.id)}">Zmazať</button>
          </div>
        </article>`
      )
      .join("");
  }

  async function reloadPresets(): Promise<void> {
    presetsCache = ((await ListPresets()) as ProcessingPreset[]) ?? [];
    renderPresetList();
  }

  function toDraftFromTimeWindow(index: number, tw: { start: string; end: string }): TimeWindowDraft {
    const start = tw.start || "";
    const end = tw.end || "";
    const [startDateInput = "", startTimeRaw = ""] = start.split("T");
    const [endDateInput = "", endTimeRaw = ""] = end.split("T");
    const startTimeInput = normalizeTimeInput(startTimeRaw || "").slice(0, 8);
    const endTimeInput = normalizeTimeInput(endTimeRaw || "").slice(0, 8);
    return {
      id: `preset-window-${Date.now()}-${index}-${Math.random().toString(36).slice(2, 8)}`,
      startDateInput,
      startTimeInput,
      endDateInput,
      endTimeInput,
      startTimestampMS: resolveDraftTimestamp(startDateInput, startTimeInput),
      endTimestampMS: resolveDraftTimestamp(endDateInput, endTimeInput),
      applied: true,
      excludedRows: 0,
    };
  }

  function applyPresetToUI(preset: ProcessingPreset): void {
    const ui = preset.uiState || ({} as PresetUIState);
    const cfg = preset.config || ({} as backend.ProcessingConfig);

    state.inputCsvPaths = dedupePaths((ui.input_csv_paths && ui.input_csv_paths.length > 0 ? ui.input_csv_paths : cfg.input_file_paths || (cfg.file_path ? [cfg.file_path] : [])).map((p) => (p || "").trim()).filter((p) => p.length > 0));
    state.mobileLtePaths = dedupePaths((ui.mobile_lte_paths && ui.mobile_lte_paths.length > 0 ? ui.mobile_lte_paths : cfg.mobile_lte_file_paths || (cfg.mobile_lte_file_path ? [cfg.mobile_lte_file_path] : [])).map((p) => (p || "").trim()).filter((p) => p.length > 0));
    state.customFilterPaths = dedupePaths((ui.custom_filter_paths || []).map((p) => (p || "").trim()).filter((p) => p.length > 0));

    mobileModeCheckbox.checked = !!cfg.mobile_mode_enabled;
    useAutoFiltersCheckbox.checked = typeof ui.use_auto_filters === "boolean" ? ui.use_auto_filters : cfg.filter_paths === undefined;
    useAdditionalFiltersCheckbox.checked = typeof ui.use_additional_filters === "boolean" ? ui.use_additional_filters : state.customFilterPaths.length > 0;
    enableTimeSelectorCheckbox.checked = typeof ui.enable_time_selector === "boolean" ? ui.enable_time_selector : (cfg.time_windows?.length ?? 0) > 0;

    zoneModeSelect.value = cfg.zone_mode || "segments";
    zoneSizeInput.value = String(cfg.zone_size_m ?? 100);
    rsrpThresholdInput.value = String(cfg.rsrp_threshold ?? -110);
    sinrThresholdInput.value = String(cfg.sinr_threshold ?? -5);
    keepOriginalRowsCheckbox.checked = !!cfg.keep_original_rows;
    includeEmptyZonesCheckbox.checked = !!cfg.include_empty_zones;
    addCustomOperatorsCheckbox.checked = !!cfg.add_custom_operators;
    customOperatorsTextInput.value = ui.custom_operators_text || "";
    mobileToleranceInput.value = String(cfg.mobile_time_tolerance_ms ?? 1000);
    outputZonesPathInput.value = cfg.output_zones_file_path || "";
    outputStatsPathInput.value = cfg.output_stats_file_path || "";

    state.columnMapping = { ...(ui.column_mapping || cfg.column_mapping || {}) };

    const presetTimeWindows = ui.time_windows && ui.time_windows.length > 0 ? ui.time_windows : cfg.time_windows || [];
    state.timeWindows = presetTimeWindows.map((tw, index) => toDraftFromTimeWindow(index, tw));

    if (state.preview && preset.inputRadioTech && state.preview.inputRadioTech && preset.inputRadioTech !== state.preview.inputRadioTech) {
      throw new Error(`Preset je pre vstup ${inputRadioTechUiLabel(preset.inputRadioTech)}, ale aktuálny náhľad je ${inputRadioTechUiLabel(state.preview.inputRadioTech)}.`);
    }

    renderCsvList();
    renderMobileLteList();
    renderFilterList();
    renderMappingGrid();
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
    updateDependentUI();
    renderReadiness();
    if (state.inputCsvPaths.length > 0) {
      scheduleAutoLoadPreview();
    }
  }

  async function buildPresetPayload(name: string, id?: string): Promise<{ id?: string; name: string; inputRadioTech: string; config: backend.ProcessingConfig; uiState: PresetUIState }> {
    const cfg = await buildProcessingConfig();
    const uiState: PresetUIState = {
      input_csv_paths: [...state.inputCsvPaths],
      mobile_lte_paths: [...state.mobileLtePaths],
      custom_filter_paths: [...state.customFilterPaths],
      use_auto_filters: useAutoFiltersCheckbox.checked,
      use_additional_filters: useAdditionalFiltersCheckbox.checked,
      enable_time_selector: enableTimeSelectorCheckbox.checked,
      custom_operators_text: customOperatorsTextInput.value,
      column_mapping: { ...(state.columnMapping as Record<string, number>) },
      time_windows: buildConfiguredTimeWindows(state.timeWindows),
    };
    return {
      ...(id ? { id } : {}),
      name,
      inputRadioTech: state.preview?.inputRadioTech || "unknown",
      config: cfg,
      uiState,
    };
  }

  async function saveCurrentPreset(): Promise<void> {
    const name = presetNameInput.value.trim();
    if (!name) {
      showPresetStatus("Zadaj názov presetu.", true);
      return;
    }
    hidePresetStatus();
    try {
      const payload = await buildPresetPayload(name, activePresetId ?? undefined);
      const saved = (await SavePreset(payload)) as ProcessingPreset;
      activePresetId = saved.id;
      await reloadPresets();
      showPresetStatus(`Preset '${saved.name}' bol uložený.`);
      appendLog(`Uložený preset: ${saved.name}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      showPresetStatus(`Uloženie presetu zlyhalo: ${message}`, true);
    }
  }

  async function openPresetDrawer(): Promise<void> {
    presetOverlay.hidden = false;
    updateModalScrollLock();
    hidePresetStatus();
    await reloadPresets();
  }

  function closePresetDrawer(): void {
    presetOverlay.hidden = true;
    updateModalScrollLock();
    hidePresetStatus();
  }

  async function addOneMobileLteFromPicker(): Promise<void> {
    const path = await PickMobileLTECSVFile();
    if (!path) {
      return;
    }
    if (state.mobileLtePaths.includes(path)) {
      appendLog(`LTE súbor už je v zozname: ${path}`);
      return;
    }
    state.mobileLtePaths = [...state.mobileLtePaths, path];
    renderMobileLteList();
    appendLog(`Pridaný LTE CSV: ${path}`);
    renderReadiness();
  }

  async function addMultipleMobileLteFromPicker(): Promise<void> {
    const paths = (await PickMobileLTECSVPaths()) as string[];
    if (!paths || paths.length === 0) {
      return;
    }
    let added = 0;
    for (const p of paths) {
      if (state.mobileLtePaths.includes(p)) {
        continue;
      }
      state.mobileLtePaths = [...state.mobileLtePaths, p];
      added++;
    }
    renderMobileLteList();
    const skipped = paths.length - added;
    appendLog(`Pridané nové LTE CSV: ${added}${skipped > 0 ? ` (preskočené duplicitné: ${skipped})` : ""}`);
    renderReadiness();
  }

  function removeSelectedMobileLtePaths(): void {
    const selected = new Set(Array.from(mobileLteList.selectedOptions).map((opt) => opt.value));
    if (selected.size === 0) {
      return;
    }
    state.mobileLtePaths = state.mobileLtePaths.filter((path) => !selected.has(path));
    renderMobileLteList();
    appendLog(`Odstránené LTE CSV (${selected.size})`);
    renderReadiness();
  }

  function clearMobileLtePaths(): void {
    if (state.mobileLtePaths.length === 0) {
      return;
    }
    const count = state.mobileLtePaths.length;
    state.mobileLtePaths = [];
    renderMobileLteList();
    appendLog(`Vyčistený zoznam LTE CSV (${count})`);
    renderReadiness();
  }

  async function addFilterFiles(): Promise<void> {
    const files = (await PickFilterFiles()) as string[];
    if (!files || files.length === 0) {
      return;
    }
    state.customFilterPaths = dedupePaths([...state.customFilterPaths, ...files]);
    renderFilterList();
    appendLog(`Pridané filtre: ${files.length}`);
  }

  function removeSelectedFilters(): void {
    const selected = new Set(Array.from(filtersList.selectedOptions).map((opt) => opt.value));
    if (selected.size === 0) {
      return;
    }
    state.customFilterPaths = state.customFilterPaths.filter((path) => !selected.has(path));
    renderFilterList();
    appendLog(`Odstránené filtre: ${selected.size}`);
  }

  function clearFilters(): void {
    if (state.customFilterPaths.length === 0) {
      return;
    }
    const count = state.customFilterPaths.length;
    state.customFilterPaths = [];
    renderFilterList();
    appendLog(`Vyčistené filtre (${count})`);
  }

  addCsvBtn.addEventListener("click", () => {
    void addOneCsvFromPicker();
  });
  addCsvMultiBtn.addEventListener("click", () => {
    void addMultipleCsvFromPicker();
  });
  removeCsvBtn.addEventListener("click", removeSelectedCsvPaths);
  clearCsvBtn.addEventListener("click", clearCsvPaths);
  loadPreviewBtn.addEventListener("click", () => {
    void loadPreviewForCurrentCSV().catch(handlePreviewError);
  });
  addMobileLteBtn.addEventListener("click", () => {
    void addOneMobileLteFromPicker();
  });
  addMobileLteMultiBtn.addEventListener("click", () => {
    void addMultipleMobileLteFromPicker();
  });
  removeMobileLteBtn.addEventListener("click", () => {
    removeSelectedMobileLtePaths();
  });
  clearMobileLteBtn.addEventListener("click", () => {
    clearMobileLtePaths();
  });
  addFiltersBtn.addEventListener("click", () => {
    void addFilterFiles();
  });
  removeFilterBtn.addEventListener("click", removeSelectedFilters);
  clearFiltersBtn.addEventListener("click", clearFilters);
  presetsBtn.addEventListener("click", () => {
    void openPresetDrawer();
  });
  presetCloseBtn.addEventListener("click", () => {
    closePresetDrawer();
  });
  presetSaveBtn.addEventListener("click", () => {
    void saveCurrentPreset();
  });
  presetList.addEventListener("click", (event) => {
    const btn = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-preset-action]");
    if (!btn?.dataset.presetAction || !btn.dataset.presetId) {
      return;
    }
    const action = btn.dataset.presetAction;
    const presetId = btn.dataset.presetId;
    void (async () => {
      try {
        if (action === "delete") {
          await DeletePreset(presetId);
          if (activePresetId === presetId) {
            activePresetId = null;
          }
          await reloadPresets();
          showPresetStatus("Preset bol zmazaný.");
          appendLog(`Zmazaný preset: ${presetId}`);
          return;
        }
        const preset = (await LoadPreset(presetId)) as ProcessingPreset;
        applyPresetToUI(preset);
        activePresetId = preset.id;
        presetNameInput.value = preset.name;
        renderPresetList();
        showPresetStatus(`Načítaný preset '${preset.name}'.`);
        appendLog(`Načítaný preset: ${preset.name}`);
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        showPresetStatus(`Operácia s presetom zlyhala: ${message}`, true);
      }
    })();
  });
  presetOverlay.addEventListener("click", (event) => {
    if (event.target === presetOverlay) {
      closePresetDrawer();
    }
  });
  mobileModeCheckbox.addEventListener("change", updateDependentUI);
  pickOutputZonesBtn.addEventListener("click", () => {
    void pickOutputZonesPath();
  });
  pickOutputStatsBtn.addEventListener("click", () => {
    void pickOutputStatsPath();
  });
  useAdditionalFiltersCheckbox.addEventListener("change", updateDependentUI);
  includeEmptyZonesCheckbox.addEventListener("change", updateDependentUI);
  addCustomOperatorsCheckbox.addEventListener("change", updateDependentUI);
  enableTimeSelectorCheckbox.addEventListener("change", () => {
    if (!enableTimeSelectorCheckbox.checked) {
      state.timeWindows = [];
      clearTimeSelectorState();
    }
    updateDependentUI();
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
  });
  addTimeWindowBtn.addEventListener("click", () => {
    state.timeWindows = [...state.timeWindows, buildDefaultTimeWindow(state.timeWindows.length)];
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
  });
  clearTimeWindowsBtn.addEventListener("click", () => {
    state.timeWindows = [];
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
    appendLog("Vymazané časové okná.");
  });
  timeWindowList.addEventListener("click", (event) => {
    const target = event.target as HTMLElement | null;
    const button = target?.closest<HTMLButtonElement>("button[data-remove-window]");
    const windowId = button?.dataset.removeWindow;
    if (windowId) {
      state.timeWindows = state.timeWindows.filter((window) => window.id !== windowId);
      recomputeTimeWindowSelection();
      renderTimeSelector();
      renderResult();
      return;
    }

    const saveButton = target?.closest<HTMLButtonElement>("button[data-save-window]");
    const saveWindowId = saveButton?.dataset.saveWindow;
    if (saveWindowId) {
      const section = saveButton.closest<HTMLElement>("[data-window-section]");
      if (!section) {
        return;
      }
      const startDateInput = section.querySelector<HTMLInputElement>('input[data-time-field="startDate"]');
      const startTimeInput = section.querySelector<HTMLInputElement>('input[data-time-field="startTime"]');
      const endDateInput = section.querySelector<HTMLInputElement>('input[data-time-field="endDate"]');
      const endTimeInput = section.querySelector<HTMLInputElement>('input[data-time-field="endTime"]');
      state.timeWindows = state.timeWindows.map((window) =>
        window.id === saveWindowId
          ? {
              ...window,
              startDateInput: startDateInput?.value ?? "",
              startTimeInput: startTimeInput?.value ?? "",
              endDateInput: endDateInput?.value ?? "",
              endTimeInput: endTimeInput?.value ?? "",
              applied: true,
            }
          : window
      );
      recomputeTimeWindowSelection();
      renderTimeSelector();
      renderResult();
      appendLog("Časové okno uložené.");
    }
  });
  timeWindowList.addEventListener("input", (event) => {
    const target = event.target as HTMLInputElement | null;
    const field = target?.dataset.timeField;
    const windowId = target?.dataset.windowId;
    if (!field || !windowId) {
      return;
    }
    state.timeWindows = state.timeWindows.map((window) =>
      window.id === windowId
        ? {
            ...window,
            [field === "startDate"
              ? "startDateInput"
              : field === "startTime"
                ? "startTimeInput"
                : field === "endDate"
                  ? "endDateInput"
                  : "endTimeInput"]: target.value,
            applied: false,
            startTimestampMS: null,
            endTimestampMS: null,
            excludedRows: 0,
          }
        : window
    );
    const updatedWindow = state.timeWindows.find((window) => window.id === windowId);
    const section = target.closest<HTMLElement>("[data-window-section]");
    if (updatedWindow && section) {
      updateTimeWindowSectionStatus(section, updatedWindow);
    }
    renderTimeSelectorSummaryCards();
    renderResult();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !logOverlay.hidden) {
      closeLogModal();
      return;
    }
    if (event.key === "Escape" && !aboutOverlay.hidden) {
      aboutOverlay.hidden = true;
      updateModalScrollLock();
      return;
    }
    if (event.key === "Escape" && !presetOverlay.hidden) {
      closePresetDrawer();
      return;
    }
  });
  runBtn.addEventListener("click", () => {
    void runProcessing();
  });
  openLogBtn.addEventListener("click", () => {
    openLogModal();
  });
  logCloseBtn.addEventListener("click", () => {
    closeLogModal();
  });
  logOverlay.addEventListener("click", (event) => {
    if (event.target === logOverlay) {
      closeLogModal();
    }
  });
  clearLogBtn.addEventListener("click", () => {
    state.logs = [];
    logOutput.textContent = "Log vyčistený.";
  });
  exportLogBtn.addEventListener("click", () => {
    const text = state.logs.length > 0 ? state.logs.join("\n") : "(prázdny log)";
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `100mscript-log-${formatTimestampForFilename()}.txt`;
    a.click();
    URL.revokeObjectURL(url);
    appendLog("Log exportovaný.");
  });
  resultContent.addEventListener("click", (event) => {
    const btn = (event.target as HTMLElement).closest<HTMLButtonElement>("[data-result-action]");
    if (!btn?.dataset.resultAction) {
      return;
    }
    const action = btn.dataset.resultAction;
    const result = state.result;
    if (!result) {
      return;
    }
    void (async () => {
      try {
        if (action === "open-output") {
          const path = result.zones_file || result.stats_file;
          if (!path) {
            return;
          }
          await OpenContainingFolder(path);
          appendLog("Otvorený priečinok / súbor vo výstupe.");
        } else if (action === "copy-zones" && result.zones_file) {
          await ClipboardSetText(result.zones_file);
          appendLog("Skopírovaná cesta k súboru zón.");
        } else if (action === "copy-stats" && result.stats_file) {
          await ClipboardSetText(result.stats_file);
          appendLog("Skopírovaná cesta k štatistikám.");
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        appendLog(`Chyba: ${message}`);
      }
    })();
  });
  aboutBtn.addEventListener("click", () => {
    void (async () => {
      try {
        const info = (await GetAppInfo()) as main.AppInfo;
        aboutBody.textContent = `${info.productName}\nVerzia ${info.version}\n\nPredvolene sa výstupy ukladajú vedľa vstupného súboru ako _zones.csv a _stats.csv (pri Mobile režime s príponou _mobile v názve). V pravom paneli môžeš zadať vlastné cesty k obom súborom.`;
      } catch {
        aboutBody.textContent = "100mscript\n\nNepodarilo sa načítať informácie o verzii.";
      }
      aboutOverlay.hidden = false;
      updateModalScrollLock();
    })();
  });
  aboutCloseBtn.addEventListener("click", () => {
    aboutOverlay.hidden = true;
    updateModalScrollLock();
  });
  aboutOverlay.addEventListener("click", (event) => {
    if (event.target === aboutOverlay) {
      aboutOverlay.hidden = true;
      updateModalScrollLock();
    }
  });
  function handlePreviewError(err: unknown): void {
    const message = err instanceof Error ? err.message : String(err);
    state.preview = null;
    state.previewError = message;
    state.columnMapping = {};
    clearTimeSelectorState();
    renderPreview();
    renderMappingGrid();
    setStatus("Chyba pri načítaní CSV", "error");
    appendLog(`Chyba pri načítaní CSV: ${message}`);
  }

  renderPreview();
  renderCsvList();
  renderFilterList();
  renderMappingGrid();
  renderResult();
  renderTimeSelector();
  updateDependentUI();
  renderReadiness();
  renderPresetList();
  updateModalScrollLock();
  setStatus("Pripravené", "idle");
}

function qs<T extends Element>(selector: string): T {
  const node = document.querySelector<T>(selector);
  if (!node) {
    throw new Error(`Missing element: ${selector}`);
  }
  return node;
}

function buildDefaultTimeWindow(index: number): TimeWindowDraft {
  return {
    id: `window-${Date.now()}-${index}-${Math.random().toString(36).slice(2, 8)}`,
    startDateInput: "",
    startTimeInput: "",
    endDateInput: "",
    endTimeInput: "",
    startTimestampMS: null,
    endTimestampMS: null,
    applied: false,
    excludedRows: 0,
  };
}

function isCompleteTimeWindow(window: TimeWindowDraft): boolean {
  return window.applied && window.startTimestampMS !== null && window.endTimestampMS !== null && window.startTimestampMS <= window.endTimestampMS;
}

function describeTimeWindow(window: TimeWindowDraft): string {
  if (!window.startDateInput || !window.startTimeInput || !window.endDateInput || !window.endTimeInput) {
    return "Zadaj dátum aj čas pre začiatok aj koniec.";
  }
  if (!window.applied) {
    return "Zmeny čakajú na potvrdenie tlačidlom Použiť interval.";
  }
  if (window.startTimestampMS === null || window.endTimestampMS === null) {
    return "Skontroluj formát zadaného dátumu a času.";
  }
  if (window.startTimestampMS > window.endTimestampMS) {
    return "Koniec musí byť po začiatku.";
  }
  return "Interval je pripravený na vyradenie pri spracovaní.";
}

function resolveDraftTimestamp(dateLabel: string, timeLabel: string): number | null {
  const trimmedDate = dateLabel.trim();
  const trimmedTime = timeLabel.trim();
  if (!trimmedDate || !trimmedTime) {
    return null;
  }
  const normalizedTime = trimmedTime.length === 5 ? `${trimmedTime}:00` : trimmedTime;
  const parsed = new Date(`${trimmedDate}T${normalizedTime}`);
  const value = parsed.getTime();
  return Number.isNaN(value) ? null : value;
}

function buildConfiguredTimeWindows(timeWindows: TimeWindowDraft[]): Array<{ start: string; end: string }> {
  return timeWindows
    .filter((window) => isCompleteTimeWindow(window))
    .map((window) => ({
      start: `${window.startDateInput}T${normalizeTimeInput(window.startTimeInput)}`,
      end: `${window.endDateInput}T${normalizeTimeInput(window.endTimeInput)}`,
    }));
}

function normalizeTimeInput(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length === 5) {
    return `${trimmed}:00`;
  }
  return trimmed;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function timestamp(): string {
  return new Date().toLocaleTimeString("sk-SK", { hour12: false });
}

function formatTimestampForFilename(): string {
  const d = new Date();
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}`;
}

function dedupePaths(paths: string[]): string[] {
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

function parseNumberInput(input: HTMLInputElement, label: string): number {
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

function parseIntegerInput(input: HTMLInputElement, label: string): number {
  const value = parseNumberInput(input, label);
  if (!Number.isInteger(value) || value < 0) {
    throw new Error(`${label} musí byť celé číslo >= 0.`);
  }
  return value;
}

function parseCustomOperatorsText(text: string): backend.CustomOperator[] {
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

function formatPercent(value?: number): string {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "n/a";
  }
  return `${value.toFixed(2)} %`;
}
