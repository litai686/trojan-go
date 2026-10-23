export function navigate(path) {
  const hash = path.startsWith("#") ? path : `#${path}`;
  if (window.location.hash === hash) {
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    return;
  }
  window.location.hash = hash;
}

export function createRouter({ container, routes, onRoute }) {
  let cleanup = null;
  let token = 0;
  let disposer = null;

  const path = () => {
    const raw = window.location.hash.replace(/^#/, "");
    return raw || "/";
  };

  const render = async () => {
    const current = path();
    const currentToken = ++token;
    const matched = routes
      .map((route) => ({ route, match: route.pattern.exec(current) }))
      .find((item) => item.match);

    const route = matched?.route;
    const params = matched?.match?.slice(1).map((value) => decodeURIComponent(value)) || [];

    if (typeof cleanup === "function") {
      try {
        cleanup();
      } catch (error) {
        console.error("view cleanup failed", error);
      }
      cleanup = null;
    }

    if (onRoute) onRoute(route?.nav || "");

    const page = document.createElement("div");
    page.className = "page";
    const result = await route.view(page, params);
    if (currentToken !== token) {
      if (typeof result === "function") result();
      return;
    }
    container.replaceChildren(page);
    cleanup = typeof result === "function" ? result : null;
    window.scrollTo({ top: 0 });
  };

  const onHash = () => render();
  window.addEventListener("hashchange", onHash);
  disposer = () => window.removeEventListener("hashchange", onHash);
  render();

  return () => {
    if (typeof cleanup === "function") cleanup();
    if (disposer) disposer();
  };
}
