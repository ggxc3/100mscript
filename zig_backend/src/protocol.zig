const std = @import("std");

pub const StatusEvent = struct {
    type: []const u8 = "status",
    message: []const u8,
};

pub const ErrorEvent = struct {
    type: []const u8 = "error",
    code: []const u8,
    message: []const u8,
};

pub const ResultEvent = struct {
    type: []const u8 = "result",
    zones_file: []const u8,
    stats_file: []const u8,
    include_empty_zones: bool,
    unique_zones: i64,
    unique_operators: i64,
    total_zone_rows: i64,
    min_x: ?f64 = null,
    max_x: ?f64 = null,
    min_y: ?f64 = null,
    max_y: ?f64 = null,
    range_x_m: ?f64 = null,
    range_y_m: ?f64 = null,
    theoretical_total_zones: ?f64 = null,
    coverage_percent: ?f64 = null,
};

pub const ProgressEvent = struct {
    type: []const u8 = "progress",
    phase: []const u8,
    current: i64,
    total: i64,
    percent: ?f64 = null,
    message: ?[]const u8 = null,
};

fn printJsonLine(writer: *std.Io.Writer, value: anytype) !void {
    try std.json.Stringify.value(value, .{}, writer);
    try writer.writeByte('\n');
    try writer.flush();
}

pub const Emitter = struct {
    writer: *std.Io.Writer,

    pub fn status(self: Emitter, message: []const u8) !void {
        try printJsonLine(self.writer, StatusEvent{ .message = message });
    }

    pub fn err(self: Emitter, code: []const u8, message: []const u8) !void {
        try printJsonLine(self.writer, ErrorEvent{
            .code = code,
            .message = message,
        });
    }

    pub fn result(self: Emitter, event: ResultEvent) !void {
        try printJsonLine(self.writer, event);
    }

    pub fn progress(
        self: Emitter,
        phase: []const u8,
        current: usize,
        total: usize,
        message: ?[]const u8,
    ) !void {
        const percent = if (total > 0)
            (@as(f64, @floatFromInt(current)) / @as(f64, @floatFromInt(total))) * 100.0
        else
            null;
        try printJsonLine(self.writer, ProgressEvent{
            .phase = phase,
            .current = @as(i64, @intCast(current)),
            .total = @as(i64, @intCast(total)),
            .percent = percent,
            .message = message,
        });
    }
};
