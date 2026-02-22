const std = @import("std");
const cfg = @import("config.zig");
const csv_table = @import("csv_table.zig");
const zone_assign = @import("zone_assign.zig");

pub const AggregationPreview = struct {
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

const RowPrepared = struct {
    zone_key: []const u8,
    operator_key: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
    freq: []const u8,
    rsrp: f64,
    sinr: ?f64,
};

const ZoneFreqAgg = struct {
    zone_key: []const u8,
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
    return std.fmt.allocPrint(allocator, "g:{d}:{d}", .{
        @as(i64, @intFromFloat(std.math.floor(x))),
        @as(i64, @intFromFloat(std.math.floor(y))),
    });
}

fn fmtSegmentZoneKey(allocator: std.mem.Allocator, segment_id: usize) ![]const u8 {
    return std.fmt.allocPrint(allocator, "segment_{d}", .{segment_id});
}

fn compareOptionalNumericAsc(a: ?f64, b: ?f64) i2 {
    if (a == null and b == null) return 0;
    if (a == null) return 1; // NaN/invalid last, like pandas ascending
    if (b == null) return -1;
    const av = a.?;
    const bv = b.?;
    if (av < bv) return -1;
    if (av > bv) return 1;
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

pub fn build(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    mapping: cfg.ColumnMapping,
    zone_mode: []const u8,
    zone_size_m: f64,
    rsrp_threshold: f64,
    sinr_threshold: f64,
) !AggregationPreview {
    var arena = std.heap.ArenaAllocator.init(allocator);
    defer arena.deinit();
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
    var raw_rows = std.ArrayList(struct {
        mcc: []const u8,
        mnc: []const u8,
        pci: []const u8,
        freq: []const u8,
        rsrp: f64,
        sinr: ?f64,
    }).empty;

    var dropped_missing_rsrp: usize = 0;
    var dropped_invalid_lat_lon: usize = 0;

    for (table.rows) |row| {
        const rsrp = parseFloatLike(a, getValue(row, rsrp_idx)) orelse {
            dropped_missing_rsrp += 1;
            continue;
        };
        const lat = parseFloatLike(a, getValue(row, lat_idx)) orelse {
            dropped_invalid_lat_lon += 1;
            continue;
        };
        const lon = parseFloatLike(a, getValue(row, lon_idx)) orelse {
            dropped_invalid_lat_lon += 1;
            continue;
        };

        try lats.append(a, lat);
        try lons.append(a, lon);
        try raw_rows.append(a, .{
            .mcc = try dupTrimmed(a, getValue(row, mcc_idx)),
            .mnc = try dupTrimmed(a, getValue(row, mnc_idx)),
            .pci = try dupTrimmed(a, getValue(row, pci_idx)),
            .freq = try dupTrimmed(a, getValue(row, freq_idx)),
            .rsrp = rsrp,
            .sinr = if (sinr_idx_opt) |sinr_idx| parseFloatLike(a, getValue(row, sinr_idx)) else null,
        });
    }

    const used_rows = raw_rows.items.len;
    if (used_rows == 0) {
        return .{
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
        };
    }

    const xs = try a.alloc(f64, used_rows);
    const ys = try a.alloc(f64, used_rows);
    const lat0_rad = degToRad(lats.items[0]);
    const earth_r = 6371000.0;
    for (0..used_rows) |i| {
        const lat_rad = degToRad(lats.items[i]);
        const lon_rad = degToRad(lons.items[i]);
        xs[i] = earth_r * lon_rad * @cos(lat0_rad);
        ys[i] = earth_r * lat_rad;
    }

    var zone_keys = try a.alloc([]const u8, used_rows);
    if (isSegmentsMode(zone_mode)) {
        var segments = try zone_assign.assignSegments(a, xs, ys, zone_size_m);
        defer segments.deinit(a);
        for (segments.segment_ids, 0..) |seg_id, i| {
            zone_keys[i] = try fmtSegmentZoneKey(a, seg_id);
        }
    } else {
        var grid = try zone_assign.assignGrid(a, xs, ys, zone_size_m);
        defer grid.deinit(a);
        for (0..used_rows) |i| {
            zone_keys[i] = try fmtGridZoneKey(a, grid.zona_x[i], grid.zona_y[i]);
        }
    }

    var unique_zones = std.StringHashMap(void).init(a);
    var unique_operators = std.StringHashMap(void).init(a);
    var agg_index = std.StringHashMap(usize).init(a);
    var aggs = std.ArrayList(ZoneFreqAgg).empty;

    for (raw_rows.items, 0..) |raw, i| {
        const operator_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ raw.mcc, raw.mnc });
        try unique_zones.put(zone_keys[i], {});
        try unique_operators.put(operator_key, {});

        const group_key = try std.fmt.allocPrint(a, "{s}|{s}|{s}|{s}", .{
            zone_keys[i], operator_key, raw.pci, raw.freq,
        });

        if (agg_index.get(group_key)) |idx| {
            aggs.items[idx].rsrp_sum += raw.rsrp;
            aggs.items[idx].rsrp_count += 1;
            if (raw.sinr) |s| {
                aggs.items[idx].sinr_sum += s;
                aggs.items[idx].sinr_count += 1;
            }
        } else {
            const idx = aggs.items.len;
            try aggs.append(a, .{
                .zone_key = zone_keys[i],
                .operator_key = operator_key,
                .mcc = raw.mcc,
                .mnc = raw.mnc,
                .pci = raw.pci,
                .freq = raw.freq,
                .rsrp_sum = raw.rsrp,
                .rsrp_count = 1,
                .sinr_sum = if (raw.sinr) |s| s else 0,
                .sinr_count = if (raw.sinr != null) 1 else 0,
                .freq_numeric = parseFloatLike(a, raw.freq),
                .pci_numeric = parseFloatLike(a, raw.pci),
            });
            try agg_index.put(group_key, idx);
        }
    }

    var best_index_by_zone_op = std.StringHashMap(usize).init(a);
    var good_groups: usize = 0;
    var bad_groups: usize = 0;
    var with_sinr_avg: usize = 0;

    for (aggs.items, 0..) |agg, idx| {
        const key = try std.fmt.allocPrint(a, "{s}|{s}", .{ agg.zone_key, agg.operator_key });
        if (best_index_by_zone_op.get(key)) |prev_idx| {
            if (betterCandidate(agg, aggs.items[prev_idx])) {
                try best_index_by_zone_op.put(key, idx);
            }
        } else {
            try best_index_by_zone_op.put(key, idx);
        }
    }

    var it = best_index_by_zone_op.iterator();
    while (it.next()) |entry| {
        const agg = aggs.items[entry.value_ptr.*];
        const rsrp_ok = agg.rsrpAvg() >= rsrp_threshold;
        const sinr_avg = agg.sinrAvg();
        if (sinr_avg != null) with_sinr_avg += 1;

        const good = if (mapping.sinr != null)
            (rsrp_ok and sinr_avg != null and sinr_avg.? >= sinr_threshold)
        else
            rsrp_ok;

        if (good) good_groups += 1 else bad_groups += 1;
    }

    return .{
        .input_rows = table.rows.len,
        .used_rows = used_rows,
        .dropped_missing_rsrp = dropped_missing_rsrp,
        .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
        .zone_freq_groups = aggs.items.len,
        .zone_operator_groups = best_index_by_zone_op.count(),
        .unique_zones = unique_zones.count(),
        .unique_operators = unique_operators.count(),
        .good_coverage_groups = good_groups,
        .bad_coverage_groups = bad_groups,
        .groups_with_sinr_avg = with_sinr_avg,
    };
}

test "aggregation preview picks better pci by rsrp and tie-breaks" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 8),
        .rows = try a.alloc(csv_table.CsvRow, 4),
        .header_line = 0,
    };
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

    // same zone/operator, two freq/pci combos; one should win by better avg rsrp
    const rows = [_][8][]const u8{
        .{ "48.0", "17.0", "800", "10", "231", "1", "-90", "10" },
        .{ "48.0", "17.0001", "800", "10", "231", "1", "-92", "8" },
        .{ "48.0", "17.0002", "1800", "20", "231", "1", "-80", "6" },
        .{ "48.0", "17.0003", "1800", "20", "231", "1", "-82", "6" },
    };
    for (rows, 0..) |r, i| {
        for (r, 0..) |v, j| {
            table.rows[i].values[j] = try a.dupe(u8, v);
        }
    }

    const preview = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 2,
        .pci = 3,
        .mcc = 4,
        .mnc = 5,
        .rsrp = 6,
        .sinr = 7,
    }, "center", 100.0, -85.0, 5.0);

    try std.testing.expectEqual(@as(usize, 4), preview.used_rows);
    try std.testing.expectEqual(@as(usize, 2), preview.zone_freq_groups);
    try std.testing.expectEqual(@as(usize, 1), preview.zone_operator_groups);
    try std.testing.expectEqual(@as(usize, 1), preview.good_coverage_groups);
    try std.testing.expectEqual(@as(usize, 0), preview.bad_coverage_groups);
    try std.testing.expectEqual(@as(usize, 1), preview.groups_with_sinr_avg);
}

test "aggregation preview counts bad groups when sinr missing and sinr mapped" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 8),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "Frequency");
    table.column_names[3] = try a.dupe(u8, "PCI");
    table.column_names[4] = try a.dupe(u8, "MCC");
    table.column_names[5] = try a.dupe(u8, "MNC");
    table.column_names[6] = try a.dupe(u8, "RSRP");
    table.column_names[7] = try a.dupe(u8, "SINR");
    table.rows[0] = .{ .values = try a.alloc([]const u8, 8), .original_excel_row = 2 };
    const row = [_][]const u8{ "48.0", "17.0", "800", "1", "231", "1", "-70", "" };
    for (row, 0..) |v, j| table.rows[0].values[j] = try a.dupe(u8, v);

    const preview = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 2,
        .pci = 3,
        .mcc = 4,
        .mnc = 5,
        .rsrp = 6,
        .sinr = 7,
    }, "center", 100.0, -100.0, -5.0);

    try std.testing.expectEqual(@as(usize, 1), preview.zone_operator_groups);
    try std.testing.expectEqual(@as(usize, 0), preview.good_coverage_groups);
    try std.testing.expectEqual(@as(usize, 1), preview.bad_coverage_groups);
    try std.testing.expectEqual(@as(usize, 0), preview.groups_with_sinr_avg);
}
