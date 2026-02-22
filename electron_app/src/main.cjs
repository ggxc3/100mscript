const path = require("node:path");
const { app, BrowserWindow, dialog, ipcMain, shell } = require("electron");
const { findZigBinary, runZigBackend, runZigInspectCsv } = require("./backend/zigClient.cjs");
const {
  discoverAutoFilterPaths,
  dedupePaths,
  pathExists
} = require("./backend/uiTools.cjs");

function getProjectRoot() {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, "100mscript_runtime");
  }
  return path.resolve(__dirname, "..", "..");
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 820,
    minWidth: 960,
    minHeight: 700,
    title: "100mscript Desktop",
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  });

  win.loadFile(path.join(__dirname, "renderer", "index.html"));
}

ipcMain.handle("app:getContext", async () => {
  const projectRoot = getProjectRoot();
  return {
    projectRoot,
    zigBinaryPath: findZigBinary(projectRoot)
  };
});

ipcMain.handle("dialog:pickFile", async (_event, options = {}) => {
  const result = await dialog.showOpenDialog({
    properties: ["openFile"],
    filters: options.filters || [{ name: "CSV", extensions: ["csv"] }, { name: "All files", extensions: ["*"] }]
  });
  if (result.canceled || !result.filePaths.length) return null;
  return result.filePaths[0];
});

ipcMain.handle("dialog:pickFiles", async (_event, options = {}) => {
  const result = await dialog.showOpenDialog({
    properties: ["openFile", "multiSelections"],
    filters: options.filters || [{ name: "Text files", extensions: ["txt"] }, { name: "All files", extensions: ["*"] }]
  });
  if (result.canceled || !result.filePaths.length) return [];
  return result.filePaths;
});

ipcMain.handle("csv:inspectHeaders", async (_event, filePath) => {
  if (typeof filePath !== "string" || !filePath.trim()) {
    throw new Error("Chýba cesta k CSV súboru.");
  }
  const projectRoot = getProjectRoot();
  const zigBinaryPath = findZigBinary(projectRoot);
  return runZigInspectCsv({
    projectRoot,
    zigBinaryPath,
    filePath: filePath.trim()
  });
});

ipcMain.handle("filters:discoverAuto", async (_event, inputCsvPath) => {
  return discoverAutoFilterPaths(inputCsvPath);
});

ipcMain.handle("paths:dedupe", async (_event, paths) => {
  return dedupePaths(Array.isArray(paths) ? paths : []);
});

ipcMain.handle("path:exists", async (_event, filePath) => {
  return pathExists(filePath);
});

ipcMain.handle("shell:showItemInFolder", async (_event, filePath) => {
  if (!filePath || !pathExists(filePath)) return false;
  shell.showItemInFolder(filePath);
  return true;
});

ipcMain.handle("shell:openPath", async (_event, filePath) => {
  if (!filePath || !pathExists(filePath)) return { ok: false, error: "NOT_FOUND" };
  const err = await shell.openPath(filePath);
  if (err) return { ok: false, error: err };
  return { ok: true };
});

ipcMain.handle("backend:runZig", async (event, payload) => {
  if (!payload || typeof payload !== "object") {
    throw new Error("Neplatný payload pre backend:runZig");
  }
  const zigBinaryPath =
    (typeof payload.zigBinaryPath === "string" && payload.zigBinaryPath.trim()) ||
    findZigBinary(getProjectRoot());
  const config = payload.config;
  if (!config || typeof config !== "object") {
    throw new Error("Chýba config objekt.");
  }

  const projectRoot = getProjectRoot();
  return runZigBackend({
    projectRoot,
    zigBinaryPath,
    config,
    onEvent: (evt) => {
      event.sender.send("backend:event", evt);
    }
  });
});

app.whenReady().then(() => {
  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
