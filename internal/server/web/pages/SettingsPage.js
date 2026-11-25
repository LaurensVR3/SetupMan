// Settings: appearance, duplicate threshold, games (visibility + folder), logos (spec D-33, D-36, D-37).
// Every change is saved straight away.
import { ref, reactive, computed, onMounted, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import { get, put, post, enc, api } from "../api.js";
import { store, applyTheme, showToast } from "../store.js";
import { dateTime } from "../format.js";
import { setCrumbs } from "../components/common.js";
import { FolderPicker } from "../components/FolderPicker.js";
import { FamilyTree } from "../components/FamilyTree.js";
import { LastOpenedNotice } from "../components/LastOpenedNotice.js";
import { TeleLapsDialog } from "../components/TeleLaps.js";

const TABS = [["general", "General"], ["games", "Games"], ["telemetry", "Telemetry"], ["compare", "Comparing & history"], ["backups", "Backups & bin"], ["network", "Network"], ["about", "About"]];

// About → Features per game: true = yes, false = no, text = yes with a remark.
const FEATURE_GAMES = ["ACC", "AC", "AC EVO", "AC Rally"];
const FEATURES = [
  ["Read, compare and keep every version", true, true, true, true],
  ["Change values", true, true, false, false],
  ["Values in real units (degrees, mm, N/m)", true, "With AC installed", true, "As stored"],
  ["Choose which setups the game lists", true, true, true, false],
  ["Rename, remove duplicates, naming standard", true, true, true, false],
  ["Record laps and recognise the setup driven", true, "No valid-lap flag", false, false],
  ["Pit stop advice", true, true, false, false],
  ["Adjust for temperature", true, true, false, false],
  ["Race prep and fuel calculator", true, true, true, false],
  ["Share and import", true, true, true, false],
  ["Download and backups", true, true, true, true],
];

export const SettingsPage = {
  components: { FolderPicker, FamilyTree, LastOpenedNotice, TeleLapsDialog },
  setup() {
    setCrumbs([{ label: "Settings", to: "/settings" }]);
    const games = ref([]);
    const teleGames = computed(() => games.value.filter((g) => g.telemetry || g.extraLog)); // games whose telemetry SetupMan reads (D-190)
    const draft = reactive({ folders: {} }); // folder text fields, per game
    const checks = reactive({});            // folder check results, per game
    const route = useRoute();
    const router = useRouter();
    const tab = computed(() => (TABS.some((t) => t[0] === route.query.tab) ? route.query.tab : "general"));
    const setTab = (t) => router.replace({ query: { ...route.query, tab: t === "general" ? undefined : t, game: undefined } });
    // Games tab: one tab per game (user request), in the address as &game=acc. Default: the
    // first game found on this PC.
    const gameTab = computed(() => {
      const list = games.value;
      if (list.some((g) => g.id === route.query.game)) return route.query.game;
      return (list.find((g) => g.installed) || list[0])?.id || "";
    });
    const setGameTab = (id) => router.replace({ query: { ...route.query, game: id } });
    const saved = ref("");
    const error = ref("");
    const picker = reactive({ open: false, game: null, kind: "setups", start: "" });
    let savedTimer;

    const s = computed(() => store.settings);

    async function loadGames() {
      games.value = (await get("/api/games")).games;
      for (const g of games.value) {
        if (!(g.id in draft.folders)) draft.folders[g.id] = store.settings.games[g.id]?.folder || "";
      }
    }
    // Start with Windows (spec D-71): stored by Windows, not in SetupMan's settings file.
    const autostart = reactive({ supported: false, enabled: false });
    const loadAutostart = async () => Object.assign(autostart, await get("/api/autostart"));
    async function setAutostart(on) {
      try {
        Object.assign(autostart, await put("/api/autostart", { enabled: on }));
        flashSaved();
      } catch (e) {
        error.value = e.message;
      }
    }
    // Staging management (spec D-73, D-74) and folder backups (D-81, D-82).
    const manage = reactive({ busy: false, game: null, result: null, error: "" });
    async function setManaged(g, on) {
      manage.busy = true;
      manage.game = g.id;
      manage.result = null;
      manage.error = "";
      try {
        manage.result = await post(`/api/games/${enc(g.id)}/manage`, { on });
        await loadGames();
        await loadBackups();
      } catch (e) {
        manage.error = e.message;
        await loadGames();
      } finally {
        manage.busy = false;
      }
    }
    const setTidy = (id, on) => save((n) => { gameCfg(n, id).tidyOnGameClose = on; });
    const setAutoRename = (id, on) => save((n) => { gameCfg(n, id).autoRename = on; });

    const backups = reactive({});
    const backupDirs = reactive({});
    async function openFolder(which) {
      try {
        const r = await post("/api/open-folder", { which });
        showToast(r.local ? "The folder is open in Windows Explorer." : "The folder was opened in Windows Explorer on the PC SetupMan runs on.");
      } catch (e) { showToast(e.message, 7000); }
    }
    const backupBusy = ref(false);
    async function loadBackups() {
      for (const g of games.value) {
        if (g.folder) { const r = await get(`/api/games/${enc(g.id)}/backups`); backups[g.id] = r.backups; backupDirs[g.id] = r.dir || ""; }
      }
    }
    async function createBackup(id) {
      backupBusy.value = true;
      try {
        await post(`/api/games/${enc(id)}/backups`);
        await loadBackups();
        showToast("Backup created.");
      } catch (e) {
        showToast(e.message, 7000);
      } finally {
        backupBusy.value = false;
      }
    }
    const confirmDlg = ref(null);
    // Set telemetry laps to 99 (D-176): one dialog, for the game whose button was pressed
    const teleLapsDlg = ref(null);
    const teleLapsGame = reactive({ id: "", short: "" });
    async function openTeleLaps(g) { teleLapsGame.id = g.id; teleLapsGame.short = g.short; await nextTick(); teleLapsDlg.value.open(); }
    const restore = reactive({ open: false, game: null, backup: null, plan: null, error: "", busy: false });
    async function askRestore(g, b) {
      Object.assign(restore, { open: true, game: g, backup: b, plan: null, error: "", busy: false });
      confirmDlg.value.showModal();
      try {
        restore.plan = await get(`/api/games/${enc(g.id)}/backups/${enc(b.id)}/plan`);
      } catch (e) {
        restore.error = e.message;
      }
    }
    const closeRestore = () => confirmDlg.value.close();
    async function doRestore() {
      restore.busy = true;
      try {
        await post(`/api/games/${enc(restore.game.id)}/backups/${enc(restore.backup.id)}/restore`);
        closeRestore();
        showToast(`The ${restore.game.short} setups folder is back to ${dateTime(restore.backup.createdAt)}.`, 6000);
        await loadBackups();
      } catch (e) {
        restore.error = e.message;
      } finally {
        restore.busy = false;
      }
    }

    // Full SetupMan backups (D-82, D-92).
    const full = reactive({ list: [], dir: "", busy: false, asking: null, error: "" });
    const fullDlg = ref(null);
    const loadFull = async () => { const r = await get("/api/full-backups").catch(() => ({ backups: [] })); full.list = r.backups; full.dir = r.dir || ""; };
    const size = (n) => (n >= 1 << 20 ? (n / (1 << 20)).toFixed(1) + " MB" : Math.max(1, Math.round(n / 1024)) + " KB");
    async function createFull() {
      full.busy = true;
      try { await post("/api/full-backups"); await loadFull(); await loadBackups(); showToast("Full backup created."); }
      catch (e) { showToast(e.message, 7000); } finally { full.busy = false; }
    }
    async function uploadFull(ev) {
      const f = ev.target.files[0];
      ev.target.value = "";
      if (!f) return;
      full.busy = true;
      try {
        const data = await new Promise((ok, fail) => { const r = new FileReader(); r.onload = () => ok(String(r.result).split(",")[1] || ""); r.onerror = () => fail(new Error("Could not read " + f.name)); r.readAsDataURL(f); });
        const b = await post("/api/full-backups/upload", { data });
        await loadFull();
        askFull(b);
      } catch (e) { showToast(e.message, 7000); } finally { full.busy = false; }
    }
    function askFull(b) { full.asking = b; full.error = ""; fullDlg.value.showModal(); }
    async function doFull() {
      full.busy = true;
      try {
        await post(`/api/full-backups/${encodeURIComponent(full.asking.id)}/restore`);
        showToast("Restoring: SetupMan restarts and this page reloads by itself.", 10000);
      } catch (e) { full.error = e.message; full.busy = false; }
    }

    // Network access (D-100).
    const network = reactive({ allow: false, local: true, pin: "", requirePin: true, addresses: [], port: 0, listening: false, busy: false, hostName: "", newHostName: "" });
    const loadNetwork = async () => { const r = await get("/api/network").catch(() => null); if (r) { Object.assign(network, r); network.newPort = r.port; network.newHostName = r.hostName || ""; } };
    async function setNetwork(allow, newPin) {
      if (!allow && !network.local && !confirm("This computer will lose access to SetupMan until network access is enabled again on the PC it runs on. Continue?")) {
        network.allow = true;
        return;
      }
      network.busy = true;
      try {
        const r = await post("/api/network", { allow, newPin });
        network.allow = allow; network.pin = r.pin || "";
        if (r.restarting) showToast("SetupMan is restarting to " + (allow ? "enable" : "disable") + " network access; this page reloads automatically.", 8000);
        else loadNetwork();
      } catch (e) { showToast(e.message, 7000); } finally { network.busy = false; }
    }
    async function setPort() {
      const port = Number(network.newPort);
      network.busy = true;
      try {
        const r = await post("/api/network", { allow: network.allow, port });
        if (r.restarting && viaName()) {
          // Opened through the network name: the proxy must be pointed at the new port (D-129).
          showToast(`SetupMan restarts on port ${port}. Change the forward port in your proxy (e.g. Nginx Proxy Manager) to ${port}.`, 12000);
        } else if (r.restarting) {
          showToast(`SetupMan restarts on port ${port}; this page moves there by itself.`, 8000);
          setTimeout(() => { location.href = `${location.protocol}//${location.hostname}:${port}/#/settings`; }, 3500);
        }
      } catch (e) { showToast(e.message, 7000); network.busy = false; }
    }
    // PIN on or off for devices on the network (D-139). Switching it off asks first.
    const pinDlg = ref(null);
    function askRequirePin(on) {
      if (on) setRequirePin(true);
      else pinDlg.value.showModal();
    }
    async function setRequirePin(on) {
      network.busy = true;
      try {
        const r = await post("/api/network", { allow: network.allow, requirePin: on });
        network.requirePin = r.requirePin; network.pin = r.pin || network.pin;
        pinDlg.value?.close();
        showToast(on ? "Other devices need the PIN again." : "Devices on your network can now use SetupMan without the PIN.", 6000);
      } catch (e) { showToast(e.message, 7000); } finally { network.busy = false; }
    }

    // Network name for a reverse proxy, e.g. setupman.lan (D-129).
    const viaName = () => !!network.hostName && location.hostname.toLowerCase() === network.hostName;
    async function setHostName() {
      const name = (network.newHostName || "").trim();
      if (viaName() && name.toLowerCase() !== network.hostName &&
        !confirm(`This page is open through ${network.hostName}. After the change, that address stops working. Continue?`)) return;
      network.busy = true;
      try {
        const r = await post("/api/network", { allow: network.allow, hostName: name });
        network.hostName = r.hostName || ""; network.newHostName = network.hostName;
        showToast(network.hostName ? `Network name set to ${network.hostName}.` : "Network name removed.");
      } catch (e) { showToast(e.message, 7000); } finally { network.busy = false; }
    }

    // The bin (D-102).
    const bin = reactive({ list: [], days: 30 });
    const loadBin = async () => { const r = await get("/api/bin").catch(() => null); if (r) { bin.list = r.bin; bin.days = r.days; } };
    async function undelete(b) {
      try { await post(`/api/setups/${b.id}/undelete`); showToast(`${b.name} restored. It is hidden from the game.`); loadBin(); }
      catch (e) { showToast(e.message, 7000); }
    }

    onMounted(async () => { await loadGames(); loadAutostart(); loadBackups(); loadFull(); loadBin(); loadNetwork(); loadTempAuthors(); });
    watch(() => store.version, loadAutostart); // changed from the tray menu

    function flashSaved() {
      saved.value = "Saved";
      clearTimeout(savedTimer);
      savedTimer = setTimeout(() => (saved.value = ""), 1800);
    }

    async function save(change) {
      const next = JSON.parse(JSON.stringify(store.settings));
      change(next);
      try {
        store.settings = await put("/api/settings", next);
        applyTheme(store.settings.theme);
        error.value = "";
        flashSaved();
        await loadGames();
      } catch (e) {
        error.value = e.message;
      }
    }
    const gameCfg = (next, id) => (next.games[id] ||= { visibility: "auto", folder: "" });

    const setTheme = (t) => save((n) => { n.theme = t; });
    const setVisibility = (id, v) => save((n) => { gameCfg(n, id).visibility = v; });
    const setLogos = (on) => save((n) => { n.downloadLogos = on; });
    // LFM ID for the next race in Race prep (D-142).
    const lfmId = ref(store.settings?.lfmId || "");
    const saveLfm = () => save((n) => { n.lfmId = String(lfmId.value || "").trim(); });
    // 3D telemetry (D-190): the user's own MapTiler key, and the offline aerial photos kept per track
    const mapTilerKey = ref(store.settings?.mapTilerKey || "");
    const showKey = ref(false);
    const saveMapTiler = () => save((n) => { n.mapTilerKey = String(mapTilerKey.value || "").trim(); });
    const keptPhotos = ref({});
    const loadKeptPhotos = () => get("/api/maps3d").then((v) => (keptPhotos.value = v || {})).catch(() => {});
    loadKeptPhotos();
    const trackLabel = (id) => id.split("_").map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
    const kb = (n) => (n >= 1 << 20 ? (n / (1 << 20)).toFixed(1) + " MB" : Math.max(1, Math.round(n / 1024)) + " KB");
    async function deleteKept(track) { // photos belong to the track, whichever game drove it
      try { await api("DELETE", `/api/telemetry/3d/acc/${enc(track)}/photos`); await loadKeptPhotos(); }
      catch (e) { showToast(e.message); }
    }
    const initials = ref(store.settings?.initials || "");
    const saveInitials = () => save((n) => { n.initials = String(initials.value || "").trim().toUpperCase(); }).then(loadTempAuthors);
    // Temperatures in setup names, per author (D-146): there is no standard, so each author's way
    // of writing them can be set; "*" is everyone else and names without initials.
    const FOUR = [["air-track", "Air, then track (3040 = air 30 °C, track 40 °C)"], ["range", "A range (2327 = 23 to 27 °C)"]];
    const SINGLE = [["air", "Air temperature"], ["track", "Track temperature"], ["either", "Not said (air or track)"]];
    const label = (list, v) => (list.find((x) => x[0] === v) || ["", ""])[1];
    const tempAuthors = ref(null);
    const tempWords = ref([]);
    const newWord = ref("");
    async function loadTempAuthors() {
      try {
        const r = await get("/api/naming/temperatures");
        tempAuthors.value = r.authors; tempWords.value = r.words || [];
      } catch { tempAuthors.value = []; tempWords.value = []; }
    }
    // A rule for a word in names (an author without initials in front, e.g. "CDA"): it starts as
    // the default and can then be changed.
    async function addWordRule(w) {
      const key = String(w || "").trim().toUpperCase();
      if (!key) return;
      await save((n) => { n.tempRules ||= {}; n.tempRules[key] = { ...defaultRule.value }; });
      newWord.value = "";
      loadTempAuthors();
    }
    async function removeRule(key) {
      await save((n) => { if (n.tempRules) delete n.tempRules[key]; });
      loadTempAuthors();
    }
    const defaultRule = computed(() => {
      const r = (store.settings?.tempRules || {})["*"] || {};
      return { four: r.four || "air-track", single: r.single || "air" };
    });
    const ownRule = (key) => (store.settings?.tempRules || {})[key] || {};
    async function setTempRule(key, field, value) {
      await save((n) => {
        n.tempRules ||= {};
        const r = { ...(n.tempRules[key] || {}) };
        if (value) r[field] = value; else delete r[field];
        if (r.four || r.single) n.tempRules[key] = r; else delete n.tempRules[key];
      });
      loadTempAuthors();
    }
    const fitText = (f) => {
      const parts = [];
      if (f.airTrack) parts.push(`${f.airTrack} fit air and track`);
      if (f.range) parts.push(`${f.range} fit a range`);
      if (f.air) parts.push(`${f.air} fit the air temperature`);
      if (f.track) parts.push(`${f.track} fit the track temperature`);
      return parts.length ? "Recorded laps: " + parts.join(", ") + "." : "";
    };
    const setEndurance = (id, v) => save((n) => { gameCfg(n, id).enduranceMinutes = Math.max(5, Math.min(1440, Number(v) || 45)); });
    // Planned standing time at a pit stop (D-192): kept within 5–300 s; empty or not a number = the
    // default (0 = 30 s). The box shows the value actually kept.
    const setPitStill = (id, el) => {
      const x = Math.round(Number(el.value));
      const sec = String(el.value).trim() === "" || !Number.isFinite(x) ? 0 : Math.min(300, Math.max(5, x));
      el.value = sec || 30;
      save((n) => { gameCfg(n, id).pitStillSeconds = sec; });
    };
    // Pit stop advice targets (D-124); empty or invalid = the default.
    const setTarget = (id, key, v) => save((n) => { const x = Math.round(Number(v) * 10) / 10; if (x >= 15 && x <= 40) gameCfg(n, id)[key] = x; else delete gameCfg(n, id)[key]; });
    let thrTimer;
    function setThreshold(v) {
      store.settings.threshold = Number(v); // show the new value immediately
      clearTimeout(thrTimer);
      thrTimer = setTimeout(() => save((n) => { n.threshold = Number(v); }), 400);
    }

    async function useFolder(id, folder) {
      draft.folders[id] = folder;
      if (!folder) {
        delete checks[id];
        return save((n) => { gameCfg(n, id).folder = ""; });
      }
      checks[id] = { message: "Checking…" };
      try {
        checks[id] = await post(`/api/games/${enc(id)}/check-folder`, { folder });
      } catch (e) {
        checks[id] = { ok: false, message: e.message };
      }
      if (checks[id].ok) await save((n) => { gameCfg(n, id).folder = folder; });
    }
    function browse(g) {
      picker.game = g.id;
      picker.kind = "setups";
      picker.start = draft.folders[g.id] || g.folder || g.detectedFolder;
      picker.open = true;
    }
    function picked(path) {
      picker.open = false;
      if (picker.kind === "telemetry") useTeleFolder(picker.game, path);
      else useFolder(picker.game, path);
    }
    // Telemetry (D-160, D-162): the folder the game writes its telemetry to (only read), and the extra log.
    const teleDraft = reactive({});
    const teleValue = (g) => (g.id in teleDraft ? teleDraft[g.id] : store.settings.games[g.id]?.telemetryFolder || "");
    const useTeleFolder = (id, folder) => { teleDraft[id] = folder; return save((n) => { gameCfg(n, id).telemetryFolder = folder; }); };
    function browseTele(g) {
      picker.game = g.id;
      picker.kind = "telemetry";
      picker.start = teleValue(g) || g.detectedTelemetry || "";
      picker.open = true;
    }
    const setExtraLog = (id, on) => save((n) => { gameCfg(n, id).extraLogOff = !on; });

    // History rebuild preview (spec D-62).
    const hist = reactive({
      threshold: store.settings.historyThreshold, nameThreshold: store.settings.nameThreshold,
      game: null, result: null, error: "", busy: false, applied: false,
    });
    async function runHistory(gameId, apply) {
      hist.busy = true;
      hist.error = "";
      hist.applied = false;
      try {
        hist.result = await post(`/api/games/${enc(gameId)}/history/${apply ? "apply" : "preview"}`,
          { threshold: hist.threshold, nameThreshold: hist.nameThreshold });
        hist.game = gameId;
        if (apply) {
          hist.applied = true;
          await save((n) => { n.historyThreshold = hist.threshold; n.nameThreshold = hist.nameThreshold; });
        }
      } catch (e) {
        hist.error = e.message;
      } finally {
        hist.busy = false;
      }
    }
    const preview = (id) => runHistory(id, false);
    const applyHistory = (id) => runHistory(id, true);
    const countNodes = (grp) => {
      const walk = (n) => 1 + n.children.reduce((a, c) => a + walk(c), 0);
      return grp.roots.reduce((a, r) => a + walk(r), 0);
    };

    const visLabel = { auto: "When installed", show: "Always", hide: "Never" };
    // GitHub issue forms (D-156); the forms read ?version= into their version field.
    const issueUrl = (form) => `https://github.com/LaurensVR3/SetupMan/issues/new?template=${form}&version=${encodeURIComponent(store.app?.number || "")}`;
    return { issueUrl, lfmId, saveLfm, mapTilerKey, showKey, saveMapTiler, keptPhotos, trackLabel, kb, deleteKept, teleGames, initials, saveInitials, FOUR, SINGLE, label, tempAuthors, tempWords, newWord, addWordRule, removeRule, defaultRule, ownRule, setTempRule, fitText, s, games, draft, checks, saved, error, picker, setTheme, setVisibility, setLogos, setThreshold, useFolder, browse, picked, visLabel,
      hist, preview, applyHistory, countNodes, setEndurance, autostart, setAutostart,
      manage, setManaged, setTidy, setAutoRename, teleLapsDlg, teleLapsGame, openTeleLaps, backups, backupBusy, createBackup, confirmDlg, restore, askRestore, closeRestore, doRestore, dateTime,
      teleValue, useTeleFolder, browseTele, setExtraLog, full, fullDlg, size, createFull, uploadFull, askFull, doFull, bin, undelete, backupDirs, openFolder, network, setNetwork, setPort, setHostName, pinDlg, askRequirePin, setRequirePin, setTarget, setPitStill, TABS, FEATURE_GAMES, FEATURES, tab, setTab, gameTab, setGameTab, app: computed(() => store.app) };
  },
  template: `
    <main class="page page-narrow">
      <h1 class="page-title">Settings</h1>
      <nav class="tabs" role="tablist" aria-label="Settings">
        <button v-for="t in TABS" :key="t[0]" type="button" role="tab" :aria-selected="tab === t[0]" :class="{ on: tab === t[0] }" @click="setTab(t[0])">{{ t[1] }}</button>
      </nav>
      <div class="save-state" aria-live="polite"><span v-if="saved" class="saved">{{ saved }}</span><span v-if="error" class="field-error">{{ error }}</span></div>

      <template v-if="s">
        <section v-if="tab === 'general'" class="settings-section">
          <h2>Appearance</h2>
          <div class="setting">
            <div class="setting-text"><h3>Theme</h3><p>Follow Windows, or always use light or dark.</p></div>
            <div class="segmented" role="radiogroup" aria-label="Theme">
              <button v-for="t in [['system','Follow Windows'],['light','Light'],['dark','Dark']]" :key="t[0]" type="button" role="radio"
                :aria-checked="s.theme === t[0]" :class="{ on: s.theme === t[0] }" @click="setTheme(t[0])">{{ t[1] }}</button>
            </div>
          </div>
        </section>

        <section v-if="tab === 'compare'" class="settings-section">
          <h2>Duplicates</h2>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>Near-identical when at least <output>{{ s.threshold }}%</output> is the same</h3>
              <p>One different setting lowers the score by about 1.2%. Lower this to find setups that are more loosely related.</p>
            </div>
            <input type="range" min="70" max="99.5" step="0.5" :value="s.threshold" @input="setThreshold($event.target.value)" aria-label="Near-identical threshold">
            <div class="range-scale" aria-hidden="true"><span>70%: loosely related</span><span>99.5%: almost identical</span></div>
          </div>
        </section>

        <section v-if="tab === 'compare'" class="settings-section">
          <h2>History</h2>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>A setup counts as made from an older one when at least <output>{{ hist.threshold }}%</output> is the same</h3>
              <p>Used to guess where your existing setups came from, and to spot new branches.</p>
            </div>
            <input type="range" min="60" max="95" step="1" v-model.number="hist.threshold" aria-label="History threshold">
          </div>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>… or <output>{{ hist.nameThreshold }}%</output> when the names match</h3>
              <p>“LA 296 HUN Q” and “LA 296 HUN R” have the same name apart from the session, so they need less in common.</p>
            </div>
            <input type="range" min="40" max="90" step="1" v-model.number="hist.nameThreshold" aria-label="Name match threshold">
          </div>
          <div v-for="g in games.filter(x => x.visible && x.status === 'ok')" :key="'h' + g.id" class="setting setting-stack">
            <div class="setting-text">
              <h3>{{ g.name }}</h3>
              <p>Preview the result first, then apply it. Links set by hand are never changed.</p>
            </div>
            <div class="row-actions">
              <button type="button" class="btn" @click="preview(g.id)" :disabled="hist.busy">Preview</button>
              <button type="button" class="btn btn-primary" @click="applyHistory(g.id)" :disabled="hist.busy || hist.game !== g.id">Apply to history</button>
            </div>
            <div v-if="hist.result && hist.game === g.id" class="hist-result">
              <div class="stats">
                <div class="stat"><span class="stat-value">{{ hist.result.summary.families }}</span><span class="stat-label">families</span></div>
                <div class="stat"><span class="stat-value">{{ hist.result.summary.linked }}</span><span class="stat-label">setups linked to a parent</span></div>
                <div class="stat"><span class="stat-value">{{ hist.result.summary.longest }}</span><span class="stat-label">steps in the longest line</span></div>
                <div class="stat"><span class="stat-value">{{ hist.result.summary.changed }}</span><span class="stat-label">links would change</span></div>
              </div>
              <details v-for="grp in hist.result.groups.filter(x => x.roots.length < countNodes(x))" :key="grp.car + grp.track" class="hist-group">
                <summary>{{ grp.carName }}, {{ grp.trackName }}: {{ grp.roots.length }} {{ grp.roots.length === 1 ? 'family' : 'families' }}</summary>
                <FamilyTree v-for="r in grp.roots.filter(r => r.children.length)" :key="r.setupId" :root="r" :current="0" />
              </details>
              <p v-if="hist.applied" class="check check-ok">Applied.</p>
            </div>
            <p v-if="hist.error && hist.game === g.id" class="field-error">{{ hist.error }}</p>
          </div>
        </section>

        <section v-if="tab === 'games'" class="settings-section">
          <h2>Games</h2>
          <nav class="tabs tabs-sub" role="tablist" aria-label="Games">
            <button v-for="g in games" :key="g.id" type="button" role="tab" :aria-selected="gameTab === g.id" :class="{ on: gameTab === g.id }"
              :title="g.name" @click="setGameTab(g.id)">{{ g.short }}<span v-if="!g.installed" class="tab-note">Not found</span></button>
          </nav>
          <article v-for="g in games.filter(x => x.id === gameTab)" :key="g.id" class="game-setting">
            <header>
              <h3>{{ g.name }}</h3>
              <span class="pill" :class="g.installed ? 'pill-ok' : ''">{{ g.installed ? 'Found on this PC' : 'Not found on this PC' }}</span>
            </header>
            <div class="setting">
              <div class="setting-text"><h3>Show on the overview</h3><p>“When installed” shows it only if SetupMan finds the game.</p></div>
              <div class="segmented" role="radiogroup" :aria-label="'Show ' + g.name">
                <button v-for="v in ['auto','show','hide']" :key="v" type="button" role="radio"
                  :aria-checked="g.visibility === v" :class="{ on: g.visibility === v }" @click="setVisibility(g.id, v)">{{ visLabel[v] }}</button>
              </div>
            </div>
            <div class="setting setting-stack">
              <div class="setting-text">
                <h3>Setups folder</h3>
                <p v-if="g.detectedFolder">Found automatically: <code>{{ g.detectedFolder }}</code>. Leave empty to use it.</p>
                <p v-else-if="s.games[g.id]?.folder">Set manually.</p>
                <p v-else>Not found automatically. Choose the setups folder below.</p>
              </div>
              <div class="folder-row">
                <input type="text" v-model="draft.folders[g.id]" :placeholder="g.detectedFolder || 'Choose a folder'" spellcheck="false"
                  @change="useFolder(g.id, draft.folders[g.id].trim())" :aria-label="g.name + ' setups folder'">
                <button type="button" class="btn" @click="browse(g)">Browse…</button>
                <button v-if="draft.folders[g.id] && g.detectedFolder" type="button" class="btn btn-quiet" @click="useFolder(g.id, '')">Use the found folder</button>
              </div>
              <p v-if="checks[g.id]" class="check" :class="checks[g.id].ok ? 'check-ok' : (checks[g.id].ok === false ? 'check-bad' : '')">{{ checks[g.id].message }}</p>
            </div>
            <p v-if="g.readOnly && g.status === 'ok'" class="notice notice-tight">{{ g.readOnly }}</p>
            <div v-if="!g.readOnly && (g.status === 'ok' || g.managed)" class="setting setting-stack">
              <div class="setting manage-row">
                <div class="setting-text">
                  <h3>Folder management</h3>
                  <p>Allows SetupMan to change the {{ g.short }} setups folder: select which setups {{ g.short }} lists, rename setups and remove duplicates. Setups removed from the folder remain stored in SetupMan. Enabling creates a full backup of the folder and verifies that every setup is stored; the backup can be restored at any time. Changing the setups folder disables folder management until it is enabled for the new folder.</p>
                </div>
                <label class="switch switch-lg"><input type="checkbox" :checked="g.managed" :disabled="manage.busy" @change="setManaged(g, $event.target.checked)"><span></span><span class="sr">Folder management for {{ g.short }}</span></label>
              </div>
              <div v-if="manage.result && manage.game === g.id" class="manage-result">
                <p v-if="manage.result.managed" class="check check-ok">Folder management enabled. Backup created and {{ manage.result.checked }} setups verified.</p>
                <template v-else-if="manage.result.problems">
                  <p class="check check-bad">Folder management was not enabled: some setups differ from the copy stored in SetupMan. Nothing was changed.</p>
                  <ul><li v-for="p in manage.result.problems.slice(0, 8)" :key="p">{{ p }}</li></ul>
                </template>
              </div>
              <p v-if="manage.error && manage.game === g.id" class="field-error">{{ manage.error }}</p>
              <div v-if="g.managed" class="setting manage-row">
                <div class="setting-text">
                  <h3>Hide new saves when {{ g.short }} closes</h3>
                  <p>When {{ g.short }} closes, setups saved in the game during that session are hidden from {{ g.short }} unless you marked them Keep. They remain stored in SetupMan. Off: they stay in {{ g.short }} until you hide them.</p>
                </div>
                <label class="switch switch-lg"><input type="checkbox" :checked="g.tidyOnGameClose" @change="setTidy(g.id, $event.target.checked)"><span></span><span class="sr">Hide new saves when the game closes</span></label>
              </div>
              <div v-if="g.managed" class="setting manage-row">
                <div class="setting-text">
                  <h3>Apply the naming standard to new saves</h3>
                  <p>When you save a setup in the game, SetupMan renames the file to initials, session, temperature, game version and your codes (e.g. “ABC R 2327 1.10.4 TC25”), and notes the original name. Initials are yours (Settings → General). Session and temperature come from the session you saved in; outside a session, from the setup it was made from.</p>
                </div>
                <label class="switch switch-lg"><input type="checkbox" :checked="g.autoRename" @change="setAutoRename(g.id, $event.target.checked)"><span></span><span class="sr">Apply the naming standard to new saves</span></label>
              </div>
            </div>
            <div v-if="g.laps" class="setting setting-stack">
              <div class="setting-text">
                <h3>Setup recognition</h3>
                <p v-if="g.byFile">While you drive, SetupMan recognises the setup from the file {{ g.short }} opened last, so laps are stored with the right setup. When {{ g.short }} opened several at once, or none since it started, the start fuel decides. When setups fit equally well, Manual setup select offers them with one click.</p>
                <p v-else>{{ g.short }} does not say which setup is loaded. In the pit box before your first lap, and again when you leave the pits, SetupMan compares the fuel, the pit menu's tyre pressures (from the setup's pit-stop strategy), TC, ABS, engine map and the cold tyre pressures with your setups. When setups fit equally well, Manual setup select under the top bar offers them with one click.</p>
                <p v-if="g.byFile && app.lastOpened && app.lastOpened.enabled" class="check check-ok">Windows records when files are opened on this PC.</p>
              </div>
              <LastOpenedNotice v-if="g.byFile" :games="g.short" />
            </div>
            <div v-if="g.laps" class="setting">
              <div class="setting-text">
                <h3>Pit stop advice: target tyre pressures</h3>
                <p v-if="g.id === 'acc'">The hot pressure the Pit stop page aims for, averaged over a lap. Usual values: 27.0 psi on dry tyres, 30.0 psi on wets.</p>
                <p v-else>The hot pressure the Pit stop page aims for, averaged over a lap. The right value differs per car in {{ g.short }}; set it for the car you race.</p>
              </div>
              <div class="pressure-targets">
                <label>Dry <span class="unit-input"><input type="number" min="15" max="40" step="0.1" :value="Number(s.games[g.id]?.tyreTargetDry || 27).toFixed(1)" @change="setTarget(g.id, 'tyreTargetDry', $event.target.value)" aria-label="Dry tyre target pressure"> psi</span></label>
                <label>Wet <span class="unit-input"><input type="number" min="15" max="40" step="0.1" :value="Number(s.games[g.id]?.tyreTargetWet || 30).toFixed(1)" @change="setTarget(g.id, 'tyreTargetWet', $event.target.value)" aria-label="Wet tyre target pressure"> psi</span></label>
              </div>
            </div>
            <div v-if="g.laps" class="setting">
              <div class="setting-text">
                <h3>Pit stop: standing still</h3>
                <p>Race prep and the fuel calculator plan each pit stop as the measured drive through the pit lane plus this standing time (tyres and fuel, no repairs). The standing time you measured, repairs included, is shown next to it.</p>
              </div>
              <label class="minutes">
                <input type="number" min="5" max="300" step="1" :value="s.games[g.id]?.pitStillSeconds || 30" @change="setPitStill(g.id, $event.target)" :aria-label="g.name + ' planned standing time at a pit stop'"> s
              </label>
            </div>
            <div class="setting">
              <div class="setting-text">
                <h3>Endurance</h3>
                <p>A race setup counts as Endurance when one stint is longer than this. Stint = race length ÷ (pit stops + 1). You can always set it per setup.</p>
              </div>
              <label class="minutes">
                <input type="number" min="5" max="1440" step="5" :value="s.games[g.id]?.enduranceMinutes || 45" @change="setEndurance(g.id, $event.target.value)" :aria-label="g.name + ' endurance stint length'"> minutes
              </label>
            </div>
          </article>
        </section>

        <section v-if="tab === 'telemetry'" class="settings-section">
          <h2>3D track view</h2>
            <div class="setting setting-stack">
              <div class="setting-text">
                <h3>Satellite photos</h3>
                <p>Lap analysis can show your laps in 3D on the real track (Track → 3D). For sharp satellite photos at every track, create a free account at <a href="https://www.maptiler.com/cloud/" target="_blank" rel="noopener">MapTiler</a> and paste your API key here. The free plan is for personal, non-commercial use and has a monthly limit (about 100,000 tile requests; one 3D view of a track uses a few hundred). The photos load straight from MapTiler while you are online and are not stored. Without a key the view uses the government aerial photos SetupMan can download for offline use (Zandvoort, Zolder, Spa, Nürburgring, Red Bull Ring, Paul Ricard, Barcelona, Valencia, Suzuka and the US tracks), or no photo.</p>
              </div>
              <div class="folder-row">
                <input :type="showKey ? 'text' : 'password'" v-model="mapTilerKey" placeholder="MapTiler API key" spellcheck="false" autocomplete="off" aria-label="MapTiler API key" @keyup.enter="saveMapTiler">
                <button type="button" class="btn btn-sm btn-quiet" @click="showKey = !showKey">{{ showKey ? 'Hide' : 'Show' }}</button>
                <button type="button" class="btn btn-sm" :disabled="String(mapTilerKey || '').trim() === (s.mapTilerKey || '')" @click="saveMapTiler">Save</button>
              </div>
              <div v-if="Object.keys(keptPhotos).length" class="kept-photos">
                <h4>Offline aerial photos kept</h4>
                <ul>
                  <li v-for="(size, t) in keptPhotos" :key="t"><span>{{ trackLabel(t) }}</span><span class="kept-size">{{ kb(size) }}</span><button type="button" class="btn-link" @click="deleteKept(t)">Delete</button></li>
                </ul>
              </div>
            </div>
        </section>

        <section v-if="tab === 'telemetry'" class="settings-section">
          <h2>Telemetry per game</h2>
          <p v-if="!teleGames.length" class="setting-hint">None of your games has telemetry SetupMan can read yet.</p>
          <article v-for="g in teleGames" :key="g.id" class="game-setting">
            <header><h3>{{ g.name }}</h3></header>
            <div v-if="g.telemetry" class="setting setting-stack">
              <div class="setting-text">
                <h3>MoTeC folder</h3>
                <p>SetupMan reads the MoTeC telemetry {{ g.short }} writes. Sessions SetupMan recorded get their setup automatically; for older sessions, choose the setup on the track's Sessions tab. SetupMan only reads these files and never changes, moves or deletes them.</p>
                <p v-if="g.detectedTelemetry">Found automatically: <code>{{ g.detectedTelemetry }}</code>. Leave empty to use it.</p>
                <p v-else-if="teleValue(g)">{{ g.short }}'s own MoTeC folder was not found on this PC; SetupMan uses the folder below.</p>
                <p v-else>Not found automatically. {{ g.short }} creates the folder once it has written telemetry; or choose a folder below.</p>
              </div>
              <div class="folder-row">
                <input type="text" :value="teleValue(g)" :placeholder="g.detectedTelemetry || 'Choose a folder'" spellcheck="false"
                  @change="useTeleFolder(g.id, $event.target.value.trim())" :aria-label="g.name + ' telemetry folder'">
                <button type="button" class="btn" @click="browseTele(g)">Browse…</button>
                <button v-if="teleValue(g) && g.detectedTelemetry" type="button" class="btn btn-quiet" @click="useTeleFolder(g.id, '')">Use the found folder</button>
              </div>
            </div>
            <div v-if="g.extraLog" class="setting manage-row">
              <div class="setting-text">
                <h3>Extra telemetry log</h3>
                <p>While you drive, SetupMan records what the MoTeC files lack: car position (for an exact track map), fuel, lap validity, slip angle and slip ratio, brake pressure, brake pad and disc wear, ride height, pitch and roll. About 10 readings per second, a few MB per hour, stored in SetupMan's own folder and included in full backups.</p>
              </div>
              <label class="switch switch-lg"><input type="checkbox" :checked="!s.games[g.id]?.extraLogOff" @change="setExtraLog(g.id, $event.target.checked)"><span></span><span class="sr">Extra telemetry log for {{ g.short }}</span></label>
            </div>
              <div v-if="g.telemetryLaps" class="setting manage-row">
                <div class="setting-text">
                  <h3>Telemetry laps</h3>
                  <p>{{ g.short }} keeps telemetry for as many laps as each setup's Telemetry laps value (Electronics), up to 99. Sets 99 in the setups in the {{ g.short }} folder that have fewer; you see the list first, and a backup of the folder is made before anything changes. Not while driving.</p>
                </div>
                <button v-if="g.managed" type="button" class="btn" @click="openTeleLaps(g)">Set telemetry laps to 99…</button>
                <span v-else class="setting-hint">Needs folder management (Settings → Games → {{ g.short }}).</span>
              </div>
          </article>
        </section>

        <section v-if="tab === 'backups'" class="settings-section">
          <h2>Backups</h2>
          <template v-for="g in games.filter(x => x.folder)" :key="'b' + g.id">
            <div class="setting setting-stack">
              <div class="setting-text">
                <h3>{{ g.short }} setups folder backups</h3>
                <p>Only the files in {{ g.name }}'s setups folder. Restoring puts that folder back exactly as it was at that moment. SetupMan's own history and notes are not changed.</p>
              </div>
              <ul v-if="(backups[g.id] || []).length" class="backup-list">
                <li v-for="b in backups[g.id]" :key="b.id">
                  <span class="bk-when">{{ dateTime(b.createdAt) }}</span>
                  <span class="bk-why">{{ b.reason }}, {{ b.files }} files</span>
                  <span class="bk-actions"><button type="button" class="btn btn-sm" @click="askRestore(g, b)">Restore</button></span>
                </li>
              </ul>
              <p v-else class="panel-hint">No backups yet.</p>
              <p v-if="backupDirs[g.id]" class="backup-dir">Saved in <code>{{ backupDirs[g.id] }}</code></p>
              <div class="row-actions"><button type="button" class="btn" :disabled="backupBusy" @click="createBackup(g.id)">{{ backupBusy ? 'Creating backup…' : 'Create backup now' }}</button>
                <button type="button" class="btn btn-quiet" @click="openFolder(g.id)">Open folder</button></div>
            </div>
          </template>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>Full SetupMan backups</h3>
              <p>Everything SetupMan knows: all setup versions, history, notes, lap times, settings, telemetry notes and reference laps, and SetupMan's extra telemetry logs, plus a backup of each setups folder. The game's own telemetry files (MoTeC) are not included: SetupMan never changes them. Download a backup to keep a copy on another drive or PC.</p>
            </div>
            <ul v-if="full.list.length" class="backup-list">
              <li v-for="b in full.list" :key="b.id">
                <span class="bk-when">{{ dateTime(b.createdAt) }}</span>
                <span class="bk-why">{{ b.reason }}, {{ size(b.bytes) }}</span>
                <span class="bk-actions">
                  <a class="btn btn-sm btn-quiet" :href="'/api/full-backups/' + encodeURIComponent(b.id) + '/download'" download>Download</a>
                  <button type="button" class="btn btn-sm" @click="askFull(b)">Restore</button>
                </span>
              </li>
            </ul>
            <p v-else class="panel-hint">No full backups yet.</p>
            <p v-if="full.dir" class="backup-dir">Saved in <code>{{ full.dir }}</code></p>
            <div class="row-actions">
              <button type="button" class="btn" :disabled="full.busy" @click="createFull">{{ full.busy ? 'Creating backup…' : 'Create full backup now' }}</button>
              <label class="btn btn-file">Restore from a file…<input type="file" accept=".zip" :disabled="full.busy" @change="uploadFull"></label>
              <button type="button" class="btn btn-quiet" @click="openFolder('full')">Open folder</button>
            </div>
          </div>
        </section>

        <section v-if="tab === 'network'" class="settings-section" id="network">
          <h2>Network</h2>
          <div class="setting manage-row">
            <div class="setting-text">
              <h3>Network access</h3>
              <p>Allows other computers, laptops, tablets and phones on the local network to open SetupMan. Unless the PIN is disabled below, each device needs the PIN once. If Windows asks whether SetupMan may use the network, allow it for private networks. Enabling or disabling network access restarts SetupMan.</p>
            </div>
            <label class="switch switch-lg"><input type="checkbox" :checked="network.allow" :disabled="network.busy" @change="setNetwork($event.target.checked, false)"><span></span><span class="sr">Network access</span></label>
          </div>
          <div v-if="network.allow" class="setting setting-stack">
            <div class="setting-text">
              <h3>How to connect</h3>
              <p>On the other device, open <template v-if="network.hostName"><b>http://{{ network.hostName }}</b> or </template><template v-for="(a, k) in network.addresses" :key="a"><b>http://{{ a }}:{{ network.port }}</b><template v-if="k < network.addresses.length - 1"> or </template></template><template v-if="network.requirePin"> and type this PIN:</template><template v-else>. No PIN is asked.</template></p>
            </div>
            <div v-if="network.requirePin" class="pin-show"><span class="pin-digits">{{ network.pin }}</span>
              <button type="button" class="btn btn-sm btn-quiet" :disabled="network.busy" @click="setNetwork(true, true)" title="Devices that used the old PIN must enter the new one">Generate new PIN</button></div>
            <p v-if="!network.listening" class="notice notice-tight">SetupMan is restarting to listen on the network…</p>
          </div>
          <div v-if="network.allow" class="setting manage-row">
            <div class="setting-text">
              <h3>Require PIN</h3>
              <p>Every device on the network must enter the PIN once before it can use SetupMan. Recommended. Disable it only on a network where you trust every device, such as your own home network with your own PC and phone: without the PIN, <b>any device on the network has full use of SetupMan</b>, Settings included. SetupMan still accepts only devices on the local network that use its IP address or network name.</p>
              <p v-if="!network.requirePin" class="notice notice-tight">The PIN is disabled: every device on this network can use SetupMan.</p>
            </div>
            <label class="switch switch-lg"><input type="checkbox" :checked="network.requirePin" :disabled="network.busy" @change="askRequirePin($event.target.checked); $event.target.checked = network.requirePin"><span></span><span class="sr">Require PIN</span></label>
          </div>
          <dialog ref="pinDlg" class="confirm">
            <h2>Disable the PIN?</h2>
            <p>Every device on this network can then open SetupMan and use all of it, Settings included, without entering a PIN. Only do this on a network where you trust every device.</p>
            <p>The PIN can be enabled again at any time; devices then need to enter it again.</p>
            <div class="picker-actions">
              <button type="button" class="btn" @click="pinDlg.close()">Cancel</button>
              <button type="button" class="btn btn-danger-solid" :disabled="network.busy" @click="setRequirePin(false)">Disable PIN</button>
            </div>
          </dialog>
          <div class="setting">
            <div class="setting-text">
              <h3>Port number</h3>
              <p>The number at the end of SetupMan's address (now {{ network.port }}). Change it if another program uses the same number. Default: 8765. SetupMan restarts and this page moves to the new address.</p>
            </div>
            <label class="minutes"><input type="number" min="1024" max="65535" v-model="network.newPort" aria-label="Port number">
              <button type="button" class="btn btn-sm" :disabled="network.busy || !network.newPort || Number(network.newPort) === network.port" @click="setPort">Change</button></label>
          </div>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>Network name</h3>
              <p>A name such as <b>setupman.lan</b> that a reverse proxy, for example Nginx Proxy Manager, forwards to SetupMan, so devices can open it without an IP address or port number. Requires network access; devices opening SetupMan through this name need the PIN (unless it is disabled above), including this PC. Leave empty to accept IP addresses only. Setup steps: <router-link to="/manual?tab=devices&s=lan">Manual → Other devices</router-link>.</p>
            </div>
            <div class="folder-row">
              <input type="text" v-model="network.newHostName" placeholder="setupman.lan" spellcheck="false" autocapitalize="off" aria-label="Network name" @keyup.enter="setHostName">
              <button type="button" class="btn btn-sm" :disabled="network.busy || (network.newHostName || '').trim().toLowerCase() === network.hostName" @click="setHostName">Save</button>
            </div>
          </div>
        </section>

        <section v-if="tab === 'backups'" class="settings-section" id="bin">
          <h2>Bin</h2>
          <div class="setting setting-stack">
            <div class="setting-text">
              <p>Deleted setups stay here for {{ bin.days }} days, with all their versions. Restore puts a setup back in SetupMan, hidden from the game.</p>
            </div>
            <ul v-if="bin.list.length" class="backup-list">
              <li v-for="b in bin.list" :key="b.id">
                <span class="bk-when">{{ b.name }}</span>
                <span class="bk-why">{{ b.carName }}, {{ b.trackName }}. Deleted {{ dateTime(b.deletedAt) }}, permanently deleted {{ dateTime(b.goneAt) }}</span>
                <span class="bk-actions"><button type="button" class="btn btn-sm" @click="undelete(b)">Restore</button></span>
              </li>
            </ul>
            <p v-else class="panel-hint">The bin is empty.</p>
          </div>
        </section>

        <dialog ref="confirmDlg" class="confirm" @close="restore.open = false">
          <template v-if="restore.backup">
            <h2>Restore the {{ restore.game.short }} setups folder to {{ dateTime(restore.backup.createdAt) }}?</h2>
            <p>The folder will be exactly as it was then. Everything changed since then is undone in the folder.</p>
            <p v-if="restore.plan" class="muted-small">Folder: {{ restore.plan.folder }}</p>
            <p v-if="restore.plan && restore.plan.otherFolder" class="notice notice-tight">This backup was made of another folder ({{ restore.plan.backupFolder }}), for example on another PC or before the setups folder was changed. It is restored into the current {{ restore.game.short }} setups folder shown above.</p>
            <div v-if="restore.plan" class="counts stats">
              <div class="stat"><span class="stat-value">{{ restore.plan.change }}</span><span class="stat-label">files change</span></div>
              <div class="stat"><span class="stat-value">{{ restore.plan.remove }}</span><span class="stat-label">files are removed</span></div>
              <div class="stat"><span class="stat-value">{{ restore.plan.return }}</span><span class="stat-label">files come back</span></div>
            </div>
            <p v-else-if="!restore.error">Checking what will change…</p>
            <p>SetupMan's history and notes are not affected. Before restoring, SetupMan creates a backup of the current folder, so the restore can be undone.</p>
            <p v-if="restore.error" class="field-error">{{ restore.error }}</p>
            <div class="picker-actions">
              <button type="button" class="btn" @click="closeRestore">Cancel</button>
              <button type="button" class="btn btn-primary" :disabled="!restore.plan || restore.busy" @click="doRestore">{{ restore.busy ? 'Restoring…' : 'Restore' }}</button>
            </div>
          </template>
        </dialog>

        <dialog ref="fullDlg" class="confirm" @close="full.asking = null">
          <template v-if="full.asking">
            <h2>Restore SetupMan to {{ dateTime(full.asking.createdAt) }}?</h2>
            <p>SetupMan restarts, and its setup versions, history, notes, lap times and settings go back to that moment. Changes made after that moment are no longer shown.</p>
            <p>Before restoring, SetupMan creates a full backup of the current state, so the restore can be undone.</p>
            <p>Your setups folder is <b>not</b> changed. The folder as it was then is added to the setups folder backups above, where it can be restored separately.</p>
            <p v-if="full.error" class="field-error">{{ full.error }}</p>
            <div class="picker-actions">
              <button type="button" class="btn" @click="fullDlg.close()">Cancel</button>
              <button type="button" class="btn btn-primary" :disabled="full.busy" @click="doFull">{{ full.busy ? 'Restarting…' : 'Restore and restart' }}</button>
            </div>
          </template>
        </dialog>

        <section v-if="tab === 'general' && autostart.supported" class="settings-section">
          <h2>Windows</h2>
          <div class="setting">
            <div class="setting-text">
              <h3>Start with Windows</h3>
              <p>SetupMan starts in the background when you sign in, with its icon in the notification area next to the clock. This can also be changed from that icon.</p>
            </div>
            <label class="switch switch-lg"><input type="checkbox" :checked="autostart.enabled" @change="setAutostart($event.target.checked)"><span></span><span class="sr">Start with Windows</span></label>
          </div>
        </section>

        <section v-if="tab === 'general'" class="settings-section">
          <h2>Naming standard</h2>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>Your initials</h3>
              <p>Put in front of the names SetupMan gives your new saves in the game (Settings → Games → Apply the naming standard to new saves), e.g. “ABC R 2327 1.10.4”. A setup whose name already starts with initials keeps them. 2 to 6 letters; leave empty for none.</p>
            </div>
            <div class="folder-row">
              <input type="text" v-model="initials" maxlength="6" placeholder="e.g. ABC" spellcheck="false" autocapitalize="characters" aria-label="Your initials" @keyup.enter="saveInitials">
              <button type="button" class="btn btn-sm" :disabled="String(initials || '').trim().toUpperCase() === (s.initials || '')" @click="saveInitials">Save</button>
            </div>
          </div>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>Temperatures in setup names</h3>
              <p>There is no standard: most names give the air and the track temperature as ACC shows them (3040), some only the air temperature (23C), a few a range. Set how each author writes them. Authors are recognised by the initials at the front of the name, or by a word anywhere in it that you add below (for authors who put their name elsewhere, e.g. “R 2327 CDA M LFM”). Changes apply to all setups straight away, except details you edited by hand on a setup's Notes &amp; details tab.</p>
            </div>
            <div class="temp-rule temp-rule-default">
              <h4>Everyone else, and names without initials</h4>
              <label>Two numbers<select :value="defaultRule.four" @change="setTempRule('*', 'four', $event.target.value)"><option v-for="o in FOUR" :key="o[0]" :value="o[0]">{{ o[1] }}</option></select></label>
              <label>One number<select :value="defaultRule.single" @change="setTempRule('*', 'single', $event.target.value)"><option v-for="o in SINGLE" :key="o[0]" :value="o[0]">{{ o[1] }}</option></select></label>
            </div>
            <p v-if="tempAuthors === null" class="panel-hint">Looking at your setup names…</p>
            <template v-else>
              <div v-for="a in tempAuthors" :key="a.key" class="temp-rule">
                <h4>{{ a.key === '*' ? 'Names without a known author' : a.key }}<span class="temp-count">{{ a.count }} setup{{ a.count === 1 ? '' : 's' }}<template v-if="a.key === (s.initials || '')"> · you</template></span>
                  <button v-if="a.own" type="button" class="btn-link temp-remove" @click="removeRule(a.key)">Remove rule</button></h4>
                <template v-if="a.key !== '*'">
                  <label>Two numbers<select :value="ownRule(a.key).four || ''" @change="setTempRule(a.key, 'four', $event.target.value)"><option value="">As everyone else: {{ label(FOUR, defaultRule.four) }}</option><option v-for="o in FOUR" :key="o[0]" :value="o[0]">{{ o[1] }}</option></select></label>
                  <label>One number<select :value="ownRule(a.key).single || ''" @change="setTempRule(a.key, 'single', $event.target.value)"><option value="">As everyone else: {{ label(SINGLE, defaultRule.single) }}</option><option v-for="o in SINGLE" :key="o[0]" :value="o[0]">{{ o[1] }}</option></select></label>
                </template>
                <ul class="temp-examples"><li v-for="e in a.examples" :key="e.name"><span>{{ e.name }}</span> <b>{{ e.reading }}</b></li></ul>
                <p v-if="fitText(a.fit)" class="temp-fit">{{ fitText(a.fit) }}</p>
                <div v-if="a.key === '*'" class="temp-words">
                  <span v-if="tempWords.length" class="temp-words-label">Words that recur in these names:</span>
                  <button v-for="w in tempWords" :key="w.word" type="button" class="chip" :title="'Add a rule for names with ' + w.word" @click="addWordRule(w.word)">{{ w.word }} <small>{{ w.count }}</small> +</button>
                  <span class="temp-word-add"><input v-model="newWord" maxlength="20" placeholder="Another word, e.g. CDA" aria-label="Word for a new rule" @keyup.enter="addWordRule(newWord)"><button type="button" class="btn btn-sm" :disabled="!newWord.trim()" @click="addWordRule(newWord)">Add</button></span>
                </div>
              </div>
              <p v-if="!tempAuthors.length" class="panel-hint">No setup names with a temperature yet.</p>
            </template>
          </div>
        </section>

        <section v-if="tab === 'general'" class="settings-section">
          <h2>Low Fuel Motorsport</h2>
          <div class="setting setting-stack">
            <div class="setting-text">
              <h3>LFM ID</h3>
              <p>With your LFM ID, Race prep shows your next LFM race: track, session times, mandatory stops and the server's weather settings, and fills in Race prep for it. The ID is the number in your LFM profile address (lowfuelmotorsport.com/profile/<i>number</i>). It is kept on this PC and only sent to LFM. Leave empty to not use LFM.</p>
            </div>
            <div class="folder-row">
              <input type="text" v-model="lfmId" inputmode="numeric" placeholder="e.g. 12345" spellcheck="false" aria-label="LFM ID" @keyup.enter="saveLfm">
              <button type="button" class="btn btn-sm" :disabled="String(lfmId || '').trim() === (s.lfmId || '')" @click="saveLfm">Save</button>
            </div>
          </div>
        </section>

        <section v-if="tab === 'general'" class="settings-section">
          <h2>Brand logos</h2>
          <div class="setting">
            <div class="setting-text">
              <h3>Download brand logos</h3>
              <p>Car-brand logos come from Wikimedia Commons. Each one is downloaded once and kept on this PC. When this is off, tiles show the brand's initials.</p>
            </div>
            <label class="switch switch-lg"><input type="checkbox" :checked="s.downloadLogos" @change="setLogos($event.target.checked)"><span></span><span class="sr">Download brand logos</span></label>
          </div>
        </section>

        <section v-if="tab === 'about'" class="settings-section about">
          <h2>SetupMan</h2>
          <p v-if="app.number"><b>Version {{ app.number }}</b><template v-if="app.commit">, build {{ app.commit }}<template v-if="app.dirty"> (with uncommitted changes)</template></template><template v-if="app.built"> of {{ app.built }}</template></p>
          <p v-if="app.host">Running on computer <b>{{ app.host }}</b></p>
          <p>SetupMan is free and open source (MIT licence). It only changes a game's setups folder when folder management is enabled for that game, and it creates a backup first.</p>
          <ul class="about-links">
            <li><a href="https://github.com/LaurensVR3/SetupMan" target="_blank" rel="noopener">SetupMan on GitHub</a></li>
            <li><a href="https://github.com/LaurensVR3/SetupMan/releases/latest" target="_blank" rel="noopener">Latest version</a></li>
            <li><a href="https://lavaro.net" target="_blank" rel="noopener">lavaro.net</a></li>
          </ul>
        </section>

        <!-- Feedback (D-156): each button opens the matching GitHub form with this version filled in -->
        <section v-if="tab === 'about'" class="settings-section about">
          <h2>Feedback</h2>
          <p>SetupMan is new and so far tested on one sim rig, mostly with ACC. Reports from players make it better quickly, especially for AC, AC EVO and AC Rally. Each button opens a short form on GitHub (a free GitHub account is needed) with your version already filled in.</p>
          <div class="feedback-actions">
            <a class="btn btn-primary" :href="issueUrl('1-problem.yml')" target="_blank" rel="noopener">Report a problem</a>
            <a class="btn" :href="issueUrl('2-values.yml')" target="_blank" rel="noopener">A value differs from the game</a>
            <a class="btn" :href="issueUrl('3-test-report.yml')" target="_blank" rel="noopener">Send a test report</a>
            <a class="btn" :href="issueUrl('4-idea.yml')" target="_blank" rel="noopener">Suggest an idea</a>
          </div>
          <div class="setting">
            <div class="setting-text">
              <h3>Log file</h3>
              <p><code>setupman.log</code> lists what SetupMan saw of your games: sessions, laps, which setup it recognised and changes in the setups folders. Drag it into your report; it helps most. It contains your setup names and nothing else personal.</p>
            </div>
            <div class="setting-actions">
              <button v-if="network.local" type="button" class="btn" @click="openFolder('log')">Open log folder</button>
              <a class="btn" :class="{ 'btn-quiet': network.local }" href="/api/log" download="setupman.log">Download log</a>
            </div>
          </div>
        </section>

        <section v-if="tab === 'about'" class="settings-section about">
          <h2>Features per game</h2>
          <div class="table-scroll">
            <table class="feature-table">
              <thead><tr><th scope="col">Feature</th><th v-for="g in FEATURE_GAMES" :key="g" scope="col">{{ g }}</th></tr></thead>
              <tbody>
                <tr v-for="f in FEATURES" :key="f[0]">
                  <th scope="row">{{ f[0] }}</th>
                  <td v-for="(v, i) in f.slice(1)" :key="i" :class="v === true ? 'ft-yes' : (v === false ? 'ft-no' : 'ft-note')">
                    <template v-if="v === true"><span aria-hidden="true">✓</span><span class="sr">Yes</span></template>
                    <template v-else-if="v === false"><span aria-hidden="true">–</span><span class="sr">No</span></template>
                    <template v-else>{{ v }}</template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="panel-hint">AC Rally keeps all setups in one save file, so SetupMan only reads them. Details per game are in the <router-link to="/manual?s=games">Manual</router-link> (Supported games).</p>
        </section>

        <section v-if="tab === 'about'" class="settings-section about">
          <h2>Credits</h2>
          <p>Brand logos: Wikimedia Commons. Logos are trademarks of their owners and are only used to identify the cars.</p>
          <p>Real values for ACC cars: the open-source Race Element project.</p>
          <p>3D track view: three.js (MIT licence).</p>
          <p>3D map data: race track lines © OpenStreetMap contributors (ODbL); terrain: Mapzen terrain tiles; satellite photos: MapTiler, with your own key; offline aerial photos: government sources, credited on the view.</p>
          <p>Official turn numbers: circuit maps from Wikimedia Commons.</p>
        </section>
        <TeleLapsDialog ref="teleLapsDlg" :game="teleLapsGame.id" :short="teleLapsGame.short" />
      </template>

      <FolderPicker :open="picker.open" :start="picker.start" :title="picker.kind === 'telemetry' ? 'Choose the telemetry folder' : 'Choose the setups folder'" @pick="picked" @close="picker.open = false" />
    </main>`,
};
