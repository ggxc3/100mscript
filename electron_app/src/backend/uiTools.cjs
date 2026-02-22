const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");

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

const COLUMN_HEADER_CANDIDATES = {
  latitude: ["Latitude", "Lat"],
  longitude: ["Longitude", "Lon", "Lng"],
  frequency: ["SSRef", "Frequency"],
  pci: ["PCI", "NPCI", "Physical Cell ID"],
  mcc: ["MCC"],
  mnc: ["MNC"],
  rsrp: ["SSS-RSRP", "RSRP", "NR-SS-RSRP"],
  sinr: ["SSS-SINR", "SINR", "NR-SS-SINR"]
};

function normalizeHeaderName(name) {
  return String(name || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "");
}

function colIndexToLetter(idx) {
  if (!Number.isInteger(idx) || idx < 0) return "A";
  let value = idx + 1;
  let out = "";
  while (value > 0) {
    const rem = (value - 1) % 26;
    out = String.fromCharCode("A".charCodeAt(0) + rem) + out;
    value = Math.floor((value - 1) / 26);
  }
  return out;
}

function splitSemicolonLine(line) {
  const parts = String(line).split(";");
  while (parts.length && String(parts[parts.length - 1]).trim() === "") {
    parts.pop();
  }
  return parts.map((v) => String(v).trim());
}

function splitDelimitedLine(line, delimiter) {
  const parts = String(line).split(delimiter);
  while (parts.length && String(parts[parts.length - 1]).trim() === "") {
    parts.pop();
  }
  return parts.map((v) => String(v).trim());
}

function scoreHeaderCandidate(parts) {
  if (!parts.length) return -1;
  const nonEmpty = parts.filter((p) => p.trim()).length;
  if (nonEmpty < 4) return -1;
  const alphaish = parts.filter((p) => /[A-Za-z]/.test(p)).length;
  return nonEmpty * 10 + alphaish;
}

function hasTabularFollowup(lines, startIndex, expectedColumns, delimiter, minColumns = 6) {
  let seenCandidates = 0;
  let tabularRows = 0;

  for (let i = startIndex + 1; i < lines.length; i += 1) {
    const line = String(lines[i] || "");
    if (!line.trim()) continue;
    seenCandidates += 1;
    const colsCount = splitDelimitedLine(line, delimiter).length;
    if (colsCount >= Math.max(minColumns, expectedColumns - 1)) {
      tabularRows += 1;
      if (tabularRows >= 2) return true;
    }
    if (seenCandidates >= 25) break;
  }
  return false;
}

function findTabularHeader(lines, delimiter, minColumns = 6) {
  let firstCandidate = null;

  for (let i = 0; i < lines.length; i += 1) {
    const raw = String(lines[i] || "");
    const cols = splitDelimitedLine(raw, delimiter);
    const colsCount = cols.length;
    if (colsCount < minColumns) continue;

    if (!firstCandidate) {
      firstCandidate = { lineIndex: i, headerText: raw, headers: cols };
    }
    if (hasTabularFollowup(lines, i, colsCount, delimiter, minColumns)) {
      return { lineIndex: i, headerText: raw, headers: cols };
    }
  }

  return firstCandidate;
}

function decodeBufferVariants(buf) {
  const variants = [];
  const push = (label, text) => {
    if (typeof text !== "string" || !text) return;
    const normalized = text.replace(/\u0000/g, "");
    if (!normalized) return;
    if (variants.some((v) => v.text === normalized)) return;
    variants.push({ label, text: normalized });
  };

  if (buf.length >= 3 && buf[0] === 0xef && buf[1] === 0xbb && buf[2] === 0xbf) {
    push("utf8-bom", buf.subarray(3).toString("utf8"));
  }
  if (buf.length >= 2 && buf[0] === 0xff && buf[1] === 0xfe) {
    push("utf16le-bom", buf.subarray(2).toString("utf16le"));
  }
  if (buf.length >= 2 && buf[0] === 0xfe && buf[1] === 0xff) {
    const be = buf.subarray(2);
    const len = be.length - (be.length % 2);
    if (len > 0) {
      const swapped = Buffer.allocUnsafe(len);
      for (let i = 0; i < len; i += 2) {
        swapped[i] = be[i + 1];
        swapped[i + 1] = be[i];
      }
      push("utf16be-bom", swapped.toString("utf16le"));
    }
  }

  push("utf8", buf.toString("utf8"));
  push("latin1", buf.toString("latin1"));
  push("utf16le", buf.toString("utf16le"));

  return variants;
}

function chooseBestHeaderFromDecodedText(text) {
  const lines = String(text || "")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .split("\n")
    .slice(0, 120);

  let best = null;
  for (const delimiter of [";", ",", "\t"]) {
    const candidate = findTabularHeader(lines, delimiter, 6);
    if (!candidate) continue;
    const score = scoreHeaderCandidate(candidate.headers) + (delimiter === ";" ? 5 : 0);
    if (!best || score > best.score) {
      best = { ...candidate, delimiter, score };
    }
  }
  return best;
}

async function inspectCsvHeaders(filePath) {
  const fd = await fsp.open(filePath, "r");
  try {
    const { size } = await fd.stat();
    const toRead = Math.min(size || 0, 512 * 1024) || 512 * 1024;
    const buf = Buffer.allocUnsafe(toRead);
    const { bytesRead } = await fd.read(buf, 0, toRead, 0);
    const sample = buf.subarray(0, bytesRead);

    let best = null;
    for (const decoded of decodeBufferVariants(sample)) {
      const candidate = chooseBestHeaderFromDecodedText(decoded.text);
      if (!candidate) continue;
      if (!best || candidate.score > best.score) {
        best = { ...candidate, decoding: decoded.label };
      }
    }
    if (!best) {
      return {
        lineIndex: 0,
        headerText: "",
        headers: [],
        delimiter: ";",
        decoding: "unknown",
        suggestions: { ...DEFAULT_COLUMN_LETTERS },
        detected: {}
      };
    }
    const { suggestions, detected } = suggestColumnLettersFromHeaders(best.headers, DEFAULT_COLUMN_LETTERS);
    return {
      lineIndex: best.lineIndex,
      headerText: best.headerText,
      headers: best.headers,
      delimiter: best.delimiter,
      decoding: best.decoding,
      suggestions,
      detected
    };
  } finally {
    await fd.close();
  }
}

function suggestColumnLettersFromHeaders(columnNames, baseColumns = DEFAULT_COLUMN_LETTERS) {
  const suggested = { ...baseColumns };
  const detected = {};
  const normalizedCandidates = Object.fromEntries(
    Object.entries(COLUMN_HEADER_CANDIDATES).map(([key, names]) => [
      key,
      new Set(names.map(normalizeHeaderName))
    ])
  );

  for (let idx = 0; idx < columnNames.length; idx += 1) {
    const rawName = columnNames[idx];
    const normalized = normalizeHeaderName(rawName);
    if (!normalized) continue;
    for (const [mappingKey, candidates] of Object.entries(normalizedCandidates)) {
      if (detected[mappingKey]) continue;
      if (!candidates.has(normalized)) continue;
      const letter = colIndexToLetter(idx);
      suggested[mappingKey] = letter;
      detected[mappingKey] = { letter, header: String(rawName) };
    }
  }
  return { suggestions: suggested, detected };
}

async function discoverAutoFilterPaths(inputCsvPath) {
  const out = [];
  const csvPath = String(inputCsvPath || "").trim();
  if (!csvPath) return out;

  const baseDir = path.dirname(csvPath);
  for (const folder of ["filters", "filtre_5G"]) {
    const dir = path.join(baseDir, folder);
    let entries = [];
    try {
      entries = await fsp.readdir(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
      if (!entry.isFile()) continue;
      if (!entry.name.toLowerCase().endsWith(".txt")) continue;
      out.push(path.join(dir, entry.name));
    }
  }
  return out;
}

function dedupePaths(paths) {
  const seen = new Set();
  const out = [];
  for (const raw of paths || []) {
    const value = String(raw || "").trim();
    if (!value) continue;
    if (seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

function pathExists(filePath) {
  try {
    return fs.existsSync(filePath);
  } catch {
    return false;
  }
}

module.exports = {
  DEFAULT_COLUMN_LETTERS,
  inspectCsvHeaders,
  suggestColumnLettersFromHeaders,
  discoverAutoFilterPaths,
  dedupePaths,
  pathExists
};
