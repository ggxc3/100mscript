const fs = require("node:fs");
const fsp = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");
const { spawn } = require("node:child_process");

function defaultZigBinaryCandidates(projectRoot) {
  const exe = process.platform === "win32" ? "100mscript_engine.exe" : "100mscript_engine";
  return [
    path.join(projectRoot, "zig_backend", "bin", exe),
    path.join(projectRoot, "zig_backend", "zig-out", "bin", exe),
    path.join(projectRoot, "zig-out", "bin", exe)
  ];
}

function findZigBinary(projectRoot) {
  const envPath = (process.env.ZIG_BACKEND_BIN || "").trim();
  if (envPath && fs.existsSync(envPath)) {
    return envPath;
  }
  for (const candidate of defaultZigBinaryCandidates(projectRoot)) {
    if (fs.existsSync(candidate)) return candidate;
  }
  return null;
}

function parseNdjsonLine(line) {
  const text = String(line || "").trim();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return { type: "status", message: `[raw] ${text}` };
  }
}

function maybeRuntimeProjEnv(projectRoot) {
  const env = {};
  const projDataDir = path.join(projectRoot, "zig_backend", "vendor", "proj", "share", "proj");
  const projLibDir = path.join(projectRoot, "zig_backend", "vendor", "proj", "lib");
  if (fs.existsSync(projDataDir)) {
    env.ZIG_PROJ_DATA_DIR = projDataDir;
  }
  if (fs.existsSync(projLibDir)) {
    const names = process.platform === "win32"
      ? ["proj.dll", "libproj.dll"]
      : process.platform === "darwin"
        ? ["libproj.dylib"]
        : ["libproj.so", "libproj.so.25"];
    for (const name of names) {
      const full = path.join(projLibDir, name);
      if (fs.existsSync(full)) {
        env.ZIG_PROJ_DYLIB = full;
        break;
      }
    }
    if (process.platform === "win32" && !env.ZIG_PROJ_DYLIB) {
      try {
        const files = fs.readdirSync(projLibDir);
        const versioned = files.find((name) => /^proj.*\.dll$/i.test(name) || /^libproj.*\.dll$/i.test(name));
        if (versioned) {
          env.ZIG_PROJ_DYLIB = path.join(projLibDir, versioned);
        }
      } catch {
        // ignore
      }
    }
    if (process.platform === "win32") {
      env.PATH = `${projLibDir}${path.delimiter}${process.env.PATH || ""}`;
    } else {
      env.LD_LIBRARY_PATH = `${projLibDir}${path.delimiter}${process.env.LD_LIBRARY_PATH || ""}`;
      env.DYLD_LIBRARY_PATH = `${projLibDir}${path.delimiter}${process.env.DYLD_LIBRARY_PATH || ""}`;
    }
  }
  return env;
}

async function runZigInspectCsv({ projectRoot, zigBinaryPath, filePath }) {
  if (!zigBinaryPath) {
    throw new Error("Spracovací modul sa nenašiel.");
  }
  if (!filePath || !String(filePath).trim()) {
    throw new Error("Chýba cesta k CSV súboru.");
  }

  const child = spawn(zigBinaryPath, ["inspect", "--csv", String(filePath).trim()], {
    cwd: projectRoot,
    env: { ...process.env, ...maybeRuntimeProjEnv(projectRoot) },
    stdio: ["ignore", "pipe", "pipe"]
  });

  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => {
    stdout += chunk.toString("utf8");
  });
  child.stderr.on("data", (chunk) => {
    stderr += chunk.toString("utf8");
  });

  const exitCode = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", resolve);
  });

  if (exitCode !== 0) {
    throw new Error(`Zig inspect zlyhal (exit=${exitCode})${stderr ? `: ${stderr.trim()}` : ""}`);
  }

  const text = String(stdout || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find(Boolean);

  if (!text) {
    throw new Error("Zig inspect nevrátil žiadny výstup.");
  }

  try {
    return JSON.parse(text);
  } catch (err) {
    throw new Error(`Neplatný JSON zo Zig inspect: ${err.message}`);
  }
}

async function runZigBackendSpawn({ projectRoot, zigBinaryPath, config, onEvent }) {
  if (!zigBinaryPath) {
    throw new Error("Spracovací modul sa nenašiel.");
  }

  const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "100mscript-electron-"));
  const configPath = path.join(tmpDir, "config.json");
  await fsp.writeFile(configPath, JSON.stringify(config, null, 2), "utf8");

  const child = spawn(zigBinaryPath, ["run", "--config", configPath], {
    cwd: projectRoot,
    env: { ...process.env, ...maybeRuntimeProjEnv(projectRoot) },
    stdio: ["ignore", "pipe", "pipe"]
  });

  let stderr = "";
  let resultEvent = null;
  let errorEvent = null;
  const statuses = [];
  let graceKillTimer = null;
  let hardKillTimer = null;

  const clearExitTimers = () => {
    if (graceKillTimer) {
      clearTimeout(graceKillTimer);
      graceKillTimer = null;
    }
    if (hardKillTimer) {
      clearTimeout(hardKillTimer);
      hardKillTimer = null;
    }
  };

  const armExitTimersIfTerminalEvent = () => {
    if (graceKillTimer || child.exitCode !== null) return;

    graceKillTimer = setTimeout(() => {
      if (child.exitCode === null && !child.killed) {
        try {
          child.kill("SIGTERM");
        } catch {
          // ignore
        }
      }
    }, 1500);
    if (typeof graceKillTimer.unref === "function") graceKillTimer.unref();

    hardKillTimer = setTimeout(() => {
      if (child.exitCode === null && !child.killed) {
        try {
          child.kill("SIGKILL");
        } catch {
          // ignore
        }
      }
    }, 5000);
    if (typeof hardKillTimer.unref === "function") hardKillTimer.unref();
  };

  const emit = (event) => {
    if (!event) return;
    if (event.type === "result") resultEvent = event;
    if (event.type === "error") errorEvent = event;
    if (event.type === "status") statuses.push(String(event.message || ""));
    if (event.type === "result" || event.type === "error") armExitTimersIfTerminalEvent();
    if (typeof onEvent === "function") onEvent(event);
  };

  const stdoutRl = readline.createInterface({ input: child.stdout, crlfDelay: Infinity });
  stdoutRl.on("line", (line) => emit(parseNdjsonLine(line)));

  child.stderr.on("data", (chunk) => {
    stderr += chunk.toString("utf8");
  });

  const exitCode = await new Promise((resolve, reject) => {
    child.once("error", (err) => {
      clearExitTimers();
      reject(err);
    });
    child.once("close", (code) => {
      clearExitTimers();
      resolve(code);
    });
  });

  await stdoutRl.close();
  try {
    await fsp.rm(tmpDir, { recursive: true, force: true });
  } catch {
    // best effort cleanup
  }

  return {
    exitCode,
    resultEvent,
    errorEvent,
    stderr: stderr.trim(),
    statuses
  };
}

class PersistentRunWorker {
  constructor() {
    this.child = null;
    this.stdoutRl = null;
    this.projectRoot = null;
    this.zigBinaryPath = null;
    this.ready = false;
    this.startingPromise = null;
    this.currentRun = null;
    this.stderrBuffer = "";
    this.queue = Promise.resolve();
  }

  _matches(projectRoot, zigBinaryPath) {
    return this.child && this.projectRoot === projectRoot && this.zigBinaryPath === zigBinaryPath;
  }

  _resetProcessState() {
    this.child = null;
    this.stdoutRl = null;
    this.projectRoot = null;
    this.zigBinaryPath = null;
    this.ready = false;
    this.startingPromise = null;
  }

  async shutdown() {
    const child = this.child;
    this._resetProcessState();
    if (!child) return;
    try {
      child.stdin.write(`${JSON.stringify({ type: "shutdown" })}\n`);
    } catch {
      // ignore
    }
    try {
      child.kill("SIGTERM");
    } catch {
      // ignore
    }
  }

  _handleWorkerLine(line) {
    const evt = parseNdjsonLine(line);
    if (!evt || typeof evt !== "object") return;

    if (evt.type === "worker_ready") {
      this.ready = true;
      return;
    }

    const run = this.currentRun;
    if (!run) {
      return;
    }

    if (evt.type === "worker_command_done") {
      if (evt.command !== "run") return;
      run.doneSeen = true;
      run.resolve({
        exitCode: evt.ok ? 0 : 1,
        resultEvent: run.resultEvent,
        errorEvent: run.errorEvent || (!evt.ok ? { type: "error", code: "WORKER_RUN_FAILED", message: "Spracovanie sa skončilo bez výsledku." } : null),
        stderr: this.stderrBuffer.slice(run.stderrStart).trim(),
        statuses: run.statuses
      });
      this.currentRun = null;
      return;
    }

    if (evt.type === "result") run.resultEvent = evt;
    if (evt.type === "error") run.errorEvent = evt;
    if (evt.type === "status") run.statuses.push(String(evt.message || ""));

    if (typeof run.onEvent === "function") {
      run.onEvent(evt);
    }
  }

  async _ensureStarted(projectRoot, zigBinaryPath) {
    if (this._matches(projectRoot, zigBinaryPath) && this.ready) {
      return;
    }

    if (this.child && !this._matches(projectRoot, zigBinaryPath)) {
      await this.shutdown();
    }

    if (this.startingPromise) {
      return this.startingPromise;
    }

    this.startingPromise = (async () => {
      const child = spawn(zigBinaryPath, ["worker"], {
        cwd: projectRoot,
        env: { ...process.env, ...maybeRuntimeProjEnv(projectRoot) },
        stdio: ["pipe", "pipe", "pipe"]
      });

      this.child = child;
      this.projectRoot = projectRoot;
      this.zigBinaryPath = zigBinaryPath;
      this.ready = false;

      child.stderr.on("data", (chunk) => {
        this.stderrBuffer += chunk.toString("utf8");
        if (this.stderrBuffer.length > 2_000_000) {
          this.stderrBuffer = this.stderrBuffer.slice(-1_000_000);
        }
      });

      child.once("error", (err) => {
        if (this.currentRun) {
          this.currentRun.reject(err);
          this.currentRun = null;
        }
        this._resetProcessState();
      });

      child.once("close", (code) => {
        const err = new Error(`Zig worker sa ukončil (exit=${code ?? "?"}).`);
        if (this.currentRun) {
          this.currentRun.reject(err);
          this.currentRun = null;
        }
        this._resetProcessState();
      });

      this.stdoutRl = readline.createInterface({ input: child.stdout, crlfDelay: Infinity });
      this.stdoutRl.on("line", (line) => this._handleWorkerLine(line));

      await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
          reject(new Error("Zig worker sa nespustil včas."));
        }, 5000);
        const poll = () => {
          if (!this.child || this.child !== child) {
            clearTimeout(timeout);
            reject(new Error("Zig worker nebol spustený."));
            return;
          }
          if (this.ready) {
            clearTimeout(timeout);
            resolve();
            return;
          }
          setTimeout(poll, 25);
        };
        poll();
      });
    })();

    try {
      await this.startingPromise;
    } finally {
      this.startingPromise = null;
    }
  }

  async _runQueued({ projectRoot, zigBinaryPath, config, onEvent }) {
    await this._ensureStarted(projectRoot, zigBinaryPath);
    if (!this.child || !this.child.stdin || this.child.killed) {
      throw new Error("Zig worker nie je dostupný.");
    }
    if (this.currentRun) {
      throw new Error("Zig worker je zaneprázdnený.");
    }

    const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "100mscript-electron-"));
    const configPath = path.join(tmpDir, "config.json");
    await fsp.writeFile(configPath, JSON.stringify(config, null, 2), "utf8");

    return await new Promise((resolve, reject) => {
      this.currentRun = {
        onEvent,
        resultEvent: null,
        errorEvent: null,
        statuses: [],
        stderrStart: this.stderrBuffer.length,
        doneSeen: false,
        resolve: async (result) => {
          try {
            await fsp.rm(tmpDir, { recursive: true, force: true });
          } catch {
            // best effort cleanup
          }
          resolve(result);
        },
        reject: async (err) => {
          try {
            await fsp.rm(tmpDir, { recursive: true, force: true });
          } catch {
            // best effort cleanup
          }
          reject(err);
        }
      };

      const payload = JSON.stringify({ type: "run", config_path: configPath }) + "\n";
      this.child.stdin.write(payload, "utf8", (err) => {
        if (!err) return;
        const run = this.currentRun;
        this.currentRun = null;
        if (run) run.reject(err);
      });
    });
  }

  run(args) {
    const task = () => this._runQueued(args);
    const p = this.queue.then(task, task);
    this.queue = p.catch(() => {});
    return p;
  }
}

const persistentRunWorker = new PersistentRunWorker();

async function runZigBackend({ projectRoot, zigBinaryPath, config, onEvent }) {
  if (!zigBinaryPath) {
    throw new Error("Spracovací modul sa nenašiel.");
  }
  if (String(process.env.ZIG_DISABLE_PERSISTENT_WORKER || "").trim() === "1") {
    return runZigBackendSpawn({ projectRoot, zigBinaryPath, config, onEvent });
  }
  try {
    return await persistentRunWorker.run({ projectRoot, zigBinaryPath, config, onEvent });
  } catch (err) {
    // Fallback for resilience if worker startup/protocol fails.
    return runZigBackendSpawn({ projectRoot, zigBinaryPath, config, onEvent });
  }
}

process.on("exit", () => {
  try {
    persistentRunWorker.shutdown();
  } catch {
    // ignore
  }
});

module.exports = {
  defaultZigBinaryCandidates,
  findZigBinary,
  runZigBackend,
  runZigInspectCsv
};
