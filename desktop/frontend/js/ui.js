export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") {
      node.className = value;
    } else if (key === "text") {
      node.textContent = value;
    } else if (key === "dataset") {
      Object.assign(node.dataset, value);
    } else if (key === "style" && typeof value === "object") {
      Object.assign(node.style, value);
    } else if (key === "on") {
      for (const [event, handler] of Object.entries(value)) {
        node.addEventListener(event, handler);
      }
    } else if (key in node) {
      node[key] = value;
    } else {
      node.setAttribute(key, value);
    }
  }
  append(node, children);
  return node;
}

export function append(node, children) {
  for (const child of children.flat(Infinity)) {
    if (child === undefined || child === null || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

export function toast(message, type = "info", timeout = 3600) {
  const stack = document.getElementById("toast-stack");
  if (!stack) return;
  const node = el("div", { class: `toast ${type}` }, message);
  stack.append(node);
  const remove = () => {
    node.classList.add("hide");
    setTimeout(() => node.remove(), 200);
  };
  node.addEventListener("click", remove);
  setTimeout(remove, timeout);
}

export function confirmDialog(title, message, options = {}) {
  const { confirmText = "确定", cancelText = "取消", danger = false } = options;
  return new Promise((resolve) => {
    const root = document.getElementById("modal-root");
    const overlay = el("div", { class: "modal-overlay" });
    const close = (value) => {
      document.removeEventListener("keydown", onKey);
      overlay.remove();
      resolve(value);
    };
    const onKey = (event) => {
      if (event.key === "Escape") close(false);
      if (event.key === "Enter") close(true);
    };
    const confirmBtn = el("button", { class: `btn ${danger ? "danger" : "primary"}`, type: "button", text: confirmText, on: { click: () => close(true) } });
    const cancelBtn = el("button", { class: "btn ghost", type: "button", text: cancelText, on: { click: () => close(false) } });
    overlay.append(
      el("div", { class: "modal", role: "dialog", "aria-modal": "true" },
        el("div", { class: "modal-title", text: title }),
        el("div", { class: "modal-message", text: message }),
        el("div", { class: "modal-actions" }, cancelBtn, confirmBtn),
      ),
    );
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) close(false);
    });
    document.addEventListener("keydown", onKey);
    root.append(overlay);
    confirmBtn.focus();
  });
}

export function formatError(error) {
  if (!error) return "未知错误";
  if (typeof error === "string") return error;
  if (error.message) return error.message;
  if (error.cause) return String(error.cause);
  return String(error);
}

export function formatBytes(bytes) {
  if (!bytes || bytes < 0) return "-";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function formatTime(ms) {
  if (!ms) return "--:--:--";
  const date = new Date(ms);
  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

export function formatDate(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString("zh-CN", { hour12: false });
}

export function formatUptime(startedAt) {
  if (!startedAt) return "-";
  const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) return `${h} 小时 ${String(m).padStart(2, "0")} 分`;
  if (m > 0) return `${m} 分 ${String(s).padStart(2, "0")} 秒`;
  return `${s} 秒`;
}

const statusLabels = {
  stopped: "已停止",
  starting: "启动中",
  running: "运行中",
  stopping: "停止中",
  error: "异常",
};

export function statusLabel(status) {
  return statusLabels[status] || status || "未知";
}

export function applyTheme(setting) {
  const root = document.documentElement;
  root.dataset.themeSetting = setting || "system";
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const resolve = () => {
    if (root.dataset.themeSetting === "light" || root.dataset.themeSetting === "dark") {
      return root.dataset.themeSetting;
    }
    return media.matches ? "dark" : "light";
  };
  root.dataset.theme = resolve();
  if (!applyTheme.listening) {
    applyTheme.listening = true;
    media.addEventListener("change", () => {
      if (root.dataset.themeSetting === "system") {
        root.dataset.theme = media.matches ? "dark" : "light";
      }
    });
  }
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand("copy");
    } catch {
      ok = false;
    }
    area.remove();
    return ok;
  }
}

export async function readClipboard() {
  try {
    return await navigator.clipboard.readText();
  } catch {
    return "";
  }
}

export function debounce(fn, wait = 150) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
}

export function setBusy(button, busy, label = "处理中…") {
  if (!button) return;
  if (busy) {
    if (!button.dataset.original) {
      button.dataset.original = button.textContent;
      button.dataset.originalDisabled = String(button.disabled);
    }
    button.disabled = true;
    clear(button);
    button.append(el("span", { class: "spinner" }), document.createTextNode(label));
  } else {
    button.disabled = button.dataset.originalDisabled === "true";
    clear(button);
    button.append(document.createTextNode(button.dataset.original || "确定"));
    delete button.dataset.original;
    delete button.dataset.originalDisabled;
  }
}

export function statusDotClass(status) {
  return status || "stopped";
}
