const std = @import("std");
const aggregation_preview = @import("aggregation_preview.zig");
const config_mod = @import("config.zig");
const csv_table = @import("csv_table.zig");
const filter_apply = @import("filter_apply.zig");
const filters = @import("filters.zig");
const measurement_preview = @import("measurement_preview.zig");
const mobile_sync = @import("mobile_sync.zig");
const processing_preview = @import("processing_preview.zig");
const protocol = @import("protocol.zig");
const stats_writer = @import("stats_writer.zig");
const zone_stats_core = @import("zone_stats_core.zig");
const zones_writer = @import("zones_writer.zig");

fn diagnosticsPreviewsEnabled(allocator: std.mem.Allocator) bool {
    const raw = std.process.getEnvVarOwned(allocator, "ZIG_DIAGNOSTIC_PREVIEWS") catch return false;
    defer allocator.free(raw);
    const trimmed = std.mem.trim(u8, raw, " \t\r\n");
    return std.mem.eql(u8, trimmed, "1") or std.ascii.eqlIgnoreCase(trimmed, "true");
}

fn normalizeOutputSuffix(allocator: std.mem.Allocator, suffix_opt: ?[]const u8) ![]const u8 {
    if (suffix_opt == null) return try allocator.dupe(u8, "");
    const trimmed = std.mem.trim(u8, suffix_opt.?, " \t\r\n");
    if (trimmed.len == 0) return try allocator.dupe(u8, "");
    if (trimmed[0] == '_') return try allocator.dupe(u8, trimmed);
    return try std.fmt.allocPrint(allocator, "_{s}", .{trimmed});
}

fn withMobileSuffixIfNeeded(allocator: std.mem.Allocator, base_suffix: []const u8, mobile_mode_enabled: bool) ![]const u8 {
    if (!mobile_mode_enabled) return try allocator.dupe(u8, base_suffix);
    if (base_suffix.len == 0) return try allocator.dupe(u8, "_mobile");
    if (std.mem.endsWith(u8, base_suffix, "_mobile")) return try allocator.dupe(u8, base_suffix);
    return try std.fmt.allocPrint(allocator, "{s}_mobile", .{base_suffix});
}

fn deriveOutputPath(allocator: std.mem.Allocator, input_path: []const u8, suffix: []const u8, kind: []const u8) ![]const u8 {
    if (std.mem.endsWith(u8, input_path, ".csv")) {
        return try std.fmt.allocPrint(
            allocator,
            "{s}{s}_{s}.csv",
            .{ input_path[0 .. input_path.len - 4], suffix, kind },
        );
    }
    return try std.fmt.allocPrint(allocator, "{s}{s}_{s}.csv", .{ input_path, suffix, kind });
}

pub fn run(
    allocator: std.mem.Allocator,
    emitter: protocol.Emitter,
    config_path: []const u8,
) !void {
    try emitter.status("Zig backend: nacitavam konfiguraciu...");
    try emitter.progress("Načítanie konfigurácie", 0, 1, "Načítavam nastavenia spracovania");

    const cfg_file = try std.fs.cwd().openFile(config_path, .{});
    defer cfg_file.close();
    const cfg_bytes = try cfg_file.readToEndAlloc(allocator, 16 * 1024 * 1024);
    defer allocator.free(cfg_bytes);

    var cfg = config_mod.parseFromJsonBytes(allocator, cfg_bytes) catch |err| {
        const msg = try std.fmt.allocPrint(allocator, "Config JSON chyba: {s}", .{@errorName(err)});
        defer allocator.free(msg);
        try emitter.err("CONFIG_PARSE_FAILED", msg);
        return err;
    };
    defer cfg.deinit(allocator);
    if (cfg.progress_enabled) {
        try emitter.progress("Načítanie konfigurácie", 1, 1, "Nastavenia načítané");
    }

    try emitter.status("Zig backend: nacitavam CSV riadky...");
    if (cfg.progress_enabled) {
        try emitter.progress("Načítanie CSV", 0, 1, "Načítavam vstupný CSV súbor");
    }
    var table = csv_table.loadCsvTableFromFile(allocator, cfg.file_path) catch |err| {
        const msg = try std.fmt.allocPrint(allocator, "Nepodarilo sa nacitat CSV riadky: {s}", .{@errorName(err)});
        defer allocator.free(msg);
        try emitter.err("CSV_ROWS_LOAD_FAILED", msg);
        return err;
    };
    defer table.deinit(allocator);
    if (cfg.progress_enabled) {
        try emitter.progress("Načítanie CSV", 1, 1, "CSV načítané");
    }
    if (table.header_line >= 0 and table.header_text != null) {
        const msg = try std.fmt.allocPrint(
            allocator,
            "CSV hlavička nájdená na riadku {d}: {s}",
            .{ table.header_line + 1, table.header_text.? },
        );
        defer allocator.free(msg);
        try emitter.status(msg);
    } else {
        try emitter.status("CSV hlavička nebola spoľahlivo nájdená, použije sa fallback (riadok 1).");
    }
    {
        const msg = try std.fmt.allocPrint(
            allocator,
            "Zig backend: nacitanych {d} riadkov, {d} stlpcov.",
            .{ table.rows.len, table.column_names.len },
        );
        defer allocator.free(msg);
        try emitter.status(msg);
    }
    const previews_enabled = diagnosticsPreviewsEnabled(allocator);
    if (previews_enabled) {
        const preview = measurement_preview.buildPreview(allocator, table, cfg.column_mapping);
        const msg = try std.fmt.allocPrint(
            allocator,
            "Zig backend: measurement preview rows={d}, rsrp_valid={d}, rsrp_missing={d}, latlon_valid={d}, freq_valid={d}.",
            .{
                preview.input_rows,
                preview.valid_rows_rsrp,
                preview.dropped_missing_rsrp,
                preview.valid_lat_lon_rows,
                preview.valid_frequency_rows,
            },
        );
        defer allocator.free(msg);
        try emitter.status(msg);
    }

    var processing_preview_table: *const csv_table.CsvTable = &table;
    var filtered_result_opt: ?filter_apply.ApplyMaterializedResult = null;
    var mobile_synced_table_opt: ?csv_table.CsvTable = null;
    var loaded_filters_opt: ?filters.LoadedRules = null;
    defer {
        if (loaded_filters_opt) |*lf| {
            lf.deinit(allocator);
        }
        if (mobile_synced_table_opt) |*t| {
            t.deinit(allocator);
        }
        if (filtered_result_opt) |*res| {
            res.table.deinit(allocator);
        }
    }
    if (cfg.filter_paths) |paths| {
        const msg = try std.fmt.allocPrint(allocator, "Konfigurácia obsahuje {d} custom filter path(s).", .{paths.len});
        defer allocator.free(msg);
        try emitter.status(msg);

        if (paths.len > 0) {
            try emitter.status("Zig backend: parsujem filtre...");
            if (cfg.progress_enabled) {
                try emitter.progress("Načítanie filtrov", 0, 1, "Načítavam filter pravidlá");
            }
            loaded_filters_opt = filters.loadFilterRulesFromPaths(allocator, paths) catch |err| {
                const err_msg = try std.fmt.allocPrint(allocator, "Nepodarilo sa nacitat filtre: {s}", .{@errorName(err)});
                defer allocator.free(err_msg);
                try emitter.err("FILTER_LOAD_FAILED", err_msg);
                return err;
            };
            const count_msg = try std.fmt.allocPrint(allocator, "Zig backend: nacitanych {d} filtrov.", .{loaded_filters_opt.?.rules.len});
            defer allocator.free(count_msg);
            try emitter.status(count_msg);
            if (cfg.progress_enabled) {
                try emitter.progress("Načítanie filtrov", 1, 1, "Filter pravidlá načítané");
                try emitter.progress("Aplikovanie filtrov", 0, 1, "Aplikujem filtre na dáta");
            }

            filtered_result_opt = filter_apply.applyFiltersMaterialized(
                allocator,
                table,
                loaded_filters_opt.?.rules,
                cfg.keep_original_rows,
                cfg.column_mapping,
            ) catch |err| {
                const err_msg = try std.fmt.allocPrint(allocator, "Filter apply preview zlyhal: {s}", .{@errorName(err)});
                defer allocator.free(err_msg);
                try emitter.err("FILTER_APPLY_PREVIEW_FAILED", err_msg);
                return err;
            };
            processing_preview_table = &filtered_result_opt.?.table;
            const preview_msg = try std.fmt.allocPrint(
                allocator,
                "Zig backend: filter preview input={d}, output={d}, matched={d}, multi-match={d}.",
                .{
                    filtered_result_opt.?.stats.input_rows,
                    filtered_result_opt.?.stats.output_rows,
                    filtered_result_opt.?.stats.matched_rows,
                    filtered_result_opt.?.stats.rows_with_multiple_matches,
                },
            );
            defer allocator.free(preview_msg);
            try emitter.status(preview_msg);
            if (cfg.progress_enabled) {
                try emitter.progress("Aplikovanie filtrov", 1, 1, "Filtre aplikované");
            }
        }
    }

    if (cfg.mobile_mode_enabled) {
        if (cfg.mobile_lte_file_path == null) {
            try emitter.err("MOBILE_SYNC_CONFIG", "Mobile režim je zapnutý, ale mobile_lte_file_path chýba.");
            return error.MobileSyncConfigMissing;
        }
        try emitter.status("Mobile režim: synchronizujem 5G dáta podľa LTE súboru...");
        if (cfg.progress_enabled) {
            try emitter.progress("Mobile synchronizácia", 0, 1, "Načítavam LTE a synchronizujem 5G NR");
        }
        var lte_table = csv_table.loadCsvTableFromFile(allocator, cfg.mobile_lte_file_path.?) catch |err| {
            const msg = try std.fmt.allocPrint(allocator, "Mobile režim: nepodarilo sa nacitat LTE CSV: {s}", .{@errorName(err)});
            defer allocator.free(msg);
            try emitter.err("MOBILE_LTE_READ_FAILED", msg);
            return err;
        };
        defer lte_table.deinit(allocator);

        var filtered_lte_opt: ?filter_apply.ApplyMaterializedResult = null;
        defer {
            if (filtered_lte_opt) |*flt| {
                flt.table.deinit(allocator);
            }
        }
        var lte_table_for_sync: *const csv_table.CsvTable = &lte_table;
        if (loaded_filters_opt) |lf| {
            if (lf.rules.len > 0) {
                filtered_lte_opt = filter_apply.applyFiltersMaterialized(
                    allocator,
                    lte_table,
                    lf.rules,
                    cfg.keep_original_rows,
                    null,
                ) catch |err| {
                    const msg = try std.fmt.allocPrint(allocator, "Mobile sync: LTE filter apply zlyhal: {s}", .{@errorName(err)});
                    defer allocator.free(msg);
                    try emitter.err("MOBILE_LTE_FILTER_APPLY_FAILED", msg);
                    return err;
                };
                lte_table_for_sync = &filtered_lte_opt.?.table;
                try emitter.status("Mobile režim: aplikované filtre aj na LTE pomocný súbor.");
            }
        }

        const sync_res = mobile_sync.syncFivegNrFromLte(
            allocator,
            processing_preview_table.*,
            lte_table_for_sync.*,
            cfg.mobile_nr_column_name,
            cfg.mobile_time_tolerance_ms,
            cfg.mobile_require_nr_yes,
        ) catch |err| {
            const msg = try std.fmt.allocPrint(allocator, "Mobile sync zlyhal: {s}", .{@errorName(err)});
            defer allocator.free(msg);
            try emitter.err("MOBILE_SYNC_FAILED", msg);
            return err;
        };
        mobile_synced_table_opt = sync_res.table;
        processing_preview_table = &mobile_synced_table_opt.?;

        const sync_msg = try std.fmt.allocPrint(
            allocator,
            "Mobile režim: synchronizácia hotová (window={d}ms, matched={d}, yes={d}, no={d}, blank={d}, time-only-fallback={d}, conflicts={d}).",
            .{
                sync_res.stats.window_ms,
                sync_res.stats.rows_with_match,
                sync_res.stats.rows_yes,
                sync_res.stats.rows_no,
                sync_res.stats.rows_blank,
                sync_res.stats.fallback_time_only_rows,
                sync_res.stats.conflicting_windows,
            },
        );
        defer allocator.free(sync_msg);
        try emitter.status(sync_msg);
        if (cfg.progress_enabled) {
            try emitter.progress("Mobile synchronizácia", 1, 1, "Synchronizácia dokončená");
        }
    }

    if (previews_enabled) {
        const cfg_msg = try std.fmt.allocPrint(
            allocator,
            "Zig backend: config preview include_empty_zones={any}, add_custom_operators={any}, custom_operators={d}.",
            .{ cfg.include_empty_zones, cfg.add_custom_operators, cfg.custom_operators.len },
        );
        defer allocator.free(cfg_msg);
        try emitter.status(cfg_msg);

        const proc_preview = processing_preview.build(
            allocator,
            processing_preview_table.*,
            cfg.column_mapping,
            cfg.zone_mode,
            cfg.zone_size_m,
        ) catch |err| {
            const msg = try std.fmt.allocPrint(allocator, "Processing preview zlyhal: {s}", .{@errorName(err)});
            defer allocator.free(msg);
            try emitter.err("PROCESSING_PREVIEW_FAILED", msg);
            return err;
        };

        if (std.mem.eql(u8, cfg.zone_mode, "segments")) {
            const msg = try std.fmt.allocPrint(
                allocator,
                "Zig backend: processing preview (approx projection) kept={d}/{d}, dropped_rsrp={d}, dropped_latlon={d}, segment_zones={d}, operators={d}, zone_operator_pairs={d}.",
                .{
                    proc_preview.kept_rows,
                    proc_preview.input_rows,
                    proc_preview.dropped_missing_rsrp,
                    proc_preview.dropped_invalid_lat_lon,
                    proc_preview.unique_zones,
                    proc_preview.unique_operators,
                    proc_preview.unique_zone_operator_pairs,
                },
            );
            defer allocator.free(msg);
            try emitter.status(msg);
        } else {
            const msg = try std.fmt.allocPrint(
                allocator,
                "Zig backend: processing preview (approx projection) kept={d}/{d}, dropped_rsrp={d}, dropped_latlon={d}, zones={d}, operators={d}, zone_operator_pairs={d}, coverage={d:.2}%.",
                .{
                    proc_preview.kept_rows,
                    proc_preview.input_rows,
                    proc_preview.dropped_missing_rsrp,
                    proc_preview.dropped_invalid_lat_lon,
                    proc_preview.unique_zones,
                    proc_preview.unique_operators,
                    proc_preview.unique_zone_operator_pairs,
                    proc_preview.coverage_percent orelse 0.0,
                },
            );
            defer allocator.free(msg);
            try emitter.status(msg);
        }
    }

    if (previews_enabled) {
        const agg_preview = aggregation_preview.build(
            allocator,
            processing_preview_table.*,
            cfg.column_mapping,
            cfg.zone_mode,
            cfg.zone_size_m,
            cfg.rsrp_threshold,
            cfg.sinr_threshold,
        ) catch |err| {
            const msg = try std.fmt.allocPrint(allocator, "Aggregation preview zlyhal: {s}", .{@errorName(err)});
            defer allocator.free(msg);
            try emitter.err("AGGREGATION_PREVIEW_FAILED", msg);
            return err;
        };

        const msg = try std.fmt.allocPrint(
            allocator,
            "Zig backend: aggregation preview (approx projection) rows_used={d}/{d}, zone_freq_groups={d}, zone_operator_groups={d}, unique_zones={d}, unique_operators={d}, coverage_good={d}, coverage_bad={d}, with_sinr_avg={d}.",
            .{
                agg_preview.used_rows,
                agg_preview.input_rows,
                agg_preview.zone_freq_groups,
                agg_preview.zone_operator_groups,
                agg_preview.unique_zones,
                agg_preview.unique_operators,
                agg_preview.good_coverage_groups,
                agg_preview.bad_coverage_groups,
                agg_preview.groups_with_sinr_avg,
            },
        );
        defer allocator.free(msg);
        try emitter.status(msg);
    }

    {
        if (cfg.progress_enabled) {
            try emitter.progress("Spracovanie riadkov", 0, processing_preview_table.rows.len, "Pripravujem merania a zóny");
        }

        var core = zone_stats_core.buildWithProgress(
            allocator,
            processing_preview_table.*,
            cfg.column_mapping,
            cfg.zone_mode,
            cfg.zone_size_m,
            cfg.rsrp_threshold,
            cfg.sinr_threshold,
            emitter,
            cfg.progress_enabled,
        ) catch |err| {
            const msg = try std.fmt.allocPrint(allocator, "Zone stats core preview zlyhal: {s}", .{@errorName(err)});
            defer allocator.free(msg);
            try emitter.err("ZONE_STATS_CORE_PREVIEW_FAILED", msg);
            return err;
        };
        defer core.deinit();

        const msg = try std.fmt.allocPrint(
            allocator,
            "Zig backend: zone-stats core preview rows={d}, zones={d}, operators={d}, good={d}, bad={d}.",
            .{
                core.rows.len,
                core.summary.unique_zones,
                core.summary.unique_operators,
                core.summary.good_coverage_groups,
                core.summary.bad_coverage_groups,
            },
        );
        defer allocator.free(msg);
        try emitter.status(msg);

        const normalized_suffix = try normalizeOutputSuffix(allocator, cfg.output_suffix);
        defer allocator.free(normalized_suffix);
        const effective_suffix = try withMobileSuffixIfNeeded(allocator, normalized_suffix, cfg.mobile_mode_enabled);
        defer allocator.free(effective_suffix);
        const zones_path = try deriveOutputPath(allocator, cfg.file_path, effective_suffix, "zones");
        defer allocator.free(zones_path);
        const stats_path = try deriveOutputPath(allocator, cfg.file_path, effective_suffix, "stats");
        defer allocator.free(stats_path);

        if (cfg.progress_enabled) {
            try emitter.progress("Zápis výstupov", 0, 2, "Zapisujem výstupné súbory");
        }
        try zones_writer.writeZonesCsvBasic(
            allocator,
            table.header_text,
            processing_preview_table.*,
            core.rows,
            cfg.column_mapping,
            cfg.zone_mode,
            cfg.zone_size_m,
            std.mem.eql(u8, cfg.zone_mode, "center"),
            cfg.include_empty_zones,
            cfg.add_custom_operators,
            cfg.custom_operators,
            zones_path,
        );
        const zones_msg = try std.fmt.allocPrint(allocator, "Zig backend: zapisal zones preview do {s}", .{zones_path});
        defer allocator.free(zones_msg);
        try emitter.status(zones_msg);
        if (cfg.progress_enabled) {
            try emitter.progress("Zápis výstupov", 1, 2, "Zapísaný súbor zón");
        }

        try stats_writer.writeStatsCsv(
            allocator,
            core.rows,
            core.summary,
            cfg.include_empty_zones,
            cfg.add_custom_operators,
            cfg.custom_operators,
            cfg.column_mapping.sinr != null,
            cfg.rsrp_threshold,
            cfg.sinr_threshold,
            stats_path,
        );
        const stats_msg = try std.fmt.allocPrint(allocator, "Zig backend: zapisal stats preview do {s}", .{stats_path});
        defer allocator.free(stats_msg);
        try emitter.status(stats_msg);
        if (cfg.progress_enabled) {
            try emitter.progress("Zápis výstupov", 2, 2, "Výstupy zapísané");
        }

        var min_x: ?f64 = null;
        var max_x: ?f64 = null;
        var min_y: ?f64 = null;
        var max_y: ?f64 = null;
        var range_x_m: ?f64 = null;
        var range_y_m: ?f64 = null;
        var theoretical_total_zones: ?f64 = null;
        var coverage_percent: ?f64 = null;
        if (!std.mem.eql(u8, cfg.zone_mode, "segments") and core.rows.len > 0) {
            min_x = core.rows[0].zona_x;
            max_x = core.rows[0].zona_x;
            min_y = core.rows[0].zona_y;
            max_y = core.rows[0].zona_y;
            for (core.rows[1..]) |row| {
                if (row.zona_x < min_x.?) min_x = row.zona_x;
                if (row.zona_x > max_x.?) max_x = row.zona_x;
                if (row.zona_y < min_y.?) min_y = row.zona_y;
                if (row.zona_y > max_y.?) max_y = row.zona_y;
            }
            range_x_m = (max_x.? - min_x.?) + cfg.zone_size_m;
            range_y_m = (max_y.? - min_y.?) + cfg.zone_size_m;
            theoretical_total_zones = (range_x_m.? / cfg.zone_size_m) * (range_y_m.? / cfg.zone_size_m);
            if (theoretical_total_zones.? > 0) {
                coverage_percent = (@as(f64, @floatFromInt(core.summary.unique_zones)) / theoretical_total_zones.?) * 100.0;
            }
        }

        try emitter.result(.{
            .zones_file = zones_path,
            .stats_file = stats_path,
            .include_empty_zones = cfg.include_empty_zones,
            .unique_zones = @as(i64, @intCast(core.summary.unique_zones)),
            .unique_operators = @as(i64, @intCast(core.summary.unique_operators)),
            .total_zone_rows = @as(i64, @intCast(core.rows.len)),
            .min_x = min_x,
            .max_x = max_x,
            .min_y = min_y,
            .max_y = max_y,
            .range_x_m = range_x_m,
            .range_y_m = range_y_m,
            .theoretical_total_zones = theoretical_total_zones,
            .coverage_percent = coverage_percent,
        });
        return;
    }
}
