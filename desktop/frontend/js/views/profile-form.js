import { call } from "../api.js";
import {
  el,
  toast,
  formatError,
  confirmDialog,
  readClipboard,
  setBusy,
} from "../ui.js";
import { navigate } from "../router.js";

function defaultConfig() {
  return {
    serverAddr: "",
    serverPort: 443,
    password: "",
    sni: "",
    localAddr: "127.0.0.1",
    localPort: 1080,
    mux: false,
    router: false,
    verifyTls: false,
    logLevel: 1,
  };
}

export default async function profileFormView(container, params) {
  const id = params[0] || "";
  let existing = null;

  if (id) {
    try {
      existing = await call("main.ProfileService.Get", id);
    } catch (error) {
      toast(formatError(error), "error");
      navigate("/profiles");
      return;
    }
  }

  const initial = existing ? structuredClone(existing.config) : defaultConfig();
  const initialName = existing?.name || "";
  let dirty = false;
  const importBtn = id
    ? null
    : el("button", { class: "btn", type: "button", on: { click: () => importConfig() } }, "导入配置信息");

  container.append(
    el("div", { class: "page-head" },
      el("div", {},
        el("h1", { class: "page-title", text: id ? "修改配置" : "新增配置" }),
        el("p", { class: "page-subtitle", text: "常用字段会自动生成 trojan-go 的客户端 JSON 配置。" }),
      ),
      el("div", { class: "page-actions" },
        importBtn,
        el("button", { class: "btn ghost", type: "button", on: { click: () => leave() } }, "返回列表"),
      ),
    ),
  );

  const nameInput = el("input", { type: "text", id: "f-name", value: initialName, placeholder: "例如：香港节点", maxlength: "64", autocomplete: "off" });
  const serverInput = el("input", { type: "text", id: "f-server", value: initial.serverAddr, placeholder: "example.com", autocomplete: "off" });
  const serverPortInput = el("input", { type: "number", id: "f-server-port", value: String(initial.serverPort), min: "1", max: "65535" });
  const passwordInput = el("input", { type: "password", id: "f-password", value: initial.password, placeholder: "与服务端一致的密码", autocomplete: "new-password" });
  const passwordToggle = el("button", {
    class: "password-toggle",
    type: "button",
    text: "显示",
    on: {
      click: () => {
        const showing = passwordInput.type === "text";
        passwordInput.type = showing ? "password" : "text";
        passwordToggle.textContent = showing ? "显示" : "隐藏";
      },
    },
  });
  const sniInput = el("input", { type: "text", id: "f-sni", value: initial.sni, placeholder: "留空则使用服务器地址", autocomplete: "off" });
  const localAddrInput = el("input", { type: "text", id: "f-local-addr", value: initial.localAddr, placeholder: "127.0.0.1", autocomplete: "off" });
  const localPortInput = el("input", { type: "number", id: "f-local-port", value: String(initial.localPort), min: "1", max: "65535" });
  const logLevelSelect = el("select", { id: "f-log-level" },
    el("option", { value: "0", text: "0 · Debug（全部）" }),
    el("option", { value: "1", text: "1 · Info" }),
    el("option", { value: "2", text: "2 · Warn" }),
    el("option", { value: "3", text: "3 · Error" }),
    el("option", { value: "4", text: "4 · Fatal" }),
    el("option", { value: "5", text: "5 · Off（关闭）" }),
  );
  logLevelSelect.value = String(initial.logLevel ?? 1);

  const muxInput = el("input", { type: "checkbox", checked: initial.mux });
  const verifyInput = el("input", { type: "checkbox", checked: initial.verifyTls });
  const routerInput = el("input", { type: "checkbox", checked: initial.router });

  const fields = new Map();

  function field(key, label, input, hint, full = false) {
    const error = el("div", { class: "field-error" });
    const wrapper = el("div", { class: `field${full ? " full" : ""}` },
      el("label", { for: input.id || undefined, text: label }),
      input,
      hint ? el("div", { class: "hint", text: hint }) : null,
      error,
    );
    fields.set(key, { wrapper, error });
    return wrapper;
  }

  function fieldError(key, message) {
    const entry = fields.get(key);
    if (!entry) return;
    if (message) {
      entry.wrapper.classList.add("invalid");
      entry.error.textContent = message;
    } else {
      entry.wrapper.classList.remove("invalid");
      entry.error.textContent = "";
    }
  }

  function clearErrors() {
    for (const key of fields.keys()) fieldError(key, "");
  }

  function validate() {
    clearErrors();
    let ok = true;
    const fail = (key, message) => {
      ok = false;
      fieldError(key, message);
    };

    const name = nameInput.value.trim();
    if (!name) fail("name", "请填写配置名称");
    if (!serverInput.value.trim()) fail("server", "请填写服务器地址");
    else if (serverInput.value.includes("://") || /[\s/]/.test(serverInput.value.trim())) fail("server", "只填写域名或 IP，不要包含协议或路径");

    const serverPort = Number(serverPortInput.value);
    if (!Number.isInteger(serverPort) || serverPort < 1 || serverPort > 65535) fail("server-port", "端口范围 1-65535");

    if (!passwordInput.value.trim()) fail("password", "请填写密码");

    if (!localAddrInput.value.trim()) fail("local-addr", "请填写本地监听地址");
    const localPort = Number(localPortInput.value);
    if (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535) fail("local-port", "端口范围 1-65535");

    return ok;
  }

  function collect() {
    return {
      id,
      name: nameInput.value.trim(),
      config: {
        serverAddr: serverInput.value.trim(),
        serverPort: Number(serverPortInput.value),
        password: passwordInput.value,
        sni: sniInput.value.trim(),
        localAddr: localAddrInput.value.trim(),
        localPort: Number(localPortInput.value),
        mux: muxInput.checked,
        router: routerInput.checked,
        verifyTls: verifyInput.checked,
        logLevel: Number(logLevelSelect.value),
      },
    };
  }

  const initialSnapshot = JSON.stringify(collect());
  const checkDirty = () => {
    dirty = JSON.stringify(collect()) !== initialSnapshot;
  };
  for (const input of [nameInput, serverInput, serverPortInput, passwordInput, sniInput, localAddrInput, localPortInput]) {
    input.addEventListener("input", checkDirty);
  }
  for (const input of [muxInput, verifyInput, routerInput, logLevelSelect]) {
    input.addEventListener("change", checkDirty);
  }

  const connectionCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "连接信息" }),
        el("div", { class: "card-subtitle", text: "服务器、密码与本地监听端口" }),
      ),
    ),
    el("div", { class: "card-body" },
      el("div", { class: "form-grid" },
        field("name", "配置名称", nameInput, "用于在首页下拉列表中识别", true),
        field("server", "服务器地址", serverInput, "域名或 IP，不含 https://"),
        field("server-port", "服务器端口", serverPortInput, "通常为 443"),
        field("password", "密码", el("div", { class: "password-wrap" }, passwordInput, passwordToggle), "trojan-go 服务端密码"),
        field("sni", "TLS SNI", sniInput, "留空时使用服务器地址"),
        field("local-addr", "本地监听地址", localAddrInput, "一般保持 127.0.0.1"),
        field("local-port", "本地端口", localPortInput, "浏览器/系统代理指向该端口"),
        field("log-level", "日志级别", logLevelSelect, "日志会显示在首页"),
      ),
    ),
  );

  const advancedCard = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "高级选项" }),
        el("div", { class: "card-subtitle", text: "按服务端能力开启，保持默认即可" }),
      ),
    ),
    el("div", { class: "card-body" },
      el("div", { class: "check-grid" },
        el("label", { class: "check" }, muxInput,
          el("span", { class: "check-text" },
            el("span", { class: "check-title", text: "Mux 多路复用" }),
            el("span", { class: "check-desc", text: "默认关闭；服务端支持时可降低握手开销" }),
          ),
        ),
        el("label", { class: "check" }, verifyInput,
          el("span", { class: "check-text" },
            el("span", { class: "check-title", text: "校验 TLS 证书" }),
            el("span", { class: "check-desc", text: "默认关闭；开启后可校验服务器证书" }),
          ),
        ),
        el("label", { class: "check" }, routerInput,
          el("span", { class: "check-text" },
            el("span", { class: "check-title", text: "路由分流" }),
            el("span", { class: "check-desc", text: "需要 geoip.dat / geosite.dat" }),
          ),
        ),
      ),
    ),
  );

  const saveBtn = el("button", { class: "btn primary", type: "submit" }, id ? "保存修改" : "创建配置");
  const deleteBtn = id
    ? el("button", { class: "btn danger", type: "button", on: { click: () => remove() } }, "删除配置")
    : null;

  const actions = el("div", { class: "form-actions" },
    deleteBtn ? el("div", { class: "left" }, deleteBtn) : null,
    el("button", { class: "btn ghost", type: "button", on: { click: () => leave() } }, "取消"),
    saveBtn,
  );

  const form = el("form", { class: "stack", novalidate: true }, connectionCard, advancedCard, actions);
  container.append(form);

  function leave() {
    if (!dirty) {
      navigate("/profiles");
      return;
    }
    confirmDialog("放弃修改", "当前表单有未保存的修改，确定离开吗？", { confirmText: "离开", danger: true }).then((ok) => {
      if (ok) navigate("/profiles");
    });
  }

  async function decodeToken(token, silent = false) {
    try {
      return await call("main.ProfileService.DecodeConfig", token);
    } catch (error) {
      if (!silent) toast(formatError(error), "error", 5200);
      return null;
    }
  }

  function applyDraft(draft) {
    const cfg = draft.config || {};
    nameInput.value = draft.name || "";
    serverInput.value = cfg.serverAddr || "";
    serverPortInput.value = String(cfg.serverPort || 443);
    passwordInput.type = "password";
    passwordToggle.textContent = "显示";
    passwordInput.value = cfg.password || "";
    sniInput.value = cfg.sni || "";
    localAddrInput.value = cfg.localAddr || "127.0.0.1";
    localPortInput.value = String(cfg.localPort || 1080);
    muxInput.checked = !!cfg.mux;
    verifyInput.checked = !!cfg.verifyTls;
    routerInput.checked = !!cfg.router;
    logLevelSelect.value = String(cfg.logLevel ?? 1);
    validate();
    dirty = true;
  }

  function importDialog(initial = "") {
    return new Promise((resolve) => {
      const root = document.getElementById("modal-root");
      const overlay = el("div", { class: "modal-overlay" });
      const area = el("textarea", {
        class: "modal-textarea",
        placeholder: "粘贴以 TGD1: 开头的密文配置信息",
        value: initial,
      });
      const close = (value) => {
        document.removeEventListener("keydown", onKey);
        overlay.remove();
        resolve(value);
      };
      const onKey = (event) => {
        if (event.key === "Escape") close("");
        if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) close(area.value.trim());
      };
      overlay.append(
        el("div", { class: "modal", role: "dialog", "aria-modal": "true" },
          el("div", { class: "modal-title", text: "导入配置信息" }),
          el("div", { class: "modal-message", text: "粘贴客户端的密文配置信息；导入后请确认内容再保存。" }),
          area,
          el("div", { class: "modal-actions" },
            el("button", { class: "btn ghost", type: "button", on: { click: () => close("") } }, "取消"),
            el("button", { class: "btn primary", type: "button", on: { click: () => close(area.value.trim()) } }, "导入"),
          ),
        ),
      );
      overlay.addEventListener("click", (event) => {
        if (event.target === overlay) close("");
      });
      document.addEventListener("keydown", onKey);
      root.append(overlay);
      area.focus();
      area.select();
    });
  }

  async function importConfig() {
    if (dirty) {
      const ok = await confirmDialog("导入配置信息", "导入会覆盖当前填写的内容，继续吗？", { confirmText: "继续导入" });
      if (!ok) return;
    }
    setBusy(importBtn, true, "导入中…");
    try {
      const shared = (await readClipboard()).trim();
      if (shared.startsWith("TGD1:")) {
        const draft = await decodeToken(shared, true);
        if (draft) {
          applyDraft(draft);
          toast("配置信息已导入，请确认后保存", "success");
          return;
        }
      }
      const token = await importDialog(shared.startsWith("TGD1:") ? shared : "");
      if (!token) return;
      const draft = await decodeToken(token);
      if (!draft) return;
      applyDraft(draft);
      toast("配置信息已导入，请确认后保存", "success");
    } finally {
      setBusy(importBtn, false);
    }
  }

  async function remove() {
    const ok = await confirmDialog("删除配置", `确定删除「${nameInput.value.trim() || "当前配置"}」吗？`, { confirmText: "删除", danger: true });
    if (!ok) return;
    try {
      await call("main.ProfileService.Delete", id);
      toast("配置已删除", "success");
      navigate("/profiles");
    } catch (error) {
      toast(formatError(error), "error");
    }
  }

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!validate()) {
      toast("请检查表单中的错误", "warn");
      return;
    }
    setBusy(saveBtn, true, "保存中…");
    try {
      await call("main.ProfileService.Save", collect());
      dirty = false;
      toast(id ? "配置已保存" : "配置已创建", "success");
      navigate("/profiles");
    } catch (error) {
      setBusy(saveBtn, false);
      toast(formatError(error), "error", 5200);
    }
  });
}
