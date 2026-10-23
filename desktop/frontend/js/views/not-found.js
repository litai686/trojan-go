import { el } from "../ui.js";
import { navigate } from "../router.js";

export default function notFoundView(container) {
  container.append(
    el("div", { class: "empty" },
      el("div", { class: "empty-icon", text: "?" }),
      el("div", { class: "empty-title", text: "页面不存在" }),
      el("div", { class: "empty-desc", text: "你访问的页面可能已被移除。" }),
      el("button", { class: "btn primary", type: "button", on: { click: () => navigate("/") } }, "返回首页"),
    ),
  );
}
