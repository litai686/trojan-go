let runtime = null;
let mock = null;

export async function initApi() {
  try {
    runtime = await import("/wails/runtime.js");
    return false;
  } catch (error) {
    console.warn("Wails runtime unavailable, using browser mock:", error);
    const mod = await import("./mock.js");
    mock = mod.createMock();
    return true;
  }
}

export function isMock() {
  return mock !== null;
}

export function call(method, ...args) {
  if (runtime) {
    return runtime.Call.ByName(method, ...args);
  }
  return mock.call(method, ...args);
}

export function on(event, callback) {
  if (runtime) {
    return runtime.Events.On(event, callback);
  }
  return mock.on(event, callback);
}

export function offAll() {
  if (runtime) {
    runtime.Events.OffAll();
  } else if (mock) {
    mock.clear();
  }
}
