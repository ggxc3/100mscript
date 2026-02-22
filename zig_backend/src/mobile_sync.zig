const std = @import("std");
const csv_table = @import("csv_table.zig");

pub const SyncStats = struct {
    rows_total: usize,
    rows_yes: usize,
    rows_no: usize,
    rows_blank: usize,
    rows_with_match: usize,
    conflicting_windows: usize,
    fallback_time_only_rows: usize,
    window_ms: i64,
};

pub const SyncResult = struct {
    table: csv_table.CsvTable,
    stats: SyncStats,
};

const NrScore = enum(u8) {
    blank = 0,
    no = 1,
    yes = 2,
};

const TimeStrategy = enum {
    utc_numeric,
    utc_datetime,
    date_time,
};

const PreparedLteRow = struct {
    time_ms: i64,
    mcc_key: []const u8,
    mnc_key: []const u8,
    score: NrScore,
};

const PreparedFivegRow = struct {
    row_index: usize,
    time_ms: i64,
    mcc_key: []const u8,
    mnc_key: []const u8,
    has_group_keys: bool,
};

fn trimValue(raw: []const u8) []const u8 {
    return std.mem.trim(u8, raw, " \t\r\n");
}

fn getValue(row: csv_table.CsvRow, idx: usize) []const u8 {
    if (idx >= row.values.len) return "";
    return row.values[idx];
}

fn normalizeHeaderTokenAlloc(allocator: std.mem.Allocator, raw: []const u8) ![]const u8 {
    const trimmed = trimValue(raw);
    var out = std.ArrayList(u8).empty;
    for (trimmed) |ch| {
        const lower = std.ascii.toLower(ch);
        if ((lower >= 'a' and lower <= 'z') or (lower >= '0' and lower <= '9')) {
            try out.append(allocator, lower);
        }
    }
    return out.toOwnedSlice(allocator);
}

fn findColumnByCandidates(
    allocator: std.mem.Allocator,
    columns: [][]const u8,
    candidates: []const []const u8,
) !?usize {
    var normalized = try allocator.alloc([]const u8, columns.len);
    for (columns, 0..) |c, i| normalized[i] = try normalizeHeaderTokenAlloc(allocator, c);
    defer {
        for (normalized) |n| allocator.free(n);
        allocator.free(normalized);
    }

    for (candidates) |cand| {
        const want = try normalizeHeaderTokenAlloc(allocator, cand);
        defer allocator.free(want);
        for (normalized, 0..) |have, i| {
            if (std.mem.eql(u8, have, want)) return i;
        }
    }
    return null;
}

fn parseFloatLike(allocator: std.mem.Allocator, raw: []const u8) ?f64 {
    const trimmed = trimValue(raw);
    if (trimmed.len == 0) return null;
    var has_comma = false;
    for (trimmed) |ch| {
        if (ch == ',') {
            has_comma = true;
            break;
        }
    }
    const normalized = if (!has_comma) trimmed else blk: {
        var buf = std.ArrayList(u8).empty;
        defer buf.deinit(allocator);
        for (trimmed) |ch| {
            buf.append(allocator, if (ch == ',') '.' else ch) catch return null;
        }
        break :blk buf.toOwnedSlice(allocator) catch return null;
    };
    defer if (has_comma) allocator.free(normalized);
    return std.fmt.parseFloat(f64, normalized) catch null;
}

fn normalizeKeyAlloc(allocator: std.mem.Allocator, raw: []const u8) ![]const u8 {
    const trimmed = trimValue(raw);
    if (trimmed.len == 0) return try allocator.dupe(u8, "");

    const maybe_num = parseFloatLike(allocator, trimmed);
    if (maybe_num) |num| {
        const rounded = @round(num);
        if (@abs(num - rounded) < 1e-9) {
            const ival: i64 = @intFromFloat(rounded);
            return try std.fmt.allocPrint(allocator, "{d}", .{ival});
        }
    }

    var out = std.ArrayList(u8).empty;
    for (trimmed) |ch| {
        try out.append(allocator, if (ch == ',') '.' else ch);
    }
    // strip trailing .0+
    while (out.items.len >= 2 and out.items[out.items.len - 1] == '0') {
        const dot_pos = std.mem.lastIndexOfScalar(u8, out.items, '.') orelse break;
        if (dot_pos == out.items.len - 2) {
            _ = out.pop();
            _ = out.pop();
            break;
        }
        _ = out.pop();
    }
    return out.toOwnedSlice(allocator);
}

fn normalizeNrScore(raw: []const u8) NrScore {
    const t = trimValue(raw);
    if (t.len == 0) return .blank;

    var lower_buf: [32]u8 = undefined;
    const n = @min(t.len, lower_buf.len);
    for (t[0..n], 0..) |ch, i| lower_buf[i] = std.ascii.toLower(ch);
    const lower = lower_buf[0..n];

    if (std.mem.eql(u8, lower, "yes") or std.mem.eql(u8, lower, "true") or std.mem.eql(u8, lower, "1") or std.mem.eql(u8, lower, "y") or std.mem.eql(u8, lower, "t") or std.mem.eql(u8, lower, "a") or std.mem.eql(u8, lower, "ano")) return .yes;
    if (std.mem.eql(u8, t, "áno")) return .yes;
    if (std.mem.eql(u8, lower, "no") or std.mem.eql(u8, lower, "false") or std.mem.eql(u8, lower, "0") or std.mem.eql(u8, lower, "n") or std.mem.eql(u8, lower, "f")) return .no;
    return .blank;
}

fn parseUtcNumericMs(allocator: std.mem.Allocator, raw: []const u8) ?i64 {
    const value = parseFloatLike(allocator, raw) orelse return null;
    const abs_v = @abs(value);
    const factor: f64 = if (abs_v >= 1e11) 1.0 else 1000.0;
    return @intFromFloat(@round(value * factor));
}

fn civilToDays(y: i32, m: u8, d: u8) i64 {
    var year = y;
    const month = @as(i32, m);
    const day = @as(i32, d);
    year -= if (month <= 2) 1 else 0;
    const era = @divFloor(year, 400);
    const yoe = year - era * 400;
    const month_adj: i32 = if (month > 2) -3 else 9;
    const doy = @divFloor(153 * (month + month_adj) + 2, 5) + day - 1;
    const doe = yoe * 365 + @divFloor(yoe, 4) - @divFloor(yoe, 100) + doy;
    return @as(i64, era) * 146097 + doe - 719468;
}

fn parseDateParts(text: []const u8) ?struct { y: i32, m: u8, d: u8 } {
    const t = trimValue(text);
    if (t.len == 10 and t[4] == '-' and t[7] == '-') {
        const y = std.fmt.parseInt(i32, t[0..4], 10) catch return null;
        const m = std.fmt.parseInt(u8, t[5..7], 10) catch return null;
        const d = std.fmt.parseInt(u8, t[8..10], 10) catch return null;
        return .{ .y = y, .m = m, .d = d };
    }
    if (std.mem.indexOfScalar(u8, t, '.')) |_| {
        var it = std.mem.splitScalar(u8, t, '.');
        const p1 = it.next() orelse return null;
        const p2 = it.next() orelse return null;
        const p3 = it.next() orelse return null;
        const d = std.fmt.parseInt(u8, p1, 10) catch return null;
        const m = std.fmt.parseInt(u8, p2, 10) catch return null;
        const y = std.fmt.parseInt(i32, p3, 10) catch return null;
        return .{ .y = y, .m = m, .d = d };
    }
    return null;
}

fn parseTimeParts(text: []const u8) ?struct { h: u8, mi: u8, s: u8, ms: u16 } {
    const t = trimValue(text);
    var it = std.mem.splitScalar(u8, t, ':');
    const hs = it.next() orelse return null;
    const ms = it.next() orelse return null;
    const ss_full = it.next() orelse return null;

    const h = std.fmt.parseInt(u8, hs, 10) catch return null;
    const mi = std.fmt.parseInt(u8, ms, 10) catch return null;

    var sec_part = ss_full;
    var milli: u16 = 0;
    if (std.mem.indexOfScalar(u8, ss_full, '.')) |dot| {
        sec_part = ss_full[0..dot];
        const frac = ss_full[dot + 1 ..];
        if (frac.len > 0) {
            var frac_buf: [3]u8 = .{ '0', '0', '0' };
            for (frac[0..@min(frac.len, 3)], 0..) |ch, i| frac_buf[i] = ch;
            milli = std.fmt.parseInt(u16, &frac_buf, 10) catch 0;
        }
    }
    const s = std.fmt.parseInt(u8, sec_part, 10) catch return null;
    return .{ .h = h, .mi = mi, .s = s, .ms = milli };
}

fn parseDateTimeMs(date_raw: []const u8, time_raw: []const u8) ?i64 {
    const d = parseDateParts(date_raw) orelse return null;
    const t = parseTimeParts(time_raw) orelse return null;
    const days = civilToDays(d.y, d.m, d.d);
    return days * 86_400_000 +
        @as(i64, t.h) * 3_600_000 +
        @as(i64, t.mi) * 60_000 +
        @as(i64, t.s) * 1000 +
        @as(i64, t.ms);
}

fn buildTimeMsForTable(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
) !struct {
    times: []?i64,
    strategy: TimeStrategy,
} {
    const utc_idx = try findColumnByCandidates(allocator, table.column_names, &.{ "UTC" });
    if (utc_idx) |idx| {
        const times = try allocator.alloc(?i64, table.rows.len);
        var any_numeric = false;
        var any_datetime = false;

        for (table.rows, 0..) |row, i| {
            const raw = getValue(row, idx);
            times[i] = parseUtcNumericMs(allocator, raw);
            if (times[i] != null) any_numeric = true;
        }
        if (any_numeric) return .{ .times = times, .strategy = .utc_numeric };

        // fallback parse UTC text as combined datetime
        for (table.rows, 0..) |row, i| {
            const raw = trimValue(getValue(row, idx));
            if (raw.len == 0) {
                times[i] = null;
                continue;
            }
            if (std.mem.indexOfScalar(u8, raw, ' ')) |sp| {
                times[i] = parseDateTimeMs(raw[0..sp], raw[sp + 1 ..]);
            } else {
                times[i] = null;
            }
            if (times[i] != null) any_datetime = true;
        }
        if (any_datetime) return .{ .times = times, .strategy = .utc_datetime };

        allocator.free(times);
    }

    const date_idx = try findColumnByCandidates(allocator, table.column_names, &.{ "Date" });
    const time_idx = try findColumnByCandidates(allocator, table.column_names, &.{ "Time" });
    if (date_idx != null and time_idx != null) {
        const times = try allocator.alloc(?i64, table.rows.len);
        var any = false;
        for (table.rows, 0..) |row, i| {
            times[i] = parseDateTimeMs(getValue(row, date_idx.?), getValue(row, time_idx.?));
            if (times[i] != null) any = true;
        }
        if (any) return .{ .times = times, .strategy = .date_time };
        allocator.free(times);
    }

    return error.TimeColumnsMissing;
}

fn cloneTableAddColumnIfMissing(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    column_name: []const u8,
) !struct { table: csv_table.CsvTable, col_idx: usize } {
    for (table.column_names, 0..) |name, idx| {
        if (std.mem.eql(u8, name, column_name)) {
            // full clone preserving shape
            const cols = try allocator.alloc([]const u8, table.column_names.len);
            for (table.column_names, 0..) |c, i| cols[i] = try allocator.dupe(u8, c);
            const rows = try allocator.alloc(csv_table.CsvRow, table.rows.len);
            for (table.rows, 0..) |row, i| {
                rows[i] = .{ .values = try allocator.alloc([]const u8, row.values.len), .original_excel_row = row.original_excel_row };
                for (row.values, 0..) |v, j| rows[i].values[j] = try allocator.dupe(u8, v);
            }
            return .{ .table = .{ .column_names = cols, .rows = rows, .header_line = table.header_line }, .col_idx = idx };
        }
    }

    const cols = try allocator.alloc([]const u8, table.column_names.len + 1);
    for (table.column_names, 0..) |c, i| cols[i] = try allocator.dupe(u8, c);
    cols[table.column_names.len] = try allocator.dupe(u8, column_name);

    const rows = try allocator.alloc(csv_table.CsvRow, table.rows.len);
    for (table.rows, 0..) |row, i| {
        rows[i] = .{
            .values = try allocator.alloc([]const u8, row.values.len + 1),
            .original_excel_row = row.original_excel_row,
        };
        for (row.values, 0..) |v, j| rows[i].values[j] = try allocator.dupe(u8, v);
        rows[i].values[row.values.len] = try allocator.dupe(u8, "");
    }
    return .{
        .table = .{ .column_names = cols, .rows = rows, .header_line = table.header_line },
        .col_idx = table.column_names.len,
    };
}

fn filterRowsByNrYes(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    nr_col_idx: usize,
) !csv_table.CsvTable {
    var kept_count: usize = 0;
    for (table.rows) |row| {
        if (normalizeNrScore(getValue(row, nr_col_idx)) == .yes) kept_count += 1;
    }
    if (kept_count == 0) return error.NoRowsWithNrYes;

    const cols = try allocator.alloc([]const u8, table.column_names.len);
    for (table.column_names, 0..) |c, i| cols[i] = try allocator.dupe(u8, c);
    const rows = try allocator.alloc(csv_table.CsvRow, kept_count);

    var out_i: usize = 0;
    for (table.rows) |row| {
        if (normalizeNrScore(getValue(row, nr_col_idx)) != .yes) continue;
        rows[out_i] = .{
            .values = try allocator.alloc([]const u8, row.values.len),
            .original_excel_row = row.original_excel_row,
        };
        for (row.values, 0..) |v, j| rows[out_i].values[j] = try allocator.dupe(u8, v);
        out_i += 1;
    }
    return .{ .column_names = cols, .rows = rows, .header_line = table.header_line };
}

pub fn syncFivegNrFromLte(
    allocator: std.mem.Allocator,
    fiveg_table: csv_table.CsvTable,
    lte_table: csv_table.CsvTable,
    nr_column_name: []const u8,
    time_tolerance_ms: i64,
    require_nr_yes: bool,
) !SyncResult {
    var arena = std.heap.ArenaAllocator.init(allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const lte_mcc_idx = try findColumnByCandidates(a, lte_table.column_names, &.{ "MCC" }) orelse return error.LteMccMissing;
    const lte_mnc_idx = try findColumnByCandidates(a, lte_table.column_names, &.{ "MNC" }) orelse return error.LteMncMissing;
    const lte_nr_idx = try findColumnByCandidates(a, lte_table.column_names, &.{ "5G NR", "5GNR", "NR" }) orelse return error.LteNrMissing;
    const fiveg_mcc_idx = try findColumnByCandidates(a, fiveg_table.column_names, &.{ "MCC" }) orelse return error.FivegMccMissing;
    const fiveg_mnc_idx = try findColumnByCandidates(a, fiveg_table.column_names, &.{ "MNC" }) orelse return error.FivegMncMissing;

    const lte_times = try buildTimeMsForTable(a, lte_table);
    const fiveg_times = try buildTimeMsForTable(a, fiveg_table);
    _ = lte_times.strategy;
    _ = fiveg_times.strategy;

    var lte_prepared = std.ArrayList(PreparedLteRow).empty;
    for (lte_table.rows, 0..) |row, i| {
        const t = lte_times.times[i] orelse continue;
        const mcc = try normalizeKeyAlloc(a, getValue(row, lte_mcc_idx));
        const mnc = try normalizeKeyAlloc(a, getValue(row, lte_mnc_idx));
        if (mcc.len == 0 or mnc.len == 0) continue;
        const score = normalizeNrScore(getValue(row, lte_nr_idx));
        try lte_prepared.append(a, .{ .time_ms = t, .mcc_key = mcc, .mnc_key = mnc, .score = score });
    }
    if (lte_prepared.items.len == 0) return error.NoUsableLteRows;

    var cloned = try cloneTableAddColumnIfMissing(allocator, fiveg_table, nr_column_name);
    var stats: SyncStats = .{
        .rows_total = cloned.table.rows.len,
        .rows_yes = 0,
        .rows_no = 0,
        .rows_blank = 0,
        .rows_with_match = 0,
        .conflicting_windows = 0,
        .fallback_time_only_rows = 0,
        .window_ms = if (time_tolerance_ms < 0) 0 else time_tolerance_ms,
    };

    var prepared_5g = std.ArrayList(PreparedFivegRow).empty;
    for (cloned.table.rows, 0..) |row, i| {
        const t = fiveg_times.times[i] orelse continue;
        const mcc = try normalizeKeyAlloc(a, getValue(row, fiveg_mcc_idx));
        const mnc = try normalizeKeyAlloc(a, getValue(row, fiveg_mnc_idx));
        try prepared_5g.append(a, .{
            .row_index = i,
            .time_ms = t,
            .mcc_key = mcc,
            .mnc_key = mnc,
            .has_group_keys = mcc.len != 0 and mnc.len != 0,
        });
    }

    for (prepared_5g.items) |r5| {
        var yes_count: usize = 0;
        var no_count: usize = 0;
        var any_match = false;
        var used_fallback = false;

        for (lte_prepared.items) |lte| {
            if (r5.has_group_keys) {
                if (!std.mem.eql(u8, r5.mcc_key, lte.mcc_key) or !std.mem.eql(u8, r5.mnc_key, lte.mnc_key)) {
                    continue;
                }
            } else {
                used_fallback = true;
            }

            const dt = if (r5.time_ms >= lte.time_ms) r5.time_ms - lte.time_ms else lte.time_ms - r5.time_ms;
            if (dt > stats.window_ms) continue;
            any_match = true;
            switch (lte.score) {
                .yes => yes_count += 1,
                .no => no_count += 1,
                .blank => {},
            }
        }

        if (!any_match) continue;
        stats.rows_with_match += 1;
        if (used_fallback and !r5.has_group_keys) stats.fallback_time_only_rows += 1;
        if (yes_count > 0 and no_count > 0) stats.conflicting_windows += 1;

        const resolved: []const u8 = if (yes_count > 0)
            "yes"
        else if (no_count > 0)
            "no"
        else
            "";
        allocator.free(cloned.table.rows[r5.row_index].values[cloned.col_idx]);
        cloned.table.rows[r5.row_index].values[cloned.col_idx] = try allocator.dupe(u8, resolved);
    }

    for (cloned.table.rows) |row| {
        switch (normalizeNrScore(getValue(row, cloned.col_idx))) {
            .yes => stats.rows_yes += 1,
            .no => stats.rows_no += 1,
            .blank => stats.rows_blank += 1,
        }
    }

    if (require_nr_yes) {
        const filtered = try filterRowsByNrYes(allocator, cloned.table, cloned.col_idx);
        cloned.table.deinit(allocator);
        return .{ .table = filtered, .stats = stats };
    }

    return .{ .table = cloned.table, .stats = stats };
}

test "mobile sync prefers yes in conflicting window" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var fiveg = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 4),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer fiveg.deinit(a);
    fiveg.column_names[0] = try a.dupe(u8, "UTC");
    fiveg.column_names[1] = try a.dupe(u8, "MCC");
    fiveg.column_names[2] = try a.dupe(u8, "MNC");
    fiveg.column_names[3] = try a.dupe(u8, "RSRP");
    fiveg.rows[0] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = 2 };
    fiveg.rows[0].values[0] = try a.dupe(u8, "1000");
    fiveg.rows[0].values[1] = try a.dupe(u8, "231");
    fiveg.rows[0].values[2] = try a.dupe(u8, "1");
    fiveg.rows[0].values[3] = try a.dupe(u8, "-80");

    var lte = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 4),
        .rows = try a.alloc(csv_table.CsvRow, 2),
        .header_line = 0,
    };
    defer lte.deinit(a);
    lte.column_names[0] = try a.dupe(u8, "UTC");
    lte.column_names[1] = try a.dupe(u8, "MCC");
    lte.column_names[2] = try a.dupe(u8, "MNC");
    lte.column_names[3] = try a.dupe(u8, "5G NR");
    for (0..2) |i| lte.rows[i] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = @as(i64, @intCast(i + 2)) };
    lte.rows[0].values[0] = try a.dupe(u8, "999");
    lte.rows[0].values[1] = try a.dupe(u8, "231");
    lte.rows[0].values[2] = try a.dupe(u8, "1");
    lte.rows[0].values[3] = try a.dupe(u8, "no");
    lte.rows[1].values[0] = try a.dupe(u8, "1001");
    lte.rows[1].values[1] = try a.dupe(u8, "231");
    lte.rows[1].values[2] = try a.dupe(u8, "1");
    lte.rows[1].values[3] = try a.dupe(u8, "yes");

    var res = try syncFivegNrFromLte(a, fiveg, lte, "5G NR", 1000, false);
    defer res.table.deinit(a);
    try std.testing.expectEqual(@as(usize, 1), res.stats.rows_with_match);
    try std.testing.expectEqual(@as(usize, 1), res.stats.conflicting_windows);
    try std.testing.expectEqualStrings("yes", res.table.rows[0].values[4]);
}

test "mobile sync falls back to time-only when mcc mnc missing" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var fiveg = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 3),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer fiveg.deinit(a);
    fiveg.column_names[0] = try a.dupe(u8, "UTC");
    fiveg.column_names[1] = try a.dupe(u8, "MCC");
    fiveg.column_names[2] = try a.dupe(u8, "MNC");
    fiveg.rows[0] = .{ .values = try a.alloc([]const u8, 3), .original_excel_row = 2 };
    fiveg.rows[0].values[0] = try a.dupe(u8, "1000");
    fiveg.rows[0].values[1] = try a.dupe(u8, "");
    fiveg.rows[0].values[2] = try a.dupe(u8, "");

    var lte = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 4),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer lte.deinit(a);
    lte.column_names[0] = try a.dupe(u8, "UTC");
    lte.column_names[1] = try a.dupe(u8, "MCC");
    lte.column_names[2] = try a.dupe(u8, "MNC");
    lte.column_names[3] = try a.dupe(u8, "5G NR");
    lte.rows[0] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = 2 };
    lte.rows[0].values[0] = try a.dupe(u8, "1000");
    lte.rows[0].values[1] = try a.dupe(u8, "231");
    lte.rows[0].values[2] = try a.dupe(u8, "1");
    lte.rows[0].values[3] = try a.dupe(u8, "yes");

    var res = try syncFivegNrFromLte(a, fiveg, lte, "5G NR", 1000, false);
    defer res.table.deinit(a);
    try std.testing.expectEqual(@as(usize, 1), res.stats.fallback_time_only_rows);
    try std.testing.expectEqualStrings("yes", res.table.rows[0].values[3]);
}
