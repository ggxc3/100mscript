const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("codex100m", {
  getContext() {
    return ipcRenderer.invoke("app:getContext");
  },
  pickCsvFile() {
    return ipcRenderer.invoke("dialog:pickFile", {
      filters: [{ name: "CSV files", extensions: ["csv"] }]
    });
  },
  pickLteCsvFile() {
    return ipcRenderer.invoke("dialog:pickFile", {
      filters: [{ name: "CSV files", extensions: ["csv"] }]
    });
  },
  pickFilterFiles() {
    return ipcRenderer.invoke("dialog:pickFiles", {
      filters: [{ name: "Text files", extensions: ["txt"] }, { name: "All files", extensions: ["*"] }]
    });
  },
  inspectCsvHeaders(filePath) {
    return ipcRenderer.invoke("csv:inspectHeaders", filePath);
  },
  discoverAutoFilters(inputCsvPath) {
    return ipcRenderer.invoke("filters:discoverAuto", inputCsvPath);
  },
  dedupePaths(paths) {
    return ipcRenderer.invoke("paths:dedupe", paths);
  },
  pathExists(filePath) {
    return ipcRenderer.invoke("path:exists", filePath);
  },
  showItemInFolder(filePath) {
    return ipcRenderer.invoke("shell:showItemInFolder", filePath);
  },
  openPath(filePath) {
    return ipcRenderer.invoke("shell:openPath", filePath);
  },
  runZig(config, zigBinaryPath) {
    return ipcRenderer.invoke("backend:runZig", { config, zigBinaryPath });
  },
  onBackendEvent(callback) {
    const handler = (_event, payload) => callback(payload);
    ipcRenderer.on("backend:event", handler);
    return () => ipcRenderer.removeListener("backend:event", handler);
  }
});
