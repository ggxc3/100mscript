import "./style.css";
import "./app.css";

import {
  DefaultOutputPaths,
  DiscoverAutoFilterPaths,
  GetAppInfo,
  LoadCSVPreview,
  LoadTimeSelectorData,
  OpenContainingFolder,
  PickFilterFiles,
  PickInputCSVFile,
  PickInputCSVPaths,
  PickMobileLTECSVFile,
  PickMobileLTECSVPaths,
  PickOutputCSVFile,
  RunProcessingWithConfig,
} from "../wailsjs/go/main/App";
import { backend, main } from "../wailsjs/go/models";
import { ClipboardSetText, EventsOn } from "../wailsjs/runtime/runtime";
import {
  buildDefaultTimeWindow,
  buildProcessingPhaseRows,
  buildTimeDateOptions,
  COLUMN_FIELDS,
  computeReadinessItems,
  cssEscape,
  dedupePaths,
  describeTimeWindow,
  escapeHtml,
  fileBasename,
  fileParentDir,
  filterDropdownOptions,
  formatDateTimeLabel,
  formatPercent,
  formatTimestampForFilename,
  getTimeOptionsForDate,
  HIGH_EXCLUSION_RATIO,
  inputRadioTechUiLabel,
  isCompleteTimeWindow,
  labelForTimeStrategy,
  parseCustomOperatorsText,
  parseIntegerInput,
  parseNumberInput,
  pathsMatchPreview,
  phaseStatusIcon,
  pluralizeFiles,
  PROCESSING_PHASE_LABELS,
  renderDropdownField,
  recomputeTimeWindowExclusions,
  sortUniqueNumbers,
  timestamp,
  updateTimeWindowField,
  ZONE_MODES,
  type ColumnKey,
  type CsvLoadStage,
  type DropdownField,
  type PhaseRow,
  type PhaseStatus,
  type Tone,
  type UIState,
} from "./appLogic";

const PREVIEW_AUTOLOAD_MS = 420;

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
    timeSelectorData: null,
    timeDateOptions: [],
    timeWindows: [],
    timeSelectorLoading: false,
    timeSelectorError: "",
    csvLoadDialog: {
      visible: false,
      stage: "preview",
      showTimeStep: true,
      fileCount: 0,
    },
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

      <div id="csvLoadOverlay" class="modal-overlay modal-overlay--blocking" hidden>
        <div
          class="modal-dialog modal-dialog--loading"
          role="dialog"
          aria-modal="true"
          aria-labelledby="csvLoadTitle"
          aria-describedby="csvLoadBody"
        >
          <div class="loading-dialog-kicker">Načítanie</div>
          <h3 id="csvLoadTitle" class="modal-title loading-dialog-title">Pripravujem vstupné dáta</h3>
          <p id="csvLoadBody" class="modal-body loading-dialog-body"></p>
          <div id="csvLoadMeta" class="loading-dialog-meta"></div>
          <div id="csvLoadSteps" class="loading-steps" role="list"></div>
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
  const aboutBtn = qs<HTMLButtonElement>("#aboutBtn");
  const aboutOverlay = qs<HTMLDivElement>("#aboutOverlay");
  const aboutBody = qs<HTMLParagraphElement>("#aboutBody");
  const aboutCloseBtn = qs<HTMLButtonElement>("#aboutCloseBtn");
  const processingPipelineWrap = qs<HTMLDivElement>("#processingPipelineWrap");
  const processingPipeline = qs<HTMLDivElement>("#processingPipeline");
  const openLogBtn = qs<HTMLButtonElement>("#openLogBtn");
  const logOverlay = qs<HTMLDivElement>("#logOverlay");
  const logCloseBtn = qs<HTMLButtonElement>("#logCloseBtn");
  const csvLoadOverlay = qs<HTMLDivElement>("#csvLoadOverlay");
  const csvLoadBody = qs<HTMLParagraphElement>("#csvLoadBody");
  const csvLoadMeta = qs<HTMLDivElement>("#csvLoadMeta");
  const csvLoadSteps = qs<HTMLDivElement>("#csvLoadSteps");

  let previewAutoloadTimer: ReturnType<typeof setTimeout> | null = null;
  let runElapsedTimer: ReturnType<typeof setInterval> | null = null;
  let pendingLoaderExitId: string | null = null;
  let loaderExitClearTimer: ReturnType<typeof setTimeout> | null = null;
  const phaseProgressPercent: Record<string, number> = {};

  function updateModalScrollLock(): void {
    const shouldLock =
      !aboutOverlay.hidden || !logOverlay.hidden || !csvLoadOverlay.hidden;
    document.documentElement.classList.toggle("scroll-locked", shouldLock);
    document.body.classList.toggle("scroll-locked", shouldLock);
  }

  function renderCsvLoadDialog(): void {
    const dialog = state.csvLoadDialog;
    csvLoadOverlay.hidden = !dialog.visible;
    updateModalScrollLock();
    if (!dialog.visible) {
      return;
    }

    csvLoadBody.textContent = dialog.showTimeStep
      ? "Načítavam náhľad CSV a pripravujem časové údaje. Dialóg sa zavrie automaticky po dokončení."
      : "Načítavam náhľad CSV. Dialóg sa zavrie automaticky po dokončení.";
    csvLoadMeta.textContent = `Spracúvam ${dialog.fileCount} ${pluralizeFiles(dialog.fileCount)}.`;

    const steps = [
      {
        label: "Načítanie náhľadu stĺpcov",
        status: dialog.stage === "preview" ? "active" : "done",
      },
      ...(dialog.showTimeStep
        ? [
            {
              label: "Načítanie časových údajov",
              status: dialog.stage === "time" ? "active" : "pending",
            },
          ]
        : []),
    ];

    csvLoadSteps.innerHTML = steps
      .map(
        (step) => `
          <div class="loading-step loading-step--${step.status}" role="listitem">
            <span class="loading-step__icon" aria-hidden="true">${step.status === "done" ? "✓" : step.status === "active" ? "•" : "·"}</span>
            <span class="loading-step__label">${escapeHtml(step.label)}</span>
          </div>`
      )
      .join("");
  }

  function showCsvLoadDialog(fileCount: number, showTimeStep: boolean): void {
    state.csvLoadDialog = {
      visible: true,
      stage: "preview",
      showTimeStep,
      fileCount,
    };
    renderCsvLoadDialog();
  }

  function setCsvLoadDialogStage(stage: CsvLoadStage): void {
    if (!state.csvLoadDialog.visible) {
      return;
    }
    state.csvLoadDialog = {
      ...state.csvLoadDialog,
      showTimeStep: stage === "time" ? true : state.csvLoadDialog.showTimeStep,
      stage,
    };
    renderCsvLoadDialog();
  }

  function hideCsvLoadDialog(): void {
    if (!state.csvLoadDialog.visible) {
      return;
    }
    state.csvLoadDialog = {
      ...state.csvLoadDialog,
      visible: false,
    };
    renderCsvLoadDialog();
  }

  function waitForNextPaint(): Promise<void> {
    return new Promise((resolve) => {
      window.requestAnimationFrame(() => {
        window.setTimeout(resolve, 0);
      });
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
    addTimeWindowBtn.disabled = running || state.timeSelectorLoading || !state.timeSelectorData;
    clearTimeWindowsBtn.disabled = running || state.timeWindows.length === 0;
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
    const { timeWindows, excludedOriginalRows } = recomputeTimeWindowExclusions(
      state.timeSelectorData,
      state.timeWindows
    );
    state.timeWindows = timeWindows;
    state.excludedOriginalRows = excludedOriginalRows;
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

    if (state.previewLoading) {
      timeSelectorInfo.className = "time-selector-info muted";
      timeSelectorInfo.textContent = "Načítavam náhľad CSV a pripravujem časové údaje...";
    } else if (!state.preview) {
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
    renderReadiness();

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
      renderReadiness();
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
    if (state.previewLoading || state.timeSelectorLoading) {
      return;
    }

    const shouldLoadTimeSelector = !pathsMatchPreview(paths, state.preview) || !state.timeSelectorData;
    state.previewLoading = true;
    state.previewError = null;
    showCsvLoadDialog(paths.length, shouldLoadTimeSelector);
    renderPreview();
    renderTimeSelector();
    renderReadiness();
    await waitForNextPaint();

    try {
      appendLog(`Načítavam hlavičku CSV (${paths.length} súborov): ${paths.join(", ")}`);
      const preview = (await LoadCSVPreview(paths)) as main.CSVPreview;
      const previousKey = state.preview
        ? (state.preview.filePaths ?? []).join("\n") || state.preview.filePath || ""
        : "";
      const newKey = (preview.filePaths ?? []).join("\n") || preview.filePath || "";
      state.preview = preview;
      applySuggestedMapping(preview);
      state.previewLoading = false;
      renderPreview();
      renderMappingGrid();
      renderTimeSelector();
      appendLog(
        `Načítané stĺpce (${preview.columns.length}), vstup: ${inputRadioTechUiLabel(preview.inputRadioTech)}, encoding=${preview.encoding}, headerLine=${preview.headerLine + 1}`
      );

      if (newKey !== previousKey || !state.timeSelectorData) {
        setCsvLoadDialogStage("time");
        await waitForNextPaint();
        await loadTimeSelectorForCurrentCSV(paths);
      }
      void refreshOutputPathDefaults();
    } catch (err) {
      state.previewLoading = false;
      throw err;
    } finally {
      state.previewLoading = false;
      hideCsvLoadDialog();
      renderPreview();
      renderTimeSelector();
      renderReadiness();
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
      ...(output_zones_file_path ? { output_zones_file_path } : {}),
      ...(output_stats_file_path ? { output_stats_file_path } : {}),
      mobile_mode_enabled,
      mobile_lte_file_path: mobile_mode_enabled && ltePaths.length > 0 ? ltePaths[0] : "",
      ...(mobile_mode_enabled && ltePaths.length > 1 ? { mobile_lte_file_paths: ltePaths } : {}),
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
      updateModalScrollLock();
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
  renderCsvLoadDialog();
  updateDependentUI();
  renderReadiness();
  updateModalScrollLock();
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
