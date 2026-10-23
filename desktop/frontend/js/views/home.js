import { call, on, isMock } from "../api.js";
import { state, setState } from "../store.js";
import {
  el,
  clear,
  toast,
  formatError,
  formatTime,
  formatUptime,
  statusLabel,
  copyText,
  debounce,
} from "../ui.js";
import { navigate } from "../router.js";

const addProfileLabel = "添加配置";

export default async function homeView(container) {
  let profiles = state.profiles || [];
  let runtime = state.runtime || { status: "stopped" };
  let settings = state.settings || { maxLogLines: 2000 };
  let selectedId = "";
  let logs = [];
  let levelFilter = "all";
  let searchText = "";
  let autoScroll = true;
  let busy = false;
  let uptimeTimer = null;
  let latencyTimer = null;
  let latencyBusy = false;
  let latencyToken = 0;

  try {
    [profiles, runtime, settings] = await Promise.all([
      call("main.ProfileService.List"),
      call("main.RuntimeService.GetState"),
      call("main.SettingsService.Get"),
    ]);
    logs = await call("main.RuntimeService.GetLogs", settings.maxLogLines);
    setState({ profiles, runtime, settings });
    selectedId = runtime.profileId || "";
  } catch (error) {
    toast(formatError(error), "error");
  }

  const page = el("div", { class: "home-view" });
  container.append(page);

  const head = el("div", { class: "page-head" },
    el("div", {},
      el("h1", { class: "page-title", text: "首页" }),
      el("p", { class: "page-subtitle", text: "选择配置即可启动本地代理，日志会实时显示在下方。" }),
    ),
    el("div", { class: "page-actions" },
      el("button", { class: "btn", type: "button", on: { click: () => navigate("/profiles") } }, "管理配置"),
    ),
  );
  page.append(head);

  const select = el("select", { class: "hero-select", "aria-label": "选择配置" });
  const badge = el("span", { class: "badge stopped", text: "已停止" });
  const actionBtn = el("button", { class: "btn", type: "button", on: { click: () => toggleProxy() } }, "启动");
  const errorBanner = el("div", { class: "error-banner", style: { display: "none" } });

  const localStat = el("div", { class: "stat-value", text: "-" });
  const serverStat = el("div", { class: "stat-value", text: "-" });
  const latencyStat = el("div", { class: "stat-value", text: "-", title: "TCP 连接服务器耗时" });
  const uptimeStat = el("div", { class: "stat-value", text: "-" });

  const heroCard = el("section", { class: "card hero-card" },
    el("div", { class: "card-body" },
      el("div", { class: "hero-row" }, select, badge, actionBtn),
      el("div", { class: "hero-meta" },
        el("span", { text: "激活后会立即切换本地代理" }),
        el("span", { class: "sep" }),
        el("span", { text: `${profiles.length} 个配置` }),
      ),
      el("div", { class: "status-grid" },
        el("div", { class: "stat" }, el("div", { class: "stat-label", text: "本地监听" }), localStat),
        el("div", { class: "stat" }, el("div", { class: "stat-label", text: "服务器" }), serverStat),
        el("div", { class: "stat" }, el("div", { class: "stat-label", text: "延迟" }), latencyStat),
        el("div", { class: "stat" }, el("div", { class: "stat-label", text: "运行时长" }), uptimeStat),
      ),
      errorBanner,
    ),
  );

  const emptyCard = el("section", { class: "card" },
    el("div", { class: "empty" },
      el("div", { class: "empty-icon", text: "＋" }),
      el("div", { class: "empty-title", text: "还没有配置" }),
      el("div", { class: "empty-desc", text: "添加一份 trojan-go 客户端配置，然后回到首页选择它即可启动代理。" }),
      el("button", { class: "btn primary", type: "button", on: { click: () => navigate("/profiles/new") } }, "新增配置"),
    ),
  );

  const filterSelect = el("select", { "aria-label": "日志级别" },
    el("option", { value: "all", text: "全部级别" }),
    el("option", { value: "debug", text: "调试" }),
    el("option", { value: "info", text: "信息" }),
    el("option", { value: "warn", text: "警告" }),
    el("option", { value: "error", text: "错误" }),
  );
  const searchInput = el("input", { type: "text", placeholder: "搜索日志…", "aria-label": "搜索日志" });
  const autoScrollBox = el("input", { type: "checkbox", checked: true, "aria-label": "自动滚动" });
  const clearBtn = el("button", { class: "btn small", type: "button", on: { click: () => clearLogs() } }, "清空");
  const copyBtn = el("button", { class: "btn small", type: "button", on: { click: () => copyLogs() } }, "复制");

  const logView = el("div", { class: "log-view", role: "log", "aria-live": "polite" });
  const logCard = el("section", { class: "card log-card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "运行日志" }),
        el("div", { class: "card-subtitle", text: "trojan-go 标准输出 / 错误输出" }),
      ),
      el("div", { class: "toolbar" },
        filterSelect,
        searchInput,
        el("label", { class: "row", style: { gap: "6px", fontSize: "12px", color: "var(--muted)" } }, autoScrollBox, "自动滚动"),
        clearBtn,
        copyBtn,
      ),
    ),
    logView,
  );

  page.append(profiles.length ? heroCard : emptyCard, logCard);

  function renderProfiles() {
    clear(select);
    select.append(el("option", { value: "", text: addProfileLabel }));
    for (const profile of profiles) {
      select.append(el("option", {
        value: profile.id,
        text: `${profile.name}  ·  ${profile.config.serverAddr}:${profile.config.serverPort}`,
      }));
    }
    const preferred = selectedId && profiles.some((item) => item.id === selectedId)
      ? selectedId
      : (runtime.profileId && profiles.some((item) => item.id === runtime.profileId)
        ? runtime.profileId
        : (profiles[0]?.id || ""));
    selectedId = preferred;
    select.value = preferred;
    if (profiles.length === 0) {
      select.disabled = true;
    }
  }

  function selectedProfile() {
    return profiles.find((item) => item.id === (runtime.profileId || selectedId));
  }

  function stopLatency() {
    latencyToken++;
    if (latencyTimer) {
      clearInterval(latencyTimer);
      latencyTimer = null;
    }
    latencyStat.textContent = "-";
    latencyStat.style.color = "";
    latencyStat.title = "TCP 连接服务器耗时";
  }

  async function measureLatency() {
    if (latencyBusy || runtime.status !== "running") return;
    latencyBusy = true;
    const token = ++latencyToken;
    if (latencyStat.textContent === "-") latencyStat.textContent = "测试中…";
    try {
      const value = await call("main.RuntimeService.MeasureLatency");
      if (token !== latencyToken || runtime.status !== "running") return;
      const ms = Math.max(0, Math.round(Number(value) || 0));
      latencyStat.textContent = `${ms} ms`;
      latencyStat.style.color = "";
      latencyStat.title = "TCP 连接服务器耗时";
    } catch (error) {
      if (token !== latencyToken || runtime.status !== "running") return;
      latencyStat.textContent = "不可达";
      latencyStat.style.color = "var(--danger)";
      latencyStat.title = formatError(error);
    } finally {
      latencyBusy = false;
    }
  }

  function syncLatency() {
    if (runtime.status !== "running") {
      stopLatency();
      return;
    }
    if (!latencyTimer) {
      measureLatency();
      latencyTimer = setInterval(measureLatency, 15000);
    }
  }

  function renderRuntime() {
    badge.className = `badge ${runtime.status || "stopped"}`;
    badge.textContent = statusLabel(runtime.status);
    const activeId = selectedId && profiles.some((item) => item.id === selectedId)
      ? selectedId
      : (runtime.profileId && profiles.some((item) => item.id === runtime.profileId)
        ? runtime.profileId
        : (profiles[0]?.id || ""));
    selectedId = activeId;
    select.value = activeId;
    select.disabled = busy || runtime.status === "starting" || runtime.status === "stopping" || profiles.length === 0;

    const running = runtime.status === "running" || runtime.status === "starting";
    const stopping = runtime.status === "stopping";
    actionBtn.textContent = running ? "停止" : stopping ? "停止中…" : "启动";
    actionBtn.disabled = busy || stopping || (!running && !(runtime.profileId || selectedId)) || profiles.length === 0;

    const profile = selectedProfile();
    localStat.textContent = runtime.localAddr || (profile ? `${profile.config.localAddr}:${profile.config.localPort}` : "-");
    serverStat.textContent = runtime.serverAddr || (profile ? `${profile.config.serverAddr}:${profile.config.serverPort}` : "-");
    uptimeStat.textContent = runtime.status === "running" ? formatUptime(runtime.startedAt) : "-";
    syncLatency();

    if (runtime.status === "error" && runtime.lastError) {
      errorBanner.style.display = "flex";
      errorBanner.replaceChildren(
        el("strong", { text: "启动失败：" }),
        el("span", { text: runtime.lastError }),
      );
    } else {
      errorBanner.style.display = "none";
      errorBanner.replaceChildren();
    }
  }

  function matches(entry) {
    if (levelFilter === "debug" && !["DEBUG", "TRACE"].includes(entry.level)) return false;
    if (levelFilter === "info" && !["INFO", "SYSTEM"].includes(entry.level)) return false;
    if (levelFilter === "warn" && entry.level !== "WARN") return false;
    if (levelFilter === "error" && !["ERROR", "FATAL"].includes(entry.level)) return false;
    if (searchText) {
      const haystack = `${entry.level} ${entry.text}`.toLowerCase();
      if (!haystack.includes(searchText)) return false;
    }
    return true;
  }

  function logLine(entry) {
    return el("div", { class: "log-line" },
      el("span", { class: "log-time", text: formatTime(entry.time) }),
      el("span", { class: `log-level ${entry.level}`, text: entry.level }),
      el("span", { class: "log-text", text: entry.text }),
    );
  }

  function renderLogs() {
    clear(logView);
    const visible = logs.filter(matches);
    if (visible.length === 0) {
      logView.append(el("div", { class: "log-empty", text: logs.length ? "没有匹配的日志" : "暂无日志" }));
      return;
    }
    for (const entry of visible) logView.append(logLine(entry));
    if (autoScroll) logView.scrollTop = logView.scrollHeight;
  }

  function appendLogs(entries) {
    for (const entry of entries) {
      logs.push(entry);
      if (logs.length > settings.maxLogLines) logs.shift();
      if (!matches(entry)) continue;
      if (logView.firstElementChild?.classList.contains("log-empty")) clear(logView);
      logView.append(logLine(entry));
      while (logView.children.length > settings.maxLogLines) logView.firstElementChild.remove();
    }
    if (autoScroll) logView.scrollTop = logView.scrollHeight;
  }

  async function activate(id) {
    if (!id || busy) return;
    selectedId = id;
    busy = true;
    renderRuntime();
    try {
      const next = await call("main.RuntimeService.Activate", id);
      runtime = next;
      setState({ runtime: next });
      const profile = profiles.find((item) => item.id === id);
      toast(`已激活 ${profile?.name || "配置"}`, "success");
    } catch (error) {
      toast(formatError(error), "error", 5200);
    } finally {
      busy = false;
      renderRuntime();
    }
  }

  async function stopProxy() {
    if (busy) return;
    busy = true;
    renderRuntime();
    try {
      runtime = await call("main.RuntimeService.Stop");
      setState({ runtime });
      toast("已停止代理", "success");
    } catch (error) {
      toast(formatError(error), "error");
    } finally {
      busy = false;
      renderRuntime();
    }
  }

  async function clearLogs() {
    try {
      await call("main.RuntimeService.ClearLogs");
      logs = [];
      renderLogs();
    } catch (error) {
      toast(formatError(error), "error");
    }
  }

  async function copyLogs() {
    const text = logs.map((entry) => `[${formatTime(entry.time)}] [${entry.level}] ${entry.text}`).join("\n");
    if (!text) {
      toast("没有可复制的日志", "warn");
      return;
    }
    if (await copyText(text)) toast("日志已复制到剪贴板", "success");
    else toast("复制失败", "error");
  }

  function toggleProxy() {
    if (runtime.status === "running" || runtime.status === "starting") {
      stopProxy();
      return;
    }
    const id = runtime.profileId || selectedId;
    if (!id) {
      toast("请先选择配置", "warn");
      return;
    }
    activate(id);
  }
  select.addEventListener("change", () => {
    const id = select.value;
    if (!id) {
      navigate("/profiles/new");
      return;
    }
    selectedId = id;
    activate(id);
  });
  filterSelect.addEventListener("change", () => {
    levelFilter = filterSelect.value;
    renderLogs();
  });
  searchInput.addEventListener("input", debounce(() => {
    searchText = searchInput.value.trim().toLowerCase();
    renderLogs();
  }, 120));
  autoScrollBox.addEventListener("change", () => {
    autoScroll = autoScrollBox.checked;
    if (autoScroll) logView.scrollTop = logView.scrollHeight;
  });

  const unsubscribers = [
    on("runtime:state", (event) => {
      runtime = event.data || runtime;
      setState({ runtime });
      renderRuntime();
    }),
    on("runtime:log", (event) => {
      const entries = event?.data?.lines;
      if (Array.isArray(entries)) appendLogs(entries);
    }),
    on("runtime:cleared", () => {
      logs = [];
      renderLogs();
    }),
    on("profiles:changed", async () => {
      try {
        profiles = await call("main.ProfileService.List");
        setState({ profiles });
        renderProfiles();
        renderRuntime();
      } catch (error) {
        console.error(error);
      }
    }),
  ];

  uptimeTimer = setInterval(() => {
    if (runtime.status === "running") uptimeStat.textContent = formatUptime(runtime.startedAt);
  }, 1000);

  renderProfiles();
  renderRuntime();
  renderLogs();

  if (isMock()) {
    setTimeout(() => {
      if (profiles.length && runtime.status === "stopped") activate(profiles[0].id);
    }, 800);
  }

  return () => {
    for (const unsubscribe of unsubscribers) {
      if (typeof unsubscribe === "function") unsubscribe();
    }
    clearInterval(uptimeTimer);
    stopLatency();
  };
}
