const std = @import("std");
const cfg = @import("config.zig");
const csv_probe = @import("csv_probe.zig");
const csv_table = @import("csv_table.zig");
const projection = @import("projection.zig");
const zone_assign = @import("zone_assign.zig");
const zone_stats_core = @import("zone_stats_core.zig");

const ValidRowRef = struct {
    table_row_index: usize,
    lat: f64,
    lon: f64,
    rsrp: f64,
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
    freq: []const u8,
    x: f64,
    y: f64,
};

const SelectedMeta = struct {
    sample_row_index: ?usize = null,
    original_rows: std.ArrayList(i64),
};

const ZoneMeta = struct {
    zona_x: f64,
    zona_y: f64,
};

const ZoneLatLon = struct {
    lat: f64,
    lon: f64,
};

const OperatorTemplate = struct {
    operator_key: []const u8,
    mcc: []const u8,
    mnc: []const u8,
    sample_row_index: ?usize,
    is_custom: bool,
    custom_pci: ?[]const u8 = null,
};

fn trimValue(raw: []const u8) []const u8 {
    return std.mem.trim(u8, raw, " \t\r\n");
}

fn getValue(row: csv_table.CsvRow, idx: usize) []const u8 {
    if (idx >= row.values.len) return "";
    return row.values[idx];
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

fn radToDeg(v: f64) f64 {
    return v * 180.0 / std.math.pi;
}

fn isSegmentsMode(zone_mode: []const u8) bool {
    return std.mem.eql(u8, zone_mode, "segments");
}

fn splitHeaderColumnsOwned(allocator: std.mem.Allocator, header_text_opt: ?[]const u8, fallback_cols: [][]const u8) ![][]const u8 {
    if (header_text_opt) |header_text| {
        const cols = try csv_probe.splitSemicolonColumnsAlloc(allocator, header_text);
        if (cols.len > 0) {
            // duplicate strings because splitSemicolonColumnsAlloc borrows slices from header_text
            const out = try allocator.alloc([]const u8, cols.len);
            for (cols, 0..) |c, i| out[i] = try allocator.dupe(u8, c);
            allocator.free(cols);
            return out;
        }
        allocator.free(cols);
    }

    const out = try allocator.alloc([]const u8, fallback_cols.len);
    for (fallback_cols, 0..) |c, i| out[i] = try allocator.dupe(u8, c);
    return out;
}

fn indexOfColumnExact(columns: [][]const u8, name: []const u8) ?usize {
    for (columns, 0..) |c, idx| {
        if (std.mem.eql(u8, c, name)) return idx;
    }
    return null;
}

fn indexOfColumnCaseInsensitive(columns: [][]const u8, name: []const u8) ?usize {
    for (columns, 0..) |c, idx| {
        if (std.ascii.eqlIgnoreCase(c, name)) return idx;
    }
    return null;
}

fn exportIndexByName(allocator: std.mem.Allocator, export_cols: [][]const u8) !std.StringHashMap(usize) {
    var map = std.StringHashMap(usize).init(allocator);
    for (export_cols, 0..) |c, i| {
        if (!map.contains(c)) try map.put(c, i);
    }
    return map;
}

fn compositeSelectedKey(
    allocator: std.mem.Allocator,
    zona_key: []const u8,
    operator_key: []const u8,
    pci: []const u8,
    freq: []const u8,
) ![]const u8 {
    return std.fmt.allocPrint(allocator, "{s}|{s}|{s}|{s}", .{ zona_key, operator_key, pci, freq });
}

fn compositeZoneOperatorKey(
    allocator: std.mem.Allocator,
    zona_key: []const u8,
    operator_key: []const u8,
) ![]const u8 {
    return std.fmt.allocPrint(allocator, "{s}|{s}", .{ zona_key, operator_key });
}

fn parseNumericForSort(value: []const u8) ?f64 {
    const t = trimValue(value);
    if (t.len == 0) return null;
    return std.fmt.parseFloat(f64, t) catch null;
}

fn freqLessThan(_: void, a: []const u8, b: []const u8) bool {
    const an = parseNumericForSort(a);
    const bn = parseNumericForSort(b);
    if (an != null and bn != null) {
        if (an.? < bn.?) return true;
        if (an.? > bn.?) return false;
    } else if (an != null and bn == null) {
        return true;
    } else if (an == null and bn != null) {
        return false;
    }
    return std.mem.order(u8, a, b) == .lt;
}

fn statsSortLessThan(_: void, a: zone_stats_core.ZoneOperatorStat, b: zone_stats_core.ZoneOperatorStat) bool {
    const amcc = std.fmt.parseFloat(f64, a.mcc) catch null;
    const bmcc = std.fmt.parseFloat(f64, b.mcc) catch null;
    if (amcc != null and bmcc != null) {
        if (amcc.? < bmcc.?) return true;
        if (amcc.? > bmcc.?) return false;
    } else switch (std.mem.order(u8, a.mcc, b.mcc)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }

    const amnc = std.fmt.parseFloat(f64, a.mnc) catch null;
    const bmnc = std.fmt.parseFloat(f64, b.mnc) catch null;
    if (amnc != null and bmnc != null) {
        if (amnc.? < bmnc.?) return true;
        if (amnc.? > bmnc.?) return false;
    } else switch (std.mem.order(u8, a.mnc, b.mnc)) {
        .lt => return true,
        .gt => return false,
        .eq => {},
    }

    switch (std.mem.order(u8, a.pci, b.pci)) {
        .lt => return true,
        .gt => return false,
        .eq => return false,
    }
}

fn appendCsvEscapedLine(writer: anytype, values: []const []const u8) !void {
    for (values, 0..) |v, i| {
        if (i > 0) try writer.writeByte(';');
        try writer.writeAll(v);
    }
    try writer.writeByte('\n');
}

pub fn writeZonesCsvBasic(
    allocator: std.mem.Allocator,
    header_text_opt: ?[]const u8,
    table: csv_table.CsvTable,
    core_rows: []const zone_stats_core.ZoneOperatorStat,
    mapping: cfg.ColumnMapping,
    zone_mode: []const u8,
    zone_size_m: f64,
    use_zone_center: bool,
    include_empty_zones: bool,
    add_custom_operators: bool,
    custom_operators: []const cfg.CustomOperator,
    output_path: []const u8,
) !void {
    var arena = std.heap.ArenaAllocator.init(allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var export_cols = try splitHeaderColumnsOwned(a, header_text_opt, table.column_names);
    if (indexOfColumnExact(table.column_names, "5G NR") != null and indexOfColumnExact(export_cols, "5G NR") == null) {
        const extended = try a.alloc([]const u8, export_cols.len + 1);
        for (export_cols, 0..) |c, i| extended[i] = c;
        extended[export_cols.len] = try a.dupe(u8, "5G NR");
        export_cols = extended;
    }
    var export_index_map = try exportIndexByName(a, export_cols);
    defer export_index_map.deinit();

    const lat_col_name = table.column_names[@intCast(mapping.latitude)];
    const lon_col_name = table.column_names[@intCast(mapping.longitude)];
    const rsrp_col_name = table.column_names[@intCast(mapping.rsrp)];
    const freq_col_name = table.column_names[@intCast(mapping.frequency)];
    const pci_col_name = table.column_names[@intCast(mapping.pci)];
    const mcc_col_name = table.column_names[@intCast(mapping.mcc)];
    const mnc_col_name = table.column_names[@intCast(mapping.mnc)];
    const sinr_col_name = if (mapping.sinr) |si| table.column_names[@intCast(si)] else null;

    const lat_export_idx = export_index_map.get(lat_col_name);
    const lon_export_idx = export_index_map.get(lon_col_name);
    const rsrp_export_idx = export_index_map.get(rsrp_col_name);
    const freq_export_idx = export_index_map.get(freq_col_name);
    const pci_export_idx = export_index_map.get(pci_col_name);
    const mcc_export_idx = export_index_map.get(mcc_col_name);
    const mnc_export_idx = export_index_map.get(mnc_col_name);
    const nr_export_idx = export_index_map.get("5G NR");
    const sinr_export_idx = if (sinr_col_name) |n| export_index_map.get(n) else null;

    const lat_idx: usize = @intCast(mapping.latitude);
    const lon_idx: usize = @intCast(mapping.longitude);
    const freq_idx: usize = @intCast(mapping.frequency);
    const pci_idx: usize = @intCast(mapping.pci);
    const mcc_idx: usize = @intCast(mapping.mcc);
    const mnc_idx: usize = @intCast(mapping.mnc);
    const rsrp_idx: usize = @intCast(mapping.rsrp);

    var valid_rows = std.ArrayList(ValidRowRef).empty;
    var lat0_rad_opt: ?f64 = null;
    const earth_r = 6371000.0;
    for (table.rows, 0..) |row, row_idx| {
        const rsrp = parseFloatLike(a, getValue(row, rsrp_idx)) orelse continue;
        const lat = parseFloatLike(a, getValue(row, lat_idx)) orelse continue;
        const lon = parseFloatLike(a, getValue(row, lon_idx)) orelse continue;
        if (lat0_rad_opt == null) lat0_rad_opt = degToRad(lat);
        try valid_rows.append(a, .{
            .table_row_index = row_idx,
            .lat = lat,
            .lon = lon,
            .rsrp = rsrp,
            .mcc = trimValue(getValue(row, mcc_idx)),
            .mnc = trimValue(getValue(row, mnc_idx)),
            .pci = trimValue(getValue(row, pci_idx)),
            .freq = trimValue(getValue(row, freq_idx)),
            .x = 0,
            .y = 0,
        });
    }

    if (valid_rows.items.len > 0) {
        const lats = try a.alloc(f64, valid_rows.items.len);
        const lons = try a.alloc(f64, valid_rows.items.len);
        for (valid_rows.items, 0..) |vr, i| {
            lats[i] = vr.lat;
            lons[i] = vr.lon;
        }
        if (try projection.forwardBatchPyproj(a, lats, lons)) |proj_xy| {
            for (valid_rows.items, 0..) |*vr, i| {
                vr.x = proj_xy.xs[i];
                vr.y = proj_xy.ys[i];
            }
        } else {
            const lat0_rad = lat0_rad_opt.?;
            for (valid_rows.items) |*vr| {
                vr.x = earth_r * degToRad(vr.lon) * @cos(lat0_rad);
                vr.y = earth_r * degToRad(vr.lat);
            }
        }
    }

    var selected_map = std.StringHashMap(SelectedMeta).init(a);
    var zone_operator_freqs = std.StringHashMap(std.ArrayList([]const u8)).init(a);
    var processed_zone_ops = std.StringHashMap(void).init(a);
    var zone_meta_map = std.StringHashMap(ZoneMeta).init(a);
    var zone_latlon_map = std.StringHashMap(ZoneLatLon).init(a);
    for (core_rows) |sr| {
        const key = try compositeSelectedKey(a, sr.zona_key, sr.operator_key, sr.pci, sr.selected_frequency);
        try selected_map.put(key, .{ .original_rows = std.ArrayList(i64).empty });
        try processed_zone_ops.put(try compositeZoneOperatorKey(a, sr.zona_key, sr.operator_key), {});
        if (!zone_meta_map.contains(sr.zona_key)) {
            try zone_meta_map.put(sr.zona_key, .{ .zona_x = sr.zona_x, .zona_y = sr.zona_y });
        }
    }

    if (zone_meta_map.count() > 0) {
        const keys = try a.alloc([]const u8, zone_meta_map.count());
        const inv_xs = try a.alloc(f64, zone_meta_map.count());
        const inv_ys = try a.alloc(f64, zone_meta_map.count());
        var zi = zone_meta_map.iterator();
        var k: usize = 0;
        while (zi.next()) |entry| {
            keys[k] = entry.key_ptr.*;
            var cx = entry.value_ptr.zona_x;
            var cy = entry.value_ptr.zona_y;
            if (!isSegmentsMode(zone_mode)) {
                cx += zone_size_m / 2.0;
                cy += zone_size_m / 2.0;
            }
            inv_xs[k] = cx;
            inv_ys[k] = cy;
            k += 1;
        }

        if (try projection.inverseBatchPyproj(a, inv_xs, inv_ys)) |inv_ll| {
            for (keys, 0..) |zk, i| {
                try zone_latlon_map.put(zk, .{ .lat = inv_ll.lats[i], .lon = inv_ll.lons[i] });
            }
        }
    }

    if (valid_rows.items.len > 0) {
        const xs = try a.alloc(f64, valid_rows.items.len);
        const ys = try a.alloc(f64, valid_rows.items.len);
        for (valid_rows.items, 0..) |vr, i| {
            xs[i] = vr.x;
            ys[i] = vr.y;
        }

        if (isSegmentsMode(zone_mode)) {
            var seg = try zone_assign.assignSegments(a, xs, ys, zone_size_m);
            defer seg.deinit(a);
            for (valid_rows.items, 0..) |vr, i| {
                const zona_key = try std.fmt.allocPrint(a, "segment_{d}", .{seg.segment_ids[i]});
                const operator_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ vr.mcc, vr.mnc });
                const key = try compositeSelectedKey(a, zona_key, operator_key, vr.pci, vr.freq);
                if (selected_map.getPtr(key)) |meta| {
                    if (meta.sample_row_index == null) meta.sample_row_index = vr.table_row_index;
                    try meta.original_rows.append(a, table.rows[vr.table_row_index].original_excel_row);
                }
                const zk = try compositeZoneOperatorKey(a, zona_key, operator_key);
                if (zone_operator_freqs.getPtr(zk)) |freqs| {
                    var exists = false;
                    for (freqs.items) |f| {
                        if (std.mem.eql(u8, f, vr.freq)) {
                            exists = true;
                            break;
                        }
                    }
                    if (!exists) try freqs.append(a, try a.dupe(u8, vr.freq));
                } else {
                    var list = std.ArrayList([]const u8).empty;
                    try list.append(a, try a.dupe(u8, vr.freq));
                    try zone_operator_freqs.put(zk, list);
                }
            }
        } else {
            var grid = try zone_assign.assignGrid(a, xs, ys, zone_size_m);
            defer grid.deinit(a);
            for (valid_rows.items, 0..) |vr, i| {
                const zona_key = try std.fmt.allocPrint(a, "{d}_{d}", .{
                    @as(i64, @intFromFloat(std.math.floor(grid.zona_x[i]))),
                    @as(i64, @intFromFloat(std.math.floor(grid.zona_y[i]))),
                });
                const operator_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ vr.mcc, vr.mnc });
                const key = try compositeSelectedKey(a, zona_key, operator_key, vr.pci, vr.freq);
                if (selected_map.getPtr(key)) |meta| {
                    if (meta.sample_row_index == null) meta.sample_row_index = vr.table_row_index;
                    try meta.original_rows.append(a, table.rows[vr.table_row_index].original_excel_row);
                }
                const zk = try compositeZoneOperatorKey(a, zona_key, operator_key);
                if (zone_operator_freqs.getPtr(zk)) |freqs| {
                    var exists = false;
                    for (freqs.items) |f| {
                        if (std.mem.eql(u8, f, vr.freq)) {
                            exists = true;
                            break;
                        }
                    }
                    if (!exists) try freqs.append(a, try a.dupe(u8, vr.freq));
                } else {
                    var list = std.ArrayList([]const u8).empty;
                    try list.append(a, try a.dupe(u8, vr.freq));
                    try zone_operator_freqs.put(zk, list);
                }
            }
        }
    }

    const file = try std.fs.cwd().createFile(output_path, .{ .truncate = true, .read = true });
    defer file.close();
    var buf: [16384]u8 = undefined;
    var fw = file.writer(&buf);
    const w = &fw.interface;

    // blank line
    try w.writeByte('\n');

    const header_cols_joined = try std.mem.join(a, ";", export_cols);
    const full_header = try std.fmt.allocPrint(a, "{s};Riadky_v_zone;Frekvencie_v_zone", .{header_cols_joined});
    try w.print("{s}\n", .{full_header});
    var wrote_empty_rows = false;

    const sorted = try a.alloc(zone_stats_core.ZoneOperatorStat, core_rows.len);
    @memcpy(sorted, core_rows);
    std.sort.heap(zone_stats_core.ZoneOperatorStat, sorted, {}, statsSortLessThan);
    var ordered_zone_keys = std.ArrayList([]const u8).empty;
    var ordered_zone_seen = std.StringHashMap(void).init(a);
    for (sorted) |sr| {
        if (!ordered_zone_seen.contains(sr.zona_key)) {
            try ordered_zone_seen.put(sr.zona_key, {});
            try ordered_zone_keys.append(a, sr.zona_key);
        }
    }

    for (sorted) |sr| {
        const sel_key = try compositeSelectedKey(a, sr.zona_key, sr.operator_key, sr.pci, sr.selected_frequency);
        const meta = selected_map.getPtr(sel_key) orelse continue;
        const sample_idx = meta.sample_row_index orelse continue;
        const sample = table.rows[sample_idx];

        const row_values = try a.alloc([]const u8, export_cols.len);
        for (export_cols, 0..) |col_name, i| {
            if (indexOfColumnExact(table.column_names, col_name)) |src_idx| {
                row_values[i] = try a.dupe(u8, trimValue(getValue(sample, src_idx)));
            } else if (indexOfColumnCaseInsensitive(table.column_names, col_name)) |src_idx| {
                row_values[i] = try a.dupe(u8, trimValue(getValue(sample, src_idx)));
            } else {
                row_values[i] = try a.dupe(u8, "");
            }
        }

        if (rsrp_export_idx) |idx| row_values[idx] = try std.fmt.allocPrint(a, "{d:.2}", .{sr.rsrp_avg});
        if (freq_export_idx) |idx| row_values[idx] = try a.dupe(u8, sr.selected_frequency);
        if (pci_export_idx) |idx| row_values[idx] = try a.dupe(u8, sr.pci);
        if (mcc_export_idx) |idx| row_values[idx] = try a.dupe(u8, sr.mcc);
        if (mnc_export_idx) |idx| row_values[idx] = try a.dupe(u8, sr.mnc);
        if (sinr_export_idx) |idx| {
            if (sr.sinr_avg) |v| row_values[idx] = try std.fmt.allocPrint(a, "{d:.2}", .{v});
        }

        if (nr_export_idx) |idx| {
            if (row_values[idx].len == 0) row_values[idx] = try a.dupe(u8, "");
        }

        if ((isSegmentsMode(zone_mode) or use_zone_center) and lat_export_idx != null and lon_export_idx != null) {
            if (zone_latlon_map.get(sr.zona_key)) |ll| {
                row_values[lat_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{ll.lat});
                row_values[lon_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{ll.lon});
            } else if (lat0_rad_opt != null) {
                var center_x = sr.zona_x;
                var center_y = sr.zona_y;
                if (!isSegmentsMode(zone_mode)) {
                    center_x += zone_size_m / 2.0;
                    center_y += zone_size_m / 2.0;
                }
                const lat = radToDeg(center_y / earth_r);
                const lon = radToDeg(center_x / (earth_r * @cos(lat0_rad_opt.?)));
                row_values[lat_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{lat});
                row_values[lon_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{lon});
            }
        }

        var excel_rows_text = std.ArrayList(u8).empty;
        for (meta.original_rows.items, 0..) |excel_row, i| {
            if (i > 0) try excel_rows_text.append(a, ',');
            try excel_rows_text.writer(a).print("{d}", .{excel_row});
        }
        const frequencies_text: []const u8 = sr.selected_frequency;
        const base_csv = try std.mem.join(a, ";", row_values);
        const full_line = try std.fmt.allocPrint(
            a,
            "{s};{s};{s} # Meraní: {d}\n",
            .{
                base_csv,
                excel_rows_text.items,
                frequencies_text,
                sr.pocet_merani,
            },
        );
        try w.writeAll(full_line);
    }

    if (include_empty_zones and core_rows.len > 0) {
        var operator_templates = std.ArrayList(OperatorTemplate).empty;
        var operator_seen = std.StringHashMap(usize).init(a);

        // Existing operators from current core rows
        for (sorted) |sr| {
            if (operator_seen.contains(sr.operator_key)) continue;
            var sample_idx_opt: ?usize = null;
            var sm_it = selected_map.iterator();
            while (sm_it.next()) |entry| {
                const k = entry.key_ptr.*;
                if (std.mem.indexOf(u8, k, sr.operator_key) == null) continue;
                if (entry.value_ptr.sample_row_index) |idx| {
                    sample_idx_opt = idx;
                    break;
                }
            }
            const idx = operator_templates.items.len;
            try operator_templates.append(a, .{
                .operator_key = sr.operator_key,
                .mcc = sr.mcc,
                .mnc = sr.mnc,
                .sample_row_index = sample_idx_opt,
                .is_custom = false,
            });
            try operator_seen.put(sr.operator_key, idx);
        }

        if (add_custom_operators) {
            for (custom_operators) |op| {
                const op_key = try std.fmt.allocPrint(a, "{s}_{s}", .{ op.mcc, op.mnc });
                if (operator_seen.contains(op_key)) continue;
                const idx = operator_templates.items.len;
                try operator_templates.append(a, .{
                    .operator_key = op_key,
                    .mcc = op.mcc,
                    .mnc = op.mnc,
                    .sample_row_index = if (table.rows.len > 0) 0 else null,
                    .is_custom = true,
                    .custom_pci = op.pci,
                });
                try operator_seen.put(op_key, idx);
            }
        }

        for ([_]bool{ false, true }) |custom_phase| {
            for (ordered_zone_keys.items) |zona_key| {
                const zmeta = zone_meta_map.get(zona_key) orelse continue;
                for (operator_templates.items) |op_tpl| {
                    if (op_tpl.is_custom != custom_phase) continue;
                    const zo_key = try compositeZoneOperatorKey(a, zona_key, op_tpl.operator_key);
                    if (processed_zone_ops.contains(zo_key)) continue;

                    const sample_idx = op_tpl.sample_row_index orelse continue;
                    const sample = table.rows[sample_idx];
                    const row_values = try a.alloc([]const u8, export_cols.len);
                    for (export_cols, 0..) |col_name, i| {
                        if (indexOfColumnExact(table.column_names, col_name)) |src_idx| {
                            row_values[i] = try a.dupe(u8, trimValue(getValue(sample, src_idx)));
                        } else if (indexOfColumnCaseInsensitive(table.column_names, col_name)) |src_idx| {
                            row_values[i] = try a.dupe(u8, trimValue(getValue(sample, src_idx)));
                        } else {
                            row_values[i] = try a.dupe(u8, "");
                        }
                    }

                    if (mcc_export_idx) |idx| row_values[idx] = try a.dupe(u8, op_tpl.mcc);
                    if (mnc_export_idx) |idx| row_values[idx] = try a.dupe(u8, op_tpl.mnc);
                    if (pci_export_idx) |idx| {
                        if (op_tpl.custom_pci) |cpci| {
                            row_values[idx] = try a.dupe(u8, cpci);
                        }
                    }
                    if (rsrp_export_idx) |idx| row_values[idx] = try a.dupe(u8, "-174");
                    if (nr_export_idx) |idx| row_values[idx] = try a.dupe(u8, "no");
                    if (sinr_export_idx) |idx| {
                        const parsed = parseFloatLike(a, row_values[idx]);
                        if (parsed) |v| row_values[idx] = try std.fmt.allocPrint(a, "{d:.1}", .{v});
                    }

                    if (lat_export_idx != null and lon_export_idx != null) {
                        if (zone_latlon_map.get(zona_key)) |ll| {
                            row_values[lat_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{ll.lat});
                            row_values[lon_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{ll.lon});
                        } else if (lat0_rad_opt != null) {
                            var center_x = zmeta.zona_x;
                            var center_y = zmeta.zona_y;
                            if (!isSegmentsMode(zone_mode)) {
                                center_x += zone_size_m / 2.0;
                                center_y += zone_size_m / 2.0;
                            }
                            const lat = radToDeg(center_y / earth_r);
                            const lon = radToDeg(center_x / (earth_r * @cos(lat0_rad_opt.?)));
                            row_values[lat_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{lat});
                            row_values[lon_export_idx.?] = try std.fmt.allocPrint(a, "{d:.6}", .{lon});
                        }
                    }

                    const base_csv = try std.mem.join(a, ";", row_values);
                    const line2 = try std.fmt.allocPrint(a, "{s};; {s}\n", .{
                        base_csv,
                        if (isSegmentsMode(zone_mode))
                            (if (op_tpl.is_custom) "# Prázdny úsek - vlastný operátor" else "# Prázdny úsek - automaticky vygenerovaný")
                        else
                            (if (op_tpl.is_custom) "# Prázdna zóna - vlastný operátor" else "# Prázdna zóna - automaticky vygenerovaná"),
                    });
                    try w.writeAll(line2);
                    wrote_empty_rows = true;
                }
            }
        }
    }

    try w.flush();
    const end_pos = try file.getEndPos();
    if (!wrote_empty_rows and end_pos > 0) {
        var last: [1]u8 = undefined;
        const got = try file.preadAll(&last, end_pos - 1);
        if (got == 1 and last[0] == '\n') {
            try file.setEndPos(end_pos - 1);
        }
    }
}

test "zones writer basic module compiles and writes header only for empty rows" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 3),
        .rows = try a.alloc(csv_table.CsvRow, 0),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "RSRP");

    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const dir_path = try tmp.dir.realpathAlloc(a, ".");
    defer a.free(dir_path);
    const out = try std.fmt.allocPrint(a, "{s}/zones.csv", .{dir_path});

    try writeZonesCsvBasic(a, "Latitude;Longitude;RSRP", table, &.{}, .{
        .latitude = 0, .longitude = 1, .frequency = 0, .pci = 0, .mcc = 0, .mnc = 0, .rsrp = 2, .sinr = null,
    }, "center", 100, true, false, false, &.{}, out);

    const f = try std.fs.cwd().openFile(out, .{});
    defer f.close();
    const text = try f.readToEndAlloc(a, 4096);
    try std.testing.expect(std.mem.indexOf(u8, text, "Riadky_v_zone;Frekvencie_v_zone") != null);
}

test "zones writer can generate custom operator empty zone row" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 6),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Latitude");
    table.column_names[1] = try a.dupe(u8, "Longitude");
    table.column_names[2] = try a.dupe(u8, "Frequency");
    table.column_names[3] = try a.dupe(u8, "PCI");
    table.column_names[4] = try a.dupe(u8, "MCC");
    table.column_names[5] = try a.dupe(u8, "MNC");
    table.rows[0] = .{ .values = try a.alloc([]const u8, 6), .original_excel_row = 2 };
    table.rows[0].values[0] = try a.dupe(u8, "48.1");
    table.rows[0].values[1] = try a.dupe(u8, "17.1");
    table.rows[0].values[2] = try a.dupe(u8, "800");
    table.rows[0].values[3] = try a.dupe(u8, "10");
    table.rows[0].values[4] = try a.dupe(u8, "231");
    table.rows[0].values[5] = try a.dupe(u8, "2");

    const core_rows = [_]zone_stats_core.ZoneOperatorStat{
        .{
            .zona_key = "0_0",
            .operator_key = "231_2",
            .mcc = "231",
            .mnc = "2",
            .pci = "10",
            .selected_frequency = "800",
            .zona_x = 0,
            .zona_y = 0,
            .rsrp_avg = -90,
            .sinr_avg = null,
            .pocet_merani = 1,
            .coverage = .good,
        },
    };
    const custom_ops = [_]cfg.CustomOperator{
        .{ .mcc = "231", .mnc = "99", .pci = "" },
    };

    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const dir_path = try tmp.dir.realpathAlloc(a, ".");
    defer a.free(dir_path);
    const out = try std.fmt.allocPrint(a, "{s}/zones.csv", .{dir_path});

    try writeZonesCsvBasic(a, "Latitude;Longitude;Frequency;PCI;MCC;MNC", table, &core_rows, .{
        .latitude = 0, .longitude = 1, .frequency = 2, .pci = 3, .mcc = 4, .mnc = 5, .rsrp = 2, .sinr = null,
    }, "center", 100, true, true, true, &custom_ops, out);

    const f = try std.fs.cwd().openFile(out, .{});
    defer f.close();
    const text = try f.readToEndAlloc(a, 8192);
    try std.testing.expect(std.mem.indexOf(u8, text, "231;99") != null);
    try std.testing.expect(std.mem.indexOf(u8, text, "vlastný operátor") != null);
}
