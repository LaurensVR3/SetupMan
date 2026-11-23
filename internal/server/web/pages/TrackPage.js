// Track page: the setups of one car on one track, in tabs (spec D-30, D-73 … D-80, D-91, D-96, D-98):
//   Setups         – sortable list, which ones ACC shows, actions on the selected setups, adding setups
//   Compare        – the selected setups side by side
//   Similarity map – how alike every pair is
//   Families       – which setup was made from which, as trees
import { ref, computed, watch, watchEffect } from "vue";
import { useRoute, useRouter } from "vue-router";
import { get, post, put, enc } from "../api.js";
import { useDrop, readFiles } from "../drop.js";
import { store, stagingDone, showToast } from "../store.js";
import { dateTime, pct, plural } from "../format.js";
import { usePageData } from "./load.js";
import { Loading, setCrumbs } from "../components/common.js";
import { SimilarityGrid } from "../components/SimilarityGrid.js";
import { CompareTable } from "../components/CompareTable.js";
import { SessionBadge, FamilyNode } from "../components/FamilyTree.js";
import { DuplicatesDialog } from "../components/Duplicates.js";

const TABS = [["setups", "Setups"], ["compare", "Compare"], ["map", "Similarity map"], ["families", "Families"]];
const SESSION_RANK = { Q: 1, R: 2, Endurance: 3 };

export const TrackPage = {
  components: { Loading, SimilarityGrid, CompareTable, SessionBadge, FamilyNode, DuplicatesDialog },
  setup() {
    const route = useRoute();
    const router = useRouter();
    const { data, error } = usePageData((p) => get(`/api/games/${enc(p.game)}/cars/${enc(p.car)}/tracks/${enc(p.track)}`));
    const tab = computed(() => (TABS.some((t) => t[0] === route.query.tab) ? route.query.tab : "setups"));
    const setTab = (t) => router.replace({ query: { ...route.query, tab: t === "setups" ? undefined : t } });

    watchEffect(() => {
      if (!data.value) return;
      const { game: g, car: c, track: t } = data.value;
      setCrumbs([{ label: g.short || g.name, to: "/" + g.id }, { label: c.name, to: `/${g.id}/${c.id}` }, { label: t.name }]);
    });

    // One list of all setups: those in ACC's folder (with an index into data.setups) and hidden ones.
    const rows = computed(() => {
      const d = data.value;
      if (!d) return [];
      const pos = new Map(d.clusters.order.map((s, i) => [s, i + 1]));
      const shown = d.setups.map((s, i) => ({ key: "s" + i, i, id: d.setupIds[i], name: s.name, date: s.modified, session: d.sessions[i],
        version: d.versions?.[i] || "", guessed: !!d.versionGuessed?.[i], inAcc: true, group: d.clusters.cluster[i], pos: pos.get(i) }));
      const hidden = d.removed.map((r) => ({ key: "r" + r.id, i: -1, id: r.id, name: r.name, date: r.date, session: r.session, version: r.version || "", guessed: !!r.versionGuessed, inAcc: false, group: 0, pos: 999 }));
      return [...shown, ...hidden];
    });

    // Sorting: click a column title; click again to reverse.
    const sort = ref({ by: "pos", dir: 1 });
    function sortBy(by) { sort.value = { by, dir: sort.value.by === by ? -sort.value.dir : by === "date" ? -1 : 1 }; }
    const sorted = computed(() => {
      const { by, dir } = sort.value;
      const val = (r) => by === "name" ? r.name.toLowerCase() : by === "date" ? new Date(r.date || 0).getTime()
        : by === "session" ? SESSION_RANK[r.session] || 9 : by === "version" ? versionKey(r.version) : by === "acc" ? (r.inAcc ? 0 : 1) : r.pos;
      return [...rows.value].sort((a, b) => (val(a) < val(b) ? -dir : val(a) > val(b) ? dir : a.pos - b.pos));
    });
    // "1.10.4" sorts after "1.9.2": each part as a number, padded; no version sorts first.
    const versionKey = (v) => (v ? v.split(".").map((p) => String(parseInt(p, 10) || 0).padStart(4, "0")).join(".") : "");
    const sortMark = (by) => (sort.value.by !== by ? "" : sort.value.dir > 0 ? "▲" : "▼");

    // Selection (setup ids).
    const selected = ref([]);
    const dupDlg = ref(null);
    const isSel = (r) => selected.value.includes(r.id);
    function toggle(r) { selected.value = isSel(r) ? selected.value.filter((x) => x !== r.id) : [...selected.value, r.id]; }
    const allSelected = computed(() => rows.value.length > 0 && rows.value.every(isSel));
    function toggleAll() { selected.value = allSelected.value ? [] : rows.value.map((r) => r.id); }
    const selShown = computed(() => rows.value.filter((r) => r.inAcc && isSel(r)));
    const selHidden = computed(() => rows.value.filter((r) => !r.inAcc && isSel(r)));
    // Compare shows exactly the selected setups. With fewer than two selected it shows the most
    // similar pair instead (nothing is ticked for you, so your own ticks are never mixed with it).
    // Setups hidden from the game are compared with their newest version (D-138).
    const bestPair = computed(() => (data.value ? [...data.value.pairs].sort((x, y) => y.score - x.score)[0] : null));
    const selCount = computed(() => selShown.value.length + selHidden.value.length);
    const autoPicked = computed(() => selCount.value < 2 && !!bestPair.value);
    const hiddenCols = ref({}); // setup id -> column with its newest values
    watch(selHidden, async (list) => {
      for (const r of list) {
        if (hiddenCols.value[r.id]) continue;
        try {
          const x = await get(`/api/setups/${r.id}/latest`);
          hiddenCols.value = { ...hiddenCols.value, [r.id]: { id: "h" + r.id, name: x.name, relPath: `Hidden from ${G.value} (newest version)`, fields: x.fields } };
        } catch (e) { showToast(e.message, 7000); }
      }
    });
    const compareIdx = computed(() => (selCount.value >= 2 ? selShown.value.map((r) => r.i) : bestPair.value ? [bestPair.value.a, bestPair.value.b] : []));
    const chosen = computed(() => {
      const shown = compareIdx.value.map((i) => data.value.setups[i]);
      return selCount.value >= 2 ? [...shown, ...selHidden.value.map((r) => hiddenCols.value[r.id]).filter(Boolean)] : shown;
    });
    // Setup ids in the same order as the compared columns.
    const chosenIds = computed(() => {
      if (!data.value) return [];
      if (selCount.value >= 2) return [...selShown.value.map((r) => r.id), ...selHidden.value.filter((r) => hiddenCols.value[r.id]).map((r) => r.id)];
      return compareIdx.value.map((i) => data.value.setupIds[i]);
    });
    const chosenLaps = computed(() => chosenIds.value.map((id) => perSetup.value[id] || null));
    const pair = computed(() => {
      if (!data.value || compareIdx.value.length !== 2 || chosen.value.length !== 2) return null;
      const [a, b] = compareIdx.value;
      return data.value.pairs.find((p) => (p.a === a && p.b === b) || (p.a === b && p.b === a));
    });
    function pickPair([a, b]) {
      selected.value = [data.value.setupIds[a], data.value.setupIds[b]].filter(Boolean);
      setTab("compare");
    }
    const groupCount = computed(() => (data.value ? Math.max(0, ...data.value.clusters.cluster) : 0));

    // Recorded sessions whose setup SetupMan could not recognise (D-44).
    // ...and each setup's recorded lap times, shown above the values in Compare (D-141).
    const unassigned = ref([]);
    const perSetup = ref({});
    watch([data, () => store.lapVersion], async ([d]) => {
      if (!d) return;
      const r = await get(`/api/laps/${enc(d.game.id)}/${enc(d.car.id)}/${enc(d.track.id)}`).catch(() => ({ unassigned: [], perSetup: {} }));
      unassigned.value = r.unassigned || [];
      perSetup.value = r.perSetup || {};
    });
    async function assign(sessionId, setupId) {
      if (!setupId) return;
      try { await put(`/api/sessions/${sessionId}/setup`, { setupId }); showToast("Laps added to that setup."); } catch (e) { showToast(e.message, 7000); }
    }

    // Families (which setup was made from which).
    const families = ref(null);
    watch([tab, data], async () => {
      const d = data.value;
      if (d && tab.value === "families") families.value = (await get(`/api/games/${enc(d.game.id)}/cars/${enc(d.car.id)}/tracks/${enc(d.track.id)}/families`)).families;
    }, { immediate: true });
    const bigFamilies = computed(() => (families.value || []).filter((f) => f.children.length));
    const loners = computed(() => (families.value || []).filter((f) => !f.children.length));

    // Shown in ACC or hidden (staging, spec D-73 … D-80).
    const busy = ref(false);
    const freshSetups = computed(() => (data.value ? data.value.setups.map((s, i) => ({ s, id: data.value.setupIds[i] })).filter((x) => data.value.fresh.includes(x.id)) : []));
    async function act(fn, done) {
      busy.value = true;
      try { stagingDone(await fn(), done); } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    const stage = (id, name) => act(() => post("/api/setups/" + id + "/stage", {}), name + ` is shown in ${G.value}.`);
    const unstage = (id, name) => act(() => post("/api/setups/" + id + "/unstage"), name + ` is hidden from ${G.value}. It stays safe in SetupMan.`);
    const keep = (id, name) => act(() => post("/api/setups/" + id + "/keep"), name + ` stays in ${G.value}.`);
    // Several setups in one request: one folder lock and one rescan (D-138).
    async function many(list, path, done) {
      busy.value = true;
      try {
        const res = await post(`/api/setups/${path}-many`, { ids: list.map((r) => r.id) });
        const bad = res.results.filter((r) => r.error);
        const ok = res.results.length - bad.length;
        if (!bad.length) stagingDone(res, done(ok));
        else showToast((ok ? done(ok) + " " : "") + `${plural(bad.length, "setup")} could not be changed. ${bad[0].name || "Setup"}: ${bad[0].error}`, 10000);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    const hideSelected = () => many(selShown.value, "unstage", (n) => `${plural(n, "setup")} hidden from ${G.value}. They stay safe in SetupMan.`);
    const showSelected = () => many(selHidden.value, "stage", (n) => `${plural(n, "setup")} shown in ${G.value}.`);
    function showOnlySelected() {
      const d = data.value;
      const ids = selShown.value.map((r) => r.id);
      const n = d.setups.length - ids.length;
      act(() => post(`/api/games/${enc(d.game.id)}/cars/${enc(d.car.id)}/tracks/${enc(d.track.id)}/keep-only`, { setupIds: ids }),
        n ? `${G.value} now shows ${plural(ids.length, "setup")} here; ${plural(n, "setup")} hidden (safe in SetupMan).` : "Nothing to hide.");
    }
    async function tidyNames() {
      busy.value = true;
      try {
        const res = (await post("/api/setups/rename-tidy", { ids: selected.value })).results;
        const ok = res.filter((r) => !r.error).length, bad = res.filter((r) => r.error);
        showToast(!res.length ? "The selected setups already follow the naming standard." : `${plural(ok, "setup")} renamed.` + (bad.length ? ` ${bad.length} could not be renamed: ${bad[0].error}` : ""), 8000);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    // Delete into the bin (D-102), after a confirmation.
    // Without folder management, setups in the game folder can't be deleted: the dialog says so
    // before anything happens, and only the others are deleted (D-138).
    const delDlg = ref(null);
    const delBlocked = computed(() => (data.value && !data.value.managed && !data.value.game.readOnly ? selShown.value : []));
    const delIds = computed(() => selected.value.filter((id) => !delBlocked.value.some((r) => r.id === id)));
    const askDelete = () => delDlg.value.showModal();
    async function doDelete() {
      busy.value = true;
      try {
        const res = (await post("/api/setups/delete", { ids: delIds.value })).results;
        const ok = res.filter((r) => !r.error), bad = res.filter((r) => r.error);
        delDlg.value.close();
        selected.value = [...bad.map((r) => r.id), ...delBlocked.value.map((r) => r.id)];
        showToast(`${plural(ok.length, "setup")} deleted (in the bin for 30 days).` + (bad.length ? ` ${plural(bad.length, "setup")} could not be deleted. ${bad[0].name}: ${bad[0].error}.` : ""), 9000);
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    const downloadIds = computed(() => (selected.value.length ? selected.value : rows.value.map((r) => r.id)).join(","));

    // Adding setups (D-91, D-96).
    const imported = ref(null);
    async function importFiles(ev) {
      const list = [...ev.target.files];
      ev.target.value = "";
      await importList(list);
    }
    useDrop("Drop setups here to add them to this car and track", importList);
    async function importList(list) {
      if (!list.length || busy.value || !data.value) return;
      busy.value = true;
      try {
        const d = data.value;
        const files = await readFiles(list);
        imported.value = await post(`/api/games/${enc(d.game.id)}/cars/${enc(d.car.id)}/tracks/${enc(d.track.id)}/import`, { files });
        setTab("setups");
      } catch (e) { showToast(e.message, 7000); } finally { busy.value = false; }
    }
    const tabCount = (t) => (t === "setups" ? rows.value.length : t === "compare" ? selCount.value : 0);
    // Clicking anywhere on a row selects it, except on its links and buttons.
    function rowClick(e, r) { if (!e.target.closest("a, button, input, select, label")) toggle(r); }
    const G = computed(() => data.value?.game?.short || "the game"); // short game name, e.g. "AC EVO"
    return { G, autoPicked, selCount, chosenLaps, rowClick, delBlocked, delIds, data, error, TABS, tab, setTab, tabCount, sorted, sortBy, sortMark, selected, isSel, toggle, allSelected, toggleAll, dupDlg,
      selShown, selHidden, chosen, pair, pickPair, groupCount, unassigned, assign, rows, families, bigFamilies, loners,
      busy, freshSetups, stage, unstage, keep, hideSelected, showSelected, showOnlySelected, tidyNames, downloadIds, delDlg, askDelete, doDelete,
      importFiles, imported, dateTime, pct, plural };
  },
  template: `
    <main class="page page-wide">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <div class="page-head">
          <div>
            <h1 class="page-title">{{ data.track.name }}</h1>
            <p class="page-sub">{{ data.car.name }}. {{ plural(rows.length, 'setup') }}<template v-if="groupCount">, {{ groupCount === 1 ? '1 duplicate set' : groupCount + ' duplicate sets' }}</template>.</p>
          </div>
          <div class="page-actions">
            <button v-if="!data.game.readOnly" type="button" class="btn" @click="dupDlg.open()">Remove duplicates…</button>
            <router-link :to="{ path: '/prep', query: { game: data.game.id, car: data.car.id, track: data.track.id } }" class="btn">Race prep</router-link>
            <router-link :to="'/' + data.game.id + '/' + data.car.id + '/' + data.track.id + '/fuel'" class="btn">Fuel calculator</router-link>
          </div>
          <DuplicatesDialog ref="dupDlg" :game="data.game.id" :short="data.game.short" :car="data.car.id" :track="data.track.id" :managed="data.managed" :threshold="data.threshold" />
        </div>

        <div v-if="data.queued" class="banner banner-info" role="status"><h2>{{ plural(data.queued, 'queued change') }} will be applied when you stop driving</h2></div>
        <section v-if="freshSetups.length" class="banner" aria-labelledby="fresh-h">
          <h2 id="fresh-h">{{ freshSetups.length === 1 ? '1 new setup' : freshSetups.length + ' new setups' }} saved in {{ G }}</h2>
          <p class="panel-hint panel-hint-tight">Keep it in {{ G }}'s list, or hide it so the list stays short. Hidden setups stay safe in SetupMan.</p>
          <ul>
            <li v-for="f in freshSetups" :key="f.id">
              <span class="bn-name">{{ f.s.name }}</span>
              <button type="button" class="btn btn-sm" :disabled="busy" @click="keep(f.id, f.s.name)">Keep in {{ G }}</button>
              <button type="button" class="btn btn-sm" :disabled="busy" @click="unstage(f.id, f.s.name)">Hide from {{ G }}</button>
            </li>
          </ul>
        </section>
        <section v-if="unassigned.length" class="banner banner-info assign" aria-labelledby="un-h">
          <h2 id="un-h">Unassigned sessions</h2>
          <p class="panel-hint panel-hint-tight">SetupMan recorded your laps but couldn't determine which setup was used. Select it, and the laps are added to that setup.</p>
          <ul>
            <li v-for="s in unassigned" :key="s.id">
              <router-link :to="'/session/' + s.id" class="bn-name">{{ dateTime(s.startedAt) }}, {{ s.kind || 'session' }}, {{ s.laps }} laps</router-link>
              <select @change="assign(s.id, Number($event.target.value))" :aria-label="'Setup used on ' + dateTime(s.startedAt)">
                <option value="0">Choose the setup…</option>
                <option v-for="o in rows" :key="o.id" :value="o.id">{{ o.name }}</option>
              </select>
            </li>
          </ul>
        </section>

        <nav class="tabs" role="tablist" aria-label="Setups on this track">
          <button v-for="t in TABS" :key="t[0]" type="button" role="tab" :aria-selected="tab === t[0]" :class="{ on: tab === t[0] }" @click="setTab(t[0])">
            {{ t[1] }}<span v-if="tabCount(t[0])" class="tab-count">{{ tabCount(t[0]) }}</span>
          </button>
        </nav>

        <dialog ref="delDlg" class="confirm">
          <template v-if="delIds.length">
            <h2>Delete {{ delIds.length === 1 ? 'this setup' : delIds.length + ' setups' }}?</h2>
            <p>{{ delIds.length === 1 ? 'It is' : 'They are' }} removed from {{ data.managed ? G + ' and from ' : '' }}SetupMan's lists, with all versions. The bin keeps {{ delIds.length === 1 ? 'it' : 'them' }} for 30 days: <router-link to="/settings?tab=backups">Settings → Bin</router-link> can restore {{ delIds.length === 1 ? 'it' : 'them' }} until then.</p>
            <ul class="del-list"><li v-for="r in rows.filter((x) => delIds.includes(x.id)).slice(0, 8)" :key="r.key">{{ r.name }}</li><li v-if="delIds.length > 8">and {{ delIds.length - 8 }} more</li></ul>
          </template>
          <h2 v-else>The selected setups can't be deleted yet</h2>
          <p v-if="delBlocked.length" class="notice notice-tight">{{ delBlocked.length === 1 ? '1 selected setup is' : delBlocked.length + ' selected setups are' }} in {{ G }}'s setups folder. Removing files there requires folder management, so {{ delBlocked.length === 1 ? 'it stays' : 'they stay' }}. <router-link :to="'/settings?tab=games&game=' + data.game.id">Enable folder management</router-link> to delete {{ delBlocked.length === 1 ? 'it' : 'them' }} too.</p>
          <div class="picker-actions">
            <button type="button" class="btn" @click="delDlg.close()">{{ delIds.length ? 'Cancel' : 'Close' }}</button>
            <button v-if="delIds.length" type="button" class="btn btn-danger-solid" :disabled="busy" @click="doDelete">Delete</button>
          </div>
        </dialog>

        <!-- Setups -->
        <section v-if="tab === 'setups'" class="panel setups" aria-label="Setups">
          <p v-if="data.game.readOnly" class="notice notice-tight">{{ data.game.readOnly }}</p>
          <p v-else-if="!data.managed" class="notice notice-tight">Folder management is disabled, so this folder can't be changed. <router-link :to="'/settings?tab=games&game=' + data.game.id">Enable folder management</router-link> to select which setups {{ G }} lists, rename setups and remove duplicates.</p>
          <div class="selbar" :class="{ active: selected.length }">
            <span class="selbar-count">{{ selected.length ? selected.length + ' selected' : 'Select setups to act on them' }}</span>
            <button type="button" class="btn btn-sm" :disabled="selCount < 2" @click="setTab('compare')">Compare</button>
            <template v-if="data.managed">
              <button type="button" class="btn btn-sm" :disabled="busy || !selShown.length" @click="showOnlySelected" :title="'The selected setups stay in ' + G + '; the others are hidden (they stay safe in SetupMan)'">Show only these in {{ G }}</button>
              <button type="button" class="btn btn-sm" :disabled="busy || !selShown.length" @click="hideSelected">Hide from {{ G }}</button>
              <button type="button" class="btn btn-sm" :disabled="busy || !selHidden.length" @click="showSelected">Show in {{ G }}</button>
            </template>
            <button v-if="!data.game.readOnly" type="button" class="btn btn-sm" :disabled="busy || !selected.length" @click="tidyNames" title="Initials, session, temperature, game version, your codes, then the rest of the old name">Apply naming standard</button>
            <a class="btn btn-sm btn-quiet" :href="'/api/export?ids=' + downloadIds" download :title="data.game.readOnly ? 'The settings of each setup, as SetupMan read them, in a .zip' : 'Plain ' + G + ' setup files (' + data.game.ext + ') in a .zip, without SetupMan notes'">Download {{ selected.length ? 'selected' : 'all' }}</a>
            <template v-if="!data.game.readOnly">
              <button type="button" class="btn btn-sm btn-danger" :disabled="busy || !selected.length" @click="askDelete">Delete…</button>
            </template>
          </div>

          <div class="table-scroll">
          <table class="setup-table">
            <thead>
              <tr>
                <th class="st-check"><input type="checkbox" :checked="allSelected" @change="toggleAll" aria-label="Select all"></th>
                <th class="st-num"><button type="button" class="th-sort" @click="sortBy('pos')" title="Duplicates next to each other">#<span>{{ sortMark('pos') }}</span></button></th>
                <th><button type="button" class="th-sort" @click="sortBy('name')">Name<span>{{ sortMark('name') }}</span></button></th>
                <th class="st-ses"><button type="button" class="th-sort" @click="sortBy('session')">Session<span>{{ sortMark('session') }}</span></button></th>
                <th class="st-ver"><button type="button" class="th-sort" @click="sortBy('version')" title="Game version from the setup name, or estimated from the date it was last saved">Game version<span>{{ sortMark('version') }}</span></button></th>
                <th class="st-date"><button type="button" class="th-sort" @click="sortBy('date')">Saved<span>{{ sortMark('date') }}</span></button></th>
                <th v-if="data.managed"><button type="button" class="th-sort" @click="sortBy('acc')">In {{ G }}<span>{{ sortMark('acc') }}</span></button></th>
                <th v-if="data.managed" class="st-act"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in sorted" :key="r.key" :class="{ chosen: isSel(r), 'st-hidden': !r.inAcc }" @click="rowClick($event, r)">
                <td class="st-check"><input type="checkbox" :checked="isSel(r)" @change="toggle(r)" :aria-label="'Select ' + r.name"></td>
                <td class="st-num"><span class="sl-num" :class="r.group ? 'grp grp-' + ((r.group - 1) % 6) : ''" :title="r.group ? 'Same colour = duplicate set ' + r.group : ''">{{ r.inAcc ? r.pos : '–' }}</span></td>
                <td class="st-name"><router-link v-if="r.id" :to="'/setup/' + r.id">{{ r.name }}</router-link><template v-else>{{ r.name }}</template> <span class="st-ses-inline"><SessionBadge :session="r.session" small /></span></td>
                <td class="st-ses"><SessionBadge :session="r.session" small /></td>
                <td class="st-ver"><span v-if="r.version" :class="{ 'ver-est': r.guessed }" :title="r.guessed ? 'Estimated from the date the setup was last saved' : 'From the setup name'">{{ r.version }}</span></td>
                <td class="st-date">{{ r.date ? dateTime(r.date) : '' }}</td>
                <td v-if="data.managed"><span class="where" :class="{ 'where-in': r.inAcc }">{{ r.inAcc ? 'Shown' : 'Hidden' }}</span></td>
                <td v-if="data.managed" class="st-act">
                  <button v-if="r.inAcc && r.id" type="button" class="btn btn-sm btn-quiet" :disabled="busy" @click="unstage(r.id, r.name)">Hide</button>
                  <button v-else-if="!r.inAcc" type="button" class="btn btn-sm" :disabled="busy" @click="stage(r.id, r.name)">Show in {{ G }}</button>
                </td>
              </tr>
            </tbody>
          </table>
          </div>
          <p v-if="!data.managed && data.removed.length" class="muted-small">Setups marked “–” are no longer in {{ G }}'s folder; SetupMan still stores them.</p>

          <label v-if="!data.game.readOnly" class="dropzone" :class="{ busy }">
            <input type="file" :accept="data.game.ext + ',.zip,.setupman'" multiple :disabled="busy" @change="importFiles">
            <b>{{ busy ? 'Adding…' : 'Add setups' }}</b>
            <span>Drop setup files anywhere on this page, or click to choose them. {{ G }} files ({{ data.game.ext }}), ZIP files and shared setups (.setupman) all work.</span>
          </label>
          <div v-if="imported" class="import-result" role="status">
            <p><b>{{ imported.imported === 1 ? '1 setup' : imported.imported + ' setups' }} added</b><template v-if="imported.skipped">, {{ imported.skipped }} skipped</template>.
              <template v-if="imported.imported && data.managed"> They are hidden from {{ G }} for now: click <b>Show in {{ G }}</b> to use them.</template></p>
            <ul v-if="imported.skipped"><li v-for="(it, k) in imported.items.filter((x) => x.result === 'skipped')" :key="k">{{ it.name }}: {{ it.reason }}</li></ul>
            <button type="button" class="btn btn-quiet btn-sm" @click="imported = null">Close</button>
          </div>
        </section>

        <!-- Compare -->
        <section v-else-if="tab === 'compare'" class="panel" aria-labelledby="cmp-h">
          <h2 id="cmp-h" class="panel-title">
            <template v-if="chosen.length === 2">{{ chosen[0].name }} <span class="vs">vs</span> {{ chosen[1].name }}</template>
            <template v-else-if="chosen.length > 2">Comparing {{ chosen.length }} setups</template>
            <template v-else>Compare setups</template>
          </h2>
          <p v-if="pair" class="panel-hint">
            <template v-if="pair.identical">Identical in every setting that counts.</template>
            <template v-else>{{ pct(pair.score) }} the same, {{ plural(pair.clicks, 'click') }} apart in total.</template>
          </p>
          <p v-if="autoPicked" class="notice notice-tight">These are the two most similar setups. To compare others, select them on the <button type="button" class="btn-link" @click="setTab('setups')">Setups</button> tab.</p>
          <CompareTable v-if="chosen.length >= 2" :setups="chosen" :laps="chosenLaps" />
          <p v-else class="panel-hint">Select two or more setups on the <button type="button" class="btn-link" @click="setTab('setups')">Setups</button> tab, or click a square on the <button type="button" class="btn-link" @click="setTab('map')">Similarity map</button>.</p>
        </section>

        <!-- Similarity map -->
        <section v-else-if="tab === 'map'" class="panel" aria-labelledby="map-h">
          <h2 id="map-h" class="panel-title">Similarity</h2>
          <template v-if="data.setups.length > 1">
            <p class="panel-hint">Each square shows how much of two setups is the same. Squares with a thick border are duplicates: at least {{ data.threshold }}% the same. Click a square to compare those two.</p>
            <SimilarityGrid :setups="data.setups" :order="data.clusters.order" :pairs="data.pairs" :threshold="data.threshold" :selected="selShown.map((r) => r.i)" @pick="pickPair" />
          </template>
          <p v-else class="panel-hint">There is only one setup here, so there is nothing to compare.</p>
        </section>

        <!-- Families -->
        <section v-else class="panel" aria-labelledby="fam-h">
          <h2 id="fam-h" class="panel-title">Families</h2>
          <p class="panel-hint">Which setup was made from which. A dashed line is SetupMan's guess from file dates and how alike setups are; open a setup's History tab to correct it.</p>
          <p v-if="!families" class="panel-hint">Loading…</p>
          <template v-else>
            <div v-for="f in bigFamilies" :key="f.setupId" class="family-card">
              <ul class="ft"><FamilyNode :node="f" :current="0" /></ul>
            </div>
            <template v-if="loners.length">
              <h3 class="sub-title">Standalone setups</h3>
              <p class="panel-hint panel-hint-tight">Setups not made from another setup here, and with no setups made from them.</p>
              <ul class="ft ft-loners"><FamilyNode v-for="f in loners" :key="f.setupId" :node="f" :current="0" /></ul>
            </template>
          </template>
        </section>
      </template>
    </main>`,
};
