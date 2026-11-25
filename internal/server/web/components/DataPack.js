// Data packs (spec D-201): one ZIP with setups, laps and telemetry to send to other SetupMan users.
// Pages add setups or laps to the pack being put together (packState); the Create data pack window
// shows it with the choices per setup. Opening a pack someone sent shows what is in it first.
import { reactive, ref, watch, computed } from "vue";
import { useRouter } from "vue-router";
import { post, needPin } from "../api.js";
import { showToast, store } from "../store.js";
import { plural, sentence, dateTime } from "../format.js";
import { bytes, kindLabel } from "../telemetry.js";
import { lapTime } from "./LapTimes.js";

const KEEP = "setupman.datapack"; // the pack being put together survives a reload (a convenience)

function loadBasket() {
  try { const b = JSON.parse(localStorage.getItem(KEEP)); if (b && Array.isArray(b.setups) && Array.isArray(b.laps)) return b; } catch {}
  return { setups: [], laps: [] };
}

export const packState = reactive({ ...loadBasket(), open: false, imp: null });

watch(() => [packState.setups, packState.laps], () => {
  try { localStorage.setItem(KEEP, JSON.stringify({ setups: packState.setups, laps: packState.laps })); } catch {}
}, { deep: true });

// addSetupsToPack([{ id, name, telemetry?, sessions? }]): adds setups (by default with their fastest lap
// and the lap time summary). With sessions (keys from the preview), those whole sessions go along.
export function addSetupsToPack(list, open = true) {
  for (const s of list) {
    if (!s.id) continue;
    const have = packState.setups.find((x) => x.id === s.id);
    if (have) {
      if (s.sessions?.length) {
        if (have.telemetry !== "chosen") { have.telemetry = "chosen"; have.sessions = []; }
        for (const k of s.sessions) if (!have.sessions.includes(k)) have.sessions.push(k);
      }
      continue;
    }
    packState.setups.push({ id: s.id, name: s.name || "Setup", telemetry: s.telemetry || "best", sessions: s.sessions || [], lapTimes: true, note: false, history: false });
  }
  if (open) packState.open = true;
}

// addLapsToPack([{ fileId, n, label }]): adds laps picked by hand.
export function addLapsToPack(list, open = true) {
  for (const l of list) {
    if (packState.laps.some((x) => x.fileId === l.fileId && x.n === l.n)) continue;
    packState.laps.push({ fileId: l.fileId, n: l.n, label: l.label || "Lap" });
  }
  if (open) packState.open = true;
}

// openPackFile(file): opens a data pack someone sent. Returns false when the file is no data pack
// (a ZIP of setup files), so the page can import it the usual way.
export async function openPackFile(file) {
  let resp;
  try {
    resp = await fetch("/api/datapacks/inspect", { method: "POST", headers: { "X-SetupMan-Upload": "1", "Content-Type": "application/zip" }, body: file });
  } catch {
    showToast("SetupMan isn't running. Start SetupMan.exe again.", 7000);
    return true;
  }
  const data = await resp.json().catch(() => ({}));
  if (resp.status === 415) return false;
  if (resp.status === 401 && data.needPin) { needPin.value = true; return true; }
  if (!resp.ok) { showToast(sentence(data.error || resp.statusText), 8000); return true; }
  packState.imp = { token: data.token, pack: data.pack, stage: false, busy: false, result: null };
  return true;
}

// ExtBadge: the External badge with the sender's initials.
export const ExtBadge = {
  props: { by: String },
  template: `<span v-if="by" class="ext-badge" :title="'From ' + by + '\\'s data pack: shown and comparable, never counted as your own'">External · {{ by }}</span>`,
};

// PackChip: a small button while a pack is being put together and its window is closed.
export const PackChip = {
  setup() {
    const count = computed(() => packState.setups.length + packState.laps.length);
    return { packState, count };
  },
  template: `<button v-if="count && !packState.open" type="button" class="pack-chip" @click="packState.open = true">Data pack · {{ count }}</button>`,
};

const TELE = [["none", "No telemetry"], ["best", "Fastest lap"], ["session", "Fastest lap per session"], ["all", "All valid laps"], ["chosen", "Whole sessions I pick"]];

export const DataPackDialog = {
  setup() {
    const dlg = ref(null);
    const st = reactive({ preview: null, error: "", busy: false });
    let timer = 0, seq = 0;
    const request = () => ({
      setups: packState.setups.map((s) => ({ id: s.id, telemetry: s.telemetry, sessions: s.sessions || [], lapTimes: s.lapTimes, note: s.note, history: s.history })),
      laps: packState.laps.map((l) => ({ fileId: l.fileId, n: l.n })),
    });
    async function refresh() {
      const my = ++seq;
      if (!packState.setups.length && !packState.laps.length) { st.preview = null; st.error = ""; return; }
      try {
        const p = await post("/api/datapacks/preview", request());
        if (my === seq) { st.preview = p; st.error = ""; }
      } catch (e) { if (my === seq) { st.preview = null; st.error = e.message; } }
    }
    watch(() => [packState.setups, packState.laps], () => { clearTimeout(timer); timer = setTimeout(refresh, 250); }, { deep: true });
    watch(() => packState.open, (on) => {
      if (on && !dlg.value?.open) { dlg.value?.showModal(); refresh(); }
      if (!on && dlg.value?.open) dlg.value.close();
    });
    watch(() => store.version, () => packState.open && refresh());
    const info = (id) => st.preview?.setups.find((s) => s.id === id);
    const lapsFor = (id, t) => { const p = info(id); return !p ? null : { best: p.lapsBest, session: p.lapsSession, all: p.lapsAll }[t]; };
    const teleLabel = (id, t) => { const n = lapsFor(id, t); return TELE.find((x) => x[0] === t)[1] + (n == null || t === "none" || t === "chosen" ? "" : ` (${plural(n, "lap")})`); };
    function toggleSession(s, key) {
      if (!s.sessions) s.sessions = [];
      const i = s.sessions.indexOf(key);
      if (i >= 0) s.sessions.splice(i, 1); else s.sessions.push(key);
    }
    const removeSetup = (i) => packState.setups.splice(i, 1);
    const removeLap = (i) => packState.laps.splice(i, 1);
    function clearAll() { packState.setups.splice(0); packState.laps.splice(0); }
    const noInitials = computed(() => st.preview && !st.preview.by);
    async function create() {
      st.busy = true;
      try {
        const r = await post("/api/datapacks", request());
        const a = document.createElement("a");
        a.href = "/api/datapacks/download/" + r.token;
        a.download = r.name;
        document.body.appendChild(a);
        a.click();
        a.remove();
        showToast(`Data pack saved to your downloads: ${r.name} (${bytes(r.bytes)}). Send it to anyone with SetupMan; they open it with Open data pack.`, 10000);
        clearAll();
        packState.open = false;
      } catch (e) { st.error = e.message; } finally { st.busy = false; }
    }
    return { dlg, st, packState, TELE, info, teleLabel, toggleSession, dateTime, lapTime, kindLabel, removeSetup, removeLap, clearAll, create, noInitials, bytes, plural };
  },
  template: `
    <dialog ref="dlg" class="confirm pack-dialog" @close="packState.open = false" aria-labelledby="pack-title">
      <h2 id="pack-title">Create data pack</h2>
      <p class="panel-hint">One ZIP file with setups, laps and telemetry for other SetupMan users. They open it with <b>Open data pack</b> on the start page. It names you only by your initials; nothing leaves this PC unless you send the file.</p>
      <p v-if="noInitials" class="notice notice-tight">Your initials name you as the sender. Set them in <router-link to="/settings?tab=general" @click="packState.open = false">Settings → General</router-link> (Naming standard) first.</p>

      <p v-if="!packState.setups.length && !packState.laps.length" class="pack-empty">Nothing in the pack yet. Add setups with <b>Add to data pack</b> on a setup page or the setup list of a track page, and laps from a Telemetry session or Lap analysis.</p>

      <section v-if="packState.setups.length" class="pack-section">
        <h3>Setups</h3>
        <div v-for="(s, i) in packState.setups" :key="s.id" class="pack-setup">
          <div class="pack-setup-head">
            <div>
              <router-link :to="'/setup/' + s.id" class="pack-name" @click="packState.open = false">{{ info(s.id)?.name || s.name }}</router-link>
              <span v-if="info(s.id)" class="pack-where">{{ info(s.id).short }} · {{ info(s.id).carName }} · {{ info(s.id).trackName }}</span>
            </div>
            <button type="button" class="btn btn-sm btn-quiet" @click="removeSetup(i)" :aria-label="'Remove ' + s.name + ' from the pack'">Remove</button>
          </div>
          <div class="pack-opts">
            <label class="pack-tele">Telemetry
              <select v-model="s.telemetry">
                <option v-for="t in TELE" :key="t[0]" :value="t[0]">{{ teleLabel(s.id, t[0]) }}</option>
              </select>
            </label>
            <label><input type="checkbox" v-model="s.lapTimes"> Lap time summary</label>
            <label><input type="checkbox" v-model="s.history"> Full history<template v-if="s.history && info(s.id)"> ({{ plural(info(s.id).versions, 'version') }})</template></label>
            <label><input type="checkbox" v-model="s.note"> My notes</label>
          </div>
          <div v-if="s.telemetry === 'chosen'" class="pack-sessions">
            <p v-if="info(s.id) && !info(s.id).sessionList.length" class="pack-where">No sessions with telemetry for this setup.</p>
            <label v-for="x in (info(s.id)?.sessionList || [])" :key="x.key" class="pack-session">
              <input type="checkbox" :checked="(s.sessions || []).includes(x.key)" @change="toggleSession(s, x.key)">
              <span>{{ dateTime(x.started) }}<template v-if="x.kind"> · {{ kindLabel(x.kind) }}</template></span>
              <span class="pack-where">{{ plural(x.laps, 'lap') }}<template v-if="x.bestMs">, fastest {{ lapTime(x.bestMs) }}</template></span>
            </label>
          </div>
        </div>
      </section>

      <section v-if="packState.laps.length" class="pack-section">
        <h3>Laps picked by hand</h3>
        <ul class="pack-laps">
          <li v-for="(l, i) in packState.laps" :key="l.fileId + ':' + l.n"><span>{{ l.label }}</span>
            <button type="button" class="btn btn-sm btn-quiet" @click="removeLap(i)" :aria-label="'Remove ' + l.label">Remove</button></li>
        </ul>
      </section>

      <ul v-if="st.preview && st.preview.warnings.length" class="pack-warn"><li v-for="(w, i) in st.preview.warnings" :key="i">{{ w }}</li></ul>
      <p v-if="st.error" class="field-error">{{ st.error }}</p>

      <div class="pack-foot">
        <span v-if="st.preview" class="pack-size">{{ plural(st.preview.setups.length, 'setup') }} · {{ plural(st.preview.laps, 'lap') }} with telemetry · about {{ bytes(st.preview.bytes) }}</span>
        <span v-else></span>
        <div class="picker-actions">
          <button v-if="packState.setups.length || packState.laps.length" type="button" class="btn btn-quiet" @click="clearAll">Empty the pack</button>
          <button type="button" class="btn" @click="packState.open = false">Close</button>
          <button type="button" class="btn btn-primary" :disabled="st.busy || !st.preview || noInitials || (!st.preview.setups.length && !st.preview.laps)" @click="create">{{ st.busy ? 'Creating…' : 'Create data pack' }}</button>
        </div>
      </div>
    </dialog>`,
};

const RESULT = { new: "New", update: "New versions", present: "Already in your library", skip: "Skipped" };

export const PackImportDialog = {
  components: { ExtBadge },
  setup() {
    const dlg = ref(null);
    const router = useRouter();
    watch(() => packState.imp, (v) => {
      if (v && !dlg.value?.open) dlg.value?.showModal();
      if (!v && dlg.value?.open) dlg.value.close();
    });
    const newSetups = computed(() => (packState.imp?.pack.setups || []).filter((s) => s.result === "new" || s.result === "update").length);
    async function run() {
      const imp = packState.imp;
      imp.busy = true;
      try {
        imp.result = await post("/api/datapacks/import", { token: imp.token, stage: imp.stage });
      } catch (e) { showToast(e.message, 8000); } finally { imp.busy = false; }
    }
    function go(to) { packState.imp = null; router.push(to); }
    const close = () => { packState.imp = null; };
    return { dlg, packState, RESULT, newSetups, run, go, close, dateTime, plural, lapTime, kindLabel };
  },
  template: `
    <dialog ref="dlg" class="confirm pack-dialog" @close="close" aria-labelledby="imp-title">
      <template v-if="packState.imp">
        <template v-if="!packState.imp.result">
          <h2 id="imp-title">Data pack from {{ packState.imp.pack.by }} <ExtBadge :by="packState.imp.pack.by" /></h2>
          <p class="panel-hint">Made {{ dateTime(packState.imp.pack.created) }}<template v-if="packState.imp.pack.app"> with SetupMan {{ packState.imp.pack.app }}</template>. Everything is added to SetupMan with the External badge. Their laps can be compared with yours and used as a reference lap, but never count as your own (best laps, averages, Race prep, fuel).</p>
          <p v-if="packState.imp.pack.known" class="notice notice-tight">This data pack is already in your library (Settings → Data packs).</p>
          <section v-if="packState.imp.pack.setups.length" class="pack-section">
            <h3>Setups</h3>
            <table class="pack-table">
              <tr v-for="(s, i) in packState.imp.pack.setups" :key="i">
                <td><b>{{ s.name }}</b><span class="pack-where">{{ s.short }} · {{ s.carName }} · {{ s.trackName }}<template v-if="s.versions > 1"> · {{ plural(s.versions, 'version') }}</template></span></td>
                <td class="pack-res" :class="'res-' + s.result">{{ RESULT[s.result] }}<span v-if="s.reason" class="pack-where">{{ s.reason }}</span></td>
              </tr>
            </table>
          </section>
          <section v-if="packState.imp.pack.sessions.length" class="pack-section">
            <h3>Laps</h3>
            <table class="pack-table">
              <tr v-for="(s, i) in packState.imp.pack.sessions" :key="i">
                <td><b>{{ s.carName }}</b><span class="pack-where">{{ s.trackName }} · {{ kindLabel(s.kind) }} · {{ dateTime(s.started) }}</span></td>
                <td class="pack-res">{{ plural(s.laps, 'lap') }}<template v-if="s.bestMs">, fastest {{ lapTime(s.bestMs) }}</template><span class="pack-where">{{ s.telemetry ? (s.extra ? 'telemetry and extra log' : 'telemetry') : 'lap times only' }}</span></td>
              </tr>
            </table>
          </section>
          <ul v-if="packState.imp.pack.problems.length" class="pack-warn"><li v-for="(p, i) in packState.imp.pack.problems" :key="i">{{ p }}</li></ul>
          <label v-if="packState.imp.pack.canStage && newSetups" class="pack-stage"><input type="checkbox" v-model="packState.imp.stage"> Also put the new setups in the game folder (a folder backup is made first)</label>
          <div class="picker-actions">
            <button type="button" class="btn" @click="close">Cancel</button>
            <button type="button" class="btn btn-primary" :disabled="packState.imp.busy || packState.imp.pack.known" @click="run">{{ packState.imp.busy ? 'Importing…' : 'Import' }}</button>
          </div>
        </template>
        <template v-else>
          <h2 id="imp-title">Data pack imported</h2>
          <p>{{ plural(packState.imp.result.setups, 'new setup') }}<template v-if="packState.imp.result.updated">, {{ plural(packState.imp.result.updated, 'setup') }} with new versions</template><template v-if="packState.imp.result.present">, {{ packState.imp.result.present }} already in your library</template>; {{ plural(packState.imp.result.laps, 'lap') }}.<template v-if="packState.imp.result.staged"> {{ plural(packState.imp.result.staged, 'setup') }} put in the game folder.</template></p>
          <ul class="pack-laps">
            <li v-for="(it, k) in packState.imp.result.items" :key="k"><a v-if="it.setupId" href="#" @click.prevent="go('/setup/' + it.setupId)">{{ it.name }}</a><template v-else>{{ it.name }}</template><span v-if="it.reason" class="pack-where">{{ it.reason }}</span></li>
          </ul>
          <ul v-if="packState.imp.result.messages.length" class="pack-warn"><li v-for="(m, i) in packState.imp.result.messages" :key="i">{{ m }}</li></ul>
          <div class="picker-actions">
            <button v-if="packState.imp.result.track" type="button" class="btn" @click="go('/telemetry/' + packState.imp.result.track.game + '/' + encodeURIComponent(packState.imp.result.track.track))">Open the laps</button>
            <button type="button" class="btn btn-primary" @click="close">Done</button>
          </div>
        </template>
      </template>
    </dialog>`,
};
