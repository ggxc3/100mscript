const std = @import("std");

pub const CsvProbeResult = struct {
    bytes: []u8,
    lines: [][]const u8,
    header_line: isize,
    header_text: ?[]const u8,

    pub fn deinit(self: *CsvProbeResult, allocator: std.mem.Allocator) void {
        allocator.free(self.lines);
        allocator.free(self.bytes);
        self.* = undefined;
    }
};

fn trimLineEndings(line: []const u8) []const u8 {
    return std.mem.trimRight(u8, line, "\r\n");
}

pub fn countSemicolonColumns(line_raw: []const u8) usize {
    const line = trimLineEndings(line_raw);
    if (line.len == 0) return 0;

    var count: usize = 1;
    for (line) |ch| {
        if (ch == ';') count += 1;
    }

    // Remove trailing empty columns caused by ending semicolons.
    var i: usize = line.len;
    while (i > 0 and line[i - 1] == ';') : (i -= 1) {
        count -= 1;
    }
    return count;
}

pub fn splitSemicolonColumnsAlloc(allocator: std.mem.Allocator, line_raw: []const u8) ![][]const u8 {
    const line = trimLineEndings(line_raw);
    var list = std.ArrayList([]const u8).empty;
    errdefer list.deinit(allocator);

    var it = std.mem.splitScalar(u8, line, ';');
    while (it.next()) |part| {
        try list.append(allocator, part);
    }
    while (list.items.len > 0 and list.items[list.items.len - 1].len == 0) {
        _ = list.pop();
    }
    return try list.toOwnedSlice(allocator);
}

pub fn hasTabularFollowup(
    lines: [][]const u8,
    start_index: usize,
    expected_columns: usize,
    min_columns: usize,
) bool {
    var seen_candidates: usize = 0;
    var tabular_rows: usize = 0;
    var i = start_index + 1;
    while (i < lines.len) : (i += 1) {
        const line = lines[i];
        if (std.mem.trim(u8, line, " \t\r\n").len == 0) continue;
        seen_candidates += 1;
        const cols_count = countSemicolonColumns(line);
        const threshold = @max(min_columns, if (expected_columns > 0) expected_columns - 1 else 0);
        if (cols_count >= threshold) {
            tabular_rows += 1;
            if (tabular_rows >= 2) return true;
        }
        if (seen_candidates >= 25) break;
    }
    return false;
}

pub fn findTabularHeader(lines: [][]const u8, min_columns: usize) struct { line: isize, header: ?[]const u8 } {
    var first_candidate_line: isize = -1;
    var first_candidate_header: ?[]const u8 = null;

    for (lines, 0..) |line, idx| {
        const cols_count = countSemicolonColumns(line);
        if (cols_count < min_columns) continue;

        if (first_candidate_line == -1) {
            first_candidate_line = @intCast(idx);
            first_candidate_header = trimLineEndings(line);
        }
        if (hasTabularFollowup(lines, idx, cols_count, min_columns)) {
            return .{ .line = @intCast(idx), .header = trimLineEndings(line) };
        }
    }

    return .{ .line = first_candidate_line, .header = first_candidate_header };
}

pub fn makeUniqueColumnNamesAlloc(allocator: std.mem.Allocator, columns: [][]const u8) ![][]const u8 {
    var out = try allocator.alloc([]const u8, columns.len);
    var seen = std.StringHashMap(usize).init(allocator);
    defer seen.deinit();

    for (columns, 0..) |raw_name, idx| {
        const trimmed = std.mem.trim(u8, raw_name, " \t\r\n");
        const base_name = if (trimmed.len == 0)
            try std.fmt.allocPrint(allocator, "column_{d}", .{idx + 1})
        else
            try allocator.dupe(u8, trimmed);

        const occurrence = seen.get(base_name) orelse 0;
        try seen.put(base_name, occurrence + 1);
        if (occurrence == 0) {
            out[idx] = base_name;
        } else {
            out[idx] = try std.fmt.allocPrint(allocator, "{s}_{d}", .{ base_name, occurrence + 1 });
        }
    }
    return out;
}

pub fn probeCsvFile(allocator: std.mem.Allocator, path: []const u8) !CsvProbeResult {
    const file = try std.fs.cwd().openFile(path, .{});
    defer file.close();

    const bytes = try file.readToEndAlloc(allocator, 64 * 1024 * 1024);
    errdefer allocator.free(bytes);

    var lines_list = std.ArrayList([]const u8).empty;
    errdefer lines_list.deinit(allocator);
    var it = std.mem.splitScalar(u8, bytes, '\n');
    while (it.next()) |line| {
        try lines_list.append(allocator, line);
    }
    const lines = try lines_list.toOwnedSlice(allocator);
    errdefer allocator.free(lines);

    const header = findTabularHeader(lines, 6);
    return .{
        .bytes = bytes,
        .lines = lines,
        .header_line = header.line,
        .header_text = header.header,
    };
}

test "findTabularHeader skips metadata block" {
    const lines = [_][]const u8{
        "Export",
        "Meta: test",
        "A;B;C;D;E;F",
        "1;2;3;4;5;6",
        "7;8;9;10;11;12",
    };
    const res = findTabularHeader(@constCast(lines[0..]), 6);
    try std.testing.expectEqual(@as(isize, 2), res.line);
    try std.testing.expectEqualStrings("A;B;C;D;E;F", res.header.?);
}

test "countSemicolonColumns ignores trailing empty fields" {
    try std.testing.expectEqual(@as(usize, 3), countSemicolonColumns("A;B;C;;"));
    try std.testing.expectEqual(@as(usize, 3), countSemicolonColumns("1;2;3"));
}
