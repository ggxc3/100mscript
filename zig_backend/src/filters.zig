const std = @import("std");

pub const Numeric = union(enum) {
    int: i64,
    float: f64,

    pub fn eql(a: Numeric, b: Numeric) bool {
        return switch (a) {
            .int => |ai| switch (b) {
                .int => |bi| ai == bi,
                .float => false,
            },
            .float => |af| switch (b) {
                .int => false,
                .float => |bf| af == bf,
            },
        };
    }
};

pub const Assignment = struct {
    field: []const u8,
    value: Numeric,
};

pub const ConditionValue = union(enum) {
    eq: Numeric,
    range: struct {
        low: Numeric,
        high: Numeric,
    },
};

pub const Condition = struct {
    field: []const u8,
    value: ConditionValue,
};

pub const ConditionGroup = struct {
    conditions: []Condition,
};

pub const FilterRule = struct {
    name: []const u8,
    assignments: []Assignment,
    condition_groups: []ConditionGroup,
};

pub const LoadedRules = struct {
    rules: []FilterRule,

    pub fn deinit(self: *LoadedRules, allocator: std.mem.Allocator) void {
        for (self.rules) |rule| {
            allocator.free(rule.name);
            for (rule.assignments) |a| {
                allocator.free(a.field);
            }
            allocator.free(rule.assignments);
            for (rule.condition_groups) |g| {
                for (g.conditions) |c| {
                    allocator.free(c.field);
                }
                allocator.free(g.conditions);
            }
            allocator.free(rule.condition_groups);
        }
        allocator.free(self.rules);
        self.* = undefined;
    }
};

fn isNumChar(ch: u8) bool {
    return switch (ch) {
        '-', '0'...'9', '.', ',', ' ', '\t', '\r', '\n' => true,
        else => false,
    };
}

fn trimWs(s: []const u8) []const u8 {
    return std.mem.trim(u8, s, " \t\r\n");
}

pub fn extractQueryContent(text: []const u8) []const u8 {
    const start_tag = "<Query>";
    const end_tag = "</Query>";
    const start_idx = std.mem.indexOf(u8, text, start_tag) orelse return text;
    const rest = text[start_idx + start_tag.len ..];
    const end_idx_rel = std.mem.indexOf(u8, rest, end_tag) orelse return text;
    return rest[0..end_idx_rel];
}

pub fn splitAssignmentAndConditions(query_text: []const u8) struct { assignment: []const u8, conditions: []const u8 } {
    if (std.mem.indexOfScalar(u8, query_text, ';')) |idx| {
        return .{ .assignment = query_text[0..idx], .conditions = query_text[idx + 1 ..] };
    }
    return .{ .assignment = query_text, .conditions = "" };
}

fn parseNumber(allocator: std.mem.Allocator, raw: []const u8) !Numeric {
    const cleaned = trimWs(raw);
    if (cleaned.len == 0) return error.EmptyNumber;

    var needs_copy = false;
    for (cleaned) |ch| {
        if (ch == ',') {
            needs_copy = true;
            break;
        }
    }

    const normalized = if (!needs_copy) cleaned else blk: {
        var buf = std.ArrayList(u8).empty;
        errdefer buf.deinit(allocator);
        for (cleaned) |ch| {
            try buf.append(allocator, if (ch == ',') '.' else ch);
        }
        break :blk try buf.toOwnedSlice(allocator);
    };
    defer if (needs_copy) allocator.free(normalized);

    const f = std.fmt.parseFloat(f64, normalized) catch return error.InvalidNumber;
    if (@abs(f - @round(f)) < 1e-12 and f <= @as(f64, @floatFromInt(std.math.maxInt(i64))) and f >= @as(f64, @floatFromInt(std.math.minInt(i64)))) {
        return Numeric{ .int = @intFromFloat(@round(f)) };
    }
    return Numeric{ .float = f };
}

fn parseConditionValue(allocator: std.mem.Allocator, raw: []const u8) !ConditionValue {
    const text = trimWs(raw);
    if (text.len == 0) return error.EmptyConditionValue;

    var hyphen_idx: ?usize = null;
    var i: usize = 1; // skip leading sign for first number
    while (i < text.len) : (i += 1) {
        if (text[i] == '-') {
            hyphen_idx = i;
            break;
        }
    }

    if (hyphen_idx) |idx| {
        const left = trimWs(text[0..idx]);
        const right = trimWs(text[idx + 1 ..]);
        if (left.len > 0 and right.len > 0) {
            const n1 = try parseNumber(allocator, left);
            const n2 = try parseNumber(allocator, right);
            const less_or_equal = compareNumeric(n1, n2) <= 0;
            return .{ .range = .{
                .low = if (less_or_equal) n1 else n2,
                .high = if (less_or_equal) n2 else n1,
            } };
        }
    }

    return .{ .eq = try parseNumber(allocator, text) };
}

fn compareNumeric(a: Numeric, b: Numeric) i2 {
    const af: f64 = switch (a) {
        .int => |v| @floatFromInt(v),
        .float => |v| v,
    };
    const bf: f64 = switch (b) {
        .int => |v| @floatFromInt(v),
        .float => |v| v,
    };
    if (af < bf) return -1;
    if (af > bf) return 1;
    return 0;
}

pub fn parseNumericForRow(allocator: std.mem.Allocator, raw: []const u8) !Numeric {
    return parseNumber(allocator, raw);
}

pub fn compareNumericForEval(a: Numeric, b: Numeric) i2 {
    return compareNumeric(a, b);
}

fn parseQuotedPairs(
    allocator: std.mem.Allocator,
    text: []const u8,
    out_fields: *std.ArrayList([]const u8),
    out_values: *std.ArrayList([]const u8),
) !void {
    var i: usize = 0;
    while (i < text.len) {
        const q1_rel = std.mem.indexOfScalarPos(u8, text, i, '"') orelse break;
        const q2_rel = std.mem.indexOfScalarPos(u8, text, q1_rel + 1, '"') orelse break;
        const field_raw = trimWs(text[q1_rel + 1 .. q2_rel]);

        var j = q2_rel + 1;
        while (j < text.len and text[j] != '=') : (j += 1) {}
        if (j >= text.len) break;
        j += 1;
        while (j < text.len and (text[j] == ' ' or text[j] == '\t')) : (j += 1) {}

        const value_start = j;
        while (j < text.len and isNumChar(text[j])) : (j += 1) {}
        const raw_value = trimWs(text[value_start..j]);
        if (field_raw.len == 0 or raw_value.len == 0) {
            i = q2_rel + 1;
            continue;
        }

        try out_fields.append(allocator, try allocator.dupe(u8, field_raw));
        try out_values.append(allocator, try allocator.dupe(u8, raw_value));
        i = j;
    }
}

pub fn parseAssignments(allocator: std.mem.Allocator, text: []const u8) ![]Assignment {
    var fields = std.ArrayList([]const u8).empty;
    defer fields.deinit(allocator);
    var values = std.ArrayList([]const u8).empty;
    defer values.deinit(allocator);
    try parseQuotedPairs(allocator, text, &fields, &values);

    var out = std.ArrayList(Assignment).empty;
    errdefer out.deinit(allocator);

    for (fields.items, values.items) |field, raw_value| {
        const parsed = try parseConditionValue(allocator, raw_value);
        switch (parsed) {
            .eq => |num| {
                var duplicate = false;
                for (out.items) |existing| {
                    if (std.mem.eql(u8, existing.field, field) and existing.value.eql(num)) {
                        duplicate = true;
                        break;
                    }
                }
                if (!duplicate) {
                    try out.append(allocator, .{ .field = field, .value = num });
                }
            },
            .range => return error.AssignmentRangeNotAllowed,
        }
    }
    return try out.toOwnedSlice(allocator);
}

fn parseConditionGroup(allocator: std.mem.Allocator, text: []const u8) ![]Condition {
    var fields = std.ArrayList([]const u8).empty;
    defer fields.deinit(allocator);
    var values = std.ArrayList([]const u8).empty;
    defer values.deinit(allocator);
    try parseQuotedPairs(allocator, text, &fields, &values);

    var out = std.ArrayList(Condition).empty;
    errdefer out.deinit(allocator);

    for (fields.items, values.items) |field, raw_value| {
        try out.append(allocator, .{
            .field = field,
            .value = try parseConditionValue(allocator, raw_value),
        });
    }
    return try out.toOwnedSlice(allocator);
}

pub fn parseConditionGroups(allocator: std.mem.Allocator, text: []const u8) ![]ConditionGroup {
    var groups = std.ArrayList(ConditionGroup).empty;
    errdefer groups.deinit(allocator);

    var i: usize = 0;
    while (i < text.len) {
        const start = std.mem.indexOfScalarPos(u8, text, i, '(') orelse break;
        const end = std.mem.indexOfScalarPos(u8, text, start + 1, ')') orelse break;
        const content = text[start + 1 .. end];
        const conditions = try parseConditionGroup(allocator, content);
        if (conditions.len > 0) {
            try groups.append(allocator, .{ .conditions = conditions });
        }
        i = end + 1;
    }

    if (groups.items.len == 0) {
        const conditions = try parseConditionGroup(allocator, text);
        if (conditions.len > 0) {
            try groups.append(allocator, .{ .conditions = conditions });
        }
    }

    return try groups.toOwnedSlice(allocator);
}

pub fn loadFilterRuleFromFile(allocator: std.mem.Allocator, path: []const u8) !FilterRule {
    const file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    const bytes = try file.readToEndAlloc(allocator, 4 * 1024 * 1024);

    const query = extractQueryContent(bytes);
    const split = splitAssignmentAndConditions(query);
    const assignments = try parseAssignments(allocator, split.assignment);
    const groups = try parseConditionGroups(allocator, split.conditions);
    if (assignments.len == 0 or groups.len == 0) return error.InvalidFilterRule;

    return .{
        .name = try allocator.dupe(u8, std.fs.path.basename(path)),
        .assignments = assignments,
        .condition_groups = groups,
    };
}

pub fn loadFilterRulesFromPaths(allocator: std.mem.Allocator, paths: []const []const u8) !LoadedRules {
    var rules = std.ArrayList(FilterRule).empty;
    errdefer rules.deinit(allocator);
    for (paths) |path| {
        const rule = try loadFilterRuleFromFile(allocator, path);
        try rules.append(allocator, rule);
    }
    return .{ .rules = try rules.toOwnedSlice(allocator) };
}

test "extract query and parse assignments/groups from real-like text" {
    const a = std.testing.allocator;
    const text =
        \\<Query>("MCC" = 231 AND "MNC" = 1);
        \\ OR ;
        \\ (;
        \\ ("Frequency" = 778000000-788000000);
        \\  OR ;
        \\  ("Frequency" = 801000000-811000000);
        \\ );
        \\</Query>
    ;

    var arena = std.heap.ArenaAllocator.init(a);
    defer arena.deinit();

    const q = extractQueryContent(text);
    const split = splitAssignmentAndConditions(q);
    const assignments = try parseAssignments(arena.allocator(), split.assignment);
    const groups = try parseConditionGroups(arena.allocator(), split.conditions);

    try std.testing.expectEqual(@as(usize, 2), assignments.len);
    try std.testing.expectEqualStrings("MCC", assignments[0].field);
    try std.testing.expectEqualStrings("MNC", assignments[1].field);
    try std.testing.expectEqual(@as(usize, 2), groups.len);
    try std.testing.expectEqualStrings("Frequency", groups[0].conditions[0].field);
}

test "assignment parser rejects ranges" {
    const a = std.testing.allocator;
    var arena = std.heap.ArenaAllocator.init(a);
    defer arena.deinit();
    try std.testing.expectError(
        error.AssignmentRangeNotAllowed,
        parseAssignments(arena.allocator(), "\"Frequency\" = 1-2"),
    );
}
