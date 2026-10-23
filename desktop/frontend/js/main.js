import { initApi, call, on } from "./api.js";
import { state, setState } from "./store.js";
import { createRouter } from "./router.js";
import { el, applyTheme, statusLabel, toast, formatError } from "./ui.js";
import homeView from "./views/home.js";
import profilesView from "./views/profiles.js";
import profileFormView from "./views/profile-form.js";
import settingsView from "./views/settings.js";
import notFoundView from "./views/not-found.js";

const routes = [
  { pattern: /^\/$/, view: homeView, nav: "home" },
  { pattern: /^\/profiles$/, view: profilesView, nav: "profiles" },
  { pattern: /^\/profiles\/new$/, view: profileFormView, nav: "profiles" },
  { pattern: /^\/profiles\/([^/]+)\/edit$/, view: profileFormView, nav: "profiles" },
  { pattern: /^\/settings$/, view: settingsView, nav: "settings" },
  { pattern: /.*/, view: notFoundView, nav: "" },
];

function updateSidebar(runtime) {
  const dot = document.getElementById("sidebar-status-dot");
  const text = document.getElementById("sidebar-status-text");
  if (dot) dot.className = `status-dot ${runtime?.status || "stopped"}`;
  if (text) text.textContent = statusLabel(runtime?.status);
}

function setActiveNav(name) {
  for (const item of document.querySelectorAll(".nav-item")) {
    item.classList.toggle("active", item.dataset.nav === name);
  }
}

async function autoCheckUpdate() {
  try {
    const result = await call("main.UpdateService.Check");
    setState({ update: result });
    if (result?.available) {
      toast(`发现新版本 v${result.latestVersion}，可在设置页更新`, "info", 6000);
    }
  } catch (error) {
    console.warn("auto update check failed:", error);
  }
}

async function bootstrap() {
  state.mock = await initApi();

  if (state.mock) {
    document.body.append(el("div", { class: "mock-badge", text: "浏览器预览模式 · Mock 数据" }));
  }

  let settings = null;
  try {
    const [loadedSettings, profiles, runtime, version] = await Promise.all([
      call("main.SettingsService.Get"),
      call("main.ProfileService.List"),
      call("main.RuntimeService.GetState"),
      call("main.UpdateService.VersionInfo"),
    ]);
    settings = loadedSettings;
    setState({ settings, profiles, runtime, version });
    applyTheme(settings.theme);
    updateSidebar(runtime);
    const versionNode = document.getElementById("sidebar-version");
    if (versionNode) versionNode.textContent = `v${version.appVersion}`;
    call("main.SystemService.FrontendReady", state.mock ? "mock" : "wails").catch(() => {});
  } catch (error) {
    toast(`加载初始化数据失败: ${formatError(error)}`, "error", 6000);
  }

  on("runtime:state", (event) => {
    setState({ runtime: event.data });
    updateSidebar(event.data);
  });

  on("settings:changed", (event) => {
    setState({ settings: event.data });
    applyTheme(event.data.theme);
  });

  on("profiles:changed", async () => {
    try {
      const profiles = await call("main.ProfileService.List");
      setState({ profiles });
    } catch (error) {
      console.error(error);
    }
  });

  on("wails:updater:update-ready", () => {
    toast("更新已下载，重启客户端后生效", "success", 6000);
  });

  createRouter({
    container: document.getElementById("view"),
    routes,
    onRoute: setActiveNav,
  });

  if (settings?.autoCheckUpdate) {
    setTimeout(autoCheckUpdate, 3000);
  }
}

bootstrap();
