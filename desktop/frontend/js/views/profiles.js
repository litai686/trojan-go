import { call, on } from "../api.js";
import { state, setState } from "../store.js";
import {
  el,
  clear,
  toast,
  formatError,
  formatDate,
  confirmDialog,
  copyText,
} from "../ui.js";
import { navigate } from "../router.js";

export default async function profilesView(container) {
  let profiles = state.profiles || [];
  let runtime = state.runtime || { status: "stopped" };
  let search = "";

  try {
    [profiles, runtime] = await Promise.all([
      call("main.ProfileService.List"),
      call("main.RuntimeService.GetState"),
    ]);
    setState({ profiles, runtime });
  } catch (error) {
    toast(formatError(error), "error");
  }

  container.append(
    el("div", { class: "page-head" },
      el("div", {},
        el("h1", { class: "page-title", text: "配置" }),
        el("p", { class: "page-subtitle", text: "管理多份 trojan-go 客户端配置，随时切换。" }),
      ),
      el("div", { class: "page-actions" },
        el("button", { class: "btn primary", type: "button", on: { click: () => navigate("/profiles/new") } }, "新增配置"),
      ),
    ),
  );

  const searchInput = el("input", {
    type: "text",
    class: "list-search",
    placeholder: "搜索名称或服务器…",
    on: {
      input: () => {
        search = searchInput.value.trim().toLowerCase();
        renderRows();
      },
    },
  });

  const tbody = el("tbody");
  const table = el("table", {},
    el("thead", {},
      el("tr", {},
        el("th", { text: "名称" }),
        el("th", { text: "服务器" }),
        el("th", { text: "本地端口" }),
        el("th", { text: "更新时间" }),
        el("th", { text: "状态" }),
        el("th", { text: "", style: { textAlign: "right" } }),
      ),
    ),
    tbody,
  );
  const tableWrap = el("div", { class: "table-wrap" }, table);

  const card = el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("div", { class: "card-title", text: "配置列表" }),
        el("div", { class: "card-subtitle", text: `${profiles.length} 个配置` }),
      ),
      el("div", { class: "toolbar" }, searchInput),
    ),
    tableWrap,
  );

  const empty = el("section", { class: "card" },
    el("div", { class: "empty" },
      el("div", { class: "empty-icon", text: "＋" }),
      el("div", { class: "empty-title", text: "还没有配置" }),
      el("div", { class: "empty-desc", text: "新增一份配置后，就可以在首页选择并启动代理。" }),
      el("button", { class: "btn primary", type: "button", on: { click: () => navigate("/profiles/new") } }, "新增配置"),
    ),
  );

  container.append(profiles.length ? card : empty);

  function isActive(profile) {
    return runtime.profileId === profile.id && ["running", "starting", "stopping"].includes(runtime.status);
  }

  function filtered() {
    if (!search) return profiles;
    return profiles.filter((profile) => {
      const haystack = `${profile.name} ${profile.config.serverAddr}`.toLowerCase();
      return haystack.includes(search);
    });
  }

  function renderRows() {
    clear(tbody);
    const list = filtered();
    if (list.length === 0) {
      tbody.append(el("tr", {},
        el("td", { colspan: "6", style: { textAlign: "center", color: "var(--muted)", padding: "30px" }, text: "没有匹配的配置" }),
      ));
      return;
    }
    for (const profile of list) {
      const badge = el("span", {
        class: `badge ${isActive(profile) ? "running" : "stopped"}`,
        text: isActive(profile) ? "当前使用" : "未使用",
      });
      const actions = el("div", { class: "row-actions" },
        el("button", { class: "btn small", type: "button", on: { click: () => navigate(`/profiles/${profile.id}/edit`) } }, "编辑"),
        el("button", { class: "btn small", type: "button", title: "复制密文配置信息", on: { click: () => copyConfig(profile) } }, "复制配置"),
        el("button", { class: "btn small danger", type: "button", on: { click: () => remove(profile) } }, "删除"),
      );

      tbody.append(el("tr", {},
        el("td", { class: "name", title: profile.name, text: profile.name }),
        el("td", { class: "mono", text: `${profile.config.serverAddr}:${profile.config.serverPort}` }),
        el("td", { class: "mono", text: `${profile.config.localAddr}:${profile.config.localPort}` }),
        el("td", { class: "muted", text: formatDate(profile.updatedAt) }),
        el("td", {}, badge),
        el("td", {}, actions),
      ));
    }
  }

  async function copyConfig(profile) {
    try {
      const token = await call("main.ProfileService.ExportConfig", profile.id);
      if (await copyText(token)) {
        toast("配置信息已复制（密文）", "success");
      } else {
        toast("复制失败，请检查剪贴板权限", "error");
      }
    } catch (error) {
      toast(formatError(error), "error", 5200);
    }
  }

  async function remove(profile) {
    const ok = await confirmDialog(
      "删除配置",
      `确定删除「${profile.name}」吗？如果它正在运行，会先停止代理。`,
      { confirmText: "删除", danger: true },
    );
    if (!ok) return;
    try {
      await call("main.ProfileService.Delete", profile.id);
      toast("配置已删除", "success");
    } catch (error) {
      toast(formatError(error), "error");
    }
  }

  const unsubscribeProfiles = on("profiles:changed", async () => {
    try {
      profiles = await call("main.ProfileService.List");
      setState({ profiles });
      renderRows();
    } catch (error) {
      console.error(error);
    }
  });

  const unsubscribeRuntime = on("runtime:state", (event) => {
    runtime = event.data || runtime;
    setState({ runtime });
    renderRows();
  });

  renderRows();

  return () => {
    unsubscribeProfiles();
    unsubscribeRuntime();
  };
}
