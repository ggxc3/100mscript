const std = @import("std");
const cfg = @import("config.zig");
const csv_table = @import("csv_table.zig");
const filters = @import("filters.zig");

pub const ApplyPreviewStats = struct {
    input_rows: usize,
    output_rows: usize,
    matched_rows: usize,
    rows_with_multiple_matches: usize,
};

pub const ApplyMaterializedResult = struct {
    table: csv_table.CsvTable,
    stats: ApplyPreviewStats,
};

const AssignmentValueSet = struct {
    field_name: []const u8,
    col_index: ?usize,
    values: []filters.Numeric,
};

const ResolvedCondition = struct {
    col_index: ?usize,
    value: filters.ConditionValue,
};

const ResolvedConditionGroup = struct {
    conditions: []ResolvedCondition,
};

const ResolvedRule = struct {
    name: []const u8,
    assignment_sets: []AssignmentValueSet,
    condition_groups: []ResolvedConditionGroup,
};

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

fn aliasCanonical(field: []const u8) ?[]const u8 {
    if (std.mem.eql(u8, field, "latitude") or std.mem.eql(u8, field, "lat")) return "latitude";
    if (std.mem.eql(u8, field, "longitude") or std.mem.eql(u8, field, "lon")) return "longitude";
    if (std.mem.eql(u8, field, "frequency") or std.mem.eql(u8, field, "freq") or std.mem.eql(u8, field, "earfcn")) return "frequency";
    if (std.mem.eql(u8, field, "pci")) return "pci";
    if (std.mem.eql(u8, field, "mcc")) return "mcc";
    if (std.mem.eql(u8, field, "mnc")) return "mnc";
    if (std.mem.eql(u8, field, "rsrp")) return "rsrp";
    if (std.mem.eql(u8, field, "sinr")) return "sinr";
    return null;
}

fn mappingIndex(mapping: cfg.ColumnMapping, canonical: []const u8) ?usize {
    if (std.mem.eql(u8, canonical, "latitude")) return @intCast(mapping.latitude);
    if (std.mem.eql(u8, canonical, "longitude")) return @intCast(mapping.longitude);
    if (std.mem.eql(u8, canonical, "frequency")) return @intCast(mapping.frequency);
    if (std.mem.eql(u8, canonical, "pci")) return @intCast(mapping.pci);
    if (std.mem.eql(u8, canonical, "mcc")) return @intCast(mapping.mcc);
    if (std.mem.eql(u8, canonical, "mnc")) return @intCast(mapping.mnc);
    if (std.mem.eql(u8, canonical, "rsrp")) return @intCast(mapping.rsrp);
    if (std.mem.eql(u8, canonical, "sinr")) {
        if (mapping.sinr) |idx| return @intCast(idx);
    }
    return null;
}

fn resolveFieldToColumn(
    columns: [][]const u8,
    field_name: []const u8,
    mapping: ?cfg.ColumnMapping,
) ?usize {
    if (indexOfColumnExact(columns, field_name)) |idx| return idx;
    if (indexOfColumnCaseInsensitive(columns, field_name)) |idx| return idx;

    if (mapping) |m| {
        var lower_storage: [64]u8 = undefined;
        var lower_len: usize = 0;
        for (std.mem.trim(u8, field_name, " \t\r\n")) |ch| {
            if (lower_len >= lower_storage.len) break;
            lower_storage[lower_len] = std.ascii.toLower(ch);
            lower_len += 1;
        }
        if (aliasCanonical(lower_storage[0..lower_len])) |canonical| {
            if (mappingIndex(m, canonical)) |idx| {
                if (idx < columns.len) return idx;
            }
        }
    }
    return null;
}

fn parseRowNumeric(allocator: std.mem.Allocator, raw: []const u8) ?filters.Numeric {
    const parsed = filters.parseNumericForRow(allocator, raw) catch return null;
    return parsed;
}

fn numericCompare(a: filters.Numeric, b: filters.Numeric) i2 {
    return filters.compareNumericForEval(a, b);
}

fn rowMatchesCondition(
    allocator: std.mem.Allocator,
    row: csv_table.CsvRow,
    cond: ResolvedCondition,
) bool {
    const idx = cond.col_index orelse return false;
    if (idx >= row.values.len) return false;
    const row_num = parseRowNumeric(allocator, row.values[idx]) orelse return false;

    return switch (cond.value) {
        .eq => |v| numericCompare(row_num, v) == 0,
        .range => |r| blk: {
            const low_cmp = numericCompare(row_num, r.low);
            const high_cmp = numericCompare(row_num, r.high);
            if (numericCompare(r.low, r.high) == 0) break :blk low_cmp == 0;
            break :blk low_cmp >= 0 and high_cmp < 0;
        },
    };
}

fn rowMatchesGroup(
    allocator: std.mem.Allocator,
    row: csv_table.CsvRow,
    group: ResolvedConditionGroup,
) bool {
    for (group.conditions) |cond| {
        if (!rowMatchesCondition(allocator, row, cond)) return false;
    }
    return true;
}

fn resolveRules(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    rules: []filters.FilterRule,
    mapping: ?cfg.ColumnMapping,
) ![]ResolvedRule {
    var out = std.ArrayList(ResolvedRule).empty;
    errdefer out.deinit(allocator);

    for (rules) |rule| {
        // group assignment values by field
        var assignment_sets = std.ArrayList(AssignmentValueSet).empty;
        errdefer assignment_sets.deinit(allocator);
        for (rule.assignments) |assignment| {
            var found_idx: ?usize = null;
            for (assignment_sets.items, 0..) |set, idx| {
                if (std.mem.eql(u8, set.field_name, assignment.field)) {
                    found_idx = idx;
                    break;
                }
            }

            if (found_idx) |idx| {
                var vals = std.ArrayList(filters.Numeric).fromOwnedSlice(assignment_sets.items[idx].values);
                defer vals.deinit(allocator);
                var exists = false;
                for (vals.items) |v| {
                    if (v.eql(assignment.value)) {
                        exists = true;
                        break;
                    }
                }
                if (!exists) try vals.append(allocator, assignment.value);
                assignment_sets.items[idx].values = try vals.toOwnedSlice(allocator);
            } else {
                const vals = try allocator.alloc(filters.Numeric, 1);
                vals[0] = assignment.value;
                try assignment_sets.append(allocator, .{
                    .field_name = assignment.field,
                    .col_index = resolveFieldToColumn(table.column_names, assignment.field, mapping),
                    .values = vals,
                });
            }
        }

        var resolved_groups = std.ArrayList(ResolvedConditionGroup).empty;
        errdefer resolved_groups.deinit(allocator);
        for (rule.condition_groups) |group| {
            const conds = try allocator.alloc(ResolvedCondition, group.conditions.len);
            for (group.conditions, 0..) |cond, i| {
                conds[i] = .{
                    .col_index = resolveFieldToColumn(table.column_names, cond.field, mapping),
                    .value = cond.value,
                };
            }
            try resolved_groups.append(allocator, .{ .conditions = conds });
        }

        try out.append(allocator, .{
            .name = rule.name,
            .assignment_sets = try assignment_sets.toOwnedSlice(allocator),
            .condition_groups = try resolved_groups.toOwnedSlice(allocator),
        });
    }

    return try out.toOwnedSlice(allocator);
}

fn assignmentCombinationCount(rule: ResolvedRule) usize {
    if (rule.assignment_sets.len == 0) return 1;
    var total: usize = 1;
    for (rule.assignment_sets) |set| {
        if (set.values.len == 0) continue;
        total *= set.values.len;
    }
    return total;
}

const MatchDecision = struct {
    best_rule_idx: ?usize,
    matched: bool,
    rows_with_multiple_same_best: bool,
};

fn chooseBestRuleForRow(
    allocator: std.mem.Allocator,
    row: csv_table.CsvRow,
    resolved: []ResolvedRule,
) MatchDecision {
    var best_rule_idx: ?usize = null;
    var best_group_size: usize = 0;
    var same_best_count: usize = 0;
    var any_match_count: usize = 0;

    for (resolved, 0..) |rule, rule_idx| {
        var rule_best_group: usize = 0;
        for (rule.condition_groups) |group| {
            if (rowMatchesGroup(allocator, row, group)) {
                if (group.conditions.len > rule_best_group) {
                    rule_best_group = group.conditions.len;
                }
            }
        }
        if (rule_best_group > 0) {
            any_match_count += 1;
            if (rule_best_group > best_group_size) {
                best_group_size = rule_best_group;
                best_rule_idx = rule_idx;
                same_best_count = 1;
            } else if (rule_best_group == best_group_size and best_group_size > 0) {
                same_best_count += 1;
                if (best_rule_idx) |cur_idx| {
                    if (std.mem.order(u8, rule.name, resolved[cur_idx].name) == .lt) {
                        best_rule_idx = rule_idx;
                    }
                }
            }
        }
    }

    return .{
        .best_rule_idx = best_rule_idx,
        .matched = any_match_count > 0,
        .rows_with_multiple_same_best = same_best_count > 1,
    };
}

fn stringifyNumeric(allocator: std.mem.Allocator, value: filters.Numeric) ![]const u8 {
    return switch (value) {
        .int => |v| try std.fmt.allocPrint(allocator, "{d}", .{v}),
        .float => |v| try std.fmt.allocPrint(allocator, "{d}", .{v}),
    };
}

fn cloneRowWithExtraCols(
    allocator: std.mem.Allocator,
    row: csv_table.CsvRow,
    extra_cols: usize,
) !csv_table.CsvRow {
    const total_cols = row.values.len + extra_cols;
    const out_vals = try allocator.alloc([]const u8, total_cols);
    for (row.values, 0..) |val, idx| {
        out_vals[idx] = try allocator.dupe(u8, val);
    }
    for (row.values.len..total_cols) |idx| {
        out_vals[idx] = try allocator.dupe(u8, "");
    }
    return .{
        .values = out_vals,
        .original_excel_row = row.original_excel_row,
    };
}

fn appendAssignedCombinations(
    allocator: std.mem.Allocator,
    out_rows: *std.ArrayList(csv_table.CsvRow),
    base_row: csv_table.CsvRow,
    rule: ResolvedRule,
    extra_base_index: usize,
    extra_index_by_field: *std.StringHashMap(usize),
) !void {
    if (rule.assignment_sets.len == 0) {
        try out_rows.append(allocator, base_row);
        return;
    }

    const choice_indices = try allocator.alloc(usize, rule.assignment_sets.len);
    defer allocator.free(choice_indices);
    @memset(choice_indices, 0);

    while (true) {
        var row_copy = try cloneRowWithExtraCols(allocator, base_row, extra_index_by_field.count());

        for (rule.assignment_sets, 0..) |set, set_idx| {
            if (set.values.len == 0) continue;
            const chosen = set.values[choice_indices[set_idx]];
            const text = try stringifyNumeric(allocator, chosen);

            if (set.col_index) |col_idx| {
                if (col_idx < row_copy.values.len) {
                    row_copy.values[col_idx] = text;
                }
            } else if (extra_index_by_field.get(set.field_name)) |extra_idx| {
                const abs_idx = extra_base_index + extra_idx;
                if (abs_idx < row_copy.values.len) {
                    row_copy.values[abs_idx] = text;
                }
            }
        }

        try out_rows.append(allocator, row_copy);

        var carry_pos: isize = @as(isize, @intCast(rule.assignment_sets.len)) - 1;
        while (carry_pos >= 0) : (carry_pos -= 1) {
            const u: usize = @intCast(carry_pos);
            const len = rule.assignment_sets[u].values.len;
            if (len == 0) continue;
            choice_indices[u] += 1;
            if (choice_indices[u] < len) break;
            choice_indices[u] = 0;
        }
        if (carry_pos < 0) break;
    }
}

fn buildExtraAssignmentColumns(
    allocator: std.mem.Allocator,
    resolved: []ResolvedRule,
) !struct {
    names: [][]const u8,
    index_by_field: std.StringHashMap(usize),
} {
    var names = std.ArrayList([]const u8).empty;
    errdefer names.deinit(allocator);
    var map = std.StringHashMap(usize).init(allocator);
    errdefer map.deinit();

    for (resolved) |rule| {
        for (rule.assignment_sets) |set| {
            if (set.col_index != null) continue;
            if (map.contains(set.field_name)) continue;
            const idx = names.items.len;
            const dup = try allocator.dupe(u8, set.field_name);
            try names.append(allocator, dup);
            try map.put(dup, idx);
        }
    }

    return .{
        .names = try names.toOwnedSlice(allocator),
        .index_by_field = map,
    };
}

pub fn applyFiltersMaterialized(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    rules: []filters.FilterRule,
    keep_original_on_match: bool,
    mapping: ?cfg.ColumnMapping,
) !ApplyMaterializedResult {
    if (rules.len == 0) {
        // clone input table
        const cols = try allocator.alloc([]const u8, table.column_names.len);
        for (table.column_names, 0..) |c, i| cols[i] = try allocator.dupe(u8, c);

        const rows = try allocator.alloc(csv_table.CsvRow, table.rows.len);
        for (table.rows, 0..) |row, i| {
            rows[i] = try cloneRowWithExtraCols(allocator, row, 0);
        }
        return .{
            .table = .{ .column_names = cols, .rows = rows, .header_line = table.header_line },
            .stats = .{
                .input_rows = table.rows.len,
                .output_rows = table.rows.len,
                .matched_rows = 0,
                .rows_with_multiple_matches = 0,
            },
        };
    }

    const resolved = try resolveRules(allocator, table, rules, mapping);
    defer allocator.free(resolved);

    var extra = try buildExtraAssignmentColumns(allocator, resolved);
    defer {
        for (extra.names) |n| allocator.free(n);
        allocator.free(extra.names);
        extra.index_by_field.deinit();
    }

    const out_col_count = table.column_names.len + extra.names.len;
    const out_cols = try allocator.alloc([]const u8, out_col_count);
    for (table.column_names, 0..) |c, i| out_cols[i] = try allocator.dupe(u8, c);
    for (extra.names, 0..) |c, i| out_cols[table.column_names.len + i] = try allocator.dupe(u8, c);

    var out_rows = std.ArrayList(csv_table.CsvRow).empty;
    errdefer out_rows.deinit(allocator);

    var matched_rows: usize = 0;
    var multi_match: usize = 0;

    for (table.rows) |row| {
        const decision = chooseBestRuleForRow(allocator, row, resolved);
        if (!decision.matched) {
            try out_rows.append(allocator, try cloneRowWithExtraCols(allocator, row, extra.names.len));
            continue;
        }

        matched_rows += 1;
        if (decision.rows_with_multiple_same_best) multi_match += 1;

        if (keep_original_on_match) {
            try out_rows.append(allocator, try cloneRowWithExtraCols(allocator, row, extra.names.len));
        }
        const rule = resolved[decision.best_rule_idx.?];
        try appendAssignedCombinations(
            allocator,
            &out_rows,
            row,
            rule,
            table.column_names.len,
            &extra.index_by_field,
        );
    }

    const rows_slice = try out_rows.toOwnedSlice(allocator);
    return .{
        .table = .{
            .column_names = out_cols,
            .rows = rows_slice,
            .header_line = table.header_line,
        },
        .stats = .{
            .input_rows = table.rows.len,
            .output_rows = rows_slice.len,
            .matched_rows = matched_rows,
            .rows_with_multiple_matches = multi_match,
        },
    };
}

pub fn applyFiltersPreview(
    allocator: std.mem.Allocator,
    table: csv_table.CsvTable,
    rules: []filters.FilterRule,
    keep_original_on_match: bool,
    mapping: ?cfg.ColumnMapping,
) !ApplyPreviewStats {
    var res = try applyFiltersMaterialized(allocator, table, rules, keep_original_on_match, mapping);
    defer res.table.deinit(allocator);
    return res.stats;
}

test "applyFiltersPreview duplicates rows by assignment combinations" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 3),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Frequency");
    table.column_names[1] = try a.dupe(u8, "MCC");
    table.column_names[2] = try a.dupe(u8, "MNC");
    table.rows[0] = .{
        .values = try a.alloc([]const u8, 3),
        .original_excel_row = 2,
    };
    table.rows[0].values[0] = try a.dupe(u8, "800");
    table.rows[0].values[1] = try a.dupe(u8, "0");
    table.rows[0].values[2] = try a.dupe(u8, "0");

    const vals1 = try a.alloc(filters.Assignment, 3);
    vals1[0] = .{ .field = "MCC", .value = .{ .int = 231 } };
    vals1[1] = .{ .field = "MCC", .value = .{ .int = 232 } };
    vals1[2] = .{ .field = "MNC", .value = .{ .int = 1 } };
    const conds = try a.alloc(filters.Condition, 1);
    conds[0] = .{ .field = "Frequency", .value = .{ .eq = .{ .int = 800 } } };
    const groups = try a.alloc(filters.ConditionGroup, 1);
    groups[0] = .{ .conditions = conds };
    const rules = try a.alloc(filters.FilterRule, 1);
    rules[0] = .{ .name = "r1", .assignments = vals1, .condition_groups = groups };

    const stats = try applyFiltersPreview(a, table, rules, false, null);
    try std.testing.expectEqual(@as(usize, 1), stats.input_rows);
    try std.testing.expectEqual(@as(usize, 2), stats.output_rows);
    try std.testing.expectEqual(@as(usize, 1), stats.matched_rows);
}

test "applyFiltersPreview prefers more specific rule" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 4),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    defer table.deinit(a);
    table.column_names[0] = try a.dupe(u8, "Frequency");
    table.column_names[1] = try a.dupe(u8, "PCI");
    table.column_names[2] = try a.dupe(u8, "MCC");
    table.column_names[3] = try a.dupe(u8, "MNC");
    table.rows[0] = .{ .values = try a.alloc([]const u8, 4), .original_excel_row = 2 };
    table.rows[0].values[0] = try a.dupe(u8, "800");
    table.rows[0].values[1] = try a.dupe(u8, "10");
    table.rows[0].values[2] = try a.dupe(u8, "0");
    table.rows[0].values[3] = try a.dupe(u8, "0");

    const r1_assign = try a.alloc(filters.Assignment, 2);
    r1_assign[0] = .{ .field = "MCC", .value = .{ .int = 231 } };
    r1_assign[1] = .{ .field = "MNC", .value = .{ .int = 1 } };
    const r1_conds = try a.alloc(filters.Condition, 1);
    r1_conds[0] = .{ .field = "Frequency", .value = .{ .eq = .{ .int = 800 } } };
    const r1_groups = try a.alloc(filters.ConditionGroup, 1);
    r1_groups[0] = .{ .conditions = r1_conds };

    const r2_assign = try a.alloc(filters.Assignment, 2);
    r2_assign[0] = .{ .field = "MCC", .value = .{ .int = 231 } };
    r2_assign[1] = .{ .field = "MNC", .value = .{ .int = 2 } };
    const r2_conds = try a.alloc(filters.Condition, 2);
    r2_conds[0] = .{ .field = "Frequency", .value = .{ .eq = .{ .int = 800 } } };
    r2_conds[1] = .{ .field = "PCI", .value = .{ .eq = .{ .int = 10 } } };
    const r2_groups = try a.alloc(filters.ConditionGroup, 1);
    r2_groups[0] = .{ .conditions = r2_conds };

    const rules = try a.alloc(filters.FilterRule, 2);
    rules[0] = .{ .name = "a_general", .assignments = r1_assign, .condition_groups = r1_groups };
    rules[1] = .{ .name = "b_specific", .assignments = r2_assign, .condition_groups = r2_groups };

    const stats = try applyFiltersPreview(a, table, rules, true, null);
    try std.testing.expectEqual(@as(usize, 2), stats.output_rows); // 1 modified + original
    try std.testing.expectEqual(@as(usize, 1), stats.rows_with_multiple_matches);
}

test "applyFiltersMaterialized adds extra assignment columns and original rows" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    var table = csv_table.CsvTable{
        .column_names = try a.alloc([]const u8, 2),
        .rows = try a.alloc(csv_table.CsvRow, 1),
        .header_line = 0,
    };
    table.column_names[0] = try a.dupe(u8, "Frequency");
    table.column_names[1] = try a.dupe(u8, "MCC");
    table.rows[0] = .{ .values = try a.alloc([]const u8, 2), .original_excel_row = 7 };
    table.rows[0].values[0] = try a.dupe(u8, "800");
    table.rows[0].values[1] = try a.dupe(u8, "0");

    const assigns = try a.alloc(filters.Assignment, 2);
    assigns[0] = .{ .field = "MCC", .value = .{ .int = 231 } };
    assigns[1] = .{ .field = "MNC", .value = .{ .int = 1 } }; // extra column
    const conds = try a.alloc(filters.Condition, 1);
    conds[0] = .{ .field = "Frequency", .value = .{ .eq = .{ .int = 800 } } };
    const groups = try a.alloc(filters.ConditionGroup, 1);
    groups[0] = .{ .conditions = conds };
    const rules = try a.alloc(filters.FilterRule, 1);
    rules[0] = .{ .name = "rule", .assignments = assigns, .condition_groups = groups };

    const res = try applyFiltersMaterialized(a, table, rules, true, null);
    try std.testing.expectEqual(@as(usize, 2), res.table.rows.len);
    try std.testing.expectEqual(@as(usize, 3), res.table.column_names.len);
    try std.testing.expectEqualStrings("MNC", res.table.column_names[2]);
    try std.testing.expectEqualStrings("0", res.table.rows[0].values[1]); // original kept
    try std.testing.expectEqualStrings("231", res.table.rows[1].values[1]); // modified MCC
    try std.testing.expectEqualStrings("1", res.table.rows[1].values[2]); // added MNC
    try std.testing.expectEqual(@as(i64, 7), res.table.rows[1].original_excel_row);
}
