const std = @import("std");
const cfg = @import("config.zig");
const csv_table = @import("csv_table.zig");
const protocol = @import("protocol.zig");
const zone_assign = @import("zone_assign.zig");
const projection = @import("projection.zig");

pub const CoverageCategory = enum {
    good,
    bad,
};

pub const ZoneOperatorStat = struct {
    zona_key: []const u8,
    operator_key: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
    selected_frequency: []const u8,
    zona_x: f64,
    zona_y: f64,
    rsrp_avg: f64,
    sinr_avg: ?f64,
    pocet_merani: usize,
    coverage: CoverageCategory,
};

pub const Summary = struct {
    input_rows: usize,
    used_rows: usize,
    dropped_missing_rsrp: usize,
    dropped_invalid_lat_lon: usize,
    zone_freq_groups: usize,
    zone_operator_groups: usize,
    unique_zones: usize,
    unique_operators: usize,
    good_coverage_groups: usize,
    bad_coverage_groups: usize,
    groups_with_sinr_avg: usize,
};

pub const Result = struct {
    arena: std.heap.ArenaAllocator,
    rows: []ZoneOperatorStat,
    summary: Summary,

    pub fn deinit(self: *Result) void {
        self.arena.deinit();
        self.* = undefined;
    }
};

fn zoneStatLessThan(_: void, a: ZoneOperatorStat, b: ZoneOperatorStat) bool {
    switch (std.mem.order(u8, a.zona_key, b.zona_key)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }
    switch (std.mem.order(u8, a.operator_key, b.operator_key)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }
    switch (std.mem.order(u8, a.selected_frequency, b.selected_frequency)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }
    return std.mem.order(u8, a.pci, b.pci) == .lt;
}

const RawPrepared = struct {
    zona_key: []const u8,
    zona_x: f64,
    zona_y: f64,
    operator_key: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
    freq: []const u8,
    rsrp: f64,
    sinr: ?f64,
};

const ZoneFreqAgg = struct {
    zona_key: []const u8,
    zona_x: f64,
    zona_y: f64,
    operator_key: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
    freq: []const u8,
    rsrp_sum: f64 = 0,
    rsrp_count: usize = 0,
    sinr_sum: f64 = 0,
    sinr_count: usize = 0,
    freq_numeric: ?f64 = null,
    pci_numeric: ?f64 = null,

    fn rsrpAvg(self: ZoneFreqAgg) f64 {
        return self.rsrp_sum / @as(f64, @floatFromInt(self.rsrp_count));
    }

    fn sinrAvg(self: ZoneFreqAgg) ?f64 {
        if (self.sinr_count == 0) return null;
        return self.sinr_sum / @as(f64, @floatFromInt(self.sinr_count));
    }
};

fn getValue(row: csv_table.CsvRow, idx: usize) []const u8 {
    if (idx >= row.values.len) return "";
    return row.values[idx];
}

fn trimValue(raw: []const u8) []const u8 {
    return std.mem.trim(u8, raw, " \t\r\n");
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

fn degToRad(v: f64) f64 {
    return v * std.math.pi / 180.0;
}

fn isSegmentsMode(zone_mode: []const u8) bool {
    return std.mem.eql(u8, zone_mode, "segments");
}

fn dupTrimmed(allocator: std.mem.Allocator, raw: []const u8) ![]const u8 {
    return allocator.dupe(u8, trimValue(raw));
}

fn fmtGridZoneKey(allocator: std.mem.Allocator, x: f64, y: f64) ![]const u8 {
    return std.fmt.allocPrint(allocator, "{d}_{d}", .{
        @as(i64, @intFromFloat(std.math.floor(x))),
        @as(i64, @intFromFloat(std.math.floor(y))),
    });
}

fn fmtSegmentZoneKey(allocator: std.mem.Allocator, segment_id: usize) ![]const u8 {
    return std.fmt.allocPrint(allocator, "segment_{d}", .{segment_id});
}

fn compareOptionalNumericAsc(a: ?f64, b: ?f64) i2 {
    if (a == null and b == null) return 0;
    if (a == null) return 1; // invalid numeric last
    if (b == null) return -1;
    if (a.? < b.?) return -1;
    if (a.? > b.?) return 1;
    return 0;
}

fn betterCandidate(a: ZoneFreqAgg, b: ZoneFreqAgg) bool {
    const a_avg = a.rsrpAvg();
    const b_avg = b.rsrpAvg();
    if (a_avg > b_avg) return true;
    if (a_avg < b_avg) return false;

    if (a.rsrp_count > b.rsrp_count) return true;
    if (a.rsrp_count < b.rsrp_count) return false;

    switch (compareOptionalNumericAsc(a.freq_numeric, b.freq_numeric)) {
        -1 => return true,
        1 => return false,
        else => {},
    }
    switch (std.mem.order(u8, a.freq, b.freq)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }

    switch (compareOptionalNumericAsc(a.pci_numeric, b.pci_numeric)) {
        -1 => return true,
        1 => return false,
        else => {},
    }
    switch (std.mem.order(u8, a.pci, b.pci)) {
        .lt => return true,
        .gt => return false,
        .eq => return false,
    }
}

fn coverageForAgg(agg: ZoneFreqAgg, mapping: cfg.ColumnMapping, rsrp_threshold: f64, sinr_threshold: f64) CoverageCategory {
    const rsrp_ok = agg.rsrpAvg() >= rsrp_threshold;
    if (mapping.sinr != null) {
        const sinr_avg = agg.sinrAvg() orelse return .bad;
        if (rsrp_ok and sinr_avg >= sinr_threshold) return .good;
        return .bad;
    }
    return if (rsrp_ok) .good else .bad;
}

fn shouldEmitProgress(current: usize, total: usize) bool {
    if (total == 0) return false;
    if (current == 0 or current >= total) return true;
    const step: usize = if (total <= 2_000) 250 else if (total <= 20_000) 1_000 else 5_000;
    return (current % step) == 0;
}

fn emitProgressMaybe(
    emitter_opt: ?protocol.Emitter,
    progress_enabled: bool,
    phase: []const u8,
    current: usize,
    total: usize,
) !void {
    if (!progress_enabled) return;
    if (emitter_opt == null) return;
    if (!shouldEmitProgress(current, total)) return;
    try emitter_opt.?.progress(phase, current, total, null);
}

pub fn build(
    backing_allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    mapping: cfg.ColumnMapping,
    zone_mode: []const u8,
    zone_size_m: f64,
    rsrp_threshold: f64,
    sinr_threshold: f64,
) !Result {
    return buildWithProgress(
        backing_allocator,
        table,
        mapping,
        zone_mode,
        zone_size_m,
        rsrp_threshold,
        sinr_threshold,
        null,
        false,
    );
}

pub fn buildWithProgress(
    backing_allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    mapping: cfg.ColumnMapping,
    zone_mode: []const u8,
    zone_size_m: f64,
    rsrp_threshold: f64,
    sinr_threshold: f64,
    emitter_opt: ?protocol.Emitter,
    progress_enabled: bool,
) !Result {
    var arena = std.heap.ArenaAllocator.init(backing_allocator);
    errdefer arena.deinit();
    const a = arena.allocator();

    const lat_idx: usize = @intCast(mapping.latitude);
    const lon_idx: usize = @intCast(mapping.longitude);
    const freq_idx: usize = @intCast(mapping.frequency);
    const pci_idx: usize = @intCast(mapping.pci);
    const mcc_idx: usize = @intCast(mapping.mcc);
    const mnc_idx: usize = @intCast(mapping.mnc);
    const rsrp_idx: usize = @intCast(mapping.rsrp);
    const sinr_idx_opt = if (mapping.sinr) |idx| @as(?usize, @intCast(idx)) else null;

    var lats = std.ArrayList(f64).empty;
    var lons = std.ArrayList(f64).empty;
    var temp_rows = std.ArrayList(struct {
        mcc: []const u8,
        mnc: []const u8,
        pci: []const u8,
        freq: []const u8,
        rsrp: f64,
        sinr: ?f64,
    }).empty;
    var dropped_missing_rsrp: usize = 0;
    var dropped_invalid_lat_lon: usize = 0;

    for (table.rows, 0..) |row, idx| {
        const rsrp = parseFloatLike(a, getValue(row, rsrp_idx)) orelse {
            dropped_missing_rsrp += 1;
            try emitProgressMaybe(emitter_opt, progress_enabled, "Spracovanie riadkov", idx + 1, table.rows.len);
            continue;
        };
        const lat = parseFloatLike(a, getValue(row, lat_idx)) orelse {
            dropped_invalid_lat_lon += 1;
            try emitProgressMaybe(emitter_opt, progress_enabled, "Spracovanie riadkov", idx + 1, table.rows.len);
            continue;
        };
        const lon = parseFloatLike(a, getValue(row, lon_idx)) orelse {
            dropped_invalid_lat_lon += 1;
            try emitProgressMaybe(emitter_opt, progress_enabled, "Spracovanie riadkov", idx + 1, table.rows.len);
            continue;
        };
        const mcc = trimValue(getValue(row, mcc_idx));
        const mnc = trimValue(getValue(row, mnc_idx));
        // Match pandas groupby(dropna=True): rows with missing MCC/MNC do not
        // participate in zone/operator aggregation.
        if (mcc.len == 0 or mnc.len == 0) {
            try emitProgressMaybe(emitter_opt, progress_enabled, "Spracovanie riadkov", idx + 1, table.rows.len);
            continue;
        }

        try lats.append(a, lat);
        try lons.append(a, lon);
        try temp_rows.append(a, .{
            .mcc = try a.dupe(u8, mcc),
            .mnc = try a.dupe(u8, mnc),
            .pci = try dupTrimmed(a, getValue(row, pci_idx)),
            .freq = try dupTrimmed(a, getValue(row, freq_idx)),
            .rsrp = rsrp,
            .sinr = if (sinr_idx_opt) |si| parseFloatLike(a, getValue(row, si)) else null,
        });
        try emitProgressMaybe(emitter_opt, progress_enabled, "Spracovanie riadkov", idx + 1, table.rows.len);
    }

    const used_rows = temp_rows.items.len;
    if (used_rows == 0) {
        return .{
            .arena = arena,
            .rows = &.{},
            .summary = .{
                .input_rows = table.rows.len,
                .used_rows = 0,
                .dropped_missing_rsrp = dropped_missing_rsrp,
                .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
                .zone_freq_groups = 0,
                .zone_operator_groups = 0,
                .unique_zones = 0,
                .unique_operators = 0,
                .good_coverage_groups = 0,
                .bad_coverage_groups = 0,
                .groups_with_sinr_avg = 0,
            },
        };
    }

    const xs = try a.alloc(f64, used_rows);
    const ys = try a.alloc(f64, used_rows);
    if (try projection.forwardBatchPyproj(a, lats.items, lons.items)) |proj_xy| {
        @memcpy(xs, proj_xy.xs);
        @memcpy(ys, proj_xy.ys);
    } else {
        const lat0_rad = degToRad(lats.items[0]);
        const earth_r = 6371000.0;
        for (0..used_rows) |i| {
            xs[i] = earth_r * degToRad(lons.items[i]) * @cos(lat0_rad);
            ys[i] = earth_r * degToRad(lats.items[i]);
        }
    }

    const prepared = try a.alloc(RawPrepared, used_rows);
    if (isSegmentsMode(zone_mode)) {
        var segments = try zone_assign.assignSegments(a, xs, ys, zone_size_m);
        defer segments.deinit(a);
        for (0..used_rows) |i| {
            const t = temp_rows.items[i];
            prepared[i] = .{
                .zona_key = try fmtSegmentZoneKey(a, segments.segment_ids[i]),
                .zona_x = segments.start_x[i],
                .zona_y = segments.start_y[i],
                .operator_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ t.mcc, t.mnc }),
                .mcc = t.mcc,
                .mnc = t.mnc,
                .pci = t.pci,
                .freq = t.freq,
                .rsrp = t.rsrp,
                .sinr = t.sinr,
            };
            try emitProgressMaybe(emitter_opt, progress_enabled, "Príprava zón", i + 1, used_rows);
        }
    } else {
        var grid = try zone_assign.assignGrid(a, xs, ys, zone_size_m);
        defer grid.deinit(a);
        for (0..used_rows) |i| {
            const t = temp_rows.items[i];
            prepared[i] = .{
                .zona_key = try fmtGridZoneKey(a, grid.zona_x[i], grid.zona_y[i]),
                .zona_x = grid.zona_x[i],
                .zona_y = grid.zona_y[i],
                .operator_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ t.mcc, t.mnc }),
                .mcc = t.mcc,
                .mnc = t.mnc,
                .pci = t.pci,
                .freq = t.freq,
                .rsrp = t.rsrp,
                .sinr = t.sinr,
            };
            try emitProgressMaybe(emitter_opt, progress_enabled, "Príprava zón", i + 1, used_rows);
        }
    }

    var unique_zones = std.StringHashMap(void).init(a);
    var unique_operators = std.StringHashMap(void).init(a);
    var agg_index = std.StringHashMap(usize).init(a);
    var aggs = std.ArrayList(ZoneFreqAgg).empty;

    for (prepared, 0..) |rowp, idx_prepared| {
        try unique_zones.put(rowp.zona_key, {});
        try unique_operators.put(rowp.operator_key, {});

        const k = try std.fmt.allocPrint(a, "{s}|{s}|{s}|{s}", .{
            rowp.zona_key,
            rowp.operator_key,
            rowp.pci,
            rowp.freq,
        });
        if (agg_index.get(k)) |idx| {
            aggs.items[idx].rsrp_sum += rowp.rsrp;
            aggs.items[idx].rsrp_count += 1;
            if (rowp.sinr) |s| {
                aggs.items[idx].sinr_sum += s;
                aggs.items[idx].sinr_count += 1;
            }
        } else {
            const idx = aggs.items.len;
            try aggs.append(a, .{
                .zona_key = rowp.zona_key,
                .zona_x = rowp.zona_x,
                .zona_y = rowp.zona_y,
                .operator_key = rowp.operator_key,
                .mcc = rowp.mcc,
                .mnc = rowp.mnc,
                .pci = rowp.pci,
                .freq = rowp.freq,
                .rsrp_sum = rowp.rsrp,
                .rsrp_count = 1,
                .sinr_sum = if (rowp.sinr) |s| s else 0,
                .sinr_count = if (rowp.sinr != null) 1 else 0,
                .freq_numeric = parseFloatLike(a, rowp.freq),
                .pci_numeric = parseFloatLike(a, rowp.pci),
            });
            try agg_index.put(k, idx);
        }
        try emitProgressMaybe(emitter_opt, progress_enabled, "Agregácia meraní", idx_prepared + 1, prepared.len);
    }

    var best_index_by_zone_op = std.StringHashMap(usize).init(a);
    for (aggs.items, 0..) |agg, idx| {
        const key = try std.fmt.allocPrint(a, "{s}|{s}", .{ agg.zona_key, agg.operator_key });
        if (best_index_by_zone_op.get(key)) |prev_idx| {
            if (betterCandidate(agg, aggs.items[prev_idx])) {
                try best_index_by_zone_op.put(key, idx);
            }
        } else {
            try best_index_by_zone_op.put(key, idx);
        }
        try emitProgressMaybe(emitter_opt, progress_enabled, "Výber najlepších kombinácií", idx + 1, aggs.items.len);
    }

    const rows = try a.alloc(ZoneOperatorStat, best_index_by_zone_op.count());
    var out_i: usize = 0;
    var good_count: usize = 0;
    var bad_count: usize = 0;
    var with_sinr_avg: usize = 0;

    var it = best_index_by_zone_op.iterator();
    while (it.next()) |entry| {
        const agg = aggs.items[entry.value_ptr.*];
        const sinr_avg = agg.sinrAvg();
        if (sinr_avg != null) with_sinr_avg += 1;
        const category = coverageForAgg(agg, mapping, rsrp_threshold, sinr_threshold);
        if (category == .good) good_count += 1 else bad_count += 1;

        rows[out_i] = .{
            .zona_key = agg.zona_key,
            .operator_key = agg.operator_key,
            .mcc = agg.mcc,
            .mnc = agg.mnc,
            .pci = agg.pci,
            .selected_frequency = agg.freq,
            .zona_x = agg.zona_x,
            .zona_y = agg.zona_y,
            .rsrp_avg = agg.rsrpAvg(),
            .sinr_avg = sinr_avg,
            .pocet_merani = agg.rsrp_count,
            .coverage = category,
        };
        out_i += 1;
        try emitProgressMaybe(emitter_opt, progress_enabled, "Príprava výsledkov", out_i, rows.len);
    }

    std.sort.heap(ZoneOperatorStat, rows, {}, zoneStatLessThan);

    return .{
        .arena = arena,
        .rows = rows,
        .summary = .{
            .input_rows = table.rows.len,
            .used_rows = used_rows,
            .dropped_missing_rsrp = dropped_missing_rsrp,
            .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
            .zone_freq_groups = aggs.items.len,
            .zone_operator_groups = rows.len,
            .unique_zones = unique_zones.count(),
            .unique_operators = unique_operators.count(),
            .good_coverage_groups = good_count,
            .bad_coverage_groups = bad_count,
            .groups_with_sinr_avg = with_sinr_avg,
        },
    };
}

test "zone stats core selects stronger freq/pci combination" {
    const a = std.testing.allocator;
    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 8),
        .rows = try a.alloc(csv_table.CsvRow, 4),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "Frequency");
    table.column_names[3] = try a.dupe(u8, "PCI");
    table.column_names[4] = try a.dupe(u8, "MCC");
    table.column_names[5] = try a.dupe(u8, "MNC");
    table.column_names[6] = try a.dupe(u8, "RSRP");
    table.column_names[7] = try a.dupe(u8, "SINR");
    for (0..4) |i| {
        table.rows[i] = .{ .values = try a.alloc([]const u8, 8), .original_excel_row = @as(i64, @intCast(i + 2)) };
    }
    const rows_src = [_][8][]const u8{
        .{ "48.0", "17.0", "800", "10", "231", "1", "-95", "7" },
        .{ "48.0", "17.0001", "800", "10", "231", "1", "-95", "7" },
        .{ "48.0", "17.0002", "1800", "20", "231", "1", "-80", "6" },
        .{ "48.0", "17.0003", "1800", "20", "231", "1", "-82", "6" },
    };
    for (rows_src, 0..) |r, i| for (r, 0..) |v, j| {
        table.rows[i].values[j] = try a.dupe(u8, v);
    };

    var res = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 2,
        .pci = 3,
        .mcc = 4,
        .mnc = 5,
        .rsrp = 6,
        .sinr = 7,
    }, "center", 100.0, -85.0, 5.0);
    defer res.deinit();

    try std.testing.expectEqual(@as(usize, 1), res.rows.len);
    try std.testing.expectEqualStrings("1800", res.rows[0].selected_frequency);
    try std.testing.expectEqualStrings("20", res.rows[0].pci);
    try std.testing.expect(res.rows[0].coverage == .good);
    try std.testing.expectEqual(@as(usize, 2), res.rows[0].pocet_merani);
}

test "zone stats core uses rsrp-only classification when sinr not mapped" {
    const a = std.testing.allocator;
    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 7),
        .rows = try a.alloc(csv_table.CsvRow, 2),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "Frequency");
    table.column_names[3] = try a.dupe(u8, "PCI");
    table.column_names[4] = try a.dupe(u8, "MCC");
    table.column_names[5] = try a.dupe(u8, "MNC");
    table.column_names[6] = try a.dupe(u8, "RSRP");
    for (0..2) |i| {
        table.rows[i] = .{ .values = try a.alloc([]const u8, 7), .original_excel_row = @as(i64, @intCast(i + 2)) };
    }
    const row1 = [_][]const u8{ "48.0", "17.0", "800", "1", "231", "1", "-70" };
    const row2 = [_][]const u8{ "48.0", "17.0001", "800", "1", "231", "2", "" }; // dropped missing RSRP
    for (row1, 0..) |v, j| table.rows[0].values[j] = try a.dupe(u8, v);
    for (row2, 0..) |v, j| table.rows[1].values[j] = try a.dupe(u8, v);

    var res = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 2,
        .pci = 3,
        .mcc = 4,
        .mnc = 5,
        .rsrp = 6,
        .sinr = null,
    }, "center", 100.0, -100.0, -5.0);
    defer res.deinit();

    try std.testing.expectEqual(@as(usize, 1), res.rows.len);
    try std.testing.expect(res.rows[0].coverage == .good);
    try std.testing.expectEqual(@as(usize, 1), res.summary.dropped_missing_rsrp);
    try std.testing.expectEqual(@as(usize, 1), res.summary.good_coverage_groups);
}
