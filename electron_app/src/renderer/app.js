(function () {
  const DEFAULT_COLUMN_LETTERS = {
    latitude: "D",
    longitude: "E",
    frequency: "K",
    pci: "L",
    mcc: "M",
    mnc: "N",
    rsrp: "W",
    sinr: "V"
  };

  const ZONE_MODE_LABELS = {
    center: "Štvorcové zóny (stred)",
    original: "Štvorcové zóny (prvý bod v zóne)",
    segments: "Úseky po trase"
  };

  const state = {
    context: null,
    running: false,
    additionalFilters: [],
    selectedFilterIndex: null,
    autoFiltersCache: null,
    autoFiltersSourceCsvPath: "",
    lastResultEvent: null,
    unsubscribeBackendEvents: null,
    currentRunStartedAt: null,
    currentRunHadErrorEvent: false,
    currentRunTerminalEventSeen: false,
    progress: {
      phase: "",
      current: 0,
      total: 0,
      percent: 0
    }
  };

  const els = {
    statusChip: document.getElementById("statusChip"),
    heroMeta: document.getElementById("heroMeta"),
    csvPath: document.getElementById("csvPath"),
    pickCsvBtn: document.getElementById("pickCsvBtn"),
    mobileMode: document.getElementById("mobileMode"),
    mobileLtePath: document.getElementById("mobileLtePath"),
    pickLteBtn: document.getElementById("pickLteBtn"),
    mobileTolerance: document.getElementById("mobileTolerance"),
    useAutoFilters: document.getElementById("useAutoFilters"),
    refreshAutoFiltersBtn: document.getElementById("refreshAutoFiltersBtn"),
    filtersList: document.getElementById("filtersList"),
    addFiltersBtn: document.getElementById("addFiltersBtn"),
    removeSelectedFilterBtn: document.getElementById("removeSelectedFilterBtn"),
    clearFiltersBtn: document.getElementById("clearFiltersBtn"),
    zoneMode: document.getElementById("zoneMode"),
    zoneSize: document.getElementById("zoneSize"),
    rsrpThreshold: document.getElementById("rsrpThreshold"),
    sinrThreshold: document.getElementById("sinrThreshold"),
    keepOriginalRows: document.getElementById("keepOriginalRows"),
    includeEmptyZones: document.getElementById("includeEmptyZones"),
    addCustomOperators: document.getElementById("addCustomOperators"),
    customOperators: document.getElementById("customOperators"),
    autofillColumnsBtn: document.getElementById("autofillColumnsBtn"),
    resetColumnsBtn: document.getElementById("resetColumnsBtn"),
    detectedColumns: document.getElementById("detectedColumns"),
    zigBinaryPath: document.getElementById("zigBinaryPath"),
    runBtn: document.getElementById("runBtn"),
    clearLogBtn: document.getElementById("clearLogBtn"),
    spinner: document.getElementById("spinner"),
    statusText: document.getElementById("statusText"),
    progressPanel: document.getElementById("progressPanel"),
    progressPhase: document.getElementById("progressPhase"),
    progressPercent: document.getElementById("progressPercent"),
    progressFill: document.getElementById("progressFill"),
    progressDetail: document.getElementById("progressDetail"),
    zonesPath: document.getElementById("zonesPath"),
    statsPath: document.getElementById("statsPath"),
    resultSummary: document.getElementById("resultSummary"),
    openZonesBtn: document.getElementById("openZonesBtn"),
    openStatsBtn: document.getElementById("openStatsBtn"),
    openOutputFolderBtn: document.getElementById("openOutputFolderBtn"),
    eventLog: document.getElementById("eventLog"),
    columnInputs: Array.from(document.querySelectorAll("#columnMappingGrid input[data-col-key]"))
  };

  function getColumnInputsMap() {
    const out = {};
    for (const input of els.columnInputs) {
      out[input.dataset.colKey] = input;
    }
    return out;
  }
  const columnInputsByKey = getColumnInputsMap();

  function timestamp() {
    return new Date().toLocaleTimeString("sk-SK", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit"
    });
  }

  function appendLog(message) {
    const line = `[${timestamp()}] ${String(message)}`;
    els.eventLog.textContent = els.eventLog.textContent
      ? `${els.eventLog.textContent}\n${line}`
      : line;
    els.eventLog.scrollTop = els.eventLog.scrollHeight;
  }

  function clearLog() {
    els.eventLog.textContent = "";
  }

  function setChip(stateName, label) {
    els.statusChip.dataset.state = stateName;
    els.statusChip.textContent = label;
  }

  function setStatusText(text) {
    els.statusText.textContent = text;
  }

  function setHeroMeta(text) {
    els.heroMeta.textContent = text;
  }

  function clampPercent(value) {
    if (!Number.isFinite(value)) return 0;
    return Math.max(0, Math.min(100, value));
  }

  function setProgressState({ active, phase, current, total, percent, detail }) {
    if (typeof active === "boolean") {
      els.progressPanel.dataset.active = active ? "true" : "false";
    }
    if (typeof phase === "string") {
      els.progressPhase.textContent = phase;
      state.progress.phase = phase;
    }
    if (typeof current === "number" && Number.isFinite(current)) {
      state.progress.current = current;
    }
    if (typeof total === "number" && Number.isFinite(total)) {
      state.progress.total = total;
    }
    if (typeof percent === "number" && Number.isFinite(percent)) {
      state.progress.percent = clampPercent(percent);
    }

    const pct = clampPercent(state.progress.percent);
    els.progressPercent.textContent = `${pct.toFixed(1)}%`;
    els.progressFill.style.width = `${pct}%`;

    if (typeof detail === "string") {
      els.progressDetail.textContent = detail;
    }
  }

  function resetProgressUi() {
    state.progress = { phase: "", current: 0, total: 0, percent: 0 };
    setProgressState({
      active: false,
      phase: "Čaká sa na spustenie",
      current: 0,
      total: 0,
      percent: 0,
      detail: "Počas spracovania sa tu zobrazí priebeh podľa riadkov."
    });
  }

  function markProgressDone(detail) {
    setProgressState({
      active: false,
      phase: "Hotovo",
      current: state.progress.total || state.progress.current || 1,
      total: state.progress.total || state.progress.current || 1,
      percent: 100,
      detail: detail || "Spracovanie bolo dokončené."
    });
  }

  function cleanStatusForUi(raw) {
    let text = String(raw || "").trim();
    text = text.replace(/^Zig backend:\s*/i, "");
    text = text.replace(/^Zig backend\s+/i, "");
    text = text.replace(/^preview\s+/i, "");
    return text || "Prebieha spracovanie...";
  }

  function setRunning(running) {
    state.running = Boolean(running);
    els.runBtn.disabled = state.running;
    els.spinner.classList.toggle("running", state.running);
  }

  function setUiStatus(kind, text, heroMeta) {
    if (kind === "running") {
      setChip("running", "RUNNING");
    } else if (kind === "success") {
      setChip("success", "DONE");
    } else if (kind === "error") {
      setChip("error", "ERROR");
    } else {
      setChip("ready", "READY");
    }
    if (typeof text === "string") setStatusText(text);
    if (typeof heroMeta === "string") setHeroMeta(heroMeta);
  }

  function normalizePath(value) {
    return String(value || "").trim();
  }

  function parseLocaleFloat(raw, label) {
    const text = String(raw || "").trim().replace(/,/g, ".");
    if (!text) {
      throw new Error(`Chýba hodnota: ${label}.`);
    }
    const value = Number(text);
    if (!Number.isFinite(value)) {
      throw new Error(`Neplatná hodnota pre ${label}.`);
    }
    return value;
  }

  function parseNonNegativeIntLike(raw, label) {
    const text = String(raw || "").trim().replace(/,/g, ".");
    if (!text) {
      throw new Error(`Chýba hodnota: ${label}.`);
    }
    const asNum = Number(text);
    if (!Number.isFinite(asNum)) {
      throw new Error(`Neplatná hodnota pre ${label}.`);
    }
    const value = Math.trunc(asNum);
    if (value < 0) {
      throw new Error(`${label} musí byť číslo >= 0.`);
    }
    return value;
  }

  function colLetterToIndex(letter) {
    const text = String(letter || "")
      .trim()
      .toUpperCase();
    if (!text) return 0;

    const lettersOnly = Array.from(text)
      .filter((ch) => ch >= "A" && ch <= "Z")
      .join("");
    if (!lettersOnly) return 0;

    let idx = 0;
    for (const ch of lettersOnly) {
      idx = idx * 26 + (ch.charCodeAt(0) - 64);
    }
    return idx - 1;
  }

  function parseCustomOperatorsText(text) {
    const raw = String(text || "").replace(/,/g, " ");
    const items = raw.split(/\s+/).filter(Boolean);
    const out = [];

    for (const item of items) {
      const parts = item.split(":");
      if (!(parts.length === 2 || parts.length === 3)) {
        throw new Error(
          `Neplatný formát operátora '${item}'. Použite MCC:MNC alebo MCC:MNC:PCI.`
        );
      }
      const mcc = String(parts[0] || "").trim();
      const mnc = String(parts[1] || "").trim();
      const pci = parts.length === 3 ? String(parts[2] || "").trim() : "";

      if (!/^\d+$/.test(mcc)) {
        throw new Error(`Neplatné MCC '${mcc}'. MCC musí obsahovať iba čísla.`);
      }
      if (!/^\d+$/.test(mnc)) {
        throw new Error(`Neplatné MNC '${mnc}'. MNC musí obsahovať iba čísla.`);
      }
      if (pci && !/^\d+$/.test(pci)) {
        throw new Error(`Neplatné PCI '${pci}'. PCI musí obsahovať iba čísla.`);
      }
      out.push([mcc, mnc, pci]);
    }
    return out;
  }

  function setColumnsFromLetters(letterMap) {
    for (const [key, input] of Object.entries(columnInputsByKey)) {
      const value = letterMap && typeof letterMap[key] === "string" ? letterMap[key] : DEFAULT_COLUMN_LETTERS[key];
      input.value = value;
    }
  }

  function collectColumnMapping() {
    const mapping = {};
    for (const [key, input] of Object.entries(columnInputsByKey)) {
      const raw = String(input.value || "").trim();
      if (!raw) {
        throw new Error(`Chýba písmeno stĺpca pre '${key}'.`);
      }
      mapping[key] = colLetterToIndex(raw);
    }
    return mapping;
  }

  function renderDetectedColumns(inspection) {
    if (!inspection || !inspection.detected || !Object.keys(inspection.detected).length) {
      els.detectedColumns.textContent = "Auto-detekcia nenašla známe názvy stĺpcov. Používajú sa predvolené hodnoty.";
      return;
    }

    const order = ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"];
    const parts = [];
    for (const key of order) {
      const item = inspection.detected[key];
      if (!item) continue;
      parts.push(`${key}=${item.letter} (${item.header})`);
    }

    const headerPreview = inspection.headers && inspection.headers.length
      ? ` • hlavička: ${inspection.headers.length} stĺpcov`
      : "";
    els.detectedColumns.textContent = `Detekované: ${parts.join(", ")}${headerPreview}`;
  }

  function renderFiltersList() {
    els.filtersList.textContent = "";
    state.additionalFilters.forEach((filePath, index) => {
      const li = document.createElement("li");
      li.className = "filters-list-item";
      li.dataset.index = String(index);
      li.setAttribute("role", "button");
      li.tabIndex = 0;
      li.setAttribute("aria-selected", String(index === state.selectedFilterIndex));
      li.title = filePath;

      const badge = document.createElement("span");
      badge.textContent = String(index + 1).padStart(2, "0");

      const code = document.createElement("code");
      code.textContent = filePath;

      li.append(badge, code);
      els.filtersList.appendChild(li);
    });

    els.removeSelectedFilterBtn.disabled = state.selectedFilterIndex == null;
    els.clearFiltersBtn.disabled = state.additionalFilters.length === 0;
    refreshFilterMeta();
  }

  function refreshFilterMeta() {
    const autoCount = Array.isArray(state.autoFiltersCache) ? state.autoFiltersCache.length : null;
    const extraCount = state.additionalFilters.length;
    const parts = [];
    parts.push(`Režim: ${ZONE_MODE_LABELS[els.zoneMode.value] || els.zoneMode.value}`);
    parts.push(`Dodatočné filtre: ${extraCount}`);
    if (autoCount != null) {
      parts.push(`Auto-filtre: ${autoCount}${els.useAutoFilters.checked ? " (zap.)" : " (vyp.)"}`);
    } else {
      parts.push(`Auto-filtre: ${els.useAutoFilters.checked ? "zapnuté" : "vypnuté"}`);
    }
    setHeroMeta(parts.join(" • "));
  }

  function selectFilterIndex(index) {
    if (!Number.isInteger(index) || index < 0 || index >= state.additionalFilters.length) {
      state.selectedFilterIndex = null;
    } else if (state.selectedFilterIndex === index) {
      state.selectedFilterIndex = null;
    } else {
      state.selectedFilterIndex = index;
    }
    renderFiltersList();
  }

  function closestFilterRow(target) {
    return target && typeof target.closest === "function"
      ? target.closest(".filters-list-item")
      : null;
  }

  function updateMobileFields() {
    const enabled = els.mobileMode.checked;
    els.mobileLtePath.disabled = !enabled;
    els.pickLteBtn.disabled = !enabled;
    els.mobileTolerance.disabled = !enabled;
  }

  function updateOperatorFields() {
    const allowCustom = els.includeEmptyZones.checked;
    els.addCustomOperators.disabled = !allowCustom;
    els.customOperators.disabled = !allowCustom;
    if (!allowCustom) {
      els.addCustomOperators.checked = false;
      els.customOperators.value = "";
    }
  }

  function resetResults() {
    state.lastResultEvent = null;
    els.zonesPath.textContent = "—";
    els.statsPath.textContent = "—";
    els.resultSummary.textContent = "—";
    els.zonesPath.title = "";
    els.statsPath.title = "";
    els.openZonesBtn.disabled = true;
    els.openStatsBtn.disabled = true;
    els.openOutputFolderBtn.disabled = true;
  }

  function fmtMaybeNumber(value, digits = 1) {
    if (typeof value !== "number" || !Number.isFinite(value)) return null;
    return value.toFixed(digits);
  }

  function applyResultEvent(evt) {
    if (!evt || typeof evt !== "object") return;
    state.lastResultEvent = evt;

    const zonesPath = normalizePath(evt.zones_file);
    const statsPath = normalizePath(evt.stats_file);
    els.zonesPath.textContent = zonesPath || "—";
    els.statsPath.textContent = statsPath || "—";
    els.zonesPath.title = zonesPath;
    els.statsPath.title = statsPath;
    els.openZonesBtn.disabled = !zonesPath;
    els.openStatsBtn.disabled = !statsPath;
    els.openOutputFolderBtn.disabled = !(zonesPath || statsPath);

    const summaryParts = [
      `zóny: ${evt.unique_zones ?? "?"}`,
      `operátori: ${evt.unique_operators ?? "?"}`,
      `riadky: ${evt.total_zone_rows ?? "?"}`
    ];
    const coverage = fmtMaybeNumber(evt.coverage_percent, 1);
    if (coverage != null) summaryParts.push(`coverage: ${coverage}%`);
    els.resultSummary.textContent = summaryParts.join(" • ");
  }

  async function inspectCsvAndAutofill(filePath) {
    const normalized = normalizePath(filePath);
    if (!normalized) {
      throw new Error("Najprv vyber vstupný CSV súbor.");
    }
    const inspection = await window.codex100m.inspectCsvHeaders(normalized);
    if (inspection && inspection.suggestions) {
      setColumnsFromLetters(inspection.suggestions);
    }
    renderDetectedColumns(inspection);

    if (inspection && inspection.detected && Object.keys(inspection.detected).length) {
      const parts = [];
      for (const key of ["latitude", "longitude", "frequency", "pci", "mcc", "mnc", "rsrp", "sinr"]) {
        const item = inspection.detected[key];
        if (item) parts.push(`${key}=${item.letter} (${item.header})`);
      }
      appendLog(`Auto-detekované stĺpce: ${parts.join(", ")}`);
    } else {
      appendLog("Auto-detekcia stĺpcov nenašla známe názvy, ostávajú predvolené hodnoty.");
    }
  }

  async function refreshAutoFilters(showLog) {
    const csvPath = normalizePath(els.csvPath.value);
    const discovered = await window.codex100m.discoverAutoFilters(csvPath);
    state.autoFiltersCache = Array.isArray(discovered) ? discovered : [];
    state.autoFiltersSourceCsvPath = csvPath;
    refreshFilterMeta();
    if (showLog !== false) {
      if (!csvPath) {
        appendLog("Auto-filtre: najprv vyber vstupný CSV súbor.");
      } else {
        appendLog(`Auto-filtre: nájdených ${state.autoFiltersCache.length} .txt súborov pri vstupnom CSV.`);
      }
    }
    return state.autoFiltersCache;
  }

  async function addFilterPaths(paths) {
    const combined = [...state.additionalFilters, ...(Array.isArray(paths) ? paths : [])];
    state.additionalFilters = await window.codex100m.dedupePaths(combined);
    if (
      state.selectedFilterIndex != null &&
      (state.selectedFilterIndex < 0 || state.selectedFilterIndex >= state.additionalFilters.length)
    ) {
      state.selectedFilterIndex = null;
    }
    renderFiltersList();
  }

  async function resolveFilterPaths() {
    let autoPaths = [];
    if (els.useAutoFilters.checked) {
      const currentCsvPath = normalizePath(els.csvPath.value);
      const cacheMatchesCurrentCsv =
        Array.isArray(state.autoFiltersCache) && state.autoFiltersSourceCsvPath === currentCsvPath;
      autoPaths = cacheMatchesCurrentCsv ? state.autoFiltersCache : await refreshAutoFilters(false);
    }
    const combined = [...autoPaths, ...state.additionalFilters];
    const deduped = await window.codex100m.dedupePaths(combined);
    return deduped.length ? deduped : null;
  }

  async function requireExistingPath(filePath, label) {
    const value = normalizePath(filePath);
    if (!value) {
      throw new Error(`${label} chýba.`);
    }
    const exists = await window.codex100m.pathExists(value);
    if (!exists) {
      throw new Error(`${label} neexistuje.`);
    }
    return value;
  }

  function buildBaseConfig() {
    return {
      keep_original_rows: els.keepOriginalRows.checked,
      zone_mode: els.zoneMode.value,
      zone_size_m: parseLocaleFloat(els.zoneSize.value, "Veľkosť zóny/úseku"),
      rsrp_threshold: parseLocaleFloat(els.rsrpThreshold.value, "RSRP hranica"),
      sinr_threshold: parseLocaleFloat(els.sinrThreshold.value, "SINR hranica"),
      include_empty_zones: els.includeEmptyZones.checked,
      add_custom_operators: els.includeEmptyZones.checked && els.addCustomOperators.checked,
      custom_operators: [],
      output_suffix: null,
      progress_enabled: true,
      mobile_require_nr_yes: true,
      mobile_nr_column_name: "5G NR"
    };
  }

  async function buildRunPayload() {
    const filePath = await requireExistingPath(els.csvPath.value, "Vstupný CSV súbor");
    const zoneSize = parseLocaleFloat(els.zoneSize.value, "Veľkosť zóny/úseku");
    if (!(zoneSize > 0)) {
      throw new Error("Veľkosť zóny/úseku musí byť kladná.");
    }

    const columnMapping = collectColumnMapping();
    const filterPaths = await resolveFilterPaths();

    let mobileLteFilePath = null;
    let mobileTimeToleranceMs = 1000;
    if (els.mobileMode.checked) {
      mobileLteFilePath = await requireExistingPath(els.mobileLtePath.value, "LTE CSV súbor pre Mobile režim");
      mobileTimeToleranceMs = parseNonNegativeIntLike(els.mobileTolerance.value, "Tolerancia času pre Mobile");
    }

    let customOperators = [];
    const addCustomOperators = els.includeEmptyZones.checked && els.addCustomOperators.checked;
    if (addCustomOperators && normalizePath(els.customOperators.value)) {
      customOperators = parseCustomOperatorsText(els.customOperators.value);
    }

    const config = {
      file_path: filePath,
      column_mapping: columnMapping,
      ...buildBaseConfig(),
      zone_size_m: zoneSize,
      add_custom_operators: addCustomOperators,
      custom_operators: customOperators,
      filter_paths: filterPaths,
      mobile_mode_enabled: els.mobileMode.checked,
      mobile_lte_file_path: mobileLteFilePath,
      mobile_time_tolerance_ms: mobileTimeToleranceMs
    };

    const zigBinaryPath = normalizePath(els.zigBinaryPath.value) || "";
    if (zigBinaryPath) {
      const exists = await window.codex100m.pathExists(zigBinaryPath);
      if (!exists) {
        throw new Error("Zadaná cesta k spracovaciemu modulu neexistuje.");
      }
    }

    return { config, zigBinaryPath };
  }

  function stringifyEventForLog(evt) {
    if (!evt || typeof evt !== "object") return String(evt);
    if (evt.type === "progress") return null;
    if (evt.type === "status") return `[status] ${evt.message || ""}`;
    if (evt.type === "error") return `[error:${evt.code || "ERROR"}] ${evt.message || ""}`;
    if (evt.type === "result") {
      const coverage = fmtMaybeNumber(evt.coverage_percent, 1);
      return `[result] zones=${evt.unique_zones} operators=${evt.unique_operators} rows=${evt.total_zone_rows}${coverage != null ? ` coverage=${coverage}%` : ""}`;
    }
    return `[event] ${JSON.stringify(evt)}`;
  }

  function handleBackendEvent(evt) {
    if (!evt || typeof evt !== "object") return;
    const logLine = stringifyEventForLog(evt);
    if (logLine) appendLog(logLine);

    if (evt.type === "progress") {
      const current = Number(evt.current);
      const total = Number(evt.total);
      const percent = Number.isFinite(Number(evt.percent))
        ? Number(evt.percent)
        : (Number.isFinite(current) && Number.isFinite(total) && total > 0 ? (current / total) * 100 : 0);
      const phase = String(evt.phase || "Spracovanie");
      const detailParts = [];
      if (Number.isFinite(current) && Number.isFinite(total) && total > 0) {
        detailParts.push(`${current.toLocaleString("sk-SK")} / ${total.toLocaleString("sk-SK")} riadkov`);
      }
      if (evt.message) detailParts.push(String(evt.message));
      setProgressState({
        active: state.running,
        phase,
        current: Number.isFinite(current) ? current : 0,
        total: Number.isFinite(total) ? total : 0,
        percent,
        detail: detailParts.join(" • ") || "Spracovanie prebieha..."
      });
      if (state.running) {
        setStatusText(`${phase} (${clampPercent(percent).toFixed(1)}%)`);
        setChip("running", "RUNNING");
      }
    } else if (evt.type === "status") {
      setStatusText(cleanStatusForUi(evt.message));
      setChip("running", "RUNNING");
    } else if (evt.type === "error") {
      state.currentRunHadErrorEvent = true;
      state.currentRunTerminalEventSeen = true;
      setRunning(false);
      setProgressState({
        active: false,
        detail: `Spracovanie skončilo chybou (${evt.code || "ERROR"}).`
      });
      setStatusText(`Chyba backendu: ${evt.code || "ERROR"}`);
      setChip("error", "ERROR");
    } else if (evt.type === "result") {
      state.currentRunTerminalEventSeen = true;
      applyResultEvent(evt);
      setRunning(false);
      markProgressDone("Výstupy boli úspešne vytvorené.");
      setStatusText("Spracovanie dokončené");
      setChip("success", "DONE");
    }
  }

  async function runProcessing() {
    if (state.running) return;

    let payload;
    try {
      payload = await buildRunPayload();
    } catch (err) {
      setUiStatus("error", "Neplatné nastavenie", err && err.message ? err.message : String(err));
      appendLog(`[client-error] ${err && err.message ? err.message : String(err)}`);
      return;
    }

    state.currentRunStartedAt = Date.now();
    state.currentRunHadErrorEvent = false;
    state.currentRunTerminalEventSeen = false;
    resetResults();
    resetProgressUi();
    clearLog();
    setRunning(true);
    setProgressState({
      active: true,
      phase: "Príprava",
      current: 0,
      total: 1,
      percent: 0,
      detail: "Pripravujem konfiguráciu a spúšťam spracovanie."
    });
    setUiStatus("running", "Spúšťam spracovanie...", "Pripravujem konfiguráciu a spúšťam spracovanie.");
    appendLog("Spúšťam spracovanie.");

    try {
      const result = await window.codex100m.runZig(payload.config, payload.zigBinaryPath);

      if (result && result.resultEvent) {
        applyResultEvent(result.resultEvent);
        const elapsedMs = Math.max(0, Date.now() - (state.currentRunStartedAt || Date.now()));
        setUiStatus(
          "success",
          "Spracovanie úspešne dokončené",
          `Hotovo za ${(elapsedMs / 1000).toFixed(1)} s • výstupy pripravené.`
        );
        markProgressDone(`Hotovo za ${(elapsedMs / 1000).toFixed(1)} s.`);
      } else if (result && result.errorEvent) {
        setUiStatus(
          "error",
          `Backend chyba (${result.errorEvent.code || "ERROR"})`,
          result.errorEvent.message || "Spracovanie skončilo s chybou."
        );
        setProgressState({
          active: false,
          detail: result.errorEvent.message || "Spracovanie skončilo s chybou."
        });
      } else if (result && Number(result.exitCode) === 0) {
        setUiStatus("success", "Backend skončil", "Proces skončil bez result eventu.");
        markProgressDone("Proces skončil.");
      } else {
        setUiStatus("error", "Backend skončil bez výsledku", `Exit code: ${result ? result.exitCode : "?"}`);
        setProgressState({
          active: false,
          detail: `Proces skončil bez výsledku (exit code: ${result ? result.exitCode : "?"}).`
        });
      }

      if (result && typeof result.exitCode !== "undefined") {
        appendLog(`[run] exit code=${result.exitCode}`);
      }
      if (result && Array.isArray(result.statuses) && result.statuses.length) {
        appendLog(`[run] status events: ${result.statuses.length}`);
      }
      if (result && result.stderr) {
        for (const line of String(result.stderr).split(/\r?\n/)) {
          if (!line.trim()) continue;
          appendLog(`[stderr] ${line}`);
        }
      }
    } catch (err) {
      setUiStatus("error", "Spustenie zlyhalo", err && err.message ? err.message : String(err));
      setProgressState({
        active: false,
        detail: err && err.message ? err.message : String(err)
      });
      appendLog(`[client-error] ${err && err.message ? err.message : String(err)}`);
    } finally {
      if (!state.currentRunTerminalEventSeen) {
        setRunning(false);
      }
    }
  }

  async function openPathInFolder(filePath) {
    const normalized = normalizePath(filePath);
    if (!normalized) return;
    const ok = await window.codex100m.showItemInFolder(normalized);
    if (!ok) {
      appendLog(`[ui] Súbor sa nepodarilo otvoriť v priečinku: ${normalized}`);
    }
  }

  async function openFilePath(filePath) {
    const normalized = normalizePath(filePath);
    if (!normalized) return;
    const res = await window.codex100m.openPath(normalized);
    if (!res || !res.ok) {
      appendLog(`[ui] Súbor sa nepodarilo otvoriť: ${normalized}${res && res.error ? ` (${res.error})` : ""}`);
    }
  }

  function bindEvents() {
    els.pickCsvBtn.addEventListener("click", async () => {
      try {
        const file = await window.codex100m.pickCsvFile();
        if (!file) return;
        els.csvPath.value = file;
        state.autoFiltersCache = null;
        state.autoFiltersSourceCsvPath = "";
        refreshFilterMeta();
        await inspectCsvAndAutofill(file);
        await refreshAutoFilters(true).catch((err) => {
          appendLog(`[client-error] Nepodarilo sa načítať auto-filtre: ${err && err.message ? err.message : String(err)}`);
        });
      } catch (err) {
        appendLog(`[client-error] ${err && err.message ? err.message : String(err)}`);
      }
    });

    els.pickLteBtn.addEventListener("click", async () => {
      const file = await window.codex100m.pickLteCsvFile();
      if (file) els.mobileLtePath.value = file;
    });

    els.mobileMode.addEventListener("change", () => {
      updateMobileFields();
      refreshFilterMeta();
    });

    els.includeEmptyZones.addEventListener("change", () => {
      updateOperatorFields();
      refreshFilterMeta();
    });

    els.zoneMode.addEventListener("change", refreshFilterMeta);
    els.useAutoFilters.addEventListener("change", refreshFilterMeta);

    els.refreshAutoFiltersBtn.addEventListener("click", async () => {
      try {
        await refreshAutoFilters(true);
      } catch (err) {
        appendLog(`[client-error] Nepodarilo sa načítať auto-filtre: ${err && err.message ? err.message : String(err)}`);
      }
    });

    els.addFiltersBtn.addEventListener("click", async () => {
      try {
        const files = await window.codex100m.pickFilterFiles();
        if (!files || !files.length) return;
        await addFilterPaths(files);
        appendLog(`Pridaných ${files.length} filter súborov.`);
      } catch (err) {
        appendLog(`[client-error] Nepodarilo sa pridať filtre: ${err && err.message ? err.message : String(err)}`);
      }
    });

    els.removeSelectedFilterBtn.addEventListener("click", () => {
      if (state.selectedFilterIndex == null) return;
      const removed = state.additionalFilters[state.selectedFilterIndex];
      state.additionalFilters = state.additionalFilters.filter((_, idx) => idx !== state.selectedFilterIndex);
      state.selectedFilterIndex = null;
      renderFiltersList();
      if (removed) appendLog(`Odstránený filter: ${removed}`);
    });

    els.clearFiltersBtn.addEventListener("click", () => {
      if (!state.additionalFilters.length) return;
      state.additionalFilters = [];
      state.selectedFilterIndex = null;
      renderFiltersList();
      appendLog("Zoznam dodatočných filtrov bol vyčistený.");
    });

    els.filtersList.addEventListener("click", (event) => {
      const row = closestFilterRow(event.target);
      if (!row) return;
      selectFilterIndex(Number(row.dataset.index));
    });

    els.filtersList.addEventListener("keydown", (event) => {
      const row = closestFilterRow(event.target);
      if (!row) return;
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        selectFilterIndex(Number(row.dataset.index));
      }
    });

    els.autofillColumnsBtn.addEventListener("click", async () => {
      try {
        await inspectCsvAndAutofill(els.csvPath.value);
      } catch (err) {
        appendLog(`[client-error] ${err && err.message ? err.message : String(err)}`);
        setUiStatus("error", "Auto-detekcia zlyhala", err && err.message ? err.message : String(err));
      }
    });

    els.resetColumnsBtn.addEventListener("click", () => {
      setColumnsFromLetters(DEFAULT_COLUMN_LETTERS);
      els.detectedColumns.textContent = "Mapovanie bolo resetované na predvolené hodnoty.";
      appendLog("Mapovanie stĺpcov resetované na predvolené hodnoty.");
    });

    els.runBtn.addEventListener("click", runProcessing);

    els.clearLogBtn.addEventListener("click", clearLog);

    els.openZonesBtn.addEventListener("click", () => openFilePath(state.lastResultEvent && state.lastResultEvent.zones_file));
    els.openStatsBtn.addEventListener("click", () => openFilePath(state.lastResultEvent && state.lastResultEvent.stats_file));
    els.openOutputFolderBtn.addEventListener("click", () => {
      const evt = state.lastResultEvent || {};
      const candidate = normalizePath(evt.zones_file) || normalizePath(evt.stats_file);
      openPathInFolder(candidate);
    });
  }

  async function init() {
    setColumnsFromLetters(DEFAULT_COLUMN_LETTERS);
    renderFiltersList();
    updateMobileFields();
    updateOperatorFields();
    resetResults();
    resetProgressUi();
    setUiStatus("ready", "Pripravené", "Inicializujem aplikáciu...");

    state.context = await window.codex100m.getContext();
    if (state.context && state.context.zigBinaryPath) {
      els.zigBinaryPath.value = state.context.zigBinaryPath;
    }

    if (state.unsubscribeBackendEvents) {
      state.unsubscribeBackendEvents();
    }
    state.unsubscribeBackendEvents = window.codex100m.onBackendEvent(handleBackendEvent);

    await refreshAutoFilters(false).catch(() => {
      state.autoFiltersCache = [];
    });

    setUiStatus("ready", "Pripravené", "Vyber CSV súbor a spusti spracovanie.");
    refreshFilterMeta();
  }

  bindEvents();
  els.csvPath.addEventListener("input", () => {
    state.autoFiltersCache = null;
    state.autoFiltersSourceCsvPath = "";
    refreshFilterMeta();
  });
  init().catch((err) => {
    appendLog(`[init-error] ${err && err.message ? err.message : String(err)}`);
    setUiStatus("error", "Inicializácia zlyhala", err && err.message ? err.message : String(err));
  });
})();
