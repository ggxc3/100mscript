const std = @import("std");
const engine = @import("engine.zig");
const inspect_csv = @import("inspect_csv.zig");
const protocol = @import("protocol.zig");
test {
    _ = @import("aggregation_preview.zig");
    _ = @import("mobile_sync.zig");
    _ = @import("zone_assign.zig");
    _ = @import("processing_preview.zig");
    _ = @import("stats_writer.zig");
    _ = @import("projection.zig");
    _ = @import("zone_stats_core.zig");
    _ = @import("zones_writer.zig");
}

fn usage(stderr: anytype) !void {
    try stderr.writeAll(
        "Usage:\n" ++
        "  100mscript_engine run --config <path>\n" ++
        "  100mscript_engine inspect --csv <path>\n" ++
        "  100mscript_engine worker\n",
    );
}

fn parseRunArgs(args: []const []const u8) ?[]const u8 {
    var i: usize = 0;
    while (i < args.len) : (i += 1) {
        if (std.mem.eql(u8, args[i], "--config")) {
            if (i + 1 < args.len) return args[i + 1];
            return null;
        }
    }
    return null;
}

fn parseInspectArgs(args: []const []const u8) ?[]const u8 {
    var i: usize = 0;
    while (i < args.len) : (i += 1) {
        if (std.mem.eql(u8, args[i], "--csv")) {
            if (i + 1 < args.len) return args[i + 1];
            return null;
        }
    }
    return null;
}

fn printJsonLine(writer: *std.Io.Writer, value: anytype) !void {
    try std.json.Stringify.value(value, .{}, writer);
    try writer.writeByte('\n');
    try writer.flush();
}

const WorkerCommand = struct {
    type: []const u8,
    config_path: ?[]const u8 = null,
    csv_path: ?[]const u8 = null,
};

const WorkerReadyEvent = struct {
    type: []const u8 = "worker_ready",
    message: []const u8 = "worker ready",
};

const WorkerCommandDoneEvent = struct {
    type: []const u8 = "worker_command_done",
    command: []const u8,
    ok: bool,
};

fn runWorker(gpa: std.mem.Allocator, stdout_io: *std.Io.Writer) !void {
    const emitter = protocol.Emitter{ .writer = stdout_io };
    try printJsonLine(stdout_io, WorkerReadyEvent{});

    var stdin_buffer: [4096]u8 = undefined;
    var stdin = std.fs.File.stdin().reader(&stdin_buffer);
    while (true) {
        const line_raw = stdin.interface.takeDelimiterExclusive('\n') catch |err| switch (err) {
            error.EndOfStream => break,
            else => |e| {
                try emitter.err("WORKER_STDIN_READ_FAILED", @errorName(e));
                return e;
            },
        };

        const line = std.mem.trim(u8, line_raw, " \t\r\n");
        if (line.len == 0) continue;

        var parsed = std.json.parseFromSlice(WorkerCommand, gpa, line, .{}) catch |err| {
            try emitter.err("WORKER_INVALID_COMMAND", @errorName(err));
            try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "invalid", .ok = false });
            continue;
        };
        defer parsed.deinit();
        const cmd = parsed.value;

        if (std.mem.eql(u8, cmd.type, "shutdown")) {
            try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "shutdown", .ok = true });
            break;
        }

        if (std.mem.eql(u8, cmd.type, "run")) {
            const config_path = cmd.config_path orelse {
                try emitter.err("WORKER_RUN_MISSING_CONFIG", "worker command 'run' requires config_path");
                try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "run", .ok = false });
                continue;
            };
            const ok = blk: {
                engine.run(gpa, emitter, config_path) catch break :blk false;
                break :blk true;
            };
            try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "run", .ok = ok });
            continue;
        }

        if (std.mem.eql(u8, cmd.type, "inspect")) {
            const csv_path = cmd.csv_path orelse {
                try emitter.err("WORKER_INSPECT_MISSING_PATH", "worker command 'inspect' requires csv_path");
                try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "inspect", .ok = false });
                continue;
            };

            var ok = true;
            inspect_csv.runInspectCsv(gpa, stdout_io, csv_path) catch |err| {
                ok = false;
                const msg = try std.fmt.allocPrint(gpa, "Inspect zlyhal: {s}", .{@errorName(err)});
                defer gpa.free(msg);
                try emitter.err("WORKER_INSPECT_FAILED", msg);
            };
            try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "inspect", .ok = ok });
            continue;
        }

        try emitter.err("WORKER_UNKNOWN_COMMAND", "Unknown worker command type");
        try printJsonLine(stdout_io, WorkerCommandDoneEvent{ .command = "unknown", .ok = false });
    }
}

pub fn main() !void {
    var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa_state.deinit();
    const gpa = gpa_state.allocator();

    var stdout_buffer: [4096]u8 = undefined;
    var stderr_buffer: [4096]u8 = undefined;
    var stdout = std.fs.File.stdout().writer(&stdout_buffer);
    var stderr = std.fs.File.stderr().writer(&stderr_buffer);
    const stdout_io = &stdout.interface;
    const stderr_io = &stderr.interface;
    defer stdout_io.flush() catch {};
    defer stderr_io.flush() catch {};

    const argv = try std.process.argsAlloc(gpa);
    defer std.process.argsFree(gpa, argv);

    if (argv.len < 2) {
        try usage(stderr_io);
        return error.InvalidArguments;
    }

    if (std.mem.eql(u8, argv[1], "run")) {
        const config_path = parseRunArgs(argv[2..]) orelse {
            const emitter = protocol.Emitter{ .writer = stdout_io };
            try emitter.err("INVALID_ARGS", "Missing --config <path> argument.");
            return error.InvalidArguments;
        };
        const emitter = protocol.Emitter{ .writer = stdout_io };
        try emitter.status("Zig backend spusteny. Nacitam konfiguraciu...");
        return engine.run(gpa, emitter, config_path);
    }

    if (std.mem.eql(u8, argv[1], "inspect")) {
        const csv_path = parseInspectArgs(argv[2..]) orelse {
            try usage(stderr_io);
            return error.InvalidArguments;
        };
        return inspect_csv.runInspectCsv(gpa, stdout_io, csv_path);
    }

    if (std.mem.eql(u8, argv[1], "worker")) {
        return runWorker(gpa, stdout_io);
    }

    try usage(stderr_io);
    return error.InvalidArguments;
}
