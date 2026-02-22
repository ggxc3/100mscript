const std = @import("std");
const cfg = @import("config.zig");
const csv_table = @import("csv_table.zig");
const zone_assign = @import("zone_assign.zig");

pub const ProcessingPreview = struct {
    input_rows: usize,
    kept_rows: usize,
    dropped_missing_rsrp: usize,
    dropped_invalid_lat_lon: usize,
    unique_zones: usize,
    unique_operators: usize,
    unique_zone_operator_pairs: usize,
    min_x: ?f64 = null,
    max_x: ?f64 = null,
    min_y: ?f64 = null,
    max_y: ?f64 = null,
    range_x_m: ?f64 = null,
    range_y_m: ?f64 = null,
    theoretical_total_zones: ?f64 = null,
    coverage_percent: ?f64 = null,
};

const GridKey = struct {
    x_bits: u64,
    y_bits: u64,
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

fn buildOperatorKey(allocator: std.mem.Allocator, mcc: []const u8, mnc: []const u8) ![]const u8 {
    return std.fmt.allocPrint(allocator, "{s}_{s}", .{ trimValue(mcc), trimValue(mnc) });
}

pub fn build(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    mapping: cfg.ColumnMapping,
    zone_mode: []const u8,
    zone_size_m: f64,
) !ProcessingPreview {
    var arena = std.heap.ArenaAllocator.init(allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const lat_idx: usize = @intCast(mapping.latitude);
    const lon_idx: usize = @intCast(mapping.longitude);
    const mcc_idx: usize = @intCast(mapping.mcc);
    const mnc_idx: usize = @intCast(mapping.mnc);
    const rsrp_idx: usize = @intCast(mapping.rsrp);

    var lats = std.ArrayList(f64).empty;
    var lons = std.ArrayList(f64).empty;
    var op_keys = std.ArrayList([]const u8).empty;
    var dropped_missing_rsrp: usize = 0;
    var dropped_invalid_lat_lon: usize = 0;

    for (table.rows) |row| {
        if (parseFloatLike(a, getValue(row, rsrp_idx)) == null) {
            dropped_missing_rsrp += 1;
            continue;
        }

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
        try op_keys.append(a, try buildOperatorKey(a, getValue(row, mcc_idx), getValue(row, mnc_idx)));
    }

    const kept_rows = lats.items.len;
    if (kept_rows == 0) {
        return .{
            .input_rows = table.rows.len,
            .kept_rows = 0,
            .dropped_missing_rsrp = dropped_missing_rsrp,
            .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
            .unique_zones = 0,
            .unique_operators = 0,
            .unique_zone_operator_pairs = 0,
        };
    }

    const xs = try a.alloc(f64, kept_rows);
    const ys = try a.alloc(f64, kept_rows);
    const lat0_rad = degToRad(lats.items[0]);
    const earth_r = 6371000.0;
    for (0..kept_rows) |i| {
        const lat_rad = degToRad(lats.items[i]);
        const lon_rad = degToRad(lons.items[i]);
        // Approx meter projection preview (runtime parity placeholder before exact EPSG:5514 port).
        xs[i] = earth_r * lon_rad * @cos(lat0_rad);
        ys[i] = earth_r * lat_rad;
    }

    var unique_ops = std.StringHashMap(void).init(a);
    var unique_zone_ops = std.StringHashMap(void).init(a);

    if (isSegmentsMode(zone_mode)) {
        var segments = try zone_assign.assignSegments(a, xs, ys, zone_size_m);
        defer segments.deinit(a);

        for (op_keys.items) |op| {
            try unique_ops.put(op, {});
        }
        for (segments.segment_ids, op_keys.items) |segment_id, op| {
            const combo = try std.fmt.allocPrint(a, "segment_{d}|{s}", .{ segment_id, op });
            try unique_zone_ops.put(combo, {});
        }

        return .{
            .input_rows = table.rows.len,
            .kept_rows = kept_rows,
            .dropped_missing_rsrp = dropped_missing_rsrp,
            .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
            .unique_zones = segments.unique_segments,
            .unique_operators = unique_ops.count(),
            .unique_zone_operator_pairs = unique_zone_ops.count(),
        };
    }

    var grid = try zone_assign.assignGrid(a, xs, ys, zone_size_m);
    defer grid.deinit(a);

    var unique_grids = std.AutoHashMap(GridKey, void).init(a);
    var min_x = grid.zona_x[0];
    var max_x = grid.zona_x[0];
    var min_y = grid.zona_y[0];
    var max_y = grid.zona_y[0];

    for (grid.zona_x, grid.zona_y, op_keys.items) |zx, zy, op| {
        if (zx < min_x) min_x = zx;
        if (zx > max_x) max_x = zx;
        if (zy < min_y) min_y = zy;
        if (zy > max_y) max_y = zy;

        try unique_grids.put(.{
            .x_bits = @bitCast(zx),
            .y_bits = @bitCast(zy),
        }, {});
        try unique_ops.put(op, {});

        const combo = try std.fmt.allocPrint(a, "{d}:{d}|{s}", .{
            @as(i64, @intFromFloat(std.math.floor(zx))),
            @as(i64, @intFromFloat(std.math.floor(zy))),
            op,
        });
        try unique_zone_ops.put(combo, {});
    }

    const range_x_m = max_x - min_x + zone_size_m;
    const range_y_m = max_y - min_y + zone_size_m;
    const theoretical_total = if (zone_size_m > 0)
        (range_x_m / zone_size_m) * (range_y_m / zone_size_m)
    else
        0.0;
    const coverage = if (theoretical_total > 0)
        (@as(f64, @floatFromInt(unique_grids.count())) / theoretical_total) * 100.0
    else
        null;

    return .{
        .input_rows = table.rows.len,
        .kept_rows = kept_rows,
        .dropped_missing_rsrp = dropped_missing_rsrp,
        .dropped_invalid_lat_lon = dropped_invalid_lat_lon,
        .unique_zones = unique_grids.count(),
        .unique_operators = unique_ops.count(),
        .unique_zone_operator_pairs = unique_zone_ops.count(),
        .min_x = min_x,
        .max_x = max_x,
        .min_y = min_y,
        .max_y = max_y,
        .range_x_m = range_x_m,
        .range_y_m = range_y_m,
        .theoretical_total_zones = theoretical_total,
        .coverage_percent = coverage,
    };
}

test "processing preview counts rows and grid zones" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 6),
        .rows = try a.alloc(csv_table.CsvRow, 3),
        .header_line = 0,
    };
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "MCC");
    table.column_names[3] = try a.dupe(u8, "MNC");
    table.column_names[4] = try a.dupe(u8, "RSRP");
    table.column_names[5] = try a.dupe(u8, "Frequency");

    for (0..3) |i| {
        table.rows[i] = .{ .values = try a.alloc([]const u8, 6), .original_excel_row = @as(i64, @intCast(i + 2)) };
    }
    table.rows[0].values[0] = try a.dupe(u8, "48.0000");
    table.rows[0].values[1] = try a.dupe(u8, "17.0000");
    table.rows[0].values[2] = try a.dupe(u8, "231");
    table.rows[0].values[3] = try a.dupe(u8, "1");
    table.rows[0].values[4] = try a.dupe(u8, "-90");
    table.rows[0].values[5] = try a.dupe(u8, "800");
    table.rows[1].values[0] = try a.dupe(u8, "48.0000");
    table.rows[1].values[1] = try a.dupe(u8, "17.0015");
    table.rows[1].values[2] = try a.dupe(u8, "231");
    table.rows[1].values[3] = try a.dupe(u8, "1");
    table.rows[1].values[4] = try a.dupe(u8, "-95");
    table.rows[1].values[5] = try a.dupe(u8, "800");
    table.rows[2].values[0] = try a.dupe(u8, "48.0000");
    table.rows[2].values[1] = try a.dupe(u8, "17.0020");
    table.rows[2].values[2] = try a.dupe(u8, "231");
    table.rows[2].values[3] = try a.dupe(u8, "1");
    table.rows[2].values[4] = try a.dupe(u8, "");
    table.rows[2].values[5] = try a.dupe(u8, "800");

    const preview = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 5,
        .pci = 0,
        .mcc = 2,
        .mnc = 3,
        .rsrp = 4,
        .sinr = null,
    }, "center", 100.0);

    try std.testing.expectEqual(@as(usize, 3), preview.input_rows);
    try std.testing.expectEqual(@as(usize, 2), preview.kept_rows);
    try std.testing.expectEqual(@as(usize, 1), preview.dropped_missing_rsrp);
    try std.testing.expectEqual(@as(usize, 0), preview.dropped_invalid_lat_lon);
    try std.testing.expectEqual(@as(usize, 1), preview.unique_operators);
    try std.testing.expect(preview.unique_zones >= 2);
    try std.testing.expect(preview.coverage_percent != null);
}

test "processing preview segments mode reports segment zones" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 5),
        .rows = try a.alloc(csv_table.CsvRow, 3),
        .header_line = 0,
    };
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "MCC");
    table.column_names[3] = try a.dupe(u8, "MNC");
    table.column_names[4] = try a.dupe(u8, "RSRP");

    for (0..3) |i| {
        table.rows[i] = .{ .values = try a.alloc([]const u8, 5), .original_excel_row = @as(i64, @intCast(i + 2)) };
    }
    table.rows[0].values[0] = try a.dupe(u8, "0");
    table.rows[0].values[1] = try a.dupe(u8, "0");
    table.rows[0].values[2] = try a.dupe(u8, "231");
    table.rows[0].values[3] = try a.dupe(u8, "1");
    table.rows[0].values[4] = try a.dupe(u8, "-80");
    table.rows[1].values[0] = try a.dupe(u8, "0");
    table.rows[1].values[1] = try a.dupe(u8, "0.0010");
    table.rows[1].values[2] = try a.dupe(u8, "231");
    table.rows[1].values[3] = try a.dupe(u8, "1");
    table.rows[1].values[4] = try a.dupe(u8, "-81");
    table.rows[2].values[0] = try a.dupe(u8, "0");
    table.rows[2].values[1] = try a.dupe(u8, "0.0022");
    table.rows[2].values[2] = try a.dupe(u8, "231");
    table.rows[2].values[3] = try a.dupe(u8, "2");
    table.rows[2].values[4] = try a.dupe(u8, "-82");

    const preview = try build(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 0,
        .pci = 0,
        .mcc = 2,
        .mnc = 3,
        .rsrp = 4,
        .sinr = null,
    }, "segments", 100.0);

    try std.testing.expectEqual(@as(usize, 3), preview.kept_rows);
    try std.testing.expect(preview.unique_zones >= 2);
    try std.testing.expectEqual(@as(usize, 2), preview.unique_operators);
    try std.testing.expect(preview.coverage_percent == null);
}
