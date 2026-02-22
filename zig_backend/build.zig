const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const exe = b.addExecutable(.{
        .name = "100mscript_engine",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    b.installArtifact(exe);
    b.installDirectory(.{
        .source_dir = b.path("vendor/proj/lib"),
        .install_dir = .prefix,
        .install_subdir = "proj/lib",
    });
    b.installDirectory(.{
        .source_dir = b.path("vendor/proj/share/proj"),
        .install_dir = .prefix,
        .install_subdir = "proj/share/proj",
    });

    const run_cmd = b.addRunArtifact(exe);
    if (b.args) |args| {
        run_cmd.addArgs(args);
    }

    const run_step = b.step("run", "Run the 100mscript Zig backend engine");
    run_step.dependOn(&run_cmd.step);

    const tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const test_run = b.addRunArtifact(tests);
    const test_step = b.step("test", "Run Zig unit tests");
    test_step.dependOn(&test_run.step);
}
