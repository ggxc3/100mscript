const std = @import("std");

const CandidateNames = struct {
    key: []const u8,
    names: []const []const u8,
};

const HEADER_CANDIDATES = [_]CandidateNames{
    .{ .key = "latitude", .names = &.{ "Latitude", "Lat" } },
    .{ .key = "longitude", .names = &.{ "Longitude", "Lon", "Lng" } },
    .{ .key = "frequency", .names = &.{ "SSRef", "Frequency" } },
    .{ .key = "pci", .names = &.{ "PCI", "NPCI", "Physical Cell ID" } },
    .{ .key = "mcc", .names = &.{ "MCC" } },
    .{ .key = "mnc", .names = &.{ "MNC" } },
    .{ .key = "rsrp", .names = &.{ "SSS-RSRP", "RSRP", "NR-SS-RSRP" } },
    .{ .key = "sinr", .names = &.{ "SSS-SINR", "SINR", "NR-SS-SINR" } },
};

pub const SuggestionMap = struct {
    latitude: []const u8,
    longitude: []const u8,
    frequency: []const u8,
    pci: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    rsrp: []const u8,
    sinr: []const u8,
};

pub const DetectedEntry = struct {
    letter: []const u8,
    header: []const u8,
};

pub const DetectedMap = struct {
    latitude: ?DetectedEntry = null,
    longitude: ?DetectedEntry = null,
    frequency: ?DetectedEntry = null,
    pci: ?DetectedEntry = null,
    mcc: ?DetectedEntry = null,
    mnc: ?DetectedEntry = null,
    rsrp: ?DetectedEntry = null,
    sinr: ?DetectedEntry = null,
};

pub const InspectCsvResult = struct {
    lineIndex: i64,
    headerText: []const u8,
    headers: []const []const u8,
    delimiter: []const u8,
    decoding: []const u8,
    suggestions: SuggestionMap,
    detected: DetectedMap,

    pub fn deinit(self: *InspectCsvResult, allocator: std.mem.Allocator) void {
        allocator.free(self.headerText);
        for (self.headers) |h| allocator.free(h);
        allocator.free(self.headers);
        allocator.free(self.delimiter);
        allocator.free(self.decoding);
        freeSuggestionMap(allocator, &self.suggestions);
        freeDetectedMap(allocator, &self.detected);
        self.* = undefined;
    }
};

const JsonOutput = struct {
    lineIndex: i64,
    headerText: []const u8,
    headers: []const []const u8,
    delimiter: []const u8,
    decoding: []const u8,
    suggestions: SuggestionMap,
    detected: DetectedMap,
};

fn defaultSuggestions(allocator: std.mem.Allocator) !SuggestionMap {
    return .{
        .latitude = try allocator.dupe(u8, "D"),
        .longitude = try allocator.dupe(u8, "E"),
        .frequency = try allocator.dupe(u8, "K"),
        .pci = try allocator.dupe(u8, "L"),
        .mcc = try allocator.dupe(u8, "M"),
        .mnc = try allocator.dupe(u8, "N"),
        .rsrp = try allocator.dupe(u8, "W"),
        .sinr = try allocator.dupe(u8, "V"),
    };
}

fn freeSuggestionMap(allocator: std.mem.Allocator, s: *SuggestionMap) void {
    allocator.free(s.latitude);
    allocator.free(s.longitude);
    allocator.free(s.frequency);
    allocator.free(s.pci);
    allocator.free(s.mcc);
    allocator.free(s.mnc);
    allocator.free(s.rsrp);
    allocator.free(s.sinr);
}

fn freeDetectedMap(allocator: std.mem.Allocator, d: *DetectedMap) void {
    inline for (.{
        &d.latitude,
        &d.longitude,
        &d.frequency,
        &d.pci,
        &d.mcc,
        &d.mnc,
        &d.rsrp,
        &d.sinr,
    }) |entry_ptr| {
        if (entry_ptr.*) |entry| {
            allocator.free(entry.letter);
            allocator.free(entry.header);
        }
    }
}

fn setSuggestion(s: *SuggestionMap, key: []const u8, value: []const u8) void {
    if (std.mem.eql(u8, key, "latitude")) s.latitude = value
    else if (std.mem.eql(u8, key, "longitude")) s.longitude = value
    else if (std.mem.eql(u8, key, "frequency")) s.frequency = value
    else if (std.mem.eql(u8, key, "pci")) s.pci = value
    else if (std.mem.eql(u8, key, "mcc")) s.mcc = value
    else if (std.mem.eql(u8, key, "mnc")) s.mnc = value
    else if (std.mem.eql(u8, key, "rsrp")) s.rsrp = value
    else if (std.mem.eql(u8, key, "sinr")) s.sinr = value;
}

fn replaceSuggestionOwned(allocator: std.mem.Allocator, s: *SuggestionMap, key: []const u8, value: []const u8) void {
    if (std.mem.eql(u8, key, "latitude")) {
        allocator.free(s.latitude);
        s.latitude = value;
    } else if (std.mem.eql(u8, key, "longitude")) {
        allocator.free(s.longitude);
        s.longitude = value;
    } else if (std.mem.eql(u8, key, "frequency")) {
        allocator.free(s.frequency);
        s.frequency = value;
    } else if (std.mem.eql(u8, key, "pci")) {
        allocator.free(s.pci);
        s.pci = value;
    } else if (std.mem.eql(u8, key, "mcc")) {
        allocator.free(s.mcc);
        s.mcc = value;
    } else if (std.mem.eql(u8, key, "mnc")) {
        allocator.free(s.mnc);
        s.mnc = value;
    } else if (std.mem.eql(u8, key, "rsrp")) {
        allocator.free(s.rsrp);
        s.rsrp = value;
    } else if (std.mem.eql(u8, key, "sinr")) {
        allocator.free(s.sinr);
        s.sinr = value;
    }
}

fn hasDetected(d: *const DetectedMap, key: []const u8) bool {
    if (std.mem.eql(u8, key, "latitude")) return d.latitude != null;
    if (std.mem.eql(u8, key, "longitude")) return d.longitude != null;
    if (std.mem.eql(u8, key, "frequency")) return d.frequency != null;
    if (std.mem.eql(u8, key, "pci")) return d.pci != null;
    if (std.mem.eql(u8, key, "mcc")) return d.mcc != null;
    if (std.mem.eql(u8, key, "mnc")) return d.mnc != null;
    if (std.mem.eql(u8, key, "rsrp")) return d.rsrp != null;
    if (std.mem.eql(u8, key, "sinr")) return d.sinr != null;
    return false;
}

fn setDetected(d: *DetectedMap, key: []const u8, entry: DetectedEntry) void {
    if (std.mem.eql(u8, key, "latitude")) d.latitude = entry
    else if (std.mem.eql(u8, key, "longitude")) d.longitude = entry
    else if (std.mem.eql(u8, key, "frequency")) d.frequency = entry
    else if (std.mem.eql(u8, key, "pci")) d.pci = entry
    else if (std.mem.eql(u8, key, "mcc")) d.mcc = entry
    else if (std.mem.eql(u8, key, "mnc")) d.mnc = entry
    else if (std.mem.eql(u8, key, "rsrp")) d.rsrp = entry
    else if (std.mem.eql(u8, key, "sinr")) d.sinr = entry;
}

fn asciiLower(c: u8) u8 {
    return if (c >= 'A' and c <= 'Z') c + 32 else c;
}

fn isAsciiAlnum(c: u8) bool {
    return (c >= 'a' and c <= 'z') or (c >= 'A' and c <= 'Z') or (c >= '0' and c <= '9');
}

fn normalizeHeaderNameAlloc(allocator: std.mem.Allocator, text: []const u8) ![]const u8 {
    const trimmed = std.mem.trim(u8, text, " \t\r\n");
    var out = std.ArrayList(u8).empty;
    errdefer out.deinit(allocator);
    for (trimmed) |ch| {
        if (!isAsciiAlnum(ch)) continue;
        try out.append(allocator, asciiLower(ch));
    }
    return try out.toOwnedSlice(allocator);
}

fn headerMatchesAny(allocator: std.mem.Allocator, normalized_header: []const u8, names: []const []const u8) !bool {
    for (names) |candidate| {
        const normalized_candidate = try normalizeHeaderNameAlloc(allocator, candidate);
        defer allocator.free(normalized_candidate);
        if (std.mem.eql(u8, normalized_header, normalized_candidate)) return true;
    }
    return false;
}

fn colIndexToLetterAlloc(allocator: std.mem.Allocator, idx: usize) ![]const u8 {
    var n = idx + 1;
    var buf: [16]u8 = undefined;
    var pos: usize = buf.len;
    while (n > 0) {
        const rem = (n - 1) % 26;
        pos -= 1;
        buf[pos] = @as(u8, @intCast('A' + rem));
        n = (n - 1) / 26;
    }
    return try allocator.dupe(u8, buf[pos..]);
}

fn countDelimitedColumns(line_raw: []const u8, delimiter: u8) usize {
    const line = std.mem.trimRight(u8, line_raw, "\r\n");
    if (line.len == 0) return 0;

    var count: usize = 1;
    for (line) |ch| {
        if (ch == delimiter) count += 1;
    }
    var i = line.len;
    while (i > 0 and line[i - 1] == delimiter) : (i -= 1) {
        count -= 1;
    }
    return count;
}

fn splitDelimitedColumnsAlloc(allocator: std.mem.Allocator, line_raw: []const u8, delimiter: u8) ![][]const u8 {
    const line = std.mem.trimRight(u8, line_raw, "\r\n");
    var list = std.ArrayList([]const u8).empty;
    errdefer list.deinit(allocator);

    var it = std.mem.splitScalar(u8, line, delimiter);
    while (it.next()) |part| {
        const trimmed = std.mem.trim(u8, part, " \t");
        try list.append(allocator, try allocator.dupe(u8, trimmed));
    }
    while (list.items.len > 0 and list.items[list.items.len - 1].len == 0) {
        _ = list.pop();
    }
    return try list.toOwnedSlice(allocator);
}

fn hasTabularFollowupGeneric(lines: [][]const u8, start_index: usize, expected_columns: usize, delimiter: u8, min_columns: usize) bool {
    var seen_candidates: usize = 0;
    var tabular_rows: usize = 0;
    var i = start_index + 1;
    while (i < lines.len) : (i += 1) {
        const line = lines[i];
        if (std.mem.trim(u8, line, " \t\r\n").len == 0) continue;
        seen_candidates += 1;
        const cols_count = countDelimitedColumns(line, delimiter);
        const threshold = @max(min_columns, if (expected_columns > 0) expected_columns - 1 else 0);
        if (cols_count >= threshold) {
            tabular_rows += 1;
            if (tabular_rows >= 2) return true;
        }
        if (seen_candidates >= 25) break;
    }
    return false;
}

const HeaderCandidate = struct {
    line_index: isize,
    header_text: []const u8,
    headers: [][]const u8,
    delimiter: u8,
    score: i64,
};

fn freeHeaderCandidate(allocator: std.mem.Allocator, candidate: *HeaderCandidate) void {
    for (candidate.headers) |h| allocator.free(h);
    allocator.free(candidate.headers);
    allocator.free(candidate.header_text);
    candidate.* = undefined;
}

fn cloneHeaderCandidate(allocator: std.mem.Allocator, c: HeaderCandidate) !HeaderCandidate {
    var headers = try allocator.alloc([]const u8, c.headers.len);
    errdefer allocator.free(headers);
    for (c.headers, 0..) |h, i| {
        headers[i] = try allocator.dupe(u8, h);
    }
    return .{
        .line_index = c.line_index,
        .header_text = try allocator.dupe(u8, c.header_text),
        .headers = headers,
        .delimiter = c.delimiter,
        .score = c.score,
    };
}

fn maybeReplaceBestCandidate(allocator: std.mem.Allocator, best: *?HeaderCandidate, candidate: HeaderCandidate) !void {
    if (best.*) |*current| {
        if (candidate.score <= current.score) {
            freeHeaderCandidate(allocator, @constCast(&candidate));
            return;
        }
        freeHeaderCandidate(allocator, current);
        current.* = candidate;
        return;
    }
    best.* = candidate;
}

fn scoreHeaderCandidateAlloc(parts: [][]const u8) i64 {
    if (parts.len == 0) return -1;
    var non_empty: i64 = 0;
    var alphaish: i64 = 0;
    for (parts) |p| {
        const t = std.mem.trim(u8, p, " \t");
        if (t.len > 0) non_empty += 1;
        var has_alpha = false;
        for (t) |ch| {
            if ((ch >= 'A' and ch <= 'Z') or (ch >= 'a' and ch <= 'z')) {
                has_alpha = true;
                break;
            }
        }
        if (has_alpha) alphaish += 1;
    }
    if (non_empty < 4) return -1;
    return non_empty * 10 + alphaish;
}

fn detectHeaderInLinesForDelimiter(allocator: std.mem.Allocator, lines: [][]const u8, delimiter: u8, min_columns: usize) !?HeaderCandidate {
    var first_candidate: ?HeaderCandidate = null;
    var i: usize = 0;
    while (i < lines.len) : (i += 1) {
        const raw = lines[i];
        const cols_count = countDelimitedColumns(raw, delimiter);
        if (cols_count < min_columns) continue;

        const headers = try splitDelimitedColumnsAlloc(allocator, raw, delimiter);
        const score = scoreHeaderCandidateAlloc(headers);
        if (score < 0) {
            for (headers) |h| allocator.free(h);
            allocator.free(headers);
            continue;
        }

        const header_text = try allocator.dupe(u8, std.mem.trimRight(u8, raw, "\r\n"));
        const candidate = HeaderCandidate{
            .line_index = @intCast(i),
            .header_text = header_text,
            .headers = headers,
            .delimiter = delimiter,
            .score = score,
        };

        if (first_candidate == null) {
            first_candidate = try cloneHeaderCandidate(allocator, candidate);
        }

        if (hasTabularFollowupGeneric(lines, i, cols_count, delimiter, min_columns)) {
            if (first_candidate) |*fc| freeHeaderCandidate(allocator, fc);
            return candidate;
        }

        freeHeaderCandidate(allocator, @constCast(&candidate));
    }
    return first_candidate;
}

fn splitLinesAlloc(allocator: std.mem.Allocator, bytes: []const u8) ![][]const u8 {
    var list = std.ArrayList([]const u8).empty;
    errdefer list.deinit(allocator);
    var it = std.mem.splitScalar(u8, bytes, '\n');
    while (it.next()) |line| {
        try list.append(allocator, line);
    }
    return try list.toOwnedSlice(allocator);
}

fn stripNulsAlloc(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    var out = std.ArrayList(u8).empty;
    errdefer out.deinit(allocator);
    for (bytes) |b| {
        if (b == 0) continue;
        try out.append(allocator, b);
    }
    return try out.toOwnedSlice(allocator);
}

fn pushDecodeVariant(allocator: std.mem.Allocator, list: *std.ArrayList(DecodeVariant), label: []const u8, bytes: []const u8) !void {
    const candidate = bytes;
    if (bytes.len == 0) return;
    // dedupe by exact bytes
    for (list.items) |item| {
        if (std.mem.eql(u8, item.bytes, candidate)) return;
    }
    try list.append(allocator, .{
        .label = try allocator.dupe(u8, label),
        .bytes = try allocator.dupe(u8, candidate),
    });
}

const DecodeVariant = struct {
    label: []const u8,
    bytes: []u8,

    fn deinit(self: *DecodeVariant, allocator: std.mem.Allocator) void {
        allocator.free(self.label);
        allocator.free(self.bytes);
        self.* = undefined;
    }
};

fn decodeVariantsAlloc(allocator: std.mem.Allocator, raw: []const u8) ![]DecodeVariant {
    var list = std.ArrayList(DecodeVariant).empty;
    errdefer {
        for (list.items) |*item| item.deinit(allocator);
        list.deinit(allocator);
    }

    if (raw.len >= 3 and raw[0] == 0xef and raw[1] == 0xbb and raw[2] == 0xbf) {
        try pushDecodeVariant(allocator, &list, "utf8-bom", raw[3..]);
    }
    if (raw.len >= 2 and raw[0] == 0xff and raw[1] == 0xfe) {
        const stripped = try stripNulsAlloc(allocator, raw[2..]);
        defer allocator.free(stripped);
        try pushDecodeVariant(allocator, &list, "utf16le-bom", stripped);
    }
    if (raw.len >= 2 and raw[0] == 0xfe and raw[1] == 0xff) {
        const stripped = try stripNulsAlloc(allocator, raw[2..]);
        defer allocator.free(stripped);
        try pushDecodeVariant(allocator, &list, "utf16be-bom", stripped);
    }

    try pushDecodeVariant(allocator, &list, "utf8", raw);
    try pushDecodeVariant(allocator, &list, "latin1", raw);
    const stripped_raw = try stripNulsAlloc(allocator, raw);
    defer allocator.free(stripped_raw);
    try pushDecodeVariant(allocator, &list, "utf16le", stripped_raw);

    return try list.toOwnedSlice(allocator);
}

fn detectBestHeader(allocator: std.mem.Allocator, raw_bytes: []const u8) !struct { header: ?HeaderCandidate, decoding: []const u8 } {
    const variants = try decodeVariantsAlloc(allocator, raw_bytes);
    defer {
        for (variants) |*v| v.deinit(allocator);
        allocator.free(variants);
    }

    var best: ?HeaderCandidate = null;
    var best_decoding = try allocator.dupe(u8, "unknown");
    errdefer allocator.free(best_decoding);

    for (variants) |variant| {
        const lines = try splitLinesAlloc(allocator, variant.bytes);
        defer allocator.free(lines);

        for ([_]u8{ ';', ',', '\t' }) |delim| {
            const cand_opt = try detectHeaderInLinesForDelimiter(allocator, lines, delim, 6);
            if (cand_opt == null) continue;
            var cand = cand_opt.?;
            if (delim == ';') cand.score += 5;
            if (std.mem.indexOfScalar(u8, variant.bytes, 0) != null) cand.score -= 50;

            if (best) |*current| {
                if (cand.score <= current.score) {
                    freeHeaderCandidate(allocator, &cand);
                    continue;
                }
                freeHeaderCandidate(allocator, current);
                allocator.free(best_decoding);
                best_decoding = try allocator.dupe(u8, variant.label);
                current.* = cand;
            } else {
                best = cand;
                allocator.free(best_decoding);
                best_decoding = try allocator.dupe(u8, variant.label);
            }
        }
    }

    return .{ .header = best, .decoding = best_decoding };
}

fn buildDetectedAndSuggestions(allocator: std.mem.Allocator, headers: [][]const u8) !struct { suggestions: SuggestionMap, detected: DetectedMap } {
    var suggestions = try defaultSuggestions(allocator);
    errdefer freeSuggestionMap(allocator, &suggestions);
    var detected = DetectedMap{};
    errdefer freeDetectedMap(allocator, &detected);

    for (headers, 0..) |raw_header, idx| {
        const normalized = try normalizeHeaderNameAlloc(allocator, raw_header);
        defer allocator.free(normalized);
        if (normalized.len == 0) continue;

        for (HEADER_CANDIDATES) |hc| {
            if (hasDetected(&detected, hc.key)) continue;
            if (!(try headerMatchesAny(allocator, normalized, hc.names))) continue;

            const letter = try colIndexToLetterAlloc(allocator, idx);
            errdefer allocator.free(letter);
            const suggestion_letter = try allocator.dupe(u8, letter);
            errdefer allocator.free(suggestion_letter);
            const header_dup = try allocator.dupe(u8, raw_header);
            errdefer allocator.free(header_dup);

            replaceSuggestionOwned(allocator, &suggestions, hc.key, suggestion_letter);
            setDetected(&detected, hc.key, .{ .letter = letter, .header = header_dup });
            break;
        }
    }

    return .{ .suggestions = suggestions, .detected = detected };
}

pub fn inspectCsvHeaders(allocator: std.mem.Allocator, file_path: []const u8) !InspectCsvResult {
    const file = try std.fs.cwd().openFile(file_path, .{});
    defer file.close();

    const stat = try file.stat();
    if (stat.size > std.math.maxInt(usize)) return error.FileTooBig;
    const max_read: usize = @intCast(if (stat.size == 0) @as(u64, 1) else stat.size);
    const bytes = try file.readToEndAlloc(allocator, max_read);
    defer allocator.free(bytes);

    var best = try detectBestHeader(allocator, bytes);
    errdefer {
        if (best.header) |*h| freeHeaderCandidate(allocator, h);
        allocator.free(best.decoding);
    }

    if (best.header == null) {
        var suggestions = try defaultSuggestions(allocator);
        errdefer freeSuggestionMap(allocator, &suggestions);
        return .{
            .lineIndex = 0,
            .headerText = try allocator.dupe(u8, ""),
            .headers = try allocator.alloc([]const u8, 0),
            .delimiter = try allocator.dupe(u8, ";"),
            .decoding = best.decoding,
            .suggestions = suggestions,
            .detected = DetectedMap{},
        };
    }

    const header = best.header.?;
    const built = try buildDetectedAndSuggestions(allocator, header.headers);
    errdefer {
        var s = built.suggestions;
        freeSuggestionMap(allocator, &s);
        var d = built.detected;
        freeDetectedMap(allocator, &d);
    }

    const delimiter_text = switch (header.delimiter) {
        ';' => ";",
        ',' => ",",
        '\t' => "\t",
        else => "?",
    };

    return .{
        .lineIndex = header.line_index,
        .headerText = header.header_text,
        .headers = header.headers,
        .delimiter = try allocator.dupe(u8, delimiter_text),
        .decoding = best.decoding,
        .suggestions = built.suggestions,
        .detected = built.detected,
    };
}

pub fn runInspectCsv(allocator: std.mem.Allocator, writer: *std.Io.Writer, file_path: []const u8) !void {
    var res = try inspectCsvHeaders(allocator, file_path);
    defer res.deinit(allocator);

    try std.json.Stringify.value(JsonOutput{
        .lineIndex = res.lineIndex,
        .headerText = res.headerText,
        .headers = res.headers,
        .delimiter = res.delimiter,
        .decoding = res.decoding,
        .suggestions = res.suggestions,
        .detected = res.detected,
    }, .{}, writer);
    try writer.writeByte('\n');
    try writer.flush();
}

test "inspectCsvHeaders detects standard fixture headers" {
    var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa_state.deinit();
    const gpa = gpa_state.allocator();

    var res = try inspectCsvHeaders(gpa, "../test_data/scenarios/test_mcc.csv");
    defer res.deinit(gpa);

    try std.testing.expect(res.headers.len > 5);
    try std.testing.expect(res.detected.latitude != null);
    try std.testing.expect(res.detected.longitude != null);
    try std.testing.expect(res.detected.rsrp != null);
}
