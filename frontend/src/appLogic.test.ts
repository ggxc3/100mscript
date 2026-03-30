import { describe, expect, it, vi } from "vitest";
import { backend, main } from "../wailsjs/go/models";
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
  flattenTimeDateOptions,
  formatDateTimeLabel,
  formatDateTimeParts,
  formatPercent,
  formatTimestampForFilename,
  getTimeOptionsForDate,
  inputRadioTechUiLabel,
  isCompleteTimeWindow,
  labelForTimeStrategy,
  mappingComplete,
  parseCustomOperatorsText,
  parseIntegerInput,
  parseNumberInput,
  pathsMatchPreview,
  phaseStatusIcon,
  pluralizeFiles,
  PROCESSING_PHASE_LABELS,
  recomputeTimeWindowExclusions,
  renderDropdownField,
  sortUniqueNumbers,
  updateTimeWindowField,
  ZONE_MODES,
  type TimeDateOption,
  type TimeWindowDraft,
  type UIState,
} from "./appLogic";

function emptyUIState(overrides: Partial<UIState> = {}): UIState {
  return {
    preview: null,
    previewError: null,
    previewLoading: false,
    columnMapping: {},
    inputCsvPaths: [],
    mobileLtePaths: [],
    customFilterPaths: [],
    running: false,
    statusText: "",
    statusTone: "idle",
    logs: [],
    result: null,
    excludedOriginalRows: [],
    timeSelectorData: null,
    timeDateOptions: [],
    timeWindows: [],
    timeSelectorLoading: false,
    timeSelectorError: "",
    csvLoadDialog: { visible: false, stage: "preview", showTimeStep: true, fileCount: 0 },
    activeDropdown: null,
    processingPhases: [],
    ...overrides,
  };
}

describe("inputRadioTechUiLabel", () => {
  it("maps known tech strings", () => {
    expect(inputRadioTechUiLabel("5g")).toBe("5G (NR)");
    expect(inputRadioTechUiLabel("lte")).toBe("LTE");
  });

  it("falls back for unknown or empty", () => {
    expect(inputRadioTechUiLabel(undefined)).toBe("neznámy typ");
    expect(inputRadioTechUiLabel("")).toBe("neznámy typ");
    expect(inputRadioTechUiLabel("wifi")).toBe("neznámy typ");
  });
});

describe("pathsMatchPreview", () => {
  const preview = new main.CSVPreview({
    filePaths: ["/a.csv", "/b.csv"],
    filePath: "",
    columns: [],
    encoding: "utf-8",
    headerLine: 0,
    originalHeader: "",
    suggestedMapping: {},
    inputRadioTech: "5g",
  });

  it("returns false for empty paths or no preview", () => {
    expect(pathsMatchPreview([], preview)).toBe(false);
    expect(pathsMatchPreview(["/x"], null)).toBe(false);
  });

  it("matches filePaths order", () => {
    expect(pathsMatchPreview(["/a.csv", "/b.csv"], preview)).toBe(true);
    expect(pathsMatchPreview(["/b.csv", "/a.csv"], preview)).toBe(false);
  });

  it("uses single filePath when filePaths empty", () => {
    const single = new main.CSVPreview({
      filePaths: [],
      filePath: "/only.csv",
      columns: [],
      encoding: "utf-8",
      headerLine: 0,
      originalHeader: "",
      suggestedMapping: {},
      inputRadioTech: "",
    });
    expect(pathsMatchPreview(["/only.csv"], single)).toBe(true);
  });
});

describe("mappingComplete", () => {
  it("requires preview and all column indices in range", () => {
    const preview = new main.CSVPreview({
      filePaths: [],
      filePath: "/x",
      columns: ["c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7"],
      encoding: "",
      headerLine: 0,
      originalHeader: "",
      suggestedMapping: {},
      inputRadioTech: "",
    });
    const base = emptyUIState({ preview });
    expect(mappingComplete(base)).toBe(false);

    const full: Partial<Record<(typeof COLUMN_FIELDS)[number]["key"], number>> = {};
    for (let i = 0; i < COLUMN_FIELDS.length; i++) {
      full[COLUMN_FIELDS[i].key] = i;
    }
    expect(mappingComplete({ ...base, columnMapping: full })).toBe(true);
    expect(mappingComplete({ ...base, columnMapping: { ...full, latitude: 99 } })).toBe(false);
  });
});

describe("computeReadinessItems", () => {
  it("aggregates csv, preview sync, mapping, and mobile LTE", () => {
    const preview = new main.CSVPreview({
      filePaths: ["/in.csv"],
      filePath: "",
      columns: ["a", "b", "c", "d", "e", "f", "g", "h"],
      encoding: "",
      headerLine: 0,
      originalHeader: "",
      suggestedMapping: {},
      inputRadioTech: "",
    });
    const mapping: UIState["columnMapping"] = {};
    COLUMN_FIELDS.forEach((f, i) => {
      mapping[f.key] = i;
    });
    const state = emptyUIState({
      preview,
      previewLoading: false,
      timeSelectorLoading: false,
      previewError: null,
      columnMapping: mapping,
    });

    const ready = computeReadinessItems(["/in.csv"], state, false, []);
    expect(ready.every((x) => x.ok)).toBe(true);

    const noLte = computeReadinessItems(["/in.csv"], state, true, []);
    expect(noLte.find((x) => x.id === "mobile")?.ok).toBe(false);

    const withLte = computeReadinessItems(["/in.csv"], state, true, ["/lte.csv"]);
    expect(withLte.find((x) => x.id === "mobile")?.ok).toBe(true);
  });
});

describe("buildProcessingPhaseRows", () => {
  it("includes mobile_sync only when mobile_mode_enabled", () => {
    const base = new backend.ProcessingConfig({
      file_path: "/x",
      column_mapping: {},
      keep_original_rows: false,
      excluded_original_rows: [],
      zone_mode: "segments",
      zone_size_m: 100,
      rsrp_threshold: -110,
      sinr_threshold: -5,
      include_empty_zones: false,
      add_custom_operators: false,
      custom_operators: [],
      mobile_mode_enabled: false,
      mobile_time_tolerance_ms: 0,
      mobile_require_nr_yes: false,
      mobile_nr_column_name: "",
      progress_enabled: false,
    });
    const withoutMobile = buildProcessingPhaseRows(base);
    expect(withoutMobile.map((p) => p.id)).not.toContain("mobile_sync");

    base.mobile_mode_enabled = true;
    const withMobile = buildProcessingPhaseRows(base);
    expect(withMobile.map((p) => p.id)).toContain("mobile_sync");
    expect(withMobile[0].label).toBe(PROCESSING_PHASE_LABELS.load_csv);
  });
});

describe("pluralizeFiles", () => {
  it("uses Slovak plural rules", () => {
    expect(pluralizeFiles(1)).toBe("súbor");
    expect(pluralizeFiles(2)).toBe("súbory");
    expect(pluralizeFiles(5)).toBe("súborov");
    expect(pluralizeFiles(22)).toBe("súbory");
    expect(pluralizeFiles(12)).toBe("súborov");
  });
});

describe("phaseStatusIcon", () => {
  it("returns symbols per status", () => {
    expect(phaseStatusIcon("done")).toBe("✓");
    expect(phaseStatusIcon("active")).toBe("›");
    expect(phaseStatusIcon("error")).toBe("✕");
    expect(phaseStatusIcon("pending")).toBe("○");
  });
});

describe("cssEscape", () => {
  it("uses CSS.escape when available", () => {
    expect(cssEscape("a.b")).toBeDefined();
  });

  it("falls back without CSS.escape", () => {
    const original = globalThis.CSS;
    // @ts-expect-error test shim
    delete globalThis.CSS;
    expect(cssEscape('say "hi"')).toContain("\\");
    globalThis.CSS = original;
  });
});

describe("filterDropdownOptions", () => {
  it("filters case-insensitively with sk locale", () => {
    const opts = ["Bratislava", "Košice"];
    expect(filterDropdownOptions(opts, "")).toEqual(opts);
    expect(filterDropdownOptions(opts, "brat")).toEqual(["Bratislava"]);
    expect(filterDropdownOptions(opts, "  ")).toEqual(opts);
  });
});

describe("time window helpers", () => {
  const dateOptions: TimeDateOption[] = [
    {
      dateLabel: "01.01.2024",
      times: [
        { timeLabel: "10:00:00.000", timestampMS: 1_704_110_400_000 },
        { timeLabel: "11:00:00.000", timestampMS: 1_704_114_000_000 },
      ],
    },
  ];

  it("getTimeOptionsForDate and flattenTimeDateOptions", () => {
    expect(getTimeOptionsForDate(dateOptions, "  ")).toEqual([]);
    expect(getTimeOptionsForDate(dateOptions, "01.01.2024")).toHaveLength(2);
    expect(flattenTimeDateOptions(dateOptions)).toHaveLength(2);
  });

  it("updateTimeWindowField clears incompatible time on date change", () => {
    let w: TimeWindowDraft = {
      id: "w1",
      startDateInput: "01.01.2024",
      startTimeInput: "10:00:00.000",
      endDateInput: "",
      endTimeInput: "",
      startTimestampMS: 1,
      endTimestampMS: null,
      excludedRows: 0,
    };
    w = updateTimeWindowField(w, "startDate", "01.01.2024", dateOptions);
    expect(w.startTimeInput).toBe("10:00:00.000");
    w = updateTimeWindowField(w, "startDate", "missing", dateOptions);
    expect(w.startTimeInput).toBe("");
  });

  it("isCompleteTimeWindow and describeTimeWindow", () => {
    const incomplete: TimeWindowDraft = {
      id: "x",
      startDateInput: "",
      startTimeInput: "",
      endDateInput: "",
      endTimeInput: "",
      startTimestampMS: null,
      endTimestampMS: null,
      excludedRows: 0,
    };
    expect(isCompleteTimeWindow(incomplete)).toBe(false);
    expect(describeTimeWindow(incomplete)).toContain("dropdownoch");

    const badOrder: TimeWindowDraft = {
      ...incomplete,
      startDateInput: "d",
      startTimeInput: "t",
      endDateInput: "d",
      endTimeInput: "t",
      startTimestampMS: 100,
      endTimestampMS: 50,
    };
    expect(describeTimeWindow(badOrder)).toContain("po začiatku");

    const ok: TimeWindowDraft = {
      ...badOrder,
      startTimestampMS: 10,
      endTimestampMS: 20,
      excludedRows: 3,
    };
    expect(isCompleteTimeWindow(ok)).toBe(true);
    expect(describeTimeWindow(ok)).toContain("3 bodov");
  });

  it("buildDefaultTimeWindow picks spaced indices", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T12:00:00Z"));
    const many: TimeDateOption[] = Array.from({ length: 40 }, (_, i) => ({
      dateLabel: `0${i}.01.2024`,
      times: [{ timeLabel: "12:00:00.000", timestampMS: 1_704_067_200_000 + i * 60_000 }],
    }));
    const win = buildDefaultTimeWindow(many, 5);
    expect(win.startDateInput).toBeTruthy();
    expect(win.endDateInput).toBeTruthy();
    vi.useRealTimers();
  });
});

describe("buildTimeDateOptions", () => {
  it("groups by date and sorts", () => {
    const rows = [
      new backend.TimeSelectorRow({ original_row: 2, timestamp_ms: 1_704_114_000_000 }),
      new backend.TimeSelectorRow({ original_row: 1, timestamp_ms: 1_704_110_400_000 }),
    ];
    const opts = buildTimeDateOptions(rows);
    expect(opts.length).toBeGreaterThan(0);
    expect(opts[0].times[0].timestampMS).toBeLessThanOrEqual(opts[0].times[opts[0].times.length - 1].timestampMS);
  });
});

describe("recomputeTimeWindowExclusions", () => {
  it("resets when no time selector data", () => {
    const windows: TimeWindowDraft[] = [
      {
        id: "w",
        startDateInput: "",
        startTimeInput: "",
        endDateInput: "",
        endTimeInput: "",
        startTimestampMS: null,
        endTimestampMS: null,
        excludedRows: 5,
      },
    ];
    const out = recomputeTimeWindowExclusions(null, windows);
    expect(out.excludedOriginalRows).toEqual([]);
    expect(out.timeWindows[0].excludedRows).toBe(0);
  });

  it("counts matches and unions original_row", () => {
    const data = new backend.TimeSelectorData({
      rows: [
        new backend.TimeSelectorRow({ original_row: 1, timestamp_ms: 100 }),
        new backend.TimeSelectorRow({ original_row: 1, timestamp_ms: 150 }),
        new backend.TimeSelectorRow({ original_row: 2, timestamp_ms: 200 }),
      ],
      total_rows: 3,
      timed_rows: 3,
      min_time_ms: 100,
      max_time_ms: 200,
      strategy: "utc_numeric",
    });
    const windows: TimeWindowDraft[] = [
      {
        id: "a",
        startDateInput: "",
        startTimeInput: "",
        endDateInput: "",
        endTimeInput: "",
        startTimestampMS: 100,
        endTimestampMS: 160,
        excludedRows: 0,
      },
      {
        id: "b",
        startDateInput: "",
        startTimeInput: "",
        endDateInput: "",
        endTimeInput: "",
        startTimestampMS: 180,
        endTimestampMS: 250,
        excludedRows: 0,
      },
    ];
    const out = recomputeTimeWindowExclusions(data, windows);
    expect(out.timeWindows[0].excludedRows).toBe(2);
    expect(out.timeWindows[1].excludedRows).toBe(1);
    expect(out.excludedOriginalRows).toEqual([1, 2]);
  });
});

describe("formatting and labels", () => {
  it("formatDateTimeParts is consistent with formatDateTimeLabel", () => {
    const ms = Date.UTC(2024, 0, 15, 14, 30, 45, 123);
    const parts = formatDateTimeParts(ms);
    expect(formatDateTimeLabel(ms)).toBe(`${parts.dateLabel} ${parts.timeLabel}`);
  });

  it("labelForTimeStrategy", () => {
    expect(labelForTimeStrategy("utc_numeric")).toBe("UTC číslo");
    expect(labelForTimeStrategy(undefined)).toBe("neznáme");
    expect(labelForTimeStrategy("custom_x")).toBe("custom_x");
  });

  it("formatPercent", () => {
    expect(formatPercent(12.345)).toBe("12.35 %");
    expect(formatPercent(undefined)).toBe("n/a");
    expect(formatPercent(NaN)).toBe("n/a");
  });
});

describe("escapeHtml", () => {
  it("escapes special characters", () => {
    expect(escapeHtml(`<a href="x">y & z</a>`)).toBe(
      "&lt;a href=&quot;x&quot;&gt;y &amp; z&lt;/a&gt;"
    );
  });
});

describe("dedupePaths and sortUniqueNumbers", () => {
  it("dedupePaths trims and skips empty", () => {
    expect(dedupePaths([" /a ", " /a ", ""])).toEqual(["/a"]);
  });

  it("sortUniqueNumbers drops non-finite", () => {
    expect(sortUniqueNumbers([3, 1, 3, NaN, 2])).toEqual([1, 2, 3]);
  });
});

describe("parseNumberInput / parseIntegerInput", () => {
  function input(value: string): HTMLInputElement {
    const el = document.createElement("input");
    el.value = value;
    return el;
  }

  it("parseNumberInput accepts comma decimal", () => {
    expect(parseNumberInput(input("1,5"), "x")).toBe(1.5);
    expect(() => parseNumberInput(input(""), "x")).toThrow("Chýba hodnota");
    expect(() => parseNumberInput(input("x"), "x")).toThrow("Neplatná");
  });

  it("parseIntegerInput enforces integer >= 0", () => {
    expect(parseIntegerInput(input("0"), "t")).toBe(0);
    expect(() => parseIntegerInput(input("1.2"), "t")).toThrow("celé číslo");
    expect(() => parseIntegerInput(input("-1"), "t")).toThrow("celé číslo");
  });
});

describe("parseCustomOperatorsText", () => {
  it("parses MCC:MNC and MCC:MNC:PCI", () => {
    expect(parseCustomOperatorsText("231:01 231:02:10")).toEqual([
      { mcc: "231", mnc: "01", pci: "" },
      { mcc: "231", mnc: "02", pci: "10" },
    ]);
  });

  it("rejects invalid tokens", () => {
    expect(() => parseCustomOperatorsText("231")).toThrow("Neplatný operátor");
    expect(() => parseCustomOperatorsText("::")).toThrow("MCC a MNC");
  });
});

describe("file path helpers", () => {
  it("fileParentDir and fileBasename", () => {
    expect(fileBasename("/home/user/file.csv")).toBe("file.csv");
    expect(fileParentDir("/home/user/file.csv")).toBe("/home/user");
    expect(fileBasename("relative.csv")).toBe("relative.csv");
    expect(fileParentDir("relative.csv")).toBe("");
  });
});

describe("formatTimestampForFilename", () => {
  it("uses local date parts", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(Date.UTC(2024, 5, 7, 15, 9, 0)));
    const s = formatTimestampForFilename();
    expect(s).toMatch(/^\d{4}-\d{2}-\d{2}_\d{4}$/);
    vi.useRealTimers();
  });
});

describe("renderDropdownField", () => {
  it("renders closed trigger and escapes id", () => {
    const windowDraft: TimeWindowDraft = {
      id: 'w"><script',
      startDateInput: "",
      startTimeInput: "",
      endDateInput: "",
      endTimeInput: "",
      startTimestampMS: null,
      endTimestampMS: null,
      excludedRows: 0,
    };
    const state = emptyUIState({ activeDropdown: null });
    const html = renderDropdownField({
      window: windowDraft,
      field: "startDate",
      label: "Dátum",
      value: "",
      placeholder: "Vyber",
      options: [],
      state,
    });
    expect(html).toContain("data-window-id=");
    expect(html).not.toContain("<script");
    expect(html).toContain("dropdown-trigger");
  });

  it("renders open panel with search when activeDropdown matches", () => {
    const windowDraft: TimeWindowDraft = {
      id: "w1",
      startDateInput: "",
      startTimeInput: "",
      endDateInput: "",
      endTimeInput: "",
      startTimestampMS: null,
      endTimestampMS: null,
      excludedRows: 0,
    };
    const state = emptyUIState({
      activeDropdown: { windowId: "w1", field: "startDate", query: "ab" },
    });
    const html = renderDropdownField({
      window: windowDraft,
      field: "startDate",
      label: "Dátum",
      value: "Alpha",
      placeholder: "Vyber",
      options: ["Alpha", "Beta"],
      state,
    });
    expect(html).toContain("dropdown-panel");
    expect(html).toContain('value="ab"');
    expect(html).toContain("is-selected");
  });
});

describe("ZONE_MODES", () => {
  it("has expected zone mode values", () => {
    expect(ZONE_MODES.map((z) => z.value)).toEqual(["segments", "center", "original"]);
  });
});
