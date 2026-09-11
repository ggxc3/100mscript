import { PickFilterFiles } from "../wailsjs/go/main/App";

export type FrequencyFile = { file_path: string; technology: string; frequency_column: string; plmn_column: string };
export type FrequencyState = {
  enabled: boolean;
  columnMappings: Record<string, Record<string, string>>;
  files: Record<string, FrequencyFile>;
  lteBW: string;
  nrBW: string;
  autoFilters: boolean;
  lteFilters: string[];
  nrFilters: string[];
};
export const newFrequencyState = (): FrequencyState => ({
  enabled: false, columnMappings: {lte: {}, "5g": {}}, files: {}, lteBW: "0", nrBW: "0", autoFilters: true, lteFilters: [], nrFilters: [],
});
const html = (s: string): string => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const basename = (s: string): string => s.replace(/\\/g, "/").split("/").pop() || s;
const token = (s: string): string => s.toLowerCase().replace(/[^a-z0-9]/g, "");
export function frequencyValidation(state: FrequencyState, paths: string[]): string | null {
  for (const path of paths) {
    const f = state.files[path];
    if (!f || !["lte", "5g"].includes(f.technology)) return `Vyber technológiu: ${basename(path)}`;
    if (!f.frequency_column) return `Vyber frekvenciu v Hz: ${basename(path)}`;
  }
  for (const value of [state.lteBW, state.nrBW]) {
    if (!Number.isFinite(Number(value.replace(",", "."))) || Number(value.replace(",", ".")) < 0) return "BW musí byť nezáporné číslo v MHz.";
  }
  return null;
}

export function renderFrequencyPanel(
  host: HTMLElement, state: FrequencyState, paths: string[],
  schemas: Array<{filePath: string; columns: string[]}>, running: boolean,
  onChange: () => void, onError: (message: string) => void,
): void {
  host.hidden = !state.enabled;
  if (!state.enabled) return;
  const disabled = running ? " disabled" : "";
  const cards = paths.map((path, i) => {
    const columns = schemas.find(s => s.filePath === path)?.columns || [];
    const physical = columns.filter(c => ["frequency", "ssref"].includes(token(c)));
    const file = state.files[path] ||= { file_path: path, technology: "", frequency_column: physical.length === 1 ? physical[0] : "", plmn_column: "" };
    if (!columns.includes(file.frequency_column)) file.frequency_column = physical.length === 1 ? physical[0] : "";
    if (!columns.includes(file.plmn_column)) file.plmn_column = "";
    const suggested = columns.some(c => token(c) === "nrarfcn") ? "5G" : columns.some(c => token(c) === "earfcn") ? "LTE" : "neurčený";
    const options = (selected: string, frequency: boolean): string => columns.filter(c => !frequency || !["earfcn", "nrarfcn"].includes(token(c)))
      .map(c => `<option value="${html(c)}"${c === selected ? " selected" : ""}>${html(c)}</option>`).join("");
    return `<div class="frequency-file" data-file-index="${i}">
      <div class="frequency-file-title"><strong title="${html(path)}">${html(basename(path))}</strong><small>Typ podľa hlavičky: ${suggested}</small></div>
      <div class="frequency-file-fields">
        <label class="field"><span>Technológia <b>*</b></span><select data-freq-field="technology"${disabled}>
          <option value="">Vyber LTE alebo 5G</option><option value="lte"${file.technology === "lte" ? " selected" : ""}>LTE</option><option value="5g"${file.technology === "5g" ? " selected" : ""}>5G</option></select></label>
        <label class="field"><span>Frekvencia v Hz <b>*</b></span><select data-freq-field="frequency_column"${disabled}>
          <option value="">Vyber stĺpec</option>${options(file.frequency_column, true)}</select></label>
        <label class="field"><span>Stĺpec s 231/2</span><select data-freq-field="plmn_column"${disabled}>
          <option value="">Automaticky (PLMN / extra)</option>${options(file.plmn_column, false)}</select></label>
      </div>
    </div>`;
  }).join("");
  host.innerHTML = `<div class="section-head"><h2>Frekvencie · LTE + 5G</h2><span class="frequency-badge">Samostatný režim</span></div>
    <p class="section-note">Každá zóna/úsek × operátor × frekvencia × technológia má vlastný riadok. Vyberie sa meranie s najvyšším RSRP; ostatné PCI sa vypíšu na konci riadku. Výstup 5G a výstup LTE sa uložia osobitne, každý so svojimi stĺpcami.</p>
    <div class="frequency-files">${cards || '<p class="frequency-empty">Pridaj CSV súbory v sekcii Vstupné dáta. Môžeš kombinovať viac LTE aj viac 5G súborov.</p>'}</div>
    <p class="section-note">Použi <strong>SSRef</strong> pre 5G a <strong>Frequency</strong> pre LTE, v Hz. Hodnota za lomkou v PLMN opraví MNC. Prázdna hodnota ho ponechá.</p>
    <div class="double-grid frequency-bw">
      <label class="field"><span>BW pre LTE (± MHz)</span><input data-bw="lteBW" type="number" min="0" step="any" placeholder="0 – iba stred" value="${html(state.lteBW)}"${disabled}/></label>
      <label class="field"><span>BW pre 5G (± MHz)</span><input data-bw="nrBW" type="number" min="0" step="any" placeholder="0 – iba stred" value="${html(state.nrBW)}"${disabled}/></label>
    </div>
    <p class="section-note">Výsledný stĺpec <code>Operator_sedi</code> pri prázdnom alebo nulovom BW kontroluje iba strednú frekvenciu f. Pri BW väčšom ako 0 kontroluje všetky tri body: f, f − BW a f + BW. BW je odchýlka na každú stranu v MHz. Ak by filter zmenil operátora, výsledok je <strong>no</strong>, inak <strong>yes</strong>. Aj bez zhodného filtra je výsledok <strong>yes</strong>. Filtre nemenia ani neduplikujú merania.</p>
    <label class="check-row"><input data-frequency-auto type="checkbox"${state.autoFilters ? " checked" : ""}${disabled}/><span>Automatické filtre: LTE z <code>filters/</code>, 5G z <code>filtre_5G/</code></span></label>
    <div class="double-grid frequency-filter-grid">${(["lteFilters", "nrFilters"] as const).map(key => `<div class="frequency-filter-box">
      <strong>Dodatočné filtre ${key === "lteFilters" ? "LTE" : "5G"}</strong>
      <ul>${state[key].map((p,i) => `<li><span title="${html(p)}">${html(basename(p))}</span><button type="button" class="btn ghost" data-filter-key="${key}" data-filter-remove="${i}" aria-label="Odstrániť ${html(basename(p))}"${disabled}>×</button></li>`).join("") || '<li class="muted">Žiadne dodatočné filtre</li>'}</ul>
      <button type="button" class="btn secondary" data-filter-add="${key}"${disabled}>Pridať filtre</button></div>`).join("")}</div>`;
  host.querySelectorAll<HTMLSelectElement>("[data-freq-field]").forEach(select => select.addEventListener("change", () => {
    const i = Number(select.closest<HTMLElement>("[data-file-index]")!.dataset.fileIndex);
    const field = select.dataset.freqField as keyof FrequencyFile;
    state.files[paths[i]][field] = select.value;
    onChange();
  }));
  host.querySelectorAll<HTMLInputElement>("[data-bw]").forEach(input => input.addEventListener("input", () => {
    state[input.dataset.bw as "lteBW" | "nrBW"] = input.value; onChange();
  }));
  host.querySelector<HTMLInputElement>("[data-frequency-auto]")!.addEventListener("change", event => {
    state.autoFilters = (event.target as HTMLInputElement).checked; onChange();
  });
  const rerender = (): void => { renderFrequencyPanel(host,state,paths,schemas,running,onChange,onError); onChange(); };
  host.querySelectorAll<HTMLButtonElement>("[data-filter-add]").forEach(button => button.addEventListener("click", async () => {
    button.disabled = true;
    try {
      const key = button.dataset.filterAdd as "lteFilters" | "nrFilters";
      const files = await PickFilterFiles();
      state[key] = [...new Set([...state[key], ...files])]; rerender();
    } catch (err) { onError(String(err)); button.disabled = false; }
  }));
  host.querySelectorAll<HTMLButtonElement>("[data-filter-remove]").forEach(button => button.addEventListener("click", () => {
    const key = button.dataset.filterKey as "lteFilters" | "nrFilters";
    state[key].splice(Number(button.dataset.filterRemove), 1); rerender();
  }));
}
