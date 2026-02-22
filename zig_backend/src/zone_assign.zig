const std = @import("std");

pub const GridZones = struct {
    zona_x: []f64,
    zona_y: []f64,

    pub fn deinit(self: *GridZones, allocator: std.mem.Allocator) void {
        allocator.free(self.zona_x);
        allocator.free(self.zona_y);
        self.* = undefined;
    }
};

pub const SegmentZones = struct {
    segment_ids: []usize,
    start_x: []f64,
    start_y: []f64,
    unique_segments: usize,

    pub fn deinit(self: *SegmentZones, allocator: std.mem.Allocator) void {
        allocator.free(self.segment_ids);
        allocator.free(self.start_x);
        allocator.free(self.start_y);
        self.* = undefined;
    }
};

fn zoneFloor(value: f64, zone_size: f64) f64 {
    return @floor(value / zone_size) * zone_size;
}

pub fn assignGrid(
    allocator: std.mem.Allocator,
    xs: []const f64,
    ys: []const f64,
    zone_size_m: f64,
) !GridZones {
    if (xs.len != ys.len) return error.LengthMismatch;
    const zx = try allocator.alloc(f64, xs.len);
    const zy = try allocator.alloc(f64, ys.len);
    for (xs, ys, 0..) |x, y, i| {
        zx[i] = zoneFloor(x, zone_size_m);
        zy[i] = zoneFloor(y, zone_size_m);
    }
    return .{ .zona_x = zx, .zona_y = zy };
}

pub fn assignSegments(
    allocator: std.mem.Allocator,
    xs: []const f64,
    ys: []const f64,
    zone_size_m: f64,
) !SegmentZones {
    if (xs.len != ys.len) return error.LengthMismatch;
    const n = xs.len;
    const segment_ids = try allocator.alloc(usize, n);
    const start_x = try allocator.alloc(f64, n);
    const start_y = try allocator.alloc(f64, n);
    if (n == 0) {
        return .{
            .segment_ids = segment_ids,
            .start_x = start_x,
            .start_y = start_y,
            .unique_segments = 0,
        };
    }

    var segment_meta = std.AutoHashMap(usize, struct { x: f64, y: f64 }).init(allocator);
    defer segment_meta.deinit();

    const epsilon = 1e-9;
    var cumulative_distance: f64 = 0;
    var prev_x = xs[0];
    var prev_y = ys[0];
    try segment_meta.put(0, .{ .x = prev_x, .y = prev_y });
    segment_ids[0] = 0;
    start_x[0] = prev_x;
    start_y[0] = prev_y;

    for (1..n) |i| {
        const x = xs[i];
        const y = ys[i];
        const dx = x - prev_x;
        const dy = y - prev_y;
        const step_distance = @sqrt(dx * dx + dy * dy);

        if (step_distance > 0) {
            const prev_cumulative = cumulative_distance;
            cumulative_distance += step_distance;
            const prev_segment = @as(usize, @intFromFloat(@floor((prev_cumulative + epsilon) / zone_size_m)));
            const new_segment = @as(usize, @intFromFloat(@floor((cumulative_distance + epsilon) / zone_size_m)));

            if (new_segment > prev_segment) {
                var segment_id = prev_segment + 1;
                while (segment_id <= new_segment) : (segment_id += 1) {
                    const boundary_distance = @as(f64, @floatFromInt(segment_id)) * zone_size_m;
                    const offset = boundary_distance - prev_cumulative;
                    var fraction = offset / step_distance;
                    if (fraction < 0) fraction = 0;
                    if (fraction > 1) fraction = 1;
                    const sx = prev_x + (x - prev_x) * fraction;
                    const sy = prev_y + (y - prev_y) * fraction;
                    try segment_meta.put(segment_id, .{ .x = sx, .y = sy });
                }
            }
        }

        const current_segment = @as(usize, @intFromFloat(@floor((cumulative_distance + epsilon) / zone_size_m)));
        segment_ids[i] = current_segment;
        prev_x = x;
        prev_y = y;
    }

    for (0..n) |i| {
        const meta = segment_meta.get(segment_ids[i]) orelse segment_meta.get(0).?;
        start_x[i] = meta.x;
        start_y[i] = meta.y;
    }

    var max_segment: usize = 0;
    for (segment_ids) |id| {
        if (id > max_segment) max_segment = id;
    }

    return .{
        .segment_ids = segment_ids,
        .start_x = start_x,
        .start_y = start_y,
        .unique_segments = max_segment + 1,
    };
}

test "assignGrid floors to zone size" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    const xs = [_]f64{ 10.0, 199.9, -1.0 };
    const ys = [_]f64{ 20.0, 201.0, -101.0 };
    var g = try assignGrid(a, &xs, &ys, 100);
    defer g.deinit(a);

    try std.testing.expectEqual(@as(f64, 0), g.zona_x[0]);
    try std.testing.expectEqual(@as(f64, 100), g.zona_x[1]);
    try std.testing.expectEqual(@as(f64, -100), g.zona_x[2]);
    try std.testing.expectEqual(@as(f64, 0), g.zona_y[0]);
    try std.testing.expectEqual(@as(f64, 200), g.zona_y[1]);
    try std.testing.expectEqual(@as(f64, -200), g.zona_y[2]);
}

test "assignSegments creates boundaries by cumulative distance" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();
    const xs = [_]f64{ 0, 60, 120, 250 };
    const ys = [_]f64{ 0, 0, 0, 0 };
    var s = try assignSegments(a, &xs, &ys, 100);
    defer s.deinit(a);

    try std.testing.expectEqual(@as(usize, 4), s.segment_ids.len);
    try std.testing.expectEqual(@as(usize, 0), s.segment_ids[0]);
    try std.testing.expectEqual(@as(usize, 0), s.segment_ids[1]);
    try std.testing.expectEqual(@as(usize, 1), s.segment_ids[2]);
    try std.testing.expectEqual(@as(usize, 2), s.segment_ids[3]);
    try std.testing.expectEqual(@as(f64, 0), s.start_x[0]);
    try std.testing.expectApproxEqAbs(@as(f64, 100), s.start_x[2], 1e-6);
    try std.testing.expectApproxEqAbs(@as(f64, 200), s.start_x[3], 1e-6);
}
