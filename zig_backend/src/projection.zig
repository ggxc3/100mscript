const std = @import("std");

pub const ForwardBatch = struct {
    xs: []f64,
    ys: []f64,
};

pub const InverseBatch = struct {
    lats: []f64,
    lons: []f64,
};

const Mode = enum { forward, inverse };

const PJ_CONTEXT = opaque {};
const PJ = opaque {};
const PJ_AREA = opaque {};
const PJ_DIRECTION = c_int;
const PJ_FWD: PJ_DIRECTION = 1;

const FnProjContextCreate = *const fn () callconv(.c) ?*PJ_CONTEXT;
const FnProjContextDestroy = *const fn (?*PJ_CONTEXT) callconv(.c) void;
const FnProjContextSetSearchPaths = *const fn (?*PJ_CONTEXT, c_int, [*c]const [*c]const u8) callconv(.c) void;
const FnProjCreateCrsToCrs = *const fn (?*PJ_CONTEXT, [*c]const u8, [*c]const u8, ?*PJ_AREA) callconv(.c) ?*PJ;
const FnProjNormalizeForVisualization = *const fn (?*PJ_CONTEXT, ?*const PJ) callconv(.c) ?*PJ;
const FnProjDestroy = *const fn (?*PJ) callconv(.c) ?*PJ;
const FnProjTransGeneric = *const fn (
    ?*PJ,
    PJ_DIRECTION,
    ?*f64,
    usize,
    usize,
    ?*f64,
    usize,
    usize,
    ?*f64,
    usize,
    usize,
    ?*f64,
    usize,
    usize,
) callconv(.c) usize;

const ProjApi = struct {
    lib: std.DynLib,
    proj_context_create: FnProjContextCreate,
    proj_context_destroy: FnProjContextDestroy,
    proj_context_set_search_paths: FnProjContextSetSearchPaths,
    proj_create_crs_to_crs: FnProjCreateCrsToCrs,
    proj_normalize_for_visualization: FnProjNormalizeForVisualization,
    proj_destroy: FnProjDestroy,
    proj_trans_generic: FnProjTransGeneric,

    fn close(self: *ProjApi) void {
        self.lib.close();
    }
};

fn debugEnabled(allocator: std.mem.Allocator) bool {
    const raw = std.process.getEnvVarOwned(allocator, "ZIG_PROJ_DEBUG") catch return false;
    defer allocator.free(raw);
    return std.mem.eql(u8, std.mem.trim(u8, raw, " \t\r\n"), "1");
}

fn fileExists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

fn dirExists(path: []const u8) bool {
    var dir = std.fs.cwd().openDir(path, .{}) catch return false;
    dir.close();
    return true;
}

fn getEnvOwnedNonEmpty(allocator: std.mem.Allocator, name: []const u8) ?[]u8 {
    const raw = std.process.getEnvVarOwned(allocator, name) catch return null;
    const trimmed = std.mem.trim(u8, raw, " \t\r\n");
    if (trimmed.len == 0) {
        allocator.free(raw);
        return null;
    }
    if (trimmed.ptr == raw.ptr and trimmed.len == raw.len) return raw;
    const out = allocator.dupe(u8, trimmed) catch {
        allocator.free(raw);
        return null;
    };
    allocator.free(raw);
    return out;
}

fn findLibprojInPyprojVenv(allocator: std.mem.Allocator, base_lib_dir: []const u8) !?[]u8 {
    var py_lib_dir = std.fs.cwd().openDir(base_lib_dir, .{ .iterate = true }) catch return null;
    defer py_lib_dir.close();
    var it = py_lib_dir.iterate();
    while (try it.next()) |entry| {
        if (entry.kind != .directory) continue;
        if (!std.mem.startsWith(u8, entry.name, "python")) continue;
        const dylib_dir = try std.fmt.allocPrint(
            allocator,
            "{s}/{s}/site-packages/pyproj/.dylibs",
            .{ base_lib_dir, entry.name },
        );
        defer allocator.free(dylib_dir);
        var dd = std.fs.cwd().openDir(dylib_dir, .{ .iterate = true }) catch continue;
        defer dd.close();
        var dit = dd.iterate();
        while (try dit.next()) |de| {
            if (de.kind != .file) continue;
            const is_proj = std.mem.startsWith(u8, de.name, "libproj");
            const ext_ok =
                std.mem.endsWith(u8, de.name, ".dylib") or
                std.mem.endsWith(u8, de.name, ".so") or
                std.mem.endsWith(u8, de.name, ".dll");
            if (!is_proj or !ext_ok) continue;
            return try std.fmt.allocPrint(allocator, "{s}/{s}", .{ dylib_dir, de.name });
        }
    }
    return null;
}

fn findLibprojInDir(allocator: std.mem.Allocator, dir_path: []const u8) !?[]u8 {
    var d = std.fs.cwd().openDir(dir_path, .{ .iterate = true }) catch return null;
    defer d.close();
    var it = d.iterate();
    while (try it.next()) |entry| {
        if (entry.kind != .file) continue;
        if (!std.mem.startsWith(u8, entry.name, "libproj")) continue;
        if (!(std.mem.endsWith(u8, entry.name, ".dylib") or std.mem.endsWith(u8, entry.name, ".so") or std.mem.endsWith(u8, entry.name, ".dll"))) continue;
        return try std.fmt.allocPrint(allocator, "{s}/{s}", .{ dir_path, entry.name });
    }
    return null;
}

fn findProjDataInPyprojVenv(allocator: std.mem.Allocator, base_lib_dir: []const u8) !?[]u8 {
    var py_lib_dir = std.fs.cwd().openDir(base_lib_dir, .{ .iterate = true }) catch return null;
    defer py_lib_dir.close();
    var it = py_lib_dir.iterate();
    while (try it.next()) |entry| {
        if (entry.kind != .directory) continue;
        if (!std.mem.startsWith(u8, entry.name, "python")) continue;
        const data_dir = try std.fmt.allocPrint(
            allocator,
            "{s}/{s}/site-packages/pyproj/proj_dir/share/proj",
            .{ base_lib_dir, entry.name },
        );
        if (dirExists(data_dir)) return data_dir;
        allocator.free(data_dir);
    }
    return null;
}

fn detectLibprojPath(allocator: std.mem.Allocator) !?[]u8 {
    if (getEnvOwnedNonEmpty(allocator, "ZIG_PROJ_DYLIB")) |p| return p;

    const bundled_dirs = [_][]const u8{
        "vendor/proj/lib",
        "zig_backend/vendor/proj/lib",
        "zig-out/proj/lib",
        "zig_backend/zig-out/proj/lib",
    };
    for (bundled_dirs) |dirp| {
        if (try findLibprojInDir(allocator, dirp)) |p| return p;
    }

    const venv_lib_roots = [_][]const u8{ ".venv/lib", "../.venv/lib" };
    for (venv_lib_roots) |root| {
        if (try findLibprojInPyprojVenv(allocator, root)) |p| return p;
    }

    const abs_candidates = [_][]const u8{
        "/opt/homebrew/lib/libproj.dylib",
        "/usr/local/lib/libproj.dylib",
        "/usr/lib/libproj.dylib",
    };
    for (abs_candidates) |c| {
        if (fileExists(c)) return try allocator.dupe(u8, c);
    }

    // Last resort: rely on dynamic loader search path.
    const loader_candidates = [_][]const u8{
        "libproj.dylib",
        "libproj.so",
        "libproj.so.25",
        "proj.dll",
    };
    for (loader_candidates) |c| {
        return try allocator.dupe(u8, c);
    }
    return null;
}

fn detectProjDataDir(allocator: std.mem.Allocator) !?[]u8 {
    if (getEnvOwnedNonEmpty(allocator, "ZIG_PROJ_DATA_DIR")) |p| return p;
    if (getEnvOwnedNonEmpty(allocator, "PROJ_LIB")) |p| return p;

    const bundled_dirs = [_][]const u8{
        "vendor/proj/share/proj",
        "zig_backend/vendor/proj/share/proj",
        "zig-out/proj/share/proj",
        "zig_backend/zig-out/proj/share/proj",
    };
    for (bundled_dirs) |c| {
        if (dirExists(c)) return try allocator.dupe(u8, c);
    }

    const candidates = [_][]const u8{
        "/opt/homebrew/share/proj",
        "/usr/local/share/proj",
        "/usr/share/proj",
    };
    for (candidates) |c| {
        if (dirExists(c)) return try allocator.dupe(u8, c);
    }

    const venv_lib_roots = [_][]const u8{ ".venv/lib", "../.venv/lib" };
    for (venv_lib_roots) |root| {
        if (try findProjDataInPyprojVenv(allocator, root)) |p| return p;
    }
    return null;
}

fn loadProjApi(allocator: std.mem.Allocator) !?ProjApi {
    const lib_path = try detectLibprojPath(allocator);
    if (lib_path == null) return null;
    defer allocator.free(lib_path.?);

    var lib = std.DynLib.open(lib_path.?) catch |err| {
        if (debugEnabled(allocator)) std.debug.print("libproj open failed ({s}): {s}\n", .{ lib_path.?, @errorName(err) });
        return null;
    };
    errdefer lib.close();

    const api = ProjApi{
        .lib = lib,
        .proj_context_create = lib.lookup(FnProjContextCreate, "proj_context_create") orelse return null,
        .proj_context_destroy = lib.lookup(FnProjContextDestroy, "proj_context_destroy") orelse return null,
        .proj_context_set_search_paths = lib.lookup(FnProjContextSetSearchPaths, "proj_context_set_search_paths") orelse return null,
        .proj_create_crs_to_crs = lib.lookup(FnProjCreateCrsToCrs, "proj_create_crs_to_crs") orelse return null,
        .proj_normalize_for_visualization = lib.lookup(FnProjNormalizeForVisualization, "proj_normalize_for_visualization") orelse return null,
        .proj_destroy = lib.lookup(FnProjDestroy, "proj_destroy") orelse return null,
        .proj_trans_generic = lib.lookup(FnProjTransGeneric, "proj_trans_generic") orelse return null,
    };
    return api;
}

fn createTransformer(
    allocator: std.mem.Allocator,
    api: *ProjApi,
    ctx: ?*PJ_CONTEXT,
    mode: Mode,
) !?*PJ {
    const src = switch (mode) {
        .forward => "EPSG:4326",
        .inverse => "EPSG:5514",
    };
    const dst = switch (mode) {
        .forward => "EPSG:5514",
        .inverse => "EPSG:4326",
    };
    const src_z = try allocator.dupeZ(u8, src);
    defer allocator.free(src_z);
    const dst_z = try allocator.dupeZ(u8, dst);
    defer allocator.free(dst_z);

    const raw = api.proj_create_crs_to_crs(ctx, src_z.ptr, dst_z.ptr, null) orelse return null;
    const norm = api.proj_normalize_for_visualization(ctx, raw);
    _ = api.proj_destroy(raw);
    return norm;
}

fn runLibprojTransform(
    allocator: std.mem.Allocator,
    mode: Mode,
    first: []const f64,
    second: []const f64,
) !?struct { a: []f64, b: []f64 } {
    if (first.len != second.len) return error.InvalidInput;
    if (first.len == 0) {
        return .{
            .a = try allocator.alloc(f64, 0),
            .b = try allocator.alloc(f64, 0),
        };
    }

    var api = try loadProjApi(allocator) orelse return null;
    defer api.close();

    const ctx = api.proj_context_create() orelse return null;
    defer api.proj_context_destroy(ctx);

    const data_dir = try detectProjDataDir(allocator);
    defer if (data_dir) |p| allocator.free(p);
    if (data_dir) |dir_path| {
        const dir_z = try allocator.dupeZ(u8, dir_path);
        defer allocator.free(dir_z);
        const paths = [_][*:0]const u8{dir_z.ptr};
        api.proj_context_set_search_paths(ctx, 1, @ptrCast(paths[0..].ptr));
    }

    const transformer = try createTransformer(allocator, &api, ctx, mode) orelse return null;
    defer _ = api.proj_destroy(transformer);

    const out_x = try allocator.alloc(f64, first.len);
    errdefer allocator.free(out_x);
    const out_y = try allocator.alloc(f64, first.len);
    errdefer allocator.free(out_y);
    @memcpy(out_x, first);
    @memcpy(out_y, second);

    const n_done = api.proj_trans_generic(
        transformer,
        PJ_FWD,
        &out_x[0],
        @sizeOf(f64),
        out_x.len,
        &out_y[0],
        @sizeOf(f64),
        out_y.len,
        null,
        0,
        0,
        null,
        0,
        0,
    );
    if (n_done != out_x.len) {
        if (debugEnabled(allocator)) {
            std.debug.print("libproj transform incomplete: done={d} expected={d}\n", .{ n_done, out_x.len });
        }
        allocator.free(out_x);
        allocator.free(out_y);
        return null;
    }

    return .{ .a = out_x, .b = out_y };
}

pub fn forwardBatchPyproj(
    allocator: std.mem.Allocator,
    lats: []const f64,
    lons: []const f64,
) !?ForwardBatch {
    // API name kept for compatibility with existing call sites/tests; implementation
    // is now native libproj (no Python subprocess).
    const out = try runLibprojTransform(allocator, .forward, lons, lats) orelse return null;
    return .{ .xs = out.a, .ys = out.b };
}

pub fn inverseBatchPyproj(
    allocator: std.mem.Allocator,
    xs: []const f64,
    ys: []const f64,
) !?InverseBatch {
    const out = try runLibprojTransform(allocator, .inverse, xs, ys) orelse return null;
    // normalize_for_visualization returns lon/lat in x/y order
    return .{ .lats = out.b, .lons = out.a };
}

test "libproj bridge forward batch works when proj assets are available" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const a = arena.allocator();

    const lats = [_]f64{48.1};
    const lons = [_]f64{17.1};
    const res_opt = try forwardBatchPyproj(a, &lats, &lons);
    try std.testing.expect(res_opt != null);
    const res = res_opt.?;
    try std.testing.expectApproxEqAbs(@as(f64, -574802.45), res.xs[0], 2.0);
    try std.testing.expectApproxEqAbs(@as(f64, -1285640.80), res.ys[0], 2.0);
}
