import { call, on } from "../api.js";
import { state, setState } from "../store.js";
import {
  el,
  clear,
  toast,
  formatError,
  formatBytes,
  formatDate,
  applyTheme,
  copyText,
} from "../ui.js";

export default async function settingsView(container) {
  let settings = state.settings;
  let info = state.version;
  let updateInfo = state.update;
  let updateState = updateInfo?.available ? "available" : "idle";
  let progress = { written: 0, total: 0 };

  try {
    [settings, info] = await Promise.all([
      call("main.SettingsService.Get"),
      call("main.UpdateService.VersionInfo"),
    ]);
    setState({ settings, version: info });
  } catch (error) {
    toast(formatError(error), "error");
    settings = settings || {};
    info = info || {};
  }
  settings = settings || {};
  info = info || {};
  applyTheme(settings.theme || "system");

  container.append(
    el("div", { class: "page-head" },
      el("div", {},
        el("h1", { class: "page-title", text: "设置" }),
        el("p", { class: "page-subtitle", text: "调整客户端行为、trojan-go 路径与更新策略。" }),
      ),
      el("div", { class: "page-actions" },
        el("button", { class: "btn primary", type: "button", on: { click: () => saveSettings() } }, "保存设置"),
      ),
    ),
  );

  const autoStartInput = el("input", { type: "checkbox", checked: !!settings.autoStart });
  const autoUpdateInput = el("input", { type: "checkbox", checked: !!settings.autoCheckUpdate });
  const maxLogsInput = el("input", { type: "number", min: "100", max: "20000", value: String(settings.maxLogLines || 2000) });
  const themeSelect = el("select", {},
    el("option", { value: "system", text: "跟随系统" }),
    el("option", { value: "dark", text: "深色" }),
    el("option", { value: "light", text: "浅色" }),
  );
  themeSelect.value = settings.theme || "system";
  themeSelect.addEventListener("change", async () => {
    const previous = settings.theme || "system";
    const next = themeSelect.value;
    applyTheme(next);
    themeSelect.disabled = true;
    try {
      const saved = await call("main.SettingsService.SetTheme", next);
      settings = saved;
      setState({ settings: saved });
      themeSelect.value = saved.theme || "system";
    } catch (error) {
      themeSelect.value = previous;
      applyTheme(previous);
      toast(formatError(error), "error");
    } finally {
      themeSelect.disabled = false;
    }
  });

  const binaryInput = el("input", { type: "text", value: settings.binaryPath || "", placeholder: "留空自动查找 trojan-go" });
  const resolvedPath = el("div", { class: "readonly-path", text: info.binaryPath || "未找到 trojan-go 二进制" });

  function settingRow(title, desc, control) {
    return el("div", { class: "setting-row" },
      el("div", { class: "setting-text" },
        el("div", { class: "setting-title", text: title }),
        el("div", { class: "setting-desc", text: desc }),
      ),
      el("div", { class: "setting-control" }, control),
    );
  }

  const generalCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "通用" }),
        el("div", { class: "card-subtitle", text: "启动行为、日志与外观" }),
      ),
    ),
    el("div", { class: "card-body" },
      settingRow("启动时激活上次配置", "桌面端启动后自动启动上一次使用的配置", autoStartInput),
      settingRow("启动时检查更新", "启动后自动查询 GitHub Releases", autoUpdateInput),
      settingRow("日志缓冲行数", "保存在内存中的最大日志条数（100 - 20000）", maxLogsInput),
      settingRow("主题", "深色、浅色或跟随系统", themeSelect),
    ),
  );

  const binaryCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "trojan-go 程序" }),
        el("div", { class: "card-subtitle", text: "默认使用安装目录随包分发的二进制" }),
      ),
    ),
    el("div", { class: "card-body stack" },
      el("div", { class: "field" },
        el("label", { text: "自定义路径" }),
        binaryInput,
        el("div", { class: "hint", text: "留空时依次查找：安装目录、开发构建目录 build/<os>-<arch>、PATH。" }),
      ),
      el("div", { class: "field" },
        el("label", { text: "当前使用" }),
        resolvedPath,
      ),
      el("div", { class: "row" },
        el("button", { class: "btn small", type: "button", on: { click: () => callSystem("OpenDataDir") } }, "打开数据目录"),
        el("button", { class: "btn small", type: "button", on: { click: () => callSystem("OpenInstallDir") } }, "打开安装目录"),
      ),
    ),
  );

  const updateCurrent = el("span", { class: "mono", text: info.appVersion ? `v${info.appVersion}` : "-" });
  const updateLatest = el("span", { class: "mono", text: updateInfo?.latestVersion ? `v${updateInfo.latestVersion}` : "-" });
  const updateStatus = el("span", { class: "muted", text: "尚未检查" });
  const updateNotes = el("pre", { class: "update-notes", text: updateInfo?.notes || "" });
  updateNotes.style.display = updateInfo?.notes ? "block" : "none";
  const updateMeta = el("div", { class: "muted", style: { fontSize: "11.5px" } });

  const progressBar = el("div", { class: "progress-bar" });
  const progressText = el("span", { text: "0%" });
  const progressCount = el("span", { text: "" });
  const progressWrap = el("div", { style: { display: "none" } },
    el("div", { class: "progress" }, progressBar),
    el("div", { class: "progress-meta" }, progressText, progressCount),
  );

  const checkBtn = el("button", { class: "btn", type: "button", on: { click: () => checkUpdate() } }, "检查更新");
  const installBtn = el("button", { class: "btn primary", type: "button", on: { click: () => installUpdate() } }, "下载并安装");
  installBtn.style.display = "none";
  const restartBtn = el("button", { class: "btn primary", type: "button", on: { click: () => restartUpdate() } }, "重启并应用");
  restartBtn.style.display = "none";
  const releaseBtn = el("button", { class: "btn ghost", type: "button", on: { click: () => openRelease() } }, "打开发布页");

  const updateCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "软件更新" }),
        el("div", { class: "card-subtitle", text: "从 GitHub Releases 检查并安装桌面端更新" }),
      ),
      el("div", { class: "toolbar" }, checkBtn, releaseBtn),
    ),
    el("div", { class: "card-body" },
      el("div", { class: "update-box" },
        el("div", { class: "update-status" },
          el("span", { class: "muted", text: "当前版本" }),
          updateCurrent,
          el("span", { class: "muted", text: "最新版本" }),
          updateLatest,
          updateStatus,
        ),
        updateMeta,
        updateNotes,
        progressWrap,
        el("div", { class: "row" }, installBtn, restartBtn),
        el("div", { class: "hint-box", text: "更新包会校验 SHA256；安装目录需要可写。Windows 默认安装到用户目录，通常无需管理员权限。" }),
      ),
    ),
  );

  const aboutItems = [
    ["客户端版本", info.appVersion ? `v${info.appVersion}` : "-"],
    ["trojan-go", info.trojanVersion || "未知"],
    ["Wails", info.wailsVersion || "未知"],
    ["提交", info.commit || "-"],
    ["构建时间", info.buildTime || "-"],
    ["Go 版本", info.goVersion || "-"],
    ["平台", info.platform || "-"],
    ["数据目录", info.dataDir || "-"],
    ["安装目录", info.installDir || "-"],
    ["二进制", info.binaryPath || "未找到"],
  ];
  const aboutList = el("dl", { class: "kv" });
  for (const [key, value] of aboutItems) {
    aboutList.append(el("dt", { text: key }), el("dd", { text: value }));
  }

  const aboutCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "关于" }),
        el("div", { class: "card-subtitle", text: "版本与路径信息" }),
      ),
      el("div", { class: "toolbar" },
        el("button", { class: "btn small", type: "button", on: { click: () => copyDiagnostics() } }, "复制诊断信息"),
      ),
    ),
    el("div", { class: "card-body" }, aboutList),
  );

  container.append(
    el("div", { class: "settings-stack" },
      generalCard,
      binaryCard,
      updateCard,
      aboutCard,
    ),
  );

  function renderUpdateMeta() {
    updateMeta.textContent = updateInfo?.assetName
      ? `${updateInfo.assetName} · ${formatBytes(updateInfo.assetSize)}${updateInfo.publishedAt ? ` · ${formatDate(updateInfo.publishedAt)}` : ""}`
      : "";
  }

  function renderUpdate() {
    checkBtn.disabled = updateState === "checking" || updateState === "downloading";
    installBtn.style.display = updateState === "available" ? "inline-flex" : "none";
    restartBtn.style.display = updateState === "ready" ? "inline-flex" : "none";
    progressWrap.style.display = updateState === "downloading" || updateState === "ready" ? "block" : "none";
    updateNotes.style.display = updateInfo?.notes ? "block" : "none";
    if (updateInfo?.notes) updateNotes.textContent = updateInfo.notes;
    if (updateInfo?.latestVersion) updateLatest.textContent = `v${updateInfo.latestVersion}`;
    if (updateInfo?.currentVersion) updateCurrent.textContent = `v${updateInfo.currentVersion}`;
    renderUpdateMeta();

    switch (updateState) {
      case "checking":
        updateStatus.textContent = "正在检查…";
        break;
      case "available":
        updateStatus.textContent = "发现新版本";
        break;
      case "downloading":
        updateStatus.textContent = "正在下载…";
        break;
      case "ready":
        updateStatus.textContent = "已下载，等待重启";
        break;
      case "uptodate":
        updateStatus.textContent = "已是最新版本";
        break;
      case "error":
        updateStatus.textContent = "更新失败";
        break;
      default:
        updateStatus.textContent = "尚未检查";
    }
  }

  function setProgress(written, total) {
    progress = { written, total };
    const percent = total > 0 ? Math.min(100, Math.round((written / total) * 100)) : 0;
    progressBar.style.width = total > 0 ? `${percent}%` : "40%";
    progressText.textContent = total > 0 ? `${percent}%` : "下载中";
    progressCount.textContent = total > 0 ? `${formatBytes(written)} / ${formatBytes(total)}` : formatBytes(written);
  }

  async function checkUpdate() {
    updateState = "checking";
    renderUpdate();
    try {
      const result = await call("main.UpdateService.Check");
      updateInfo = result;
      setState({ update: result });
      updateState = result?.available ? "available" : "uptodate";
      if (result?.available) toast(`发现新版本 v${result.latestVersion}`, "info", 5000);
      else toast("当前已是最新版本", "success");
    } catch (error) {
      updateState = "error";
      updateStatus.textContent = formatError(error);
      toast(formatError(error), "error", 5200);
    }
    renderUpdate();
  }

  async function installUpdate() {
    updateState = "downloading";
    renderUpdate();
    setProgress(0, updateInfo?.assetSize || 0);
    try {
      await call("main.UpdateService.Install");
      updateState = "ready";
      toast("更新已下载，重启后生效", "success", 5000);
    } catch (error) {
      updateState = "error";
      updateStatus.textContent = formatError(error);
      toast(formatError(error), "error", 5600);
    }
    renderUpdate();
  }

  async function restartUpdate() {
    restartBtn.disabled = true;
    restartBtn.textContent = "正在重启…";
    try {
      await call("main.UpdateService.Restart");
    } catch (error) {
      restartBtn.disabled = false;
      restartBtn.textContent = "重启并应用";
      toast(formatError(error), "error", 5200);
    }
  }

  async function openRelease() {
    try {
      await call("main.UpdateService.OpenReleasePage");
    } catch (error) {
      toast(formatError(error), "error");
    }
  }

  async function callSystem(method) {
    try {
      await call(`main.SystemService.${method}`);
    } catch (error) {
      toast(formatError(error), "error");
    }
  }

  async function saveSettings() {
    const payload = {
      ...settings,
      autoStart: autoStartInput.checked,
      autoCheckUpdate: autoUpdateInput.checked,
      maxLogLines: Number(maxLogsInput.value),
      theme: themeSelect.value,
      binaryPath: binaryInput.value.trim(),
    };
    try {
      const saved = await call("main.SettingsService.Save", payload);
      settings = saved;
      setState({ settings: saved });
      applyTheme(saved.theme);
      toast("设置已保存", "success");
    } catch (error) {
      toast(formatError(error), "error", 5200);
    }
  }

  async function copyDiagnostics() {
    const text = aboutItems.map(([key, value]) => `${key}: ${value}`).join("\n");
    if (await copyText(text)) toast("诊断信息已复制", "success");
    else toast("复制失败", "error");
  }

  const unsubscribers = [
    on("wails:updater:check-started", () => {
      updateState = "checking";
      renderUpdate();
    }),
    on("wails:updater:update-available", (event) => {
      const release = event?.data || {};
      updateInfo = {
        ...(updateInfo || {}),
        available: true,
        latestVersion: release.version || updateInfo?.latestVersion,
        name: release.name || "",
        notes: release.notes || "",
        assetName: release.artifact?.filename || "",
        assetSize: release.artifact?.size || 0,
        publishedAt: release.publishedAt || "",
      };
      updateState = "available";
      renderUpdate();
    }),
    on("wails:updater:no-update", () => {
      updateState = "uptodate";
      renderUpdate();
    }),
    on("wails:updater:download-started", () => {
      updateState = "downloading";
      renderUpdate();
    }),
    on("wails:updater:download-progress", (event) => {
      updateState = "downloading";
      setProgress(event?.data?.written || 0, event?.data?.total || 0);
      renderUpdate();
    }),
    on("wails:updater:verifying", () => {
      updateStatus.textContent = "正在校验…";
    }),
    on("wails:updater:installing", () => {
      updateStatus.textContent = "正在安装…";
    }),
    on("wails:updater:update-ready", () => {
      updateState = "ready";
      setProgress(progress.total, progress.total);
      renderUpdate();
    }),
    on("wails:updater:error", (event) => {
      updateState = "error";
      updateStatus.textContent = event?.data?.message || "更新失败";
      renderUpdate();
    }),
  ];

  renderUpdate();
  if (info.appVersion) {
    const sidebar = document.getElementById("sidebar-version");
    if (sidebar) sidebar.textContent = `v${info.appVersion}`;
  }

  return () => {
    for (const unsubscribe of unsubscribers) {
      if (typeof unsubscribe === "function") unsubscribe();
    }
  };
}
