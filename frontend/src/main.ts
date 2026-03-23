import "./style.css";
import "./app.css";

import {
  DiscoverAutoFilterPaths,
  GetAppInfo,
  LoadCSVPreview,
  LoadTimeSelectorData,
  OpenContainingFolder,
  PickFilterFiles,
  PickInputCSVFile,
  PickInputCSVPaths,
  PickMobileLTECSVFile,
  RunProcessingWithConfig,
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

type TimeValueOption = {
  timeLabel: string;
  timestampMS: number;
};

type TimeDateOption = {
  dateLabel: string;
  times: TimeValueOption[];
};

type TimeWindowDraft = {
  id: string;
  startDateInput: string;
  startTimeInput: string;
  endDateInput: string;
  endTimeInput: string;
  startTimestampMS: number | null;
  endTimestampMS: number | null;
  excludedRows: number;
};

type UIState = {
  preview: main.CSVPreview | null;
  /** Posledná chyba načítania náhľadu (hlavičky); pri úspechu null. */
  previewError: string | null;
  columnMapping: Partial<Record<ColumnKey, number>>;
  inputCsvPaths: string[];
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
const HIGH_EXCLUSION_RATIO = 0.9;

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
  mobileLtePath: string
): ReadinessItem[] {
  const items: ReadinessItem[] = [];
  items.push({
    id: "csv",
    label: "Vstupné CSV",
    ok: paths.length > 0,
    detail: paths.length === 0 ? "Pridaj aspoň jeden súbor." : `${paths.length} v zozname`,
  });
  const previewSync = pathsMatchPreview(paths, state.preview);
  items.push({
    id: "preview",
    label: "Náhľad a časové dáta",
    ok: previewSync && !state.timeSelectorLoading,
    detail: state.timeSelectorLoading
      ? "Načítavam…"
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
  const lte = mobileLtePath.trim();
  const mobileOk = !mobileEnabled || lte.length > 0;
  items.push({
    id: "mobile",
    label: "Mobile režim",
    ok: mobileOk,
    detail: mobileEnabled ? (mobileOk ? "LTE súbor zadaný." : "Vyber LTE CSV.") : "Vypnutý.",
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
    columnMapping: {},
    inputCsvPaths: [],
    customFilterPaths: [],
    running: false,
    statusText: "Pripravené",
    statusTone: "idle",
    logs: [],
    result: null,
    excludedOriginalRows: [],
    timeSelectorData: null,
    timeDateOptions: [],
    timeWindows: [],
    timeSelectorLoading: false,
    timeSelectorError: "",
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

            <div id="mobileFieldsWrap" class="collapsible-block" aria-hidden="true">
              <div class="collapsible-block__inner">
                <div id="mobileFields" class="stack">
                  <label class="field">
                    <span>LTE CSV súbor (iba pre Mobile režim)</span>
                    <div class="inline-row">
                      <input id="mobileLtePath" type="text" placeholder="C:\\cesta\\k\\lte.csv" />
                      <button id="pickMobileLteBtn" class="btn secondary" type="button">Vybrať LTE CSV</button>
                    </div>
                  </label>

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

          <article class="card section-card">
            <div class="section-head">
              <h2>Časové úseky</h2>
            </div>
            <p class="section-note">
              Voliteľné okná vylúčia merania z výpočtu. Počet vylúčených bodov sa prepočíta pri každej zmene.
            </p>
            <div id="timeSelectorInfo" class="time-selector-info muted">Najprv načítaj CSV.</div>
            <div id="timeSelectorSummary" class="time-selector-summary">
              <div><span>Časové riadky</span><strong>0</strong></div>
              <div><span>Aktívne okná</span><strong>0</strong></div>
              <div><span>Vylúčené body</span><strong>0</strong></div>
              <div><span>Pokrytie</span><strong>0 %</strong></div>
            </div>
            <div class="inline-actions time-window-actions">
              <button id="addTimeWindowBtn" class="btn primary" type="button">Pridať časové okno</button>
              <button id="clearTimeWindowsBtn" class="btn ghost" type="button">Vymazať všetky</button>
            </div>
            <div id="timeWindowList" class="time-window-list">
              <div class="time-window-empty muted">Zatiaľ nie je definované žiadne časové okno.</div>
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

            <div class="run-actions">
              <button id="runBtn" class="btn primary" type="button">Spustiť spracovanie</button>
              <button id="openLogBtn" type="button" class="btn btn-toolbar run-actions__log">Technický log</button>
            </div>

            <div id="progressBar" class="progress-bar" aria-hidden="true">
              <div class="progress-fill"></div>
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
  const mobileFieldsWrap = qs<HTMLDivElement>("#mobileFieldsWrap");
  const mobileFields = qs<HTMLDivElement>("#mobileFields");
  const mobileLtePathInput = qs<HTMLInputElement>("#mobileLtePath");
  const pickMobileLteBtn = qs<HTMLButtonElement>("#pickMobileLteBtn");
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
  const readinessPanel = qs<HTMLDivElement>("#readinessPanel");
  const aboutBtn = qs<HTMLButtonElement>("#aboutBtn");
  const aboutOverlay = qs<HTMLDivElement>("#aboutOverlay");
  const aboutBody = qs<HTMLParagraphElement>("#aboutBody");
  const aboutCloseBtn = qs<HTMLButtonElement>("#aboutCloseBtn");
  const processingPipelineWrap = qs<HTMLDivElement>("#processingPipelineWrap");
  const processingPipeline = qs<HTMLDivElement>("#processingPipeline");
  const openLogBtn = qs<HTMLButtonElement>("#openLogBtn");
  const logOverlay = qs<HTMLDivElement>("#logOverlay");
  const logCloseBtn = qs<HTMLButtonElement>("#logCloseBtn");

  let previewAutoloadTimer: ReturnType<typeof setTimeout> | null = null;
  let runElapsedTimer: ReturnType<typeof setInterval> | null = null;
  let pendingLoaderExitId: string | null = null;
  let loaderExitClearTimer: ReturnType<typeof setTimeout> | null = null;
  const phaseProgressPercent: Record<string, number> = {};

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
    return computeReadinessItems(getInputCsvPaths(), state, mobileModeCheckbox.checked, mobileLtePathInput.value).every(
      (item) => item.ok
    );
  }

  function renderReadiness(): void {
    const items = computeReadinessItems(getInputCsvPaths(), state, mobileModeCheckbox.checked, mobileLtePathInput.value);
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
    logOutput.textContent = state.logs.length > 0 ? state.logs.join("\n") : "Pripravené.";
    window.requestAnimationFrame(() => {
      logOutput.scrollTop = logOutput.scrollHeight;
    });
  }

  function closeLogModal(): void {
    logOverlay.hidden = true;
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
    pickMobileLteBtn.disabled = running || !mobileModeCheckbox.checked;
    const additionalFiltersOn = useAdditionalFiltersCheckbox.checked;
    addFiltersBtn.disabled = running || !additionalFiltersOn;
    removeFilterBtn.disabled = running || !additionalFiltersOn;
    clearFiltersBtn.disabled = running || !additionalFiltersOn;
    filtersList.disabled = running || !additionalFiltersOn;
    addTimeWindowBtn.disabled = running || state.timeSelectorLoading || !state.timeSelectorData;
    clearTimeWindowsBtn.disabled = running || state.timeWindows.length === 0;
    progressBar.classList.toggle("is-running", running);
    renderReadiness();
  }

  function renderPreview(): void {
    updateLoadPreviewButtonLabel();
    if (state.previewError) {
      csvPreviewStatus.hidden = false;
      csvPreviewStatus.className = "csv-preview-inline";
      csvPreviewStatus.innerHTML = `<span class="csv-preview-inline__err">Chyba: ${escapeHtml(state.previewError)}</span>`;
      renderReadiness();
      return;
    }
    if (!state.preview) {
      csvPreviewStatus.hidden = true;
      csvPreviewStatus.className = "csv-preview-inline";
      csvPreviewStatus.textContent = "";
      renderReadiness();
      return;
    }
    csvPreviewStatus.hidden = false;
    csvPreviewStatus.className = "csv-preview-inline";
    csvPreviewStatus.innerHTML = `<span class="csv-preview-inline__ok">Hlavička CSV načítaná úspešne.</span>`;
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

  function getInputCsvPaths(): string[] {
    return [...state.inputCsvPaths];
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
  }

  function invalidateCsvPreviewAfterListChange(): void {
    state.preview = null;
    state.previewError = null;
    state.columnMapping = {};
    clearTimeSelectorState();
    renderPreview();
    renderMappingGrid();
    renderResult();
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
        <div><span>Vylúčené riadky</span><strong>${String(state.excludedOriginalRows.length)}</strong></div>
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
    const timedRows = state.timeSelectorData?.rows ?? [];
    if (!state.timeSelectorData) {
      state.excludedOriginalRows = [];
      state.timeWindows = state.timeWindows.map((window) => ({ ...window, excludedRows: 0 }));
      return;
    }

    const excludedRows = new Set<number>();
    state.timeWindows = state.timeWindows.map((window) => {
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

    state.excludedOriginalRows = sortUniqueNumbers(Array.from(excludedRows));
  }

  function renderTimeSelector(): void {
    const data = state.timeSelectorData;
    const timedRows = data?.timed_rows ?? 0;
    const totalRows = data?.total_rows ?? 0;
    const activeWindows = state.timeWindows.filter((window) => isCompleteTimeWindow(window)).length;
    const excludedRows = state.excludedOriginalRows.length;
    const coveredPercent = timedRows > 0 ? ((excludedRows / timedRows) * 100).toFixed(1) : "0.0";

    timeSelectorSummary.innerHTML = `
      <div><span>Časové riadky</span><strong>${String(timedRows)}</strong></div>
      <div><span>Aktívne okná</span><strong>${String(activeWindows)}</strong></div>
      <div><span>Vylúčené body</span><strong>${String(excludedRows)}</strong></div>
      <div><span>Pokrytie</span><strong>${coveredPercent} %</strong></div>
    `;

    if (!state.preview) {
      timeSelectorInfo.className = "time-selector-info muted";
      timeSelectorInfo.textContent = "Najprv načítaj CSV.";
    } else if (state.timeSelectorLoading) {
      timeSelectorInfo.className = "time-selector-info muted";
      timeSelectorInfo.textContent = "Načítavam časové údaje zo súboru...";
    } else if (state.timeSelectorError) {
      timeSelectorInfo.className = "time-selector-info warning";
      timeSelectorInfo.textContent = state.timeSelectorError;
    } else if (data) {
      timeSelectorInfo.className = "time-selector-info";
      timeSelectorInfo.textContent =
        `${formatDateTimeLabel(data.min_time_ms)} – ${formatDateTimeLabel(data.max_time_ms)} ` +
        `• ${timedRows}/${totalRows} riadkov s časom • zdroj: ${labelForTimeStrategy(data.strategy)}`;
    } else {
      timeSelectorInfo.className = "time-selector-info muted";
      timeSelectorInfo.textContent = "Časové údaje zatiaľ nie sú načítané.";
    }

    addTimeWindowBtn.disabled = state.running || state.timeSelectorLoading || !data;
    clearTimeWindowsBtn.disabled = state.running || state.timeWindows.length === 0;

    if (!data) {
      const message = state.timeSelectorError || "Časový selector bude dostupný po načítaní CSV.";
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
        const countTone = window.excludedRows > 0 ? "count-active" : "count-idle";
        const startDateDropdown = renderDropdownField({
          window,
          field: "startDate",
          label: "Dátum",
          value: window.startDateInput,
          placeholder: "Vyber dátum",
          options: filterDropdownOptions(
            state.timeDateOptions.map((option) => option.dateLabel),
            state.activeDropdown?.windowId === window.id && state.activeDropdown.field === "startDate"
              ? state.activeDropdown.query
              : ""
          ),
          state,
        });
        const startTimeDropdown = renderDropdownField({
          window,
          field: "startTime",
          label: "Čas",
          value: window.startTimeInput,
          placeholder: "Vyber čas",
          options: filterDropdownOptions(
            getTimeOptionsForDate(state.timeDateOptions, window.startDateInput).map((option) => option.timeLabel),
            state.activeDropdown?.windowId === window.id && state.activeDropdown.field === "startTime"
              ? state.activeDropdown.query
              : ""
          ),
          state,
          disabled: !window.startDateInput,
        });
        const endDateDropdown = renderDropdownField({
          window,
          field: "endDate",
          label: "Dátum",
          value: window.endDateInput,
          placeholder: "Vyber dátum",
          options: filterDropdownOptions(
            state.timeDateOptions.map((option) => option.dateLabel),
            state.activeDropdown?.windowId === window.id && state.activeDropdown.field === "endDate"
              ? state.activeDropdown.query
              : ""
          ),
          state,
        });
        const endTimeDropdown = renderDropdownField({
          window,
          field: "endTime",
          label: "Čas",
          value: window.endTimeInput,
          placeholder: "Vyber čas",
          options: filterDropdownOptions(
            getTimeOptionsForDate(state.timeDateOptions, window.endDateInput).map((option) => option.timeLabel),
            state.activeDropdown?.windowId === window.id && state.activeDropdown.field === "endTime"
              ? state.activeDropdown.query
              : ""
          ),
          state,
          disabled: !window.endDateInput,
        });
        return `
          <section class="time-window-item">
            <div class="time-window-item-head">
              <div>
                <strong>Okno ${index + 1}</strong>
                <div class="time-window-status">${escapeHtml(statusText)}</div>
              </div>
              <span class="time-window-count ${countTone}">${String(window.excludedRows)} bodov</span>
            </div>
            <div class="time-window-fields">
              <div class="time-window-column">
                <div class="time-window-column-title">Od</div>
                <div class="time-window-subfields">
                  ${startDateDropdown}
                  ${startTimeDropdown}
                </div>
                <small class="time-window-hint">Najprv vyber dátum, potom čas.</small>
              </div>
              <div class="time-window-column">
                <div class="time-window-column-title">Do</div>
                <div class="time-window-subfields">
                  ${endDateDropdown}
                  ${endTimeDropdown}
                </div>
                <small class="time-window-hint">Dropdown času sa filtruje podľa dátumu.</small>
              </div>
            </div>
            <div class="time-window-foot">
              <span class="muted">Rozsah súboru: ${escapeHtml(formatDateTimeLabel(data.min_time_ms))} – ${escapeHtml(formatDateTimeLabel(data.max_time_ms))}</span>
              <button class="btn danger small-btn" type="button" data-remove-window="${escapeHtml(window.id)}">Odstrániť</button>
            </div>
          </section>
        `;
      })
      .join("");
    renderReadiness();
  }

  function clearTimeSelectorState(): void {
    state.timeSelectorData = null;
    state.timeDateOptions = [];
    state.timeWindows = [];
    state.timeSelectorLoading = false;
    state.timeSelectorError = "";
    state.activeDropdown = null;
    state.excludedOriginalRows = [];
    renderTimeSelector();
    renderResult();
    renderReadiness();
  }

  async function loadTimeSelectorForCurrentCSV(paths: string[]): Promise<void> {
    state.timeSelectorLoading = true;
    state.timeSelectorError = "";
    state.timeSelectorData = null;
    state.timeDateOptions = [];
    state.timeWindows = [];
    state.activeDropdown = null;
    state.excludedOriginalRows = [];
    renderTimeSelector();
    renderResult();

    appendLog("Načítavam časové údaje pre výber úsekov");
    try {
      const data = (await LoadTimeSelectorData(paths)) as backend.TimeSelectorData;
      state.timeSelectorData = data;
      state.timeDateOptions = buildTimeDateOptions(data.rows ?? []);
      appendLog(`Časové údaje pripravené (${data.timed_rows}/${data.total_rows} riadkov, ${labelForTimeStrategy(data.strategy)})`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      state.timeSelectorError = `Časové úseky nie sú dostupné: ${message}`;
      appendLog(state.timeSelectorError);
    } finally {
      state.timeSelectorLoading = false;
      recomputeTimeWindowSelection();
      renderTimeSelector();
      renderResult();
    }
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

  function updateDependentUI(): void {
    const mobileEnabled = mobileModeCheckbox.checked;
    mobileFieldsWrap.classList.toggle("is-open", mobileEnabled);
    mobileFieldsWrap.setAttribute("aria-hidden", mobileEnabled ? "false" : "true");
    mobileFields.querySelectorAll<HTMLInputElement | HTMLButtonElement>("input,button").forEach((el) => {
      el.disabled = !mobileEnabled || state.running;
    });

    const additionalFiltersEnabled = useAdditionalFiltersCheckbox.checked;
    extraFiltersWrap.classList.toggle("is-open", additionalFiltersEnabled);
    extraFiltersWrap.setAttribute("aria-hidden", additionalFiltersEnabled ? "false" : "true");

    const allowCustomOperators = includeEmptyZonesCheckbox.checked;
    customOperatorsPanel.classList.toggle("disabled-panel", !allowCustomOperators);
    addCustomOperatorsCheckbox.disabled = !allowCustomOperators || state.running;
    customOperatorsTextInput.disabled = !allowCustomOperators || !addCustomOperatorsCheckbox.checked || state.running;
    if (!allowCustomOperators) {
      addCustomOperatorsCheckbox.checked = false;
      customOperatorsTextInput.value = "";
    }

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

    state.previewError = null;
    appendLog(`Načítavam hlavičku CSV (${paths.length} súborov): ${paths.join(", ")}`);
    const preview = (await LoadCSVPreview(paths)) as main.CSVPreview;
    const previousKey = state.preview
      ? (state.preview.filePaths ?? []).join("\n") || state.preview.filePath || ""
      : "";
    const newKey = (preview.filePaths ?? []).join("\n") || preview.filePath || "";
    state.preview = preview;
    applySuggestedMapping(preview);
    renderPreview();
    renderMappingGrid();
    appendLog(`Načítané stĺpce (${preview.columns.length}), encoding=${preview.encoding}, headerLine=${preview.headerLine + 1}`);

    if (newKey !== previousKey || !state.timeSelectorData) {
      await loadTimeSelectorForCurrentCSV(paths);
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
    const mobile_lte_file_path = mobileLtePathInput.value.trim();
    if (mobile_mode_enabled && !mobile_lte_file_path) {
      throw new Error("Pre Mobile režim vyber LTE CSV súbor.");
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
      excluded_original_rows: sortUniqueNumbers(state.excludedOriginalRows),
      zone_mode: zoneModeSelect.value || "segments",
      zone_size_m,
      rsrp_threshold,
      sinr_threshold,
      include_empty_zones,
      add_custom_operators,
      custom_operators,
      filter_paths,
      output_suffix: "",
      mobile_mode_enabled,
      mobile_lte_file_path: mobile_mode_enabled ? mobile_lte_file_path : "",
      mobile_time_tolerance_ms,
      mobile_require_nr_yes: false,
      mobile_nr_column_name: "5G NR",
      progress_enabled: false,
    } as backend.ProcessingConfig;
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

    const timed = state.timeSelectorData?.timed_rows ?? 0;
    const excluded = state.excludedOriginalRows.length;
    if (timed > 0 && excluded > 0 && excluded / timed > HIGH_EXCLUSION_RATIO) {
      const ok = window.confirm(
        `Vylúčených je viac ako ${Math.round(HIGH_EXCLUSION_RATIO * 100)} % časovo označených riadkov. Naozaj pokračovať?`
      );
      if (!ok) {
        appendLog("Spustenie zrušené (vysoké pokrytie vylúčením).");
        return;
      }
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
        `Konfigurácia: mode=${cfg.zone_mode}, zone=${cfg.zone_size_m}m, filters=${cfg.filter_paths === undefined ? "auto" : cfg.filter_paths.length}, mobile=${cfg.mobile_mode_enabled}, excluded=${cfg.excluded_original_rows.length}`
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
        `Hotovo (zóny=${result.unique_zones}, operátori=${result.unique_operators}, riadky=${result.total_zone_rows}, vylúčené=${state.excludedOriginalRows.length})`
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

  async function pickMobileLTE(): Promise<void> {
    const path = await PickMobileLTECSVFile();
    if (!path) {
      return;
    }
    mobileLtePathInput.value = path;
    appendLog(`Vybraný LTE CSV: ${path}`);
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
  pickMobileLteBtn.addEventListener("click", () => {
    void pickMobileLTE();
  });
  addFiltersBtn.addEventListener("click", () => {
    void addFilterFiles();
  });
  removeFilterBtn.addEventListener("click", removeSelectedFilters);
  clearFiltersBtn.addEventListener("click", clearFilters);
  mobileModeCheckbox.addEventListener("change", updateDependentUI);
  useAdditionalFiltersCheckbox.addEventListener("change", updateDependentUI);
  includeEmptyZonesCheckbox.addEventListener("change", updateDependentUI);
  addCustomOperatorsCheckbox.addEventListener("change", updateDependentUI);
  addTimeWindowBtn.addEventListener("click", () => {
    if (!state.timeSelectorData) {
      return;
    }
    state.timeWindows = [...state.timeWindows, buildDefaultTimeWindow(state.timeDateOptions, state.timeWindows.length)];
    state.activeDropdown = null;
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
  });
  clearTimeWindowsBtn.addEventListener("click", () => {
    const removed = state.excludedOriginalRows.length;
    state.timeWindows = [];
    state.activeDropdown = null;
    recomputeTimeWindowSelection();
    renderTimeSelector();
    renderResult();
    if (removed > 0) {
      appendLog(`Vymazané časové okná (${removed} vylúčených riadkov)`);
    }
  });
  timeWindowList.addEventListener("input", (event) => {
    const target = event.target as HTMLInputElement | null;
    const windowId = target?.dataset.windowId;
    const field = target?.dataset.dropdownSearch as DropdownField | undefined;
    if (!target || !windowId || !field) {
      return;
    }
    if (!state.activeDropdown || state.activeDropdown.windowId !== windowId || state.activeDropdown.field !== field) {
      return;
    }
    state.activeDropdown = {
      ...state.activeDropdown,
      query: target.value,
    };
    renderTimeSelector();
    focusActiveDropdownSearch();
  });
  timeWindowList.addEventListener("click", (event) => {
    const target = event.target as HTMLElement | null;
    const button = target?.closest<HTMLButtonElement>("button[data-remove-window]");
    const windowId = button?.dataset.removeWindow;
    if (windowId) {
      state.timeWindows = state.timeWindows.filter((window) => window.id !== windowId);
      state.activeDropdown = null;
      recomputeTimeWindowSelection();
      renderTimeSelector();
      renderResult();
      return;
    }

    const toggleButton = target?.closest<HTMLButtonElement>("button[data-open-dropdown]");
    const dropdownWindowId = toggleButton?.dataset.windowId;
    const dropdownField = toggleButton?.dataset.field as DropdownField | undefined;
    if (dropdownWindowId && dropdownField) {
      if (
        state.activeDropdown &&
        state.activeDropdown.windowId === dropdownWindowId &&
        state.activeDropdown.field === dropdownField
      ) {
        state.activeDropdown = null;
      } else {
        state.activeDropdown = {
          windowId: dropdownWindowId,
          field: dropdownField,
          query: "",
        };
      }
      renderTimeSelector();
      focusActiveDropdownSearch();
      return;
    }

    const optionButton = target?.closest<HTMLButtonElement>("button[data-select-option]");
    const optionWindowId = optionButton?.dataset.windowId;
    const optionField = optionButton?.dataset.field as DropdownField | undefined;
    const optionValue = optionButton?.dataset.optionValue ?? "";
    if (optionWindowId && optionField) {
      state.timeWindows = state.timeWindows.map((window) =>
        window.id === optionWindowId ? updateTimeWindowField(window, optionField, optionValue, state.timeDateOptions) : window
      );
      if (optionField === "startDate") {
        state.activeDropdown = { windowId: optionWindowId, field: "startTime", query: "" };
      } else if (optionField === "endDate") {
        state.activeDropdown = { windowId: optionWindowId, field: "endTime", query: "" };
      } else {
        state.activeDropdown = null;
      }
      recomputeTimeWindowSelection();
      renderTimeSelector();
      focusActiveDropdownSearch();
      renderResult();
    }
  });
  document.addEventListener("click", (event) => {
    const target = event.target as HTMLElement | null;
    if (!target?.closest(".dropdown-field")) {
      if (state.activeDropdown) {
        state.activeDropdown = null;
        renderTimeSelector();
      }
    }
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !logOverlay.hidden) {
      closeLogModal();
      return;
    }
    if (event.key === "Escape" && !aboutOverlay.hidden) {
      aboutOverlay.hidden = true;
      return;
    }
    if (event.key === "Escape" && state.activeDropdown) {
      state.activeDropdown = null;
      renderTimeSelector();
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
  mobileLtePathInput.addEventListener("input", () => {
    renderReadiness();
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
        aboutBody.textContent = `${info.productName}\nVerzia ${info.version}\n\nVýstupy sa ukladajú vedľa vstupného súboru ako súbory _zones.csv a _stats.csv.`;
      } catch {
        aboutBody.textContent = "100mscript\n\nNepodarilo sa načítať informácie o verzii.";
      }
      aboutOverlay.hidden = false;
    })();
  });
  aboutCloseBtn.addEventListener("click", () => {
    aboutOverlay.hidden = true;
  });
  aboutOverlay.addEventListener("click", (event) => {
    if (event.target === aboutOverlay) {
      aboutOverlay.hidden = true;
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
  setStatus("Pripravené", "idle");

  function focusActiveDropdownSearch(): void {
    if (!state.activeDropdown) {
      return;
    }
    const selector = `input[data-window-id="${cssEscape(state.activeDropdown.windowId)}"][data-dropdown-search="${state.activeDropdown.field}"]`;
    window.requestAnimationFrame(() => {
      const input = document.querySelector<HTMLInputElement>(selector);
      if (!input) {
        return;
      }
      input.focus();
      const length = input.value.length;
      input.setSelectionRange(length, length);
    });
  }
}

function qs<T extends Element>(selector: string): T {
  const node = document.querySelector<T>(selector);
  if (!node) {
    throw new Error(`Missing element: ${selector}`);
  }
  return node;
}

function cssEscape(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
    return CSS.escape(value);
  }
  return value.replaceAll('"', '\\"');
}

function renderDropdownField(params: {
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

function filterDropdownOptions(options: string[], query: string): string[] {
  const trimmed = query.trim().toLocaleLowerCase("sk-SK");
  if (!trimmed) {
    return options;
  }
  return options.filter((option) => option.toLocaleLowerCase("sk-SK").includes(trimmed));
}

function buildDefaultTimeWindow(dateOptions: TimeDateOption[], index: number): TimeWindowDraft {
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

function isCompleteTimeWindow(window: TimeWindowDraft): boolean {
  return window.startTimestampMS !== null && window.endTimestampMS !== null && window.startTimestampMS <= window.endTimestampMS;
}

function describeTimeWindow(window: TimeWindowDraft): string {
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

function buildTimeDateOptions(rows: backend.TimeSelectorRow[]): TimeDateOption[] {
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

function getTimeOptionsForDate(dateOptions: TimeDateOption[], dateLabel: string): TimeValueOption[] {
  const trimmed = dateLabel.trim();
  if (!trimmed) {
    return [];
  }
  return dateOptions.find((option) => option.dateLabel === trimmed)?.times ?? [];
}

function flattenTimeDateOptions(dateOptions: TimeDateOption[]): Array<{ dateLabel: string; timeLabel: string; timestampMS: number }> {
  return dateOptions.flatMap((dateOption) =>
    dateOption.times.map((timeOption) => ({
      dateLabel: dateOption.dateLabel,
      timeLabel: timeOption.timeLabel,
      timestampMS: timeOption.timestampMS,
    }))
  );
}

function updateTimeWindowField(
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

function resolveTimeWindowTimestamp(dateLabel: string, timeLabel: string, dateOptions: TimeDateOption[]): number | null {
  const trimmedDate = dateLabel.trim();
  const trimmedTime = timeLabel.trim();
  if (!trimmedDate || !trimmedTime) {
    return null;
  }
  const dateOption = dateOptions.find((option) => option.dateLabel === trimmedDate);
  const timeOption = dateOption?.times.find((option) => option.timeLabel === trimmedTime);
  return timeOption ? timeOption.timestampMS : null;
}

function formatDateLabel(timestampMS: number): string {
  return formatDateTimeParts(timestampMS).dateLabel;
}

function formatTimeLabel(timestampMS: number): string {
  return formatDateTimeParts(timestampMS).timeLabel;
}

function formatDateTimeLabel(timestampMS: number): string {
  const parts = formatDateTimeParts(timestampMS);
  return `${parts.dateLabel} ${parts.timeLabel}`;
}

function formatDateTimeParts(timestampMS: number): { dateLabel: string; timeLabel: string } {
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

function labelForTimeStrategy(strategy?: string): string {
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

function sortUniqueNumbers(values: number[]): number[] {
  const out = Array.from(new Set(values.filter((value) => Number.isFinite(value))));
  out.sort((a, b) => a - b);
  return out;
}
