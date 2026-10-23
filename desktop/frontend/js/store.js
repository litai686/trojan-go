export const state = {
  profiles: [],
  settings: null,
  runtime: { status: "stopped" },
  version: null,
  update: null,
  mock: false,
};

const listeners = new Set();

export function setState(patch) {
  Object.assign(state, patch);
  for (const listener of listeners) listener(state);
}

export function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
