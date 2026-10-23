const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const clone = (value) => structuredClone(value);
const now = () => Date.now();

const encodeShareToken = (value) => {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return `TGD1:mock.${btoa(binary)}`;
};

const decodeShareToken = (token) => {
  const prefix = "TGD1:mock.";
  if (!token.startsWith(prefix)) throw new Error("配置信息格式不正确");
  try {
    const binary = atob(token.slice(prefix.length));
    const bytes = Uint8Array.from(binary, (ch) => ch.charCodeAt(0));
    return JSON.parse(new TextDecoder().decode(bytes));
  } catch {
    throw new Error("配置信息已损坏");
  }
};

const sampleNotes = `## v0.2.0

- 新增：首页日志级别过滤与搜索
- 新增：配置复制
- 优化：启动失败时的错误提示
- 修复：切换配置时端口未及时释放

> 这是浏览器 Mock 模式下的示例更新说明。`;

export function createMock() {
  const listeners = new Map();
  let sequence = 0;
  let logTimer = null;
  let progressTimer = null;

  const emit = (name, data) => {
    for (const callback of listeners.get(name) || []) {
      callback({ name, data });
    }
  };

  const settings = {
    autoStart: false,
    autoCheckUpdate: true,
    maxLogLines: 2000,
    theme: "system",
    binaryPath: "",
    lastProfileId: "",
    updateChannel: "stable",
    updateRepository: "litai686/trojan-go",
  };

  const profiles = [
    {
      id: "demo-1",
      name: "香港节点",
      config: {
        serverAddr: "hk.example.com",
        serverPort: 443,
        password: "demo-password",
        sni: "hk.example.com",
        localAddr: "127.0.0.1",
        localPort: 1080,
        mux: true,
        router: false,
        verifyTls: true,
        logLevel: 1,
      },
      createdAt: new Date(now() - 86400000 * 3).toISOString(),
      updatedAt: new Date(now() - 3600000).toISOString(),
    },
    {
      id: "demo-2",
      name: "日本节点",
      config: {
        serverAddr: "jp.example.com",
        serverPort: 443,
        password: "demo-password-2",
        sni: "jp.example.com",
        localAddr: "127.0.0.1",
        localPort: 1081,
        mux: true,
        router: false,
        verifyTls: true,
        logLevel: 1,
      },
      createdAt: new Date(now() - 86400000).toISOString(),
      updatedAt: new Date(now() - 120000).toISOString(),
    },
  ];

  const runtime = {
    status: "stopped",
    profileId: "",
    profileName: "",
    pid: 0,
    startedAt: 0,
    localAddr: "",
    serverAddr: "",
    binaryPath: "/usr/local/bin/trojan-go (mock)",
    lastError: "",
  };

  const logs = [];
  const addLog = (level, text, stream = "stdout") => {
    sequence += 1;
    const entry = {
      seq: sequence,
      time: now(),
      stream,
      level,
      text,
    };
    logs.push(entry);
    if (logs.length > settings.maxLogLines) logs.shift();
    emit("runtime:log", { lines: [entry] });
  };

  const startLogSimulation = (profile) => {
    clearInterval(logTimer);
    const messages = [
      ["INFO", `本地监听 ${profile.config.localAddr}:${profile.config.localPort}`],
      ["INFO", `连接到 ${profile.config.serverAddr}:${profile.config.serverPort}`],
      ["DEBUG", "TLS 握手完成"],
      ["INFO", "代理请求 127.0.0.1 -> example.org:443"],
      ["DEBUG", "mux 会话已复用"],
      ["INFO", "代理请求 127.0.0.1 -> api.github.com:443"],
      ["WARN", "连接超时，正在重试"],
      ["INFO", "重试成功"],
    ];
    let index = 0;
    logTimer = setInterval(() => {
      const [level, text] = messages[index % messages.length];
      index += 1;
      addLog(level, text);
    }, 1400);
  };

  const activate = async (id) => {
    const profile = profiles.find((item) => item.id === id);
    if (!profile) throw new Error("配置不存在");
    clearInterval(logTimer);
    runtime.status = "starting";
    runtime.profileId = id;
    runtime.profileName = profile.name;
    runtime.localAddr = `${profile.config.localAddr}:${profile.config.localPort}`;
    runtime.serverAddr = `${profile.config.serverAddr}:${profile.config.serverPort}`;
    runtime.lastError = "";
    emit("runtime:state", clone(runtime));
    addLog("SYSTEM", `正在启动 ${profile.name}…`);
    await sleep(350);
    runtime.status = "running";
    runtime.pid = 4242;
    runtime.startedAt = now();
    emit("runtime:state", clone(runtime));
    addLog("INFO", "trojan-go 启动成功");
    startLogSimulation(profile);
    settings.lastProfileId = id;
    return clone(runtime);
  };

  const stop = async () => {
    clearInterval(logTimer);
    if (runtime.status === "stopped") return clone(runtime);
    runtime.status = "stopping";
    emit("runtime:state", clone(runtime));
    addLog("SYSTEM", "正在停止 trojan-go…");
    await sleep(250);
    runtime.status = "stopped";
    runtime.pid = 0;
    runtime.startedAt = 0;
    emit("runtime:state", clone(runtime));
    addLog("SYSTEM", "trojan-go 已停止");
    return clone(runtime);
  };

  const versionInfo = {
    appVersion: "0.1.0-mock",
    trojanVersion: "v0.0.2",
    commit: "mock",
    buildTime: "2026-10-22T00:00:00Z",
    goVersion: "go1.26.4",
    platform: "windows/amd64",
    wailsVersion: "v3.0.0-beta.34",
    dataDir: "C:\\Users\\demo\\AppData\\Roaming\\trojan-go-desktop",
    installDir: "C:\\Users\\demo\\AppData\\Local\\Programs\\Trojan-Go Desktop",
    binaryPath: "C:\\Users\\demo\\AppData\\Local\\Programs\\Trojan-Go Desktop\\trojan-go.exe",
  };

  const handlers = {
    "main.ProfileService.List": async () => clone(profiles),
    "main.ProfileService.Get": async (id) => {
      const found = profiles.find((item) => item.id === id);
      if (!found) throw new Error("配置不存在");
      return clone(found);
    },
    "main.ProfileService.Save": async (input) => {
      const name = String(input.name || "").trim();
      if (!name) throw new Error("配置名称不能为空");
      if (!input.config?.serverAddr) throw new Error("服务器地址不能为空");
      if (!input.config?.password) throw new Error("密码不能为空");
      if (profiles.some((item) => item.id !== input.id && item.name.toLowerCase() === name.toLowerCase())) {
        throw new Error("配置名称已存在");
      }
      await sleep(150);
      const timestamp = new Date().toISOString();
      if (input.id) {
        const index = profiles.findIndex((item) => item.id === input.id);
        if (index < 0) throw new Error("配置不存在");
        profiles[index] = { ...profiles[index], ...input, name, updatedAt: timestamp };
        emit("profiles:changed", null);
        return clone(profiles[index]);
      }
      const created = {
        ...input,
        id: `mock-${now()}`,
        name,
        createdAt: timestamp,
        updatedAt: timestamp,
      };
      profiles.unshift(created);
      emit("profiles:changed", null);
      return clone(created);
    },
    "main.ProfileService.Delete": async (id) => {
      const index = profiles.findIndex((item) => item.id === id);
      if (index < 0) throw new Error("配置不存在");
      if (runtime.profileId === id) await stop();
      profiles.splice(index, 1);
      emit("profiles:changed", null);
      return null;
    },
    "main.ProfileService.Duplicate": async (id) => {
      const source = profiles.find((item) => item.id === id);
      if (!source) throw new Error("配置不存在");
      const timestamp = new Date().toISOString();
      const copy = {
        ...clone(source),
        id: `mock-${now()}`,
        name: `${source.name} 副本`,
        createdAt: timestamp,
        updatedAt: timestamp,
      };
      profiles.unshift(copy);
      emit("profiles:changed", null);
      return copy;
    },
    "main.ProfileService.ExportConfig": async (id) => {
      const source = profiles.find((item) => item.id === id);
      if (!source) throw new Error("配置不存在");
      return encodeShareToken({ name: source.name, config: source.config });
    },
    "main.ProfileService.DecodeConfig": async (token) => {
      const data = decodeShareToken(String(token || "").trim().replace(/\s+/g, ""));
      const base = String(data.name || "导入的配置").trim() || "导入的配置";
      let name = base;
      let index = 2;
      while (profiles.some((item) => item.name.toLowerCase() === name.toLowerCase())) {
        name = `${base} 导入 ${index++}`;
      }
      return { name, config: clone(data.config || {}) };
    },

    "main.RuntimeService.GetState": async () => clone(runtime),
    "main.RuntimeService.MeasureLatency": async () => {
      await sleep(80);
      if (runtime.status !== "running") throw new Error("代理未运行");
      return 24 + Math.floor(Math.random() * 36);
    },
    "main.RuntimeService.Activate": async (id) => activate(id),
    "main.RuntimeService.Stop": async () => stop(),
    "main.RuntimeService.Restart": async () => {
      if (!runtime.profileId) throw new Error("当前没有运行中的配置");
      await stop();
      return activate(runtime.profileId);
    },
    "main.RuntimeService.GetLogs": async (limit) => clone(logs.slice(-Math.max(1, limit || 2000))),
    "main.RuntimeService.ClearLogs": async () => {
      logs.length = 0;
      emit("runtime:cleared", null);
      return null;
    },

    "main.SettingsService.Get": async () => clone(settings),
    "main.SettingsService.Save": async (input) => {
      Object.assign(settings, input);
      emit("settings:changed", clone(settings));
      return clone(settings);
    },
    "main.SettingsService.SetTheme": async (theme) => {
      settings.theme = ["system", "dark", "light"].includes(theme) ? theme : "system";
      emit("settings:changed", clone(settings));
      return clone(settings);
    },

    "main.UpdateService.VersionInfo": async () => clone(versionInfo),
    "main.UpdateService.Check": async () => {
      await sleep(600);
      return {
        currentVersion: versionInfo.appVersion,
        latestVersion: "0.2.0",
        available: true,
        name: "Trojan-Go Desktop v0.2.0",
        notes: sampleNotes,
        publishedAt: "2026-10-20T10:00:00Z",
        assetName: "trojan-go-desktop-windows-amd64.exe",
        assetSize: 18874368,
        htmlUrl: "https://github.com/litai686/trojan-go/releases",
      };
    },
    "main.UpdateService.Install": async () => {
      clearInterval(progressTimer);
      let written = 0;
      const total = 18874368;
      emit("wails:updater:download-started", {});
      await new Promise((resolve) => {
        progressTimer = setInterval(() => {
          written = Math.min(total, written + total / 14);
          emit("wails:updater:download-progress", { written, total, rate: 2.4 * 1024 * 1024 });
          if (written >= total) {
            clearInterval(progressTimer);
            emit("wails:updater:verifying", {});
            setTimeout(() => emit("wails:updater:update-ready", {}), 500);
            resolve();
          }
        }, 160);
      });
      return null;
    },
    "main.UpdateService.Restart": async () => {
      await sleep(600);
      emit("runtime:state", clone(runtime));
      return null;
    },
    "main.UpdateService.OpenReleasePage": async () => null,

    "main.SystemService.GetInfo": async () => clone(versionInfo),
    "main.SystemService.FrontendReady": async () => null,
    "main.SystemService.OpenDataDir": async () => null,
    "main.SystemService.OpenInstallDir": async () => null,
    "main.SystemService.OpenURL": async () => null,
  };

  return {
    call: async (method, ...args) => {
      const handler = handlers[method];
      if (!handler) throw new Error(`Mock 模式未实现方法: ${method}`);
      return handler(...args);
    },
    on: (name, callback) => {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(callback);
      return () => {
        listeners.get(name)?.delete(callback);
      };
    },
    clear: () => {
      listeners.clear();
      clearInterval(logTimer);
      clearInterval(progressTimer);
    },
  };
}
