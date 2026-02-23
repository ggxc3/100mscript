import "./style.css";
import "./app.css";

import {
  DiscoverAutoFilterPaths,
  LoadCSVPreview,
  PickFilterFiles,
  PickInputCSVFile,
  PickMobileLTECSVFile,
  RunProcessingWithConfig,
} from "../wailsjs/go/main/App";
import { backend, main } from "../wailsjs/go/models";

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

type UIState = {
  preview: main.CSVPreview | null;
  columnMapping: Partial<Record<ColumnKey, number>>;
  customFilterPaths: string[];
  running: boolean;
  statusText: string;
  statusTone: Tone;
  logs: string[];
  result: backend.ProcessingResult | null;
};

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

const ZONE_MODES = [
  { value: "center", label: "Štvorcové zóny (stred)" },
  { value: "original", label: "Štvorcové zóny (prvý bod v zóne)" },
  { value: "segments", label: "Úseky po trase" },
];

const state: UIState = {
  preview: null,
  columnMapping: {},
  customFilterPaths: [],
  running: false,
  statusText: "Pripravené",
  statusTone: "idle",
  logs: [],
  result: null,
};

const app = document.querySelector<HTMLDivElement>("#app");
if (!app) {
  throw new Error("App root not found");
}

app.innerHTML = `
  <main class="desktop-shell">
    <header class="topbar card">
      <div>
        <p class="eyebrow">100mscript Desktop</p>
        <h1>CSV spracovanie, filtre a štatistiky</h1>
        <p class="lede">Natívny Go backend cez Wails. Žiadny Python bridge, žiadna konzolová medzivrstva.</p>
      </div>
      <div class="status-stack">
        <div id="statusChip" class="status-chip idle">READY</div>
        <div id="statusText" class="status-text">Pripravené</div>
      </div>
    </header>

    <section class="content-grid">
      <section class="left-column">
        <article class="card section-card">
          <div class="section-head">
            <h2>Vstupné dáta</h2>
            <span class="tag">CSV</span>
          </div>

          <label class="field">
            <span>CSV súbor</span>
            <div class="inline-row">
              <input id="csvPath" type="text" placeholder="/cesta/k/suboru.csv" />
              <button id="pickCsvBtn" class="btn secondary" type="button">Vybrať CSV</button>
              <button id="loadPreviewBtn" class="btn ghost" type="button">Načítať stĺpce</button>
            </div>
          </label>

          <div class="preview-box">
            <div class="preview-head">
              <strong>Auto-detekcia stĺpcov</strong>
              <span id="previewMeta" class="muted">Čaká na súbor</span>
            </div>
            <div id="previewColumns" class="columns-list muted">Zatiaľ nenačítané.</div>
          </div>

          <label class="check-row">
            <input id="mobileMode" type="checkbox" />
            <span>Mobile režim (synchronizácia 5G NR cez LTE súbor)</span>
          </label>

          <div id="mobileFields" class="stack disabled-panel">
            <label class="field">
              <span>LTE CSV súbor (iba pre Mobile režim)</span>
              <div class="inline-row">
                <input id="mobileLtePath" type="text" placeholder="/cesta/k/lte.csv" />
                <button id="pickMobileLteBtn" class="btn secondary" type="button">Vybrať LTE CSV</button>
              </div>
            </label>

            <div class="triple-grid">
              <label class="field">
                <span>Tolerancia času (ms)</span>
                <input id="mobileTolerance" type="number" min="0" step="1" value="1000" />
              </label>
              <label class="field">
                <span>Názov NR stĺpca</span>
                <input id="mobileNrColumnName" type="text" value="5G NR" />
              </label>
              <label class="check-row compact-check">
                <input id="mobileRequireNrYes" type="checkbox" checked />
                <span>Vyžadovať hodnotu NR = YES</span>
              </label>
            </div>
          </div>

          <label class="check-row">
            <input id="useAutoFilters" type="checkbox" checked />
            <span>Použiť automatické filtre z priečinkov <code>filters/</code> a <code>filtre_5G/</code></span>
          </label>

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
        </article>

        <article class="card section-card">
          <div class="section-head">
            <h2>Nastavenia spracovania</h2>
            <span class="tag">Backend</span>
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
            <label class="check-row">
              <input id="addCustomOperators" type="checkbox" />
              <span>Pridať vlastných operátorov</span>
            </label>
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
            <span class="tag">Auto</span>
          </div>
          <p class="section-note">Po načítaní CSV sa ponúknu stĺpce a predvyplní sa detekované mapovanie.</p>
          <div id="mappingGrid" class="mapping-grid"></div>
        </article>
      </section>

      <section class="right-column">
        <article class="card section-card sticky-panel">
          <div class="section-head">
            <h2>Spustenie a priebeh</h2>
            <span id="runStateBadge" class="tag muted-tag">idle</span>
          </div>

          <div class="run-actions">
            <button id="runBtn" class="btn primary" type="button">Spustiť spracovanie</button>
            <button id="clearLogBtn" class="btn ghost" type="button">Vyčistiť log</button>
          </div>

          <div id="progressBar" class="progress-bar" aria-hidden="true">
            <div class="progress-fill"></div>
          </div>

          <div class="result-box">
            <div class="preview-head">
              <strong>Výsledok</strong>
            </div>
            <div id="resultContent" class="result-grid muted">Zatiaľ nebolo spustené spracovanie.</div>
          </div>

          <div class="log-box">
            <div class="log-head">
              <strong>Log</strong>
            </div>
            <pre id="logOutput" class="log-output">Pripravené.</pre>
          </div>
        </article>
      </section>
    </section>
  </main>
`;

function qs<T extends Element>(selector: string): T {
  const node = document.querySelector<T>(selector);
  if (!node) {
    throw new Error(`Missing element: ${selector}`);
  }
  return node;
}

const csvPathInput = qs<HTMLInputElement>("#csvPath");
const pickCsvBtn = qs<HTMLButtonElement>("#pickCsvBtn");
const loadPreviewBtn = qs<HTMLButtonElement>("#loadPreviewBtn");
const previewMeta = qs<HTMLSpanElement>("#previewMeta");
const previewColumns = qs<HTMLDivElement>("#previewColumns");
const mobileModeCheckbox = qs<HTMLInputElement>("#mobileMode");
const mobileFields = qs<HTMLDivElement>("#mobileFields");
const mobileLtePathInput = qs<HTMLInputElement>("#mobileLtePath");
const pickMobileLteBtn = qs<HTMLButtonElement>("#pickMobileLteBtn");
const mobileToleranceInput = qs<HTMLInputElement>("#mobileTolerance");
const mobileNrColumnNameInput = qs<HTMLInputElement>("#mobileNrColumnName");
const mobileRequireNrYesCheckbox = qs<HTMLInputElement>("#mobileRequireNrYes");
const useAutoFiltersCheckbox = qs<HTMLInputElement>("#useAutoFilters");
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
const mappingGrid = qs<HTMLDivElement>("#mappingGrid");
const runBtn = qs<HTMLButtonElement>("#runBtn");
const clearLogBtn = qs<HTMLButtonElement>("#clearLogBtn");
const progressBar = qs<HTMLDivElement>("#progressBar");
const resultContent = qs<HTMLDivElement>("#resultContent");
const logOutput = qs<HTMLPreElement>("#logOutput");
const statusChip = qs<HTMLDivElement>("#statusChip");
const statusText = qs<HTMLDivElement>("#statusText");
const runStateBadge = qs<HTMLSpanElement>("#runStateBadge");

ZONE_MODES.forEach((mode) => {
  const opt = document.createElement("option");
  opt.value = mode.value;
  opt.textContent = mode.label;
  zoneModeSelect.appendChild(opt);
});
zoneModeSelect.value = "center";

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
  statusText.textContent = text;
  statusChip.className = `status-chip ${tone}`;
  statusChip.textContent =
    tone === "running" ? "RUNNING" : tone === "success" ? "DONE" : tone === "error" ? "ERROR" : "READY";
  runStateBadge.className = `tag ${tone === "idle" ? "muted-tag" : `${tone}-tag`}`;
  runStateBadge.textContent = tone;
}

function setRunning(running: boolean): void {
  state.running = running;
  runBtn.disabled = running;
  pickCsvBtn.disabled = running;
  loadPreviewBtn.disabled = running;
  pickMobileLteBtn.disabled = running || !mobileModeCheckbox.checked;
  addFiltersBtn.disabled = running;
  removeFilterBtn.disabled = running;
  clearFiltersBtn.disabled = running;
  progressBar.classList.toggle("is-running", running);
}

function renderPreview(): void {
  if (!state.preview) {
    previewMeta.textContent = "Čaká na súbor";
    previewColumns.innerHTML = `<span class="muted">Zatiaľ nenačítané.</span>`;
    return;
  }
  const p = state.preview;
  previewMeta.textContent = `Encoding: ${p.encoding} | Hlavička: riadok ${p.headerLine + 1}`;
  previewColumns.innerHTML = p.columns
    .map((col, idx) => `<span class="col-pill">${idx}: ${escapeHtml(col)}</span>`)
    .join("");
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

function renderResult(): void {
  if (!state.result) {
    resultContent.innerHTML = "Zatiaľ nebolo spustené spracovanie.";
    resultContent.classList.add("muted");
    return;
  }
  const r = state.result;
  resultContent.classList.remove("muted");
  resultContent.innerHTML = `
    <div><span>Zóny CSV</span><strong>${escapeHtml(r.zones_file ?? "")}</strong></div>
    <div><span>Štatistiky CSV</span><strong>${escapeHtml(r.stats_file ?? "")}</strong></div>
    <div><span>Unikátne zóny</span><strong>${String(r.unique_zones ?? 0)}</strong></div>
    <div><span>Unikátni operátori</span><strong>${String(r.unique_operators ?? 0)}</strong></div>
    <div><span>Riadky zón</span><strong>${String(r.total_zone_rows ?? 0)}</strong></div>
    <div><span>Pokrytie</span><strong>${formatPercent(r.coverage_percent)}</strong></div>
  `;
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
    return;
  }

  const options = state.preview.columns
    .map((col, idx) => `<option value="${idx}">${idx}: ${escapeHtml(col)}</option>`)
    .join("");

  mappingGrid.innerHTML = COLUMN_FIELDS.map(({ key, label }) => {
    const selected = state.columnMapping[key];
    const selectedAttr = (idx: number): string => (selected === idx ? " selected" : "");
    const optionsWithSelected = state.preview!.columns
      .map((col, idx) => `<option value="${idx}"${selectedAttr(idx)}>${idx}: ${escapeHtml(col)}</option>`)
      .join("");
    return `
      <label class="field mapping-field">
        <span>${label}</span>
        <select data-column-key="${key}">
          <option value="">-- vyber stĺpec --</option>
          ${optionsWithSelected || options}
        </select>
      </label>`;
  }).join("");

  mappingGrid.querySelectorAll<HTMLSelectElement>("select[data-column-key]").forEach((select) => {
    select.addEventListener("change", () => {
      const key = select.dataset.columnKey as ColumnKey;
      const raw = select.value;
      if (raw === "") {
        delete state.columnMapping[key];
        return;
      }
      state.columnMapping[key] = Number(raw);
    });
  });
}

function updateDependentUI(): void {
  const mobileEnabled = mobileModeCheckbox.checked;
  mobileFields.classList.toggle("disabled-panel", !mobileEnabled);
  mobileFields.querySelectorAll<HTMLInputElement | HTMLButtonElement>("input,button").forEach((el) => {
    if (el === mobileRequireNrYesCheckbox) {
      el.disabled = !mobileEnabled;
      return;
    }
    el.disabled = !mobileEnabled || state.running;
  });

  const allowCustomOperators = includeEmptyZonesCheckbox.checked;
  customOperatorsPanel.classList.toggle("disabled-panel", !allowCustomOperators);
  addCustomOperatorsCheckbox.disabled = !allowCustomOperators || state.running;
  customOperatorsTextInput.disabled = !allowCustomOperators || !addCustomOperatorsCheckbox.checked || state.running;
  if (!allowCustomOperators) {
    addCustomOperatorsCheckbox.checked = false;
    customOperatorsTextInput.value = "";
  }
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
  const filePath = csvPathInput.value.trim();
  if (!filePath) {
    throw new Error("Vyber alebo zadaj vstupný CSV súbor.");
  }
  appendLog(`Načítavam hlavičku CSV: ${filePath}`);
  const preview = (await LoadCSVPreview(filePath)) as main.CSVPreview;
  state.preview = preview;
  applySuggestedMapping(preview);
  renderPreview();
  renderMappingGrid();
  appendLog(`Načítané stĺpce (${preview.columns.length}), encoding=${preview.encoding}, headerLine=${preview.headerLine + 1}`);
}

function dedupePaths(paths: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const p of paths) {
    const trimmed = p.trim();
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
    const [mcc, mnc, pci = ""] = parts.map((v) => v.trim());
    if (!mcc || !mnc) {
      throw new Error(`Neplatný operátor '${token}'. MCC a MNC musia byť vyplnené.`);
    }
    out.push({ mcc, mnc, pci } as backend.CustomOperator);
  }
  return out;
}

async function buildProcessingConfig(): Promise<backend.ProcessingConfig> {
  const filePath = csvPathInput.value.trim();
  if (!filePath) {
    throw new Error("Vyber vstupný CSV súbor.");
  }
  if (!state.preview) {
    throw new Error("Najprv načítaj stĺpce zo vstupného CSV.");
  }

  const column_mapping: Record<string, number> = {};
  for (const field of COLUMN_FIELDS) {
    const idx = state.columnMapping[field.key];
    if (typeof idx !== "number" || Number.isNaN(idx)) {
      throw new Error(`Chýba mapovanie stĺpca pre '${field.label}'.`);
    }
    column_mapping[field.key] = idx;
  }

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
  if (!useAuto && state.customFilterPaths.length === 0) {
    filter_paths = [];
  } else if (useAuto && state.customFilterPaths.length === 0) {
    filter_paths = undefined;
  } else {
    let merged: string[] = [];
    if (useAuto) {
      const autoPaths = (await DiscoverAutoFilterPaths()) as string[];
      merged = merged.concat(autoPaths);
    }
    merged = merged.concat(state.customFilterPaths);
    filter_paths = dedupePaths(merged);
  }

  const cfg = {
    file_path: filePath,
    column_mapping,
    keep_original_rows: keepOriginalRowsCheckbox.checked,
    zone_mode: zoneModeSelect.value || "center",
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
    mobile_require_nr_yes: mobileRequireNrYesCheckbox.checked,
    mobile_nr_column_name: mobileNrColumnNameInput.value.trim() || "5G NR",
    progress_enabled: false,
  } as backend.ProcessingConfig;

  return cfg;
}

function formatPercent(value?: number): string {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "n/a";
  }
  return `${value.toFixed(2)} %`;
}

async function runProcessing(): Promise<void> {
  if (state.running) {
    return;
  }

  try {
    setRunning(true);
    setStatus("Kontrolujem vstupy...", "running");
    appendLog("Spúšťam spracovanie");

    const cfg = await buildProcessingConfig();
    appendLog(
      `Konfigurácia: mode=${cfg.zone_mode}, zone=${cfg.zone_size_m}m, filters=${cfg.filter_paths === undefined ? "auto" : cfg.filter_paths.length}, mobile=${cfg.mobile_mode_enabled}`
    );

    setStatus("Spracovanie prebieha...", "running");
    const result = (await RunProcessingWithConfig(cfg)) as backend.ProcessingResult;
    state.result = result;
    renderResult();

    setStatus("Spracovanie úspešne dokončené", "success");
    appendLog(`Výstup zón: ${result.zones_file}`);
    appendLog(`Výstup štatistík: ${result.stats_file}`);
    appendLog(`Hotovo (zóny=${result.unique_zones}, operátori=${result.unique_operators}, riadky=${result.total_zone_rows})`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    setStatus("Chyba pri spracovaní", "error");
    appendLog(`Chyba: ${message}`);
  } finally {
    setRunning(false);
  }
}

async function pickCsvAndLoadPreview(): Promise<void> {
  const path = await PickInputCSVFile();
  if (!path) {
    return;
  }
  csvPathInput.value = path;
  await loadPreviewForCurrentCSV();
}

async function pickMobileLTE(): Promise<void> {
  const path = await PickMobileLTECSVFile();
  if (!path) {
    return;
  }
  mobileLtePathInput.value = path;
  appendLog(`Vybraný LTE CSV: ${path}`);
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
  state.customFilterPaths = state.customFilterPaths.filter((p) => !selected.has(p));
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

pickCsvBtn.addEventListener("click", () => {
  void pickCsvAndLoadPreview();
});
loadPreviewBtn.addEventListener("click", () => {
  void loadPreviewForCurrentCSV().catch((err) => {
    const msg = err instanceof Error ? err.message : String(err);
    setStatus("Chyba pri načítaní CSV", "error");
    appendLog(`Chyba pri načítaní CSV: ${msg}`);
  });
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
includeEmptyZonesCheckbox.addEventListener("change", updateDependentUI);
addCustomOperatorsCheckbox.addEventListener("change", updateDependentUI);
runBtn.addEventListener("click", () => {
  void runProcessing();
});
clearLogBtn.addEventListener("click", () => {
  state.logs = [];
  logOutput.textContent = "Log vyčistený.";
});
csvPathInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    void loadPreviewForCurrentCSV().catch((err) => {
      const msg = err instanceof Error ? err.message : String(err);
      setStatus("Chyba pri načítaní CSV", "error");
      appendLog(`Chyba pri načítaní CSV: ${msg}`);
    });
  }
});

renderPreview();
renderFilterList();
renderMappingGrid();
renderResult();
updateDependentUI();
setStatus("Pripravené", "idle");
