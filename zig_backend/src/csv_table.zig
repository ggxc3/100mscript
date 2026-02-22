const std = @import("std");
const csv_probe = @import("csv_probe.zig");

pub const CsvRow = struct {
    values: [][]const u8,
    original_excel_row: i64,
};

pub const CsvTable = struct {
    column_names: [][]const u8,
    rows: []CsvRow,
    header_line: isize,
    header_text: ?[]const u8 = null,

    pub fn deinit(self: *CsvTable, allocator: std.mem.Allocator) void {
        if (self.header_text) |h| allocator.free(h);
        for (self.column_names) |name| allocator.free(name);
        allocator.free(self.column_names);

        for (self.rows) |row| {
            for (row.values) |value| allocator.free(value);
            allocator.free(row.values);
        }
        allocator.free(self.rows);
        self.* = undefined;
    }
};

fn isBlank(line: []const u8) bool {
    return std.mem.trim(u8, line, " \t\r\n").len == 0;
}

fn dupCols(allocator: std.mem.Allocator, cols: [][]const u8, expected: usize) ![][]const u8 {
    const out = try allocator.alloc([]const u8, expected);
    for (0..expected) |i| {
        const src = if (i < cols.len) cols[i] else "";
        out[i] = try allocator.dupe(u8, src);
    }
    return out;
}

pub fn loadCsvTableFromFile(allocator: std.mem.Allocator, path: []const u8) !CsvTable {
    var probe = try csv_probe.probeCsvFile(allocator, path);
    defer probe.deinit(allocator);

    const header_line_index: usize = if (probe.header_line >= 0) @intCast(probe.header_line) else 0;
    if (probe.lines.len == 0) return error.EmptyCsv;
    if (header_line_index >= probe.lines.len) return error.InvalidHeaderLine;

    var header_cols_raw = try csv_probe.splitSemicolonColumnsAlloc(allocator, probe.lines[header_line_index]);
    var generated_extra_names = std.ArrayList([]const u8).empty;
    defer {
        for (generated_extra_names.items) |name| allocator.free(name);
        generated_extra_names.deinit(allocator);
        allocator.free(header_cols_raw);
    }

    var max_fields = header_cols_raw.len;
    for (probe.lines, 0..) |line, idx| {
        if (idx <= header_line_index) continue;
        if (isBlank(line)) continue;
        const count = csv_probe.countSemicolonColumns(line);
        if (count > max_fields) max_fields = count;
    }
    if (max_fields == 0) max_fields = 1;

    if (header_cols_raw.len < max_fields) {
        const extended = try allocator.alloc([]const u8, max_fields);
        for (0..max_fields) |i| {
            if (i < header_cols_raw.len) {
                extended[i] = header_cols_raw[i];
            } else {
                const extra = try std.fmt.allocPrint(allocator, "extra_col_{d}", .{i - header_cols_raw.len + 1});
                try generated_extra_names.append(allocator, extra);
                extended[i] = extra;
            }
        }
        allocator.free(header_cols_raw);
        header_cols_raw = extended;
    }

    const unique_column_names = try csv_probe.makeUniqueColumnNamesAlloc(allocator, header_cols_raw);

    var rows_list = std.ArrayList(CsvRow).empty;
    errdefer rows_list.deinit(allocator);
    var data_row_ordinal: usize = 0;

    for (probe.lines, 0..) |line, idx| {
        if (idx <= header_line_index) continue;
        if (isBlank(line)) continue;

        const row_cols_tmp = try csv_probe.splitSemicolonColumnsAlloc(allocator, line);
        defer allocator.free(row_cols_tmp);
        const row_cols = try dupCols(allocator, row_cols_tmp, max_fields);

        data_row_ordinal += 1;
        try rows_list.append(allocator, .{
            .values = row_cols,
            // Match Python backend semantics: df.index + header_line + 1
            .original_excel_row = @intCast(header_line_index + data_row_ordinal),
        });
    }

    return .{
        .column_names = unique_column_names,
        .rows = try rows_list.toOwnedSlice(allocator),
        .header_line = probe.header_line,
        .header_text = if (probe.header_text) |h| try allocator.dupe(u8, h) else null,
    };
}

test "loadCsvTableFromFile builds rows and original_excel_row" {
    const a = std.testing.allocator;
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();

    try tmp.dir.writeFile(.{
        .sub_path = "in.csv",
        .data =
            \\meta
            \\A;B;C;D;E;F
            \\1;2;3;4;5;6
            \\7;8;9;10;11;12
        ,
    });

    const cwd = std.fs.cwd();
    _ = cwd;
    // openFile uses current cwd; use absolute path
    const path = try tmp.dir.realpathAlloc(a, "in.csv");
    defer a.free(path);

    var table = try loadCsvTableFromFile(a, path);
    defer table.deinit(a);

    try std.testing.expectEqual(@as(usize, 6), table.column_names.len);
    try std.testing.expectEqual(@as(usize, 2), table.rows.len);
    try std.testing.expectEqual(@as(i64, 2), table.rows[0].original_excel_row);
    try std.testing.expectEqualStrings("7", table.rows[1].values[0]);
}
