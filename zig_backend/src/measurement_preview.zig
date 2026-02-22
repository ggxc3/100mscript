const std = @import("std");
const cfg = @import("config.zig");
const csv_table = @import("csv_table.zig");

pub const PreviewStats = struct {
    input_rows: usize,
    valid_rows_rsrp: usize,
    dropped_missing_rsrp: usize,
    valid_lat_lon_rows: usize,
    valid_frequency_rows: usize,
};

fn parseFloatLike(allocator: std.mem.Allocator, raw: []const u8) ?f64 {
    const trimmed = std.mem.trim(u8, raw, " \t\r\n");
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

fn getValue(row: csv_table.CsvRow, idx: usize) []const u8 {
    if (idx >= row.values.len) return "";
    return row.values[idx];
}

pub fn buildPreview(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    mapping: cfg.ColumnMapping,
) PreviewStats {
    var valid_rsrp: usize = 0;
    var valid_lat_lon: usize = 0;
    var valid_freq: usize = 0;

    const lat_idx: usize = @intCast(mapping.latitude);
    const lon_idx: usize = @intCast(mapping.longitude);
    const freq_idx: usize = @intCast(mapping.frequency);
    const rsrp_idx: usize = @intCast(mapping.rsrp);

    for (table.rows) |row| {
        const rsrp_val = parseFloatLike(allocator, getValue(row, rsrp_idx));
        if (rsrp_val != null) {
            valid_rsrp += 1;
        }

        const lat_val = parseFloatLike(allocator, getValue(row, lat_idx));
        const lon_val = parseFloatLike(allocator, getValue(row, lon_idx));
        if (lat_val != null and lon_val != null) valid_lat_lon += 1;

        const freq_val = parseFloatLike(allocator, getValue(row, freq_idx));
        if (freq_val != null) valid_freq += 1;
    }

    return .{
        .input_rows = table.rows.len,
        .valid_rows_rsrp = valid_rsrp,
        .dropped_missing_rsrp = table.rows.len - valid_rsrp,
        .valid_lat_lon_rows = valid_lat_lon,
        .valid_frequency_rows = valid_freq,
    };
}

test "measurement preview counts valid rsrp and lat/lon" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 4),
        .rows = try a.alloc(csv_table.CsvRow, 2),
        .header_line = 0,
    };
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "Frequency");
    table.column_names[3] = try a.dupe(u8, "RSRP");

    table.rows[0] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = 2 };
    table.rows[0].values[0] = try a.dupe(u8, "48,1");
    table.rows[0].values[1] = try a.dupe(u8, "17.1");
    table.rows[0].values[2] = try a.dupe(u8, "800");
    table.rows[0].values[3] = try a.dupe(u8, "-90");

    table.rows[1] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = 3 };
    table.rows[1].values[0] = try a.dupe(u8, "x");
    table.rows[1].values[1] = try a.dupe(u8, "17.2");
    table.rows[1].values[2] = try a.dupe(u8, "");
    table.rows[1].values[3] = try a.dupe(u8, "");

    const preview = buildPreview(a, table, .{
        .latitude = 0,
        .longitude = 1,
        .frequency = 2,
        .pci = 0,
        .mcc = 0,
        .mnc = 0,
        .rsrp = 3,
        .sinr = null,
    });

    try std.testing.expectEqual(@as(usize, 2), preview.input_rows);
    try std.testing.expectEqual(@as(usize, 1), preview.valid_rows_rsrp);
    try std.testing.expectEqual(@as(usize, 1), preview.dropped_missing_rsrp);
    try std.testing.expectEqual(@as(usize, 1), preview.valid_lat_lon_rows);
    try std.testing.expectEqual(@as(usize, 1), preview.valid_frequency_rows);
}

