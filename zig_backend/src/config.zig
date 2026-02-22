const std = @import("std");

pub const ColumnMapping = struct {
    latitude: i32,
    longitude: i32,
    frequency: i32,
    pci: i32,
    mcc: i32,
    mnc: i32,
    rsrp: i32,
    sinr: ?i32 = null,
};

pub const CustomOperator = struct {
    mcc: []const u8,
    mnc: []const u8,
    pci: []const u8,
};

pub const Config = struct {
    file_path: []const u8,
    column_mapping: ColumnMapping,
    keep_original_rows: bool = false,
    zone_mode: []const u8 = "center",
    zone_size_m: f64 = 100,
    rsrp_threshold: f64 = -110,
    sinr_threshold: f64 = -5,
    include_empty_zones: bool = false,
    add_custom_operators: bool = false,
    custom_operators: []CustomOperator = &.{},
    filter_paths: ?[][]const u8 = null,
    output_suffix: ?[]const u8 = null,
    mobile_mode_enabled: bool = false,
    mobile_lte_file_path: ?[]const u8 = null,
    mobile_time_tolerance_ms: i64 = 1000,
    mobile_require_nr_yes: bool = true,
    mobile_nr_column_name: []const u8 = "5G NR",
    progress_enabled: bool = true,

    pub fn deinit(self: *Config, allocator: std.mem.Allocator) void {
        allocator.free(self.file_path);
        allocator.free(self.zone_mode);
        allocator.free(self.mobile_nr_column_name);
        if (self.output_suffix) |s| allocator.free(s);
        if (self.mobile_lte_file_path) |s| allocator.free(s);
        if (self.filter_paths) |paths| {
            for (paths) |p| allocator.free(p);
            allocator.free(paths);
        }
        for (self.custom_operators) |op| {
            allocator.free(op.mcc);
            allocator.free(op.mnc);
            allocator.free(op.pci);
        }
        if (self.custom_operators.len > 0) allocator.free(self.custom_operators);
        self.* = undefined;
    }
};

fn asObject(value: std.json.Value) !std.json.ObjectMap {
    return switch (value) {
        .object => |obj| obj,
        else => error.ExpectedObject,
    };
}

fn asArray(value: std.json.Value) !std.json.Array {
    return switch (value) {
        .array => |arr| arr,
        else => error.ExpectedArray,
    };
}

fn valueToInt(value: std.json.Value) !i64 {
    return switch (value) {
        .integer => |v| v,
        .float => |v| @intFromFloat(@round(v)),
        .number_string => |s| try std.fmt.parseInt(i64, s, 10),
        else => error.ExpectedNumber,
    };
}

fn valueToFloat(value: std.json.Value) !f64 {
    return switch (value) {
        .integer => |v| @floatFromInt(v),
        .float => |v| v,
        .number_string => |s| try std.fmt.parseFloat(f64, s),
        else => error.ExpectedNumber,
    };
}

fn dupString(allocator: std.mem.Allocator, value: std.json.Value) ![]const u8 {
    return switch (value) {
        .string => |s| try allocator.dupe(u8, s),
        else => error.ExpectedString,
    };
}

fn getRequired(obj: std.json.ObjectMap, key: []const u8) !std.json.Value {
    return obj.get(key) orelse error.MissingField;
}

fn getOptional(obj: std.json.ObjectMap, key: []const u8) ?std.json.Value {
    return obj.get(key);
}

fn parseStringArrayOpt(allocator: std.mem.Allocator, value_opt: ?std.json.Value) !?[][]const u8 {
    if (value_opt == null) return null;
    switch (value_opt.?) {
        .null => return null,
        else => {},
    }

    const arr = try asArray(value_opt.?);
    const out = try allocator.alloc([]const u8, arr.items.len);
    for (arr.items, 0..) |item, idx| {
        out[idx] = try dupString(allocator, item);
    }
    return out;
}

fn parseCustomOperators(allocator: std.mem.Allocator, value_opt: ?std.json.Value) ![]CustomOperator {
    if (value_opt == null) return &.{};
    switch (value_opt.?) {
        .null => return &.{},
        else => {},
    }

    const arr = try asArray(value_opt.?);
    const out = try allocator.alloc(CustomOperator, arr.items.len);
    for (arr.items, 0..) |item, idx| {
        const inner = try asArray(item);
        if (inner.items.len < 2 or inner.items.len > 3) return error.InvalidCustomOperator;
        out[idx] = .{
            .mcc = try dupString(allocator, inner.items[0]),
            .mnc = try dupString(allocator, inner.items[1]),
            .pci = if (inner.items.len >= 3) try dupString(allocator, inner.items[2]) else try allocator.dupe(u8, ""),
        };
    }
    return out;
}

fn parseColumnMapping(obj: std.json.ObjectMap) !ColumnMapping {
    const latitude = try valueToInt(try getRequired(obj, "latitude"));
    const longitude = try valueToInt(try getRequired(obj, "longitude"));
    const frequency = try valueToInt(try getRequired(obj, "frequency"));
    const pci = try valueToInt(try getRequired(obj, "pci"));
    const mcc = try valueToInt(try getRequired(obj, "mcc"));
    const mnc = try valueToInt(try getRequired(obj, "mnc"));
    const rsrp = try valueToInt(try getRequired(obj, "rsrp"));
    const sinr_value = getOptional(obj, "sinr");

    return .{
        .latitude = @intCast(latitude),
        .longitude = @intCast(longitude),
        .frequency = @intCast(frequency),
        .pci = @intCast(pci),
        .mcc = @intCast(mcc),
        .mnc = @intCast(mnc),
        .rsrp = @intCast(rsrp),
        .sinr = if (sinr_value) |v| @intCast(try valueToInt(v)) else null,
    };
}

pub fn parseFromJsonBytes(allocator: std.mem.Allocator, json_bytes: []const u8) !Config {
    var parsed = try std.json.parseFromSlice(std.json.Value, allocator, json_bytes, .{});
    defer parsed.deinit();

    const root = try asObject(parsed.value);

    const file_path = try dupString(allocator, try getRequired(root, "file_path"));
    const zone_mode = if (getOptional(root, "zone_mode")) |v| try dupString(allocator, v) else try allocator.dupe(u8, "center");
    const mobile_nr_column_name = if (getOptional(root, "mobile_nr_column_name")) |v| try dupString(allocator, v) else try allocator.dupe(u8, "5G NR");

    const output_suffix = blk: {
        const v = getOptional(root, "output_suffix") orelse break :blk null;
        if (v == .null) break :blk null;
        break :blk try dupString(allocator, v);
    };

    const mobile_lte_file_path = blk: {
        const v = getOptional(root, "mobile_lte_file_path") orelse break :blk null;
        if (v == .null) break :blk null;
        break :blk try dupString(allocator, v);
    };

    const filter_paths = try parseStringArrayOpt(allocator, getOptional(root, "filter_paths"));
    const custom_operators = try parseCustomOperators(allocator, getOptional(root, "custom_operators"));

    const mapping_obj = try asObject(try getRequired(root, "column_mapping"));
    const column_mapping = try parseColumnMapping(mapping_obj);

    return .{
        .file_path = file_path,
        .column_mapping = column_mapping,
        .keep_original_rows = if (getOptional(root, "keep_original_rows")) |v| switch (v) { .bool => |b| b, else => false } else false,
        .zone_mode = zone_mode,
        .zone_size_m = if (getOptional(root, "zone_size_m")) |v| try valueToFloat(v) else 100,
        .rsrp_threshold = if (getOptional(root, "rsrp_threshold")) |v| try valueToFloat(v) else -110,
        .sinr_threshold = if (getOptional(root, "sinr_threshold")) |v| try valueToFloat(v) else -5,
        .include_empty_zones = if (getOptional(root, "include_empty_zones")) |v| switch (v) { .bool => |b| b, else => false } else false,
        .add_custom_operators = if (getOptional(root, "add_custom_operators")) |v| switch (v) { .bool => |b| b, else => false } else false,
        .custom_operators = custom_operators,
        .filter_paths = filter_paths,
        .output_suffix = output_suffix,
        .mobile_mode_enabled = if (getOptional(root, "mobile_mode_enabled")) |v| switch (v) { .bool => |b| b, else => false } else false,
        .mobile_lte_file_path = mobile_lte_file_path,
        .mobile_time_tolerance_ms = if (getOptional(root, "mobile_time_tolerance_ms")) |v| try valueToInt(v) else 1000,
        .mobile_require_nr_yes = if (getOptional(root, "mobile_require_nr_yes")) |v| switch (v) { .bool => |b| b, else => true } else true,
        .mobile_nr_column_name = mobile_nr_column_name,
        .progress_enabled = if (getOptional(root, "progress_enabled")) |v| switch (v) { .bool => |b| b, else => true } else true,
    };
}

test "parse config json basics" {
    const a = std.testing.allocator;
    const json_text =
        \\{
        \\  "file_path":"input.csv",
        \\  "column_mapping":{"latitude":0,"longitude":1,"frequency":2,"pci":3,"mcc":4,"mnc":5,"rsrp":6,"sinr":7},
        \\  "zone_mode":"center",
        \\  "zone_size_m":100,
        \\  "mobile_mode_enabled":true,
        \\  "mobile_lte_file_path":"lte.csv",
        \\  "custom_operators":[["231","99",""]],
        \\  "filter_paths":[]
        \\}
    ;
    var arena = std.heap.ArenaAllocator.init(a);
    defer arena.deinit();
    const cfg = try parseFromJsonBytes(arena.allocator(), json_text);
    try std.testing.expectEqualStrings("input.csv", cfg.file_path);
    try std.testing.expectEqual(@as(i32, 2), cfg.column_mapping.frequency);
    try std.testing.expect(cfg.mobile_mode_enabled);
    try std.testing.expectEqualStrings("lte.csv", cfg.mobile_lte_file_path.?);
    try std.testing.expectEqual(@as(usize, 1), cfg.custom_operators.len);
}
