const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");

const ROOT = path.resolve(__dirname, "..", "..");
const ELECTRON_DIR = path.resolve(__dirname, "..");
const STAGE_DIR = path.join(ELECTRON_DIR, "build_runtime");
const ZIG_DIR = path.join(ROOT, "zig_backend");

function exeName() {
  return process.platform === "win32" ? "100mscript_engine.exe" : "100mscript_engine";
}

async function rmrf(target) {
  await fsp.rm(target, { recursive: true, force: true });
}

async function ensureDir(dir) {
  await fsp.mkdir(dir, { recursive: true });
}

async function copyFile(src, dst) {
  await ensureDir(path.dirname(dst));
  await fsp.copyFile(src, dst);
}

async function copyDir(src, dst) {
  const stat = await fsp.stat(src);
  if (!stat.isDirectory()) {
    throw new Error(`Nie je priečinok: ${src}`);
  }
  await ensureDir(dst);
  const entries = await fsp.readdir(src, { withFileTypes: true });
  for (const entry of entries) {
    const from = path.join(src, entry.name);
    const to = path.join(dst, entry.name);
    if (entry.isDirectory()) {
      await copyDir(from, to);
    } else if (entry.isFile()) {
      await copyFile(from, to);
    }
  }
}

function findExisting(paths) {
  for (const p of paths) {
    if (fs.existsSync(p)) return p;
  }
  return null;
}

async function main() {
  const backendExe = findExisting([
    path.join(ZIG_DIR, "zig-out", "bin", exeName()),
    path.join(ROOT, "zig-out", "bin", exeName())
  ]);
  if (!backendExe) {
    throw new Error("Zig backend binary sa nenašiel. Spusť najprv `zig build` v `zig_backend/`.");
  }

  const projVendorDir = path.join(ZIG_DIR, "vendor", "proj");
  if (!fs.existsSync(projVendorDir)) {
    throw new Error("Chýba `zig_backend/vendor/proj`. Runtime potrebuje PROJ assets.");
  }

  await rmrf(STAGE_DIR);

  await copyFile(backendExe, path.join(STAGE_DIR, "zig_backend", "bin", path.basename(backendExe)));
  await copyDir(projVendorDir, path.join(STAGE_DIR, "zig_backend", "vendor", "proj"));

  const manifest = {
    generatedAt: new Date().toISOString(),
    backendExe: path.relative(ROOT, backendExe),
    stageDir: path.relative(ROOT, STAGE_DIR),
    platform: process.platform
  };
  await fsp.writeFile(path.join(STAGE_DIR, "manifest.json"), JSON.stringify(manifest, null, 2), "utf8");

  process.stdout.write(`Prepared runtime bundle in ${STAGE_DIR}\n`);
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : String(err));
  process.exit(1);
});
