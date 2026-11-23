// Setup page (spec D-40 … D-51, D-85 … D-94), in tabs:
//   Setup     – all values, change them, adjust for temperature
//   History   – every version with what changed, compare any two, where it came from
//   Lap times – recorded laps per version
//   Notes     – notes, session, details read from the name
import { ref, reactive, computed, watch, watchEffect } from "vue";
import { useRoute, useRouter } from "vue-router";
import { get, put, post, enc } from "../api.js";
import { stagingDone, showToast } from "../store.js";
import { dateTime, shortDate, pct } from "../format.js";
import { usePageData } from "./load.js";
import { Loading, setCrumbs } from "../components/common.js";
import { FamilyTree, SessionBadge } from "../components/FamilyTree.js";
import { CompareTable } from "../components/CompareTable.js";
import { SetupSheet } from "../components/SetupSheet.js";
import { LapTimes } from "../components/LapTimes.js";
import { TeleLinks } from "../components/TeleLinks.js";

const TABS = [["setup", "Setup"], ["history", "History"], ["laps", "Lap times"], ["notes", "Notes & details"]];

export const SetupPage = {
  components: { Loading, FamilyTree, SessionBadge, CompareTable, SetupSheet, LapTimes, TeleLinks },
  setup() {
    const route = useRoute();
    const router = useRouter();
    const { data, error } = usePageData((p) => get("/api/setups/" + enc(p.id)), { laps: true });
    const form = reactive({ note: "", meta: null });
    const saved = ref("");
    const saveError = ref("");
    const relink = reactive({ open: false, setupId: 0, versionId: 0, error: "" });
    const open = ref({}); // version id → "What changed?" is open
    const diffs = reactive({}); // version id → [previous, this] for the comparison table
    const cmp = reactive({ from: 0, to: 0, cols: [] }); // compare any two versions
    let noteTimer, savedTimer;

    // Tabs live in the address (?tab=history), so a tab can be linked to and survives a reload.
    const tab = computed(() => (TABS.some((t) => t[0] === route.query.tab) ? route.query.tab : "setup"));
    const setTab = (t) => router.replace({ query: { ...route.query, tab: t === "setup" ? undefined : t } });

    watchEffect(() => {
      if (!data.value) return;
      const d = data.value;
      const g = d.setup.game;
      setCrumbs([
        { label: d.gameShort || d.gameName || g, to: "/" + g },
        { label: d.carName, to: `/${g}/${d.setup.car}` },
        { label: d.trackName, to: `/${g}/${d.setup.car}/${d.trackId}` },
        { label: d.setup.name },
      ]);
    });

    // Fill the form when a (different) setup loads; keep typing undisturbed on reloads.
    let loadedId = 0;
    watch(data, (d) => {
      if (!d || d.setup.id === loadedId) return;
      loadedId = d.setup.id;
      form.note = d.setup.note;
      form.meta = JSON.parse(JSON.stringify(d.setup.meta));
      form.meta.codesText = (d.setup.meta.codes || []).join(" ");
      cmp.from = d.versions[1]?.id || 0;
      cmp.to = d.versions[0]?.id || 0;
      open.value = d.versions.length > 1 ? { [d.versions[0].id]: true } : {}; // newest change open
    });

    function flash() {
      saved.value = "Saved";
      clearTimeout(savedTimer);
      savedTimer = setTimeout(() => (saved.value = ""), 1600);
    }
    async function save(body) {
      try {
        await put("/api/setups/" + data.value.setup.id, body);
        saveError.value = "";
        flash();
      } catch (e) {
        saveError.value = e.message;
      }
    }
    function noteChanged() {
      clearTimeout(noteTimer);
      noteTimer = setTimeout(() => save({ note: form.note }), 600);
    }
    const setSession = (s) => save({ session: s });
    function saveMeta() {
      const m = { ...form.meta };
      m.codes = (m.codesText || "").split(/[\s,]+/).filter(Boolean);
      delete m.codesText;
      m.air = Number(m.air) || 0;
      m.track = Number(m.track) || 0;
      m.tempMin = Number(m.tempMin) || 0;
      m.tempMax = Number(m.tempMax) || 0;
      m.raceMinutes = Number(m.raceMinutes) || 0;
      m.stops = Number(m.stops) || 0;
      save({ meta: m });
    }

    // Where it came from: change the link (spec D-43).
    function openRelink() {
      const p = data.value.parent;
      relink.setupId = p ? p.setupId : 0;
      relink.versionId = p ? p.versionId : 0;
      relink.error = "";
      relink.open = true;
    }
    const relinkVersions = computed(() => {
      const o = data.value?.relinkOptions?.find((x) => x.setupId === Number(relink.setupId));
      return o ? o.versions : [];
    });
    watch(() => relink.setupId, () => {
      const vs = relinkVersions.value;
      if (vs.length && !vs.some((v) => v.id === Number(relink.versionId))) relink.versionId = vs[vs.length - 1].id;
    });
    async function applyRelink(noParent) {
      try {
        await put(`/api/setups/${data.value.setup.id}/parent`, { parentVersionId: noParent ? 0 : Number(relink.versionId) });
        relink.open = false;
      } catch (e) {
        relink.error = e.message;
      }
    }

    // History: a version's values never change, so each is loaded once.
    const cache = new Map();
    const loadVersion = (id) => {
      if (!cache.has(id)) cache.set(id, get("/api/versions/" + id));
      return cache.get(id);
    };
    const asColumn = (r, name) => ({ id: "v" + r.version.id + name, versionId: r.version.id, name, relPath: dateTime(r.version.fileModified), fields: r.fields });
    // Lap times of the compared versions (D-141): what a change was worth on track.
    const lapsOf = (cols) => cols.map((c) => (laps.value?.versions || []).find((v) => v.versionId === c.versionId) || null);

    // "What changed?": a version next to the one before it.
    const toggleDiff = (v) => { open.value = { ...open.value, [v.id]: !open.value[v.id] }; };
    watch([open, data], async () => {
      if (!data.value) return;
      const vs = data.value.versions;
      for (const [i, v] of vs.entries()) {
        if (!open.value[v.id] || diffs[v.id] || !vs[i + 1]) continue;
        const [a, b] = await Promise.all([loadVersion(vs[i + 1].id), loadVersion(v.id)]);
        diffs[v.id] = [asColumn(a, "Version " + a.version.seq), asColumn(b, "Version " + b.version.seq)];
      }
    }, { immediate: true });

    watch(() => [cmp.from, cmp.to], async ([f, t]) => {
      if (!f || !t || f === t) { cmp.cols = []; return; }
      const [a, b] = await Promise.all([loadVersion(f), loadVersion(t)]);
      cmp.cols = [asColumn(a, "Version " + a.version.seq), asColumn(b, "Version " + b.version.seq)];
    });

    // Compare with the setup it was made from.
    const parentCols = ref([]);
    async function compareWithParent() {
      if (parentCols.value.length) { parentCols.value = []; return; }
      const p = data.value.parent;
      const first = data.value.versions[data.value.versions.length - 1];
      const [a, b] = await Promise.all([loadVersion(p.versionId), loadVersion(first.id)]);
      parentCols.value = [asColumn(a, `${p.name} v${a.version.seq}`), asColumn(b, `${data.value.setup.name} v${b.version.seq}`)];
    }

    // The setup sheet: one version's values (newest by default).
    const sheet = reactive({ seq: 0, fields: null, versionId: 0 });
    async function viewVersion(id, switchTab) {
      const r = await loadVersion(id);
      Object.assign(sheet, { seq: r.version.seq, fields: r.fields, versionId: id });
      if (switchTab) setTab("setup");
    }
    watch(data, (d) => {
      if (d && (!sheet.versionId || !d.versions.some((v) => v.id === sheet.versionId))) viewVersion(d.versions[0].id);
    });

    // Lap times (spec D-44, D-48), reloaded with the page.
    const laps = ref(null);
    watch(data, async (d) => {
      if (d) laps.value = await get(`/api/setups/${d.setup.id}/laps`).catch(() => null);
    });
    async function assignSession({ sessionId, setupId }) {
      try {
        await put(`/api/sessions/${sessionId}/setup`, { setupId });
        showToast(setupId ? "Session moved to that setup." : "Session marked as unknown setup.");
      } catch (e) { showToast(e.message, 7000); }
    }

    // Changing values (spec D-85): save as a new version, or as a new setup made from this one.
    const saving = ref(false);
    async function saveEdits({ edits, newName }) {
      saving.value = true;
      try {
        const res = await post(`/api/setups/${data.value.setup.id}/edit`, { edits, newName });
        stagingDone(res, res.message);
        if (newName && res.setupId) router.push("/setup/" + res.setupId);
        else sheet.versionId = 0; // show the new version once the page reloads
      } catch (e) {
        showToast(e.message, 7000);
      } finally {
        saving.value = false;
      }
    }

    // The "More" menu: rename, adjust for temperature, share, download.
    const panel = ref(""); // "", "rename", "temp", "share"
    const menu = ref(null);
    const newName = ref("");
    function openPanel(p) {
      panel.value = panel.value === p ? "" : p;
      if (p === "rename") newName.value = data.value.setup.name;
      if (p === "temp") setTab("setup");
      if (menu.value) menu.value.open = false;
    }
    async function rename(name) {
      busy.value = true;
      try {
        const r = await post(`/api/setups/${data.value.setup.id}/rename`, { name });
        panel.value = "";
        showToast(`Renamed to ${r.name}.`);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    const share = reactive({ note: false, laps: true });
    const temp = reactive({ air: "", track: "", plan: null, error: "" });
    async function proposeTemp() {
      temp.plan = null; temp.error = "";
      try { temp.plan = await post(`/api/setups/${data.value.setup.id}/temperature`, { air: Number(temp.air), track: Number(temp.track) }); }
      catch (e) { temp.error = e.message; }
    }
    function saveTemp() {
      saveEdits({ edits: temp.plan.edits, newName: temp.plan.newName });
      panel.value = ""; temp.plan = null;
    }

    // Shown in ACC or not (staging, spec D-73 … D-77, D-98).
    const inAcc = computed(() => !!data.value?.setup.relPath);
    const diskSeq = computed(() => data.value?.versions.find((v) => v.id === data.value.setup.diskVersionId)?.seq || 0);
    const diskBehind = computed(() => data.value && inAcc.value && diskSeq.value && data.value.setup.diskVersionId !== data.value.versions[0].id);
    const busy = ref(false);
    async function stage(versionId) {
      busy.value = true;
      try {
        const res = await post(`/api/setups/${data.value.setup.id}/stage`, { versionId });
        stagingDone(res, versionId ? `${G.value} now has that version.` : `${data.value.setup.name} is shown in ${G.value}.`);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    async function unstage() {
      busy.value = true;
      try {
        stagingDone(await post(`/api/setups/${data.value.setup.id}/unstage`), `${data.value.setup.name} is hidden from ${G.value}. It stays safe in SetupMan.`);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }

    // Delete into the bin, and restore (D-102).
    const delDlg = ref(null);
    async function doDelete() {
      busy.value = true;
      try {
        const r = (await post("/api/setups/delete", { ids: [data.value.setup.id] })).results[0];
        delDlg.value.close();
        if (r && r.error) showToast(r.error, 9000);
        else { showToast(`${data.value.setup.name} deleted (in the bin for 30 days).`, 6000); router.push(`/${data.value.setup.game}/${data.value.setup.car}/${data.value.trackId}`); }
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    async function undelete() {
      busy.value = true;
      try { await post(`/api/setups/${data.value.setup.id}/undelete`); showToast(`Restored. It is hidden from ${G.value}; use Show in ${G.value} to use it.`); }
      catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }

    const versionText = (v, i) => {
      if (v.source === "restored") return v.note || "An older version put back";
      if (i === data.value.versions.length - 1) return data.value.parent ? "First version, made from " + data.value.parent.name : "First version";
      return v.changes || "Only ignored settings changed (for example telemetry laps)";
    };
    const linkText = { probable: "SetupMan's guess, from file dates and how alike they are", observed: "Detected when the file appeared", manual: "Linked by you" };
    const tabCount = (t) => (t === "history" ? data.value?.versions.length : t === "laps" ? laps.value?.overall?.laps || 0 : 0);
    const G = computed(() => data.value?.gameShort || "the game"); // short game name, e.g. "AC EVO"
    return { lapsOf, G, data, error, form, saved, saveError, relink, relinkVersions, linkText, TABS, tab, setTab, tabCount,
      noteChanged, setSession, saveMeta, openRelink, applyRelink, dateTime, shortDate, pct,
      open, diffs, toggleDiff, cmp, parentCols, compareWithParent, versionText,
      busy, stage, unstage, inAcc, sheet, viewVersion, saving, saveEdits, diskSeq, diskBehind, laps, assignSession,
      delDlg, doDelete, undelete,
      panel, menu, openPanel, newName, rename, temp, proposeTemp, saveTemp, share };
  },
  template: `
    <main class="page">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <header class="setup-head">
          <div class="setup-title">
            <h1 class="page-title">{{ data.setup.name }}</h1>
            <p class="page-sub">
              <SessionBadge :session="data.setup.session" />
              <span>{{ data.carName }}, {{ data.trackName }}</span>
              <span v-if="data.managed" class="where" :class="{ 'where-in': inAcc }">{{ inAcc ? 'Shown in ' + G : 'Hidden from ' + G }}</span>
              <span v-else-if="!inAcc" class="where">No longer in {{ G }}'s folder</span>
              <span class="save-state" aria-live="polite"><span v-if="saved" class="saved">{{ saved }}</span><span v-if="saveError" class="field-error">{{ saveError }}</span></span>
            </p>
            <p v-if="data.suggestedName && panel !== 'rename'" class="suggest">Suggested name (naming standard): <b>{{ data.suggestedName }}</b>
              <button type="button" class="btn btn-sm" :disabled="busy" @click="rename(data.suggestedName)">Use this name</button></p>
          </div>
          <div class="setup-actions">
            <template v-if="data.managed">
              <button v-if="inAcc" type="button" class="btn" :disabled="busy" @click="unstage" :title="G + ' stops listing this setup. SetupMan keeps it, and you can show it again any time.'">Hide from {{ G }}</button>
              <button v-else type="button" class="btn btn-primary" :disabled="busy" @click="stage(0)" :title="'Puts this setup back in the setups folder of ' + G">Show in {{ G }}</button>
            </template>
            <router-link :to="'/' + data.setup.game + '/' + data.setup.car + '/' + data.trackId + '/fuel'" class="btn">Fuel calculator</router-link>
            <details ref="menu" class="menu">
              <summary class="btn">More <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2.5 4.5 6 8l3.5-3.5" fill="none" stroke="currentColor" stroke-width="1.6"/></svg></summary>
              <div class="menu-list" role="menu">
                <button v-if="!data.readOnly" type="button" role="menuitem" @click="openPanel('rename')">Rename</button>
                <button v-if="data.canEdit" type="button" role="menuitem" @click="openPanel('temp')">Adjust for another temperature</button>
                <button v-if="!data.readOnly" type="button" role="menuitem" @click="openPanel('share')">Share…</button>
                <a role="menuitem" :href="'/api/export?ids=' + data.setup.id" download @click="menu.open = false">{{ data.readOnly ? 'Download the settings (.zip)' : 'Download the ' + G + ' file (.zip)' }}</a>
                <button v-if="!data.readOnly" type="button" role="menuitem" class="menu-danger" @click="menu.open = false; delDlg.showModal()">Delete…</button>
              </div>
            </details>
          </div>
        </header>

        <section v-if="panel === 'rename'" class="panel tool-panel">
          <h2 class="panel-title">Rename</h2>
          <p class="panel-hint">{{ inAcc && data.managed ? 'The file in ' + G + ' is renamed too.' : 'Changes the name in SetupMan.' }}</p>
          <div class="row-actions">
            <input v-model="newName" class="edit-name" aria-label="New name" @keyup.enter="rename(newName)">
            <button type="button" class="btn btn-primary" :disabled="busy || !newName.trim()" @click="rename(newName)">Rename</button>
            <button type="button" class="btn btn-quiet" @click="panel = ''">Cancel</button>
          </div>
        </section>
        <section v-if="panel === 'share'" class="panel tool-panel">
          <h2 class="panel-title">Share</h2>
          <p class="panel-hint">Makes one file with this setup to send (Discord, mail). The other person opens it in SetupMan with <b>Open a shared setup</b> on the start page, or drops it on the page.</p>
          <label class="share-opt"><input type="checkbox" v-model="share.note"> Include my notes</label>
          <label class="share-opt"><input type="checkbox" v-model="share.laps"> Include my lap times (best, average, fuel per lap)</label>
          <div class="row-actions">
            <a class="btn btn-primary" :href="'/api/setups/' + data.setup.id + '/share?note=' + (share.note ? 1 : 0) + '&laps=' + (share.laps ? 1 : 0)" download @click="panel = ''">Download the file</a>
            <button type="button" class="btn btn-quiet" @click="panel = ''">Cancel</button>
          </div>
        </section>

        <div v-if="data.setup.deletedAt && !data.setup.deletedAt.startsWith('0001')" class="banner" role="status">
          <h2>This setup is in the bin</h2>
          <p class="panel-hint panel-hint-tight">It is permanently deleted 30 days after it was deleted.</p>
          <div><button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="undelete">Restore</button></div>
        </div>
        <dialog ref="delDlg" class="confirm">
          <h2>Delete {{ data.setup.name }}?</h2>
          <p>It is removed from {{ G }} and from SetupMan's lists, with all {{ data.versions.length }} versions. The bin keeps it for 30 days; you can restore it in Settings → Bin until then.</p>
          <div class="picker-actions">
            <button type="button" class="btn" @click="delDlg.close()">Cancel</button>
            <button type="button" class="btn btn-danger-solid" :disabled="busy" @click="doDelete">Delete</button>
          </div>
        </dialog>

        <nav class="tabs" role="tablist" aria-label="About this setup">
          <button v-for="t in TABS" :key="t[0]" type="button" role="tab" :aria-selected="tab === t[0]" :class="{ on: tab === t[0] }" @click="setTab(t[0])">
            {{ t[1] }}<span v-if="tabCount(t[0])" class="tab-count">{{ tabCount(t[0]) }}</span>
          </button>
        </nav>

        <!-- Setup -->
        <template v-if="tab === 'setup'">
          <section v-if="panel === 'temp'" class="panel tool-panel" aria-labelledby="temp-h">
            <h2 id="temp-h" class="panel-title">Adjust for another temperature</h2>
            <p class="panel-hint">SetupMan looks at your own setups of this car that have a temperature in their name, and copies how you changed tyre pressures and brake ducts per degree of track temperature. Enter the air and track temperature as the game shows them; the changes follow the track temperature, and both go into the new name. Nothing is saved until you choose to; the result becomes a new setup.</p>
            <div class="row-actions">
              <label>Air <input v-model="temp.air" type="number" min="5" max="45" class="num-in"> °C</label>
              <label>Track <input v-model="temp.track" type="number" min="5" max="65" class="num-in"> °C</label>
              <button type="button" class="btn btn-primary" :disabled="!temp.air || !temp.track" @click="proposeTemp">Show changes</button>
              <button type="button" class="btn btn-quiet" @click="panel = ''">Close</button>
            </div>
            <p v-if="temp.error" class="field-error" role="alert">{{ temp.error }}</p>
            <template v-if="temp.plan">
              <p class="lap-sub">From {{ temp.plan.from }} to {{ temp.plan.to }}</p>
              <p v-if="temp.plan.message" class="panel-hint">{{ temp.plan.message }}</p>
              <table v-if="temp.plan.changes.length" class="lap-table">
                <thead><tr><th>Setting</th><th>Now</th><th>Proposed</th></tr></thead>
                <tbody><tr v-for="c in temp.plan.changes" :key="c.path"><td>{{ c.label }}</td><td>{{ c.from }}</td><td><b>{{ c.to }}</b></td></tr></tbody>
              </table>
              <p v-if="temp.plan.rules.length" class="muted-small">Based on {{ temp.plan.pairs }} pair{{ temp.plan.pairs === 1 ? '' : 's' }} of your own setups. Numbers are {{ G }}'s clicks.</p>
              <div v-if="temp.plan.changes.length" class="row-actions">
                <button type="button" class="btn btn-primary" :disabled="saving" @click="saveTemp">Save as new setup “{{ temp.plan.newName }}”</button>
              </div>
            </template>
          </section>

          <section class="panel sheet-panel" aria-labelledby="sheet-h">
            <div class="sheet-head">
              <h2 id="sheet-h" class="panel-title">Version {{ sheet.seq }}<template v-if="sheet.seq && sheet.seq !== data.versions[0].seq"> (an older version)</template></h2>
              <button v-if="sheet.seq && sheet.seq !== data.versions[0].seq" type="button" class="btn btn-sm" @click="viewVersion(data.versions[0].id)">Show the newest version</button>
            </div>
            <p v-if="diskBehind" class="notice">
              {{ G }} has version {{ diskSeq }}. The newest version ({{ data.versions[0].seq }}) is only in SetupMan.
              <button v-if="data.managed" type="button" class="btn btn-sm" :disabled="busy" @click="stage(0)">Give {{ G }} the newest version</button>
            </p>
            <SetupSheet v-if="sheet.fields" :fields="sheet.fields" :editable="data.canEdit && sheet.versionId === data.versions[0].id" :saving="saving" @save="saveEdits" />
            <p v-else class="panel-hint">Loading…</p>
          </section>
        </template>

        <!-- History -->
        <template v-else-if="tab === 'history'">
          <section class="panel" aria-labelledby="ver-h">
            <h2 id="ver-h" class="panel-title">Versions</h2>
            <p class="panel-hint">Every time this setup changes, SetupMan keeps a copy. Newest first. <b>Show changes</b> lists the differences from the previous version.</p>
            <ol class="timeline">
              <li v-for="(v, i) in data.versions" :key="v.id" :class="{ 'tl-open': open[v.id] }">
                <div class="tl-row">
                  <span class="tl-seq">v{{ v.seq }}</span>
                  <div class="tl-main">
                    <span class="tl-what">{{ versionText(v, i) }}</span>
                    <span class="tl-when">{{ dateTime(v.fileModified) }}<template v-if="v.source === 'import'"> (file date)</template>
                      <span v-if="v.id === data.setup.diskVersionId" class="where where-in">In {{ G }}</span>
                      <span v-if="i === 0" class="where">Newest</span>
                    </span>
                  </div>
                  <div class="tl-actions">
                    <button v-if="data.versions[i + 1]" type="button" class="btn btn-sm" :aria-expanded="!!open[v.id]" @click="toggleDiff(v)">{{ open[v.id] ? 'Hide changes' : 'Show changes' }}</button>
                    <button type="button" class="btn btn-sm btn-quiet" @click="viewVersion(v.id, true)">All values</button>
                    <button v-if="data.managed && v.id !== data.setup.diskVersionId" type="button" class="btn btn-sm btn-quiet" :disabled="busy" @click="stage(v.id)" :title="G + ' gets exactly this version'">Use this version in {{ G }}</button>
                  </div>
                </div>
                <div v-if="open[v.id]" class="tl-diff">
                  <CompareTable v-if="diffs[v.id]" :setups="diffs[v.id]" :laps="lapsOf(diffs[v.id])" />
                  <p v-else class="panel-hint">Loading…</p>
                </div>
              </li>
            </ol>
          </section>

          <section v-if="data.versions.length > 2" class="panel section-gap" aria-labelledby="cmp-h">
            <h2 id="cmp-h" class="panel-title">Compare two versions</h2>
            <div class="row-actions cmp-pick">
              <label>From <select v-model="cmp.from"><option v-for="v in data.versions" :key="v.id" :value="v.id">Version {{ v.seq }}, {{ shortDate(v.fileModified) }}</option></select></label>
              <label>to <select v-model="cmp.to"><option v-for="v in data.versions" :key="v.id" :value="v.id">Version {{ v.seq }}, {{ shortDate(v.fileModified) }}</option></select></label>
            </div>
            <CompareTable v-if="cmp.cols.length === 2" :setups="cmp.cols" :laps="lapsOf(cmp.cols)" />
            <p v-else class="panel-hint">Choose two different versions.</p>
          </section>

          <section class="panel section-gap" aria-labelledby="from-h">
            <h2 id="from-h" class="panel-title">Where it came from</h2>
            <template v-if="data.parent">
              <p class="came-from">
                Made from <router-link :to="'/setup/' + data.parent.setupId">{{ data.parent.name }}</router-link>, version {{ data.parent.seq }}<template v-if="data.parent.score"> ({{ Math.floor(data.parent.score) }}% alike)</template>.
              </p>
              <p class="panel-hint panel-hint-tight">{{ linkText[data.parent.linkKind] }}.</p>
            </template>
            <p v-else class="came-from">This setup is the start of its own family.<span v-if="data.setup.linkKind === 'manual'" class="panel-hint"> (Set by you.)</span></p>
            <div class="row-actions">
              <button v-if="data.parent" type="button" class="btn" @click="compareWithParent">{{ parentCols.length ? 'Hide the comparison' : 'Compare with ' + data.parent.name }}</button>
              <button type="button" class="btn btn-quiet" @click="openRelink">Change parent</button>
            </div>
            <CompareTable v-if="parentCols.length === 2" :setups="parentCols" />

            <div v-if="relink.open" class="relink">
              <label>Made from setup
                <select v-model="relink.setupId">
                  <option :value="0" disabled>Choose a setup</option>
                  <option v-for="o in data.relinkOptions" :key="o.setupId" :value="o.setupId">{{ o.name }}</option>
                </select>
              </label>
              <label v-if="relinkVersions.length > 1">Version
                <select v-model="relink.versionId">
                  <option v-for="v in relinkVersions" :key="v.id" :value="v.id">Version {{ v.seq }}, {{ shortDate(v.date) }}</option>
                </select>
              </label>
              <p v-if="relink.error" class="field-error">{{ relink.error }}</p>
              <div class="row-actions">
                <button type="button" class="btn btn-primary" :disabled="!relink.setupId" @click="applyRelink(false)">Save link</button>
                <button type="button" class="btn" @click="applyRelink(true)">It's the start of its own family</button>
                <button type="button" class="btn btn-quiet" @click="relink.open = false">Cancel</button>
              </div>
              <p class="panel-hint panel-hint-tight">Links you set yourself are never changed by SetupMan.</p>
            </div>

            <h3 class="sub-title">Family</h3>
            <p class="panel-hint panel-hint-tight">Setups made from each other. This one has a red border.</p>
            <FamilyTree v-if="data.family" :root="data.family" :current="data.setup.id" />
          </section>
        </template>

        <!-- Lap times -->
        <section v-else-if="tab === 'laps'" class="panel">
          <LapTimes :laps="laps" :meta="data.setup.meta" :setup-id="data.setup.id" :options="data.relinkOptions" @assign="assignSession" />
          <TeleLinks :url="'/api/setups/' + data.setup.id + '/telemetry'" title="Telemetry with this setup" />
        </section>

        <!-- Notes & details -->
        <div v-else class="notes-layout">
          <section class="panel">
            <h2 class="panel-title">Notes</h2>
            <p class="panel-hint">Anything the setup file can't hold, like which TC settings you put on your wheel. Saved as you type.</p>
            <textarea v-model="form.note" @input="noteChanged" class="note" rows="8" placeholder="For example: TC shortcuts 2 and 5"></textarea>
          </section>
          <section class="panel">
            <h2 class="panel-title">Session</h2>
            <div class="segmented" role="radiogroup" aria-label="Session">
              <button v-for="s in [['Q','Qualifying'],['R','Race'],['Endurance','Endurance'],['','Not set']]" :key="s[0]" type="button" role="radio"
                :aria-checked="data.setup.session === s[0]" :class="{ on: data.setup.session === s[0] }" @click="setSession(s[0])">{{ s[1] }}</button>
            </div>
            <p class="panel-hint panel-hint-tight">
              <template v-if="data.setup.sessionManual">Set by you. <button type="button" class="btn-link" @click="setSession('auto')">Detect automatically</button></template>
              <template v-else>Guessed from the name and the stint length.</template>
            </p>

            <h2 class="panel-title panel-title-gap">Details</h2>
            <p class="panel-hint">{{ data.setup.metaManual ? 'Edited by you.' : 'Read from the setup name. Correct them if needed.' }}</p>
            <div v-if="form.meta" class="meta-grid">
              <label>Initials<input v-model="form.meta.initials" @change="saveMeta" placeholder="e.g. ABC"></label>
              <label>Game version<input v-model="form.meta.gameVersion" @change="saveMeta" :placeholder="data.guessedVersion ? data.guessedVersion + ' (guess from the file date)' : 'e.g. 1.10.4'"></label>
              <label>Air temperature (°C)<input v-model="form.meta.air" @change="saveMeta" inputmode="numeric" placeholder="e.g. 30"></label>
              <label>Track temperature (°C)<input v-model="form.meta.track" @change="saveMeta" inputmode="numeric" placeholder="e.g. 40"></label>
              <label v-if="form.meta.tempMin">Temperature range from (°C)<input v-model="form.meta.tempMin" @change="saveMeta" inputmode="numeric"></label>
              <label v-if="form.meta.tempMin">to (°C)<input v-model="form.meta.tempMax" @change="saveMeta" inputmode="numeric"></label>
              <label>Lap time<input v-model="form.meta.lapTime" @change="saveMeta" placeholder="e.g. 1:42.9"></label>
              <label>Your own codes<input v-model="form.meta.codesText" @change="saveMeta" placeholder="e.g. TC25"></label>
              <label>Race length (minutes)<input v-model="form.meta.raceMinutes" @change="saveMeta" inputmode="numeric" placeholder="e.g. 45"></label>
              <label>Pit stops<input v-model="form.meta.stops" @change="saveMeta" inputmode="numeric" placeholder="0"></label>
            </div>
            <p class="panel-hint panel-hint-tight">Temperatures as the game shows them: “3040” in a name is air 30 °C, track 40 °C. A range (“23-27C”) or a single temperature (“23C”) in a name is shown as a range. Race length and pit stops give the stint length, which decides Race or Endurance.</p>
          </section>
        </div>
      </template>
    </main>`,
};
