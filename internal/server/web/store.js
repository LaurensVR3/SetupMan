// App-wide state: settings, live status, and a version counter that goes up
// whenever the server reports a change, so pages know to reload.
import { reactive } from "vue";
import { get } from "./api.js";

export const store = reactive({
  settings: null,
  live: "unknown",
  app: {}, // the running build: number, commit, built
  liveGame: "", // short name of the running game, e.g. "ACC"
  version: 0,
  lapVersion: 0, // goes up when a lap is recorded (only pages that show laps reload, D-138)
  offline: false, // true while SetupMan.exe is not reachable
  toast: "",      // short message at the bottom of the screen
  crumbs: [], // set by each page: [{ label, to }]
  dropHint: "", // shown full-page while files are dragged over a page that accepts them
});

let toastTimer;
export function showToast(msg, ms = 4500) {
  store.toast = msg;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (store.toast = ""), ms);
}

// After a staging change: say what happened in plain words (spec D-79).
export function stagingDone(res, done) {
  if (res && res.queued) showToast("Queued: this change will be applied when you stop driving.");
  else if (res && res.gameRunning) showToast(done + " " + (store.liveGame || "The game") + " may need its setup screen reopened to show the change.", 6500);
  else showToast(done);
}

export function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === "light" || theme === "dark") root.dataset.theme = theme;
  else delete root.dataset.theme; // follow Windows
}

export async function loadSettings() {
  store.settings = await get("/api/settings");
  applyTheme(store.settings.theme);
}

// Server-Sent Events: the server pushes changes; nothing polls.
// If SetupMan is closed, the browser keeps trying to reconnect; when it is back,
// the page reloads its data by itself.
export async function listen() {
  const ping = await get("/api/ping");
  const first = ping.started;
  store.app = { ...(ping.version || {}), host: ping.host || "", lastOpened: ping.lastOpened || null };
  const es = new EventSource("/api/events");
  es.onopen = async () => {
    if (!store.offline) return;
    store.offline = false;
    const now = (await get("/api/ping").catch(() => ({}))).started;
    if (now && now !== first) location.reload(); // SetupMan was restarted, maybe updated
    else store.version++;
  };
  es.onmessage = (msg) => {
    const e = JSON.parse(msg.data);
    if (e.type === "live") { store.live = e.live; store.liveGame = e.liveGame || ""; }
    else if (e.type === "recording") store.lapVersion++;
    else store.version++;
  };
  es.onerror = () => { store.offline = true; };
}
