const std = @import("std");
const cfg = @import("config.zig");
const zone_stats_core = @import("zone_stats_core.zig");

pub const StatsRow = struct {
    mcc: []const u8,
    mnc: []const u8,
    good_count: usize,
    bad_count: usize,
};

fn writeThresholdHeader(writer: anytype, has_sinr: bool, rsrp_threshold: f64, sinr_threshold: f64) !void {
    if (has_sinr) {
        try writer.print("MNC;MCC;RSRP >= {d} a SINR >= {d};RSRP < {d} alebo SINR < {d}\n", .{
            rsrp_threshold, sinr_threshold, rsrp_threshold, sinr_threshold,
        });
    } else {
        try writer.print("MNC;MCC;RSRP >= {d};RSRP < {d}\n", .{ rsrp_threshold, rsrp_threshold });
    }
}

pub fn writeStatsCsv(
    allocator: std.mem.Allocator,
    rows: []const zone_stats_core.ZoneOperatorStat,
    summary: zone_stats_core.Summary,
    include_empty_zones: bool,
    add_custom_operators: bool,
    custom_operators: []const cfg.CustomOperator,
    has_sinr_column: bool,
    rsrp_threshold: f64,
    sinr_threshold: f64,
    output_path: []const u8,
) !void {
    var arena = std.heap.ArenaAllocator.init(allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var op_index = std.StringHashMap(usize).init(a);
    var op_rows = std.ArrayList(StatsRow).empty;
    var op_zone_sets = std.ArrayList(std.StringHashMap(void)).empty;

    for (rows) |row| {
        const key = try std.fmt.allocPrint(a, "{s}|{s}", .{ row.mcc, row.mnc });
        var idx: usize = undefined;
        if (op_index.get(key)) |existing| {
            idx = existing;
        } else {
            idx = op_rows.items.len;
            try op_rows.append(a, .{
                .mcc = row.mcc,
                .mnc = row.mnc,
                .good_count = 0,
                .bad_count = 0,
            });
            try op_zone_sets.append(a, std.StringHashMap(void).init(a));
            try op_index.put(key, idx);
        }

        switch (row.coverage) {
            .good => op_rows.items[idx].good_count += 1,
            .bad => op_rows.items[idx].bad_count += 1,
        }
        try op_zone_sets.items[idx].put(row.zona_key, {});
    }

    if (add_custom_operators) {
        for (custom_operators) |op| {
            const key = try std.fmt.allocPrint(a, "{s}|{s}", .{ op.mcc, op.mnc });
            if (op_index.contains(key)) continue;
            const idx = op_rows.items.len;
            try op_rows.append(a, .{
                .mcc = op.mcc,
                .mnc = op.mnc,
                .good_count = 0,
                .bad_count = 0,
            });
            try op_zone_sets.append(a, std.StringHashMap(void).init(a));
            try op_index.put(key, idx);
        }
    }

    if (include_empty_zones) {
        const total_unique_zones = summary.unique_zones;
        for (op_rows.items, 0..) |_, i| {
            const existing = op_zone_sets.items[i].count();
            if (total_unique_zones > existing) {
                op_rows.items[i].bad_count += total_unique_zones - existing;
            }
        }
    }

    const file = try std.fs.cwd().createFile(output_path, .{ .truncate = true });
    defer file.close();
    var buf: [4096]u8 = undefined;
    var fw = file.writer(&buf);
    const w = &fw.interface;

    if (op_rows.items.len == 0) {
        try w.writeByte('\n');
        try w.flush();
        return;
    }

    try writeThresholdHeader(w, has_sinr_column, rsrp_threshold, sinr_threshold);
    for (op_rows.items) |r| {
        try w.print("{s};{s};{d};{d}\n", .{ r.mnc, r.mcc, r.good_count, r.bad_count });
    }
    try w.flush();
}

test "writeStatsCsv writes operator summary and empty zones penalty" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const rows = try a.alloc(zone_stats_core.ZoneOperatorStat, 3);
    rows[0] = .{
        .zona_key = "z1", .operator_key = "231_1", .mcc = "231", .mnc = "1", .pci = "10", .selected_frequency = "800",
        .zona_x = 0, .zona_y = 0, .rsrp_avg = -80, .sinr_avg = 5, .pocet_merani = 1, .coverage = .good,
    };
    rows[1] = .{
        .zona_key = "z2", .operator_key = "231_1", .mcc = "231", .mnc = "1", .pci = "10", .selected_frequency = "800",
        .zona_x = 0, .zona_y = 0, .rsrp_avg = -120, .sinr_avg = -10, .pocet_merani = 1, .coverage = .bad,
    };
    rows[2] = .{
        .zona_key = "z1", .operator_key = "231_2", .mcc = "231", .mnc = "2", .pci = "20", .selected_frequency = "1800",
        .zona_x = 0, .zona_y = 0, .rsrp_avg = -90, .sinr_avg = 2, .pocet_merani = 1, .coverage = .bad,
    };
    const summary: zone_stats_core.Summary = .{
        .input_rows = 0, .used_rows = 0, .dropped_missing_rsrp = 0, .dropped_invalid_lat_lon = 0,
        .zone_freq_groups = 0, .zone_operator_groups = 3, .unique_zones = 2, .unique_operators = 2,
        .good_coverage_groups = 1, .bad_coverage_groups = 2, .groups_with_sinr_avg = 3,
    };

    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const out_path = try tmp.dir.realpathAlloc(a, ".");
    defer a.free(out_path);
    const full = try std.fmt.allocPrint(a, "{s}/stats.csv", .{out_path});

    try writeStatsCsv(a, rows, summary, true, false, &.{}, true, -110, -5, full);

    const f = try std.fs.cwd().openFile(full, .{});
    defer f.close();
    const text = try f.readToEndAlloc(a, 4096);
    try std.testing.expect(std.mem.indexOf(u8, text, "MNC;MCC;RSRP >= -110") != null);
    try std.testing.expect(std.mem.indexOf(u8, text, "\n1;231;1;1\n") != null);
    // operator 231/2 has only one existing zone out of 2 total => +1 missing bad
    try std.testing.expect(std.mem.indexOf(u8, text, "\n2;231;0;2\n") != null);
}

test "writeStatsCsv appends custom operators when requested" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const rows = try a.alloc(zone_stats_core.ZoneOperatorStat, 1);
    rows[0] = .{
        .zona_key = "z1", .operator_key = "231_1", .mcc = "231", .mnc = "1", .pci = "10", .selected_frequency = "800",
        .zona_x = 0, .zona_y = 0, .rsrp_avg = -80, .sinr_avg = null, .pocet_merani = 1, .coverage = .good,
    };
    const summary: zone_stats_core.Summary = .{
        .input_rows = 0, .used_rows = 0, .dropped_missing_rsrp = 0, .dropped_invalid_lat_lon = 0,
        .zone_freq_groups = 0, .zone_operator_groups = 1, .unique_zones = 2, .unique_operators = 1,
        .good_coverage_groups = 1, .bad_coverage_groups = 0, .groups_with_sinr_avg = 0,
    };

    const custom_ops = [_]cfg.CustomOperator{
        .{ .mcc = "231", .mnc = "99", .pci = "" },
    };

    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const out_dir = try tmp.dir.realpathAlloc(a, ".");
    defer a.free(out_dir);
    const full = try std.fmt.allocPrint(a, "{s}/stats.csv", .{out_dir});

    try writeStatsCsv(a, rows, summary, true, true, &custom_ops, false, -110, -5, full);

    const f = try std.fs.cwd().openFile(full, .{});
    defer f.close();
    const text = try f.readToEndAlloc(a, 4096);
    try std.testing.expect(std.mem.indexOf(u8, text, "\n99;231;0;2\n") != null);
}

test "writeStatsCsv writes blank line for empty stats without operators" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const rows = try a.alloc(zone_stats_core.ZoneOperatorStat, 0);
    const summary: zone_stats_core.Summary = .{
        .input_rows = 0, .used_rows = 0, .dropped_missing_rsrp = 0, .dropped_invalid_lat_lon = 0,
        .zone_freq_groups = 0, .zone_operator_groups = 0, .unique_zones = 0, .unique_operators = 0,
        .good_coverage_groups = 0, .bad_coverage_groups = 0, .groups_with_sinr_avg = 0,
    };

    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const out_dir = try tmp.dir.realpathAlloc(a, ".");
    defer a.free(out_dir);
    const full = try std.fmt.allocPrint(a, "{s}/stats.csv", .{out_dir});

    try writeStatsCsv(a, rows, summary, false, false, &.{}, true, -110, -5, full);

    const f = try std.fs.cwd().openFile(full, .{});
    defer f.close();
    const text = try f.readToEndAlloc(a, 64);
    try std.testing.expectEqualStrings("\n", text);
}
