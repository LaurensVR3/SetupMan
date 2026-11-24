// Telemetry of one track, every car (spec D-159 … D-167; owner request 2026-11-23: track, then
// car, then setup). The cars driven here come first: choose one or more to narrow everything
// below, or compare their fastest laps. Three ways to find a lap among hundreds of sessions: Best
// laps (every full lap, fastest first, filtered), Setups (best lap per setup, per car) and
// Sessions (by day, with every lap). Up to four laps from anywhere, of any car, can be picked for
// the lap analysis.
import { ref, reactive, computed, watch, watchEffect, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import { get, put, enc } from "../api.js";
import { usePageData } from "./load.js";
import { Loading, setCrumbs } from "../components/common.js";
import { TrackOutline } from "../components/TrackOutline.js";
import { TeleStint } from "../components/TeleStint.js";
import { lapTime } from "../components/LapTimes.js";
import { plural } from "../format.js";
import { bytes, duration, lapColors, MAX_LAPS, parseLaps, lapKey, when, dayLabel, dateLabel, kindLabel, classCls } from "../telemetry.js";
import { showToast } from "../store.js";

const TABS = [["laps", "Best laps"], ["setups", "Setups"], ["sessions", "Sessions"]];
const PERIODS = [["all", "All time"], ["365", "Last 12 months"], ["90", "Last 3 months"], ["30", "Last 30 days"], ["7", "Last 7 days"]];
const PAGE = 50;
const NO_DATA = 100; // metres: a lap shorter than this has no data (ACC kept only its lap time, D-167)

export const TelemetryTrackPage = {
  components: { Loading, TrackOutline, TeleStint },
  setup() {
    const route = useRoute(), router = useRouter();
    const game = route.params.game;
    const track = computed(() => route.params.track);
    const { data, error, reload } = usePageData(() => get(`/api/telemetry/games/${game}/files?track=${enc(track.value)}`));
    watch(track, () => reload());
    watchEffect(() => setCrumbs([{ label: "Telemetry", to: "/telemetry" }, { label: data.value ? data.value.trackName : "…" }]));
    const tab = computed(() => (route.query.open ? "sessions" : TABS.some((t) => t[0] === route.query.tab) ? route.query.tab : "laps"));
    const setTab = (t) => router.replace({ query: { ...route.query, tab: t === "laps" ? undefined : t, open: undefined } });
    const colors = lapColors();

    // ---- cars: choose one or more (?car=a,b); none chosen = every car
    const allCars = computed(() => data.value?.cars || []);
    const chosen = computed(() => String(route.query.car || "").split(",").filter((c) => allCars.value.some((x) => x.car === c)));
    const carOn = (c) => chosen.value.includes(c);
    function toggleCar(c) {
      const next = carOn(c) ? chosen.value.filter((x) => x !== c) : [...chosen.value, c];
      router.replace({ query: { ...route.query, car: next.join(",") || undefined } });
    }
    const allCarsOn = () => router.replace({ query: { ...route.query, car: undefined } });
    const inCars = (car) => !chosen.value.length || chosen.value.includes(car);
    const carOf = (id) => allCars.value.find((c) => c.car === id);
    const carsWithLaps = computed(() => allCars.value.filter((c) => c.laps > 0));
    const shownCars = computed(() => carsWithLaps.value.filter((c) => inCars(c.car)));
    const many = computed(() => shownCars.value.length > 1); // more than one car in view: show which car a lap is

    const laps = computed(() => (data.value?.laps || []).filter((l) => inCars(l.car)));
    const files = computed(() => (data.value?.files || []).filter((f) => inCars(f.car)));
    const best = computed(() => laps.value[0] || null);
    const overallBest = computed(() => data.value?.laps?.[0]?.lapMs || 0); // every car (the car cards)
    const shownBest = computed(() => best.value?.lapMs || 0);             // the cars in view (everything else)
    const refLapNo = (r) => (data.value?.laps || []).find((l) => l.fileId === r.fileId && l.n === r.n)?.lap;
    const recentLaps = computed(() => laps.value.some((l) => Date.now() - new Date(l.startedAt).getTime() < 365 * 86400000));

    // ---- Best laps: filters (each shown only when it can change something)
    const fSetup = ref("all"), fKind = ref("all"), fPeriod = ref("all"), fValid = ref(true), fPit = ref(true);
    const shown = ref(PAGE);
    const kinds = computed(() => [...new Set(laps.value.map((l) => l.kind).filter(Boolean))]);
    const anyInvalid = computed(() => laps.value.some((l) => l.valid === false));
    const anyPit = computed(() => laps.value.some((l) => l.pit));
    const anyTemps = computed(() => laps.value.some((l) => l.hasTemps));
    const setupKey = (l) => l.car + "|" + (l.setupId || 0);
    const setupsWithLaps = computed(() => {
      const m = new Map();
      for (const l of laps.value) {
        const k = setupKey(l);
        const s = m.get(k) || { key: k, id: l.setupId || 0, car: l.car, carName: l.carName, cls: l.class,
          name: l.setupId ? l.setupName || "Setup " + l.setupId : "Setup not known", laps: [], files: new Set() };
        s.laps.push(l);
        s.files.add(l.sessionFile);
        m.set(k, s);
      }
      return [...m.values()].map((s) => {
        const top = s.laps.slice(0, 5);
        return { ...s, best: s.laps[0], avg5: Math.round(top.reduce((a, b) => a + b.lapMs, 0) / top.length), sessions: s.files.size,
          last: s.laps.reduce((a, b) => (b.startedAt > a ? b.startedAt : a), "") };
      }).sort((a, b) => a.best.lapMs - b.best.lapMs);
    });
    const knownSetups = computed(() => setupsWithLaps.value.filter((s) => s.id));
    const filtered = computed(() => {
      const since = fPeriod.value === "all" ? 0 : Date.now() - Number(fPeriod.value) * 86400000;
      return laps.value.filter((l) =>
        (fSetup.value === "all" || setupKey(l) === fSetup.value) &&
        (fKind.value === "all" || l.kind === fKind.value) &&
        (!since || new Date(l.startedAt).getTime() >= since) &&
        (!fValid.value || l.valid !== false) && (!fPit.value || !l.pit));
    });
    const filtersOn = computed(() => fSetup.value !== "all" || fKind.value !== "all" || fPeriod.value !== "all");
    const resetFilters = () => { fSetup.value = "all"; fKind.value = "all"; fPeriod.value = "all"; fValid.value = true; fPit.value = true; shown.value = PAGE; };
    watch(chosen, () => { fSetup.value = "all"; shown.value = PAGE; });

    // ---- selection for the analysis page (?sel=file:n,…)
    const sel = ref(parseLaps(route.query.sel).map((l) => ({ ...l })));
    const selIndex = (fileId, n) => sel.value.findIndex((l) => l.fileId === fileId && l.n === n);
    const saveSel = () => router.replace({ query: { ...route.query, sel: sel.value.map(lapKey).join(",") || undefined } });
    // a lap's name: the car when several are in view, the setup (or the session's time) and the lap number
    const lapLabel = (l) => [many.value || chosen.value.length > 1 ? l.carName : "", l.setupName || when(l.startedAt), "lap " + l.lap].filter(Boolean).join(", ");
    function pick(l) {
      const i = selIndex(l.fileId, l.n);
      if (i >= 0) sel.value.splice(i, 1);
      else if (sel.value.length >= MAX_LAPS) { showToast(`Compare up to ${MAX_LAPS} laps at a time.`); return; }
      else sel.value.push({ fileId: l.fileId, n: l.n, lapMs: l.lapMs, label: lapLabel(l) });
      saveSel();
    }
    const clearSel = () => { sel.value = []; saveSel(); };
    const selInfo = (s) => {
      const l = (data.value?.laps || []).find((x) => x.fileId === s.fileId && x.n === s.n);
      return { time: lapTime(s.lapMs || l?.lapMs), label: s.label || (l ? lapLabel(l) : "Lap") };
    };
    // setups of an opened session in the order they were driven (a stint per setup, D-177)
    function stints(f) {
      const d = details[fk(f)];
      if (!d) return [];
      const out = [];
      for (const l of d.laps) {
        if (!l.lapSetupId) continue;
        if (!out.length || out[out.length - 1].id !== l.lapSetupId) out.push({ id: l.lapSetupId, name: l.lapSetupName || "Setup " + l.lapSetupId });
      }
      return out;
    }
    const stintSession = (f) => details[fk(f)]?.laps.find((l) => l.sessionId)?.sessionId || f.sessionId || 0;
    // purple in an opened session: its fastest full lap that was not invalid (as on a timing screen)
    const sessionBest = (l, f) => !!(l.complete && l.valid !== false && f.bestMs && l.lapMs === f.bestMs);
    const analyse = (list) => router.push({ path: "/telemetry/analyse", query: { laps: list.map(lapKey).join(",") } });
    // the reference lap of the car of the fastest lap in view
    const ref0 = computed(() => (best.value ? carOf(best.value.car)?.reference || null : null));
    function bestVsRef() {
      const b = best.value, r = ref0.value;
      if (!b) return;
      analyse(r && !(r.fileId === b.fileId && r.n === b.n) ? [r, b] : [b]);
    }
    const compareCars = () => analyse(shownCars.value.slice(0, MAX_LAPS).map((c) => laps.value.find((l) => l.car === c.car)).filter(Boolean));

    // Setups tab: tick setups (of any car), compare their fastest laps
    const setupSel = ref([]);
    function toggleSetup(s) {
      const i = setupSel.value.indexOf(s.key);
      if (i >= 0) setupSel.value.splice(i, 1);
      else if (setupSel.value.length >= MAX_LAPS) showToast(`Compare up to ${MAX_LAPS} setups at a time.`);
      else setupSel.value.push(s.key);
    }
    const setupBy = (key) => setupsWithLaps.value.find((s) => s.key === key);
    const compareSetups = () => analyse(setupSel.value.map((k) => setupBy(k)?.best).filter(Boolean));
    const setupGroups = computed(() => {
      const out = [];
      for (const c of shownCars.value) {
        const list = setupsWithLaps.value.filter((s) => s.car === c.car);
        if (list.length) out.push({ car: c, list });
      }
      return out;
    });

    // ---- Sessions tab: by day, each session opens its laps; jump to a month
    const showEmpty = ref(false);
    const days = computed(() => {
      const out = [];
      for (const f of files.value) {
        if (!showEmpty.value && !f.laps) continue;
        const key = new Date(f.startedAt).toDateString();
        let d = out[out.length - 1];
        if (!d || d.key !== key) out.push((d = { key, label: dayLabel(f.startedAt), month: f.startedAt.slice(0, 7), files: [], laps: 0 }));
        d.files.push(f);
        d.laps += f.laps;
      }
      return out;
    });
    const months = computed(() => {
      const seen = new Map();
      for (const d of days.value) if (!seen.has(d.month)) seen.set(d.month, new Date(d.month + "-15").toLocaleDateString(undefined, { month: "long", year: "numeric" }));
      return [...seen.entries()];
    });
    const jumpTo = (m) => document.getElementById("m-" + m)?.scrollIntoView({ block: "start", behavior: "smooth" });
    const firstOfMonth = (d, i) => i === 0 || days.value[i - 1].month !== d.month;
    const emptyCount = computed(() => files.value.filter((f) => !f.laps).length);
    const sessionsWithLaps = computed(() => files.value.filter((f) => f.laps).length);
    // An entry is a file, or one game session of a file that holds several (f.part, D-188).
    const fk = (f) => (f.part ? f.id + "-" + f.part : String(f.id));
    const open = reactive({}), details = reactive({});
    async function load(f) {
      details[fk(f)] = await get(`/api/telemetry/files/${f.id}` + (f.part ? `?session=${f.part}` : ""));
    }
    async function toggle(f) {
      const k = fk(f);
      if (open[k]) { delete open[k]; return; }
      open[k] = true;
      if (!details[k]) {
        try { await load(f); } catch (e) { showToast(e.message); delete open[k]; }
      }
    }
    // ?open=<session>: open it and scroll to it (also when the address changes on this page)
    watch(() => route.query.open, async (v) => {
      const id = Number(v);
      if (!id) return;
      const f = files.value.find((x) => x.id === id) || { id };
      const k = fk(f);
      try {
        if (!details[k]) await load(f);
        open[k] = true;
        await nextTick();
        setTimeout(() => document.getElementById("f" + k)?.scrollIntoView({ block: "start" }), 50);
      } catch { /* not in this track's telemetry */ }
    }, { immediate: true });
    const noData = (l) => l.distM < NO_DATA;
    function flags(l, all) {
      const out = [];
      if (l.lap === 0) out.push("Out-lap");
      else if (l === all[all.length - 1] && !l.complete) out.push("In-lap");
      else if (noData(l)) out.push("No data");
      else if (!l.complete) out.push("Not a full lap");
      else if (l.pit) out.push("Pit lane");
      if (l.valid === false) out.push("Invalid");
      return out;
    }
    const anySessionFuel = (d) => d.laps.some((l) => l.fuelUsed);
    const anySessionTemps = (d) => d.laps.some((l) => l.hasTemps);
    const timedLaps = (d) => d.laps.filter((l, i) => l.lap > 0 && l.lapMs > 0 && i < d.laps.length - 1 && (l.complete || noData(l))).length;

    // ---- actions
    async function setSetup(f, id) {
      try {
        if (f.part) await put(`/api/sessions/${f.part}/setup`, { setupId: Number(id) || 0 }); // one game session of the file (D-188)
        else await put(`/api/telemetry/files/${f.id}/setup`, { setupId: Number(id) || 0 });
        showToast(f.sessionId ? "Setup changed for this session and its recorded laps." : "Setup saved for this session.");
        reload();
      } catch (e) { showToast(e.message); }
    }
    async function refreshOpen() {
      for (const k of Object.keys(details)) {
        const f = files.value.find((x) => fk(x) === k);
        try { if (f) await load(f); else delete details[k]; } catch { delete details[k]; }
      }
    }
    async function setRef(l) {
      try {
        await put(`/api/telemetry/files/${l.fileId}/laps/${l.n}/reference`, { on: !l.reference });
        await reload();
        await refreshOpen();
      } catch (e) { showToast(e.message); }
    }
    const editing = ref(null), draft = ref("");
    const editNote = (key, text) => { editing.value = key; draft.value = text || ""; };
    async function saveNote(fileId, n) {
      try {
        await put(`/api/telemetry/files/${fileId}/note`, n === undefined ? { note: draft.value } : { note: draft.value, lap: n });
        editing.value = null;
        await reload();
        await refreshOpen();
      } catch (e) { showToast(e.message); }
    }
    const gap = (ms, to) => (!to || ms === to ? "" : "+" + ((ms - to) / 1000).toFixed(3));
    const isBest = (l) => shownBest.value && l.lapMs === shownBest.value;
    const carBest = (l) => carOf(l.car)?.bestMs === l.lapMs;

    return { stints, stintSession, sessionBest, data, error, tab, setTab, TABS, PERIODS, colors, allCars, carsWithLaps, chosen, carOn, toggleCar, allCarsOn, shownCars, many, carOf,
      laps, files, best, overallBest, shownBest, refLapNo, recentLaps, fSetup, fKind, fPeriod, fValid, fPit, shown, kinds, anyInvalid, anyPit, anyTemps, setupsWithLaps, knownSetups,
      filtered, filtersOn, resetFilters, sel, selIndex, pick, clearSel, selInfo, lapLabel, analyse, ref0, bestVsRef, compareCars, setupSel, toggleSetup,
      setupBy, compareSetups, setupGroups, showEmpty, days, months, jumpTo, firstOfMonth, emptyCount, sessionsWithLaps, open, details, toggle, fk, flags,
      noData, anySessionFuel, anySessionTemps, timedLaps, setSetup, setRef, editing, draft, editNote, saveNote, gap, isBest, carBest,
      lapTime, plural, bytes, duration, when, dateLabel, kindLabel, classCls, PAGE, MAX_LAPS };
  },
  template: `
    <main class="page page-wide tt-page" :class="{ 'has-selbar': sel.length || (tab === 'setups' && setupSel.length) }">
      <Loading v-if="!data" :error="error" back="/telemetry" back-label="Back to Telemetry" />
      <template v-else>
        <header class="tt-head">
          <div class="tt-map" :class="classCls(shownCars[0] && shownCars[0].class)"><TrackOutline :outline="data.outline" :label="data.trackName" /></div>
          <div class="tt-title">
            <h1 class="page-title">{{ data.trackName }}</h1>
            <p class="tt-count">{{ plural(sessionsWithLaps, 'session') }} with {{ plural(laps.length, 'full lap') }}<template v-if="carsWithLaps.length > 1">, {{ plural(shownCars.length, 'car') }}<template v-if="chosen.length && shownCars.length < carsWithLaps.length"> of {{ carsWithLaps.length }}</template></template></p>
          </div>
          <div class="tt-records">
            <div v-if="best" class="tt-record tt-record-best">
              <span class="tt-record-label">Fastest lap</span>
              <b class="tl-time is-best">{{ lapTime(best.lapMs) }}</b>
              <span class="tt-record-sub"><template v-if="carsWithLaps.length > 1">{{ best.carName }} · </template>{{ best.setupName || 'Setup not known' }} · {{ when(best.startedAt) }}</span>
            </div>
            <div v-if="shownCars.length === 1" class="tt-record tt-record-ref">
              <span class="tt-record-label">Reference lap</span>
              <template v-if="ref0"><b class="tl-time">{{ lapTime(ref0.lapMs) }}</b><span class="tt-record-sub">{{ when(ref0.startedAt) }}<template v-if="refLapNo(ref0) !== undefined">, lap {{ refLapNo(ref0) }}</template></span></template>
              <span v-else class="tt-record-sub">Mark a lap with ★ to compare against it.</span>
            </div>
            <button v-if="shownCars.length > 1" type="button" class="btn btn-primary tt-go" @click="compareCars">Compare fastest laps</button>
            <button v-else-if="best" type="button" class="btn btn-primary tt-go" @click="bestVsRef">{{ ref0 && !(ref0.fileId === best.fileId && ref0.n === best.n) ? 'Fastest vs reference' : 'Analyse fastest lap' }}</button>
          </div>
        </header>

        <section v-if="carsWithLaps.length > 1" class="tt-cars" aria-label="Cars driven here">
          <div class="tt-cars-head">
            <h2 class="tl-h2">Cars</h2>
            <span class="tt-muted">Choose cars to narrow the laps below; choose two or more to compare their fastest laps.</span>
            <button v-if="chosen.length" type="button" class="btn-link" @click="allCarsOn">Show all cars</button>
          </div>
          <div class="tt-car-list">
            <button v-for="c in carsWithLaps" :key="c.car" type="button" class="tt-car" :class="[classCls(c.class), { on: carOn(c.car) }]" :aria-pressed="carOn(c.car)" @click="toggleCar(c.car)">
              <span class="tt-car-name"><span v-if="c.class" class="class-chip" :class="classCls(c.class)">{{ c.class }}</span>{{ c.carName }}</span>
              <b class="tl-time" :class="{ 'is-best': c.bestMs === overallBest }">{{ lapTime(c.bestMs) }}</b>
              <span class="tt-car-sub">{{ plural(c.laps, 'full lap') }}, {{ plural(c.files, 'session') }}, last {{ dateLabel(c.last) }}</span>
            </button>
          </div>
        </section>

        <nav class="tabs" role="tablist" aria-label="Find a lap">
          <button v-for="t in TABS" :key="t[0]" type="button" role="tab" :aria-selected="tab === t[0]" :class="{ on: tab === t[0] }" @click="setTab(t[0])">
            {{ t[1] }}<span class="tab-count">{{ t[0] === 'laps' ? laps.length : t[0] === 'setups' ? (knownSetups.length ? setupsWithLaps.length : 0) : sessionsWithLaps }}</span>
          </button>
        </nav>

        <!-- Best laps -->
        <section v-if="tab === 'laps'" class="tt-panel">
          <div class="tt-filters">
            <label v-if="knownSetups.length && setupsWithLaps.length > 1" class="tt-filter"><span>Setup</span>
              <select v-model="fSetup" @change="shown = PAGE">
                <option value="all">All setups</option>
                <option v-for="s in setupsWithLaps" :key="s.key" :value="s.key">{{ many ? s.carName + ': ' : '' }}{{ s.name }} ({{ s.laps.length }})</option>
              </select>
            </label>
            <label v-if="kinds.length > 1" class="tt-filter"><span>Session</span>
              <select v-model="fKind" @change="shown = PAGE">
                <option value="all">All sessions</option>
                <option v-for="k in kinds" :key="k" :value="k">{{ kindLabel(k) }}</option>
              </select>
            </label>
            <label v-if="recentLaps" class="tt-filter"><span>Period</span>
              <select v-model="fPeriod" @change="shown = PAGE"><option v-for="p in PERIODS" :key="p[0]" :value="p[0]">{{ p[1] }}</option></select>
            </label>
            <label v-if="anyInvalid" class="tt-check"><input type="checkbox" v-model="fValid"> Leave out invalid laps</label>
            <label v-if="anyPit" class="tt-check"><input type="checkbox" v-model="fPit"> Leave out pit-lane laps</label>
            <span class="tt-filter-count">{{ plural(filtered.length, 'lap') }}</span>
          </div>
          <p v-if="!laps.length" class="panel-hint">No full laps here yet.</p>
          <p v-else-if="!filtered.length" class="panel-hint">No laps match these filters. <button v-if="filtersOn || !fValid || !fPit" type="button" class="btn-link" @click="resetFilters">Clear the filters</button></p>
          <div v-else class="table-scroll">
            <table class="tt-table">
              <thead><tr><th class="tt-pick"><span class="sr">Compare</span></th><th class="tt-num">#</th><th>Lap time</th><th class="tt-num">Gap</th><th v-if="many">Car</th><th>Setup</th><th>Session</th><th v-if="anyTemps" class="tt-num">Track °C</th><th v-if="anyInvalid || anyPit"></th><th>Note</th><th><span class="sr">Reference</span></th></tr></thead>
              <tbody>
                <tr v-for="(l, i) in filtered.slice(0, shown)" :key="l.fileId + ':' + l.n" :class="{ picked: selIndex(l.fileId, l.n) >= 0 }">
                  <td class="tt-pick">
                    <label class="lap-check" :style="selIndex(l.fileId, l.n) >= 0 ? { '--c': colors[selIndex(l.fileId, l.n)] } : {}">
                      <input type="checkbox" :checked="selIndex(l.fileId, l.n) >= 0" @change="pick(l)" :aria-label="'Compare lap ' + lapTime(l.lapMs)"><span></span>
                    </label>
                  </td>
                  <td class="tt-num tt-rank">{{ i + 1 }}</td>
                  <td><button type="button" class="tl-time tl-open" :class="{ 'is-best': isBest(l) }" @click="analyse([l])" :title="(isBest(l) ? 'Fastest lap on this track. ' : carBest(l) && many ? 'Fastest lap of this car here. ' : '') + 'Open this lap in Lap analysis'">{{ lapTime(l.lapMs) }}</button></td>
                  <td class="tt-num tt-gap">{{ gap(l.lapMs, best && best.lapMs) }}</td>
                  <td v-if="many" class="tt-carcell"><span v-if="l.class" class="class-chip" :class="classCls(l.class)">{{ l.class }}</span>{{ l.carName }}</td>
                  <td class="tt-setup"><router-link v-if="l.setupId && l.setupName" :to="'/setup/' + l.setupId">{{ l.setupName }}</router-link><span v-else class="tt-muted">Setup not known</span></td>
                  <td class="tt-when">{{ when(l.startedAt) }}<span class="tt-lapno">lap {{ l.lap }}</span><span v-if="l.kind" class="tt-kind">{{ kindLabel(l.kind) }}</span></td>
                  <td v-if="anyTemps" class="tt-num">{{ l.hasTemps ? l.road.toFixed(0) + ' °C' : '' }}</td>
                  <td v-if="anyInvalid || anyPit"><span v-if="l.valid === false" class="lap-flag bad">Invalid</span><span v-if="l.pit" class="lap-flag">Pit lane</span></td>
                  <td class="tt-note">
                    <input v-if="editing === 'l' + l.fileId + ':' + l.n" v-model="draft" class="tt-note-input" maxlength="2000" @keyup.enter="$event.target.blur()" @keyup.esc="editing = null" @blur="editing && saveNote(l.fileId, l.n)" aria-label="Lap note" v-focus>
                    <button v-else type="button" class="tt-note-btn" @click="editNote('l' + l.fileId + ':' + l.n, l.note)">{{ l.note || 'Add note' }}</button>
                  </td>
                  <td><button type="button" class="ref-star" :class="{ on: l.reference }" @click="setRef(l)" :aria-pressed="l.reference"
                    :title="l.reference ? 'Reference lap for this car on this track. Click to clear.' : 'Make this the reference lap for this car on this track'">★</button></td>
                </tr>
              </tbody>
            </table>
            <button v-if="filtered.length > shown" type="button" class="btn tt-more" @click="shown += PAGE">Show {{ Math.min(PAGE, filtered.length - shown) }} more of {{ filtered.length - shown }}</button>
          </div>
        </section>

        <!-- Setups -->
        <section v-else-if="tab === 'setups'" class="tt-panel">
          <template v-if="knownSetups.length">
            <p class="panel-hint">Every setup driven here with its fastest lap<template v-if="many">, per car</template>. Tick two or more (of any car) to compare their fastest laps.</p>
            <div v-for="grp in setupGroups" :key="grp.car.car" class="tt-setup-group">
              <h3 v-if="many" class="tt-day-head"><span v-if="grp.car.class" class="class-chip" :class="classCls(grp.car.class)">{{ grp.car.class }}</span>{{ grp.car.carName }}</h3>
              <div class="tt-setups">
                <article v-for="(s, i) in grp.list" :key="s.key" class="tt-setup-row" :class="{ picked: setupSel.includes(s.key) }" :style="setupSel.includes(s.key) ? { '--c': colors[setupSel.indexOf(s.key)] } : {}">
                  <label class="lap-check" :style="setupSel.includes(s.key) ? { '--c': colors[setupSel.indexOf(s.key)] } : {}">
                    <input type="checkbox" :checked="setupSel.includes(s.key)" @change="toggleSetup(s)" :aria-label="'Compare ' + s.name"><span></span>
                  </label>
                  <div class="tt-setup-name">
                    <router-link v-if="s.id" :to="'/setup/' + s.id">{{ s.name }}</router-link>
                    <span v-else class="tt-muted">{{ s.name }}</span>
                    <span class="tt-setup-sub">{{ plural(s.laps.length, 'full lap') }} in {{ plural(s.sessions, 'session') }}, last {{ dateLabel(s.last) }}</span>
                  </div>
                  <div class="tt-setup-stat"><b class="tl-time" :class="{ 'is-best': s.best.lapMs === shownBest }">{{ lapTime(s.best.lapMs) }}</b><span>fastest</span></div>
                  <div class="tt-setup-stat"><b class="tl-time tt-loss">{{ i ? gap(s.best.lapMs, grp.list[0].best.lapMs) : '' }}</b><span>{{ i ? 'to the fastest setup' : 'fastest setup' }}</span></div>
                  <div class="tt-setup-stat"><b class="tl-time">{{ lapTime(s.avg5) }}</b><span>average of best {{ Math.min(5, s.laps.length) }}</span></div>
                  <button type="button" class="btn btn-sm" @click="analyse([s.best])">Analyse</button>
                </article>
              </div>
            </div>
          </template>
          <div v-else class="panel-hint tt-empty">
            <p><b>No setups linked yet.</b> Sessions SetupMan records while you drive get their setup automatically. For the others, choose the setup on each session in <button type="button" class="btn-link" @click="setTab('sessions')">Sessions</button>.</p>
          </div>
        </section>

        <!-- Sessions -->
        <section v-else class="tt-panel">
          <div v-if="months.length > 2" class="tt-filters">
            <label class="tt-filter"><span>Go to</span>
              <select @change="jumpTo($event.target.value); $event.target.value = ''">
                <option value="">Month…</option>
                <option v-for="m in months" :key="m[0]" :value="m[0]">{{ m[1] }}</option>
              </select>
            </label>
          </div>
          <p v-if="!days.length" class="panel-hint">No sessions with full laps here.</p>
          <div v-for="(d, di) in days" :key="d.key" class="tt-day" :id="firstOfMonth(d, di) ? 'm-' + d.month : null">
            <h3 class="tt-day-head">{{ d.label }}<span>{{ plural(d.files.length, 'session') }}, {{ plural(d.laps, 'full lap') }}</span></h3>
            <ul class="tele-sessions">
              <li v-for="f in d.files" :key="fk(f)" :id="'f' + fk(f)" class="tele-session" :class="{ open: open[fk(f)] }">
                <div class="ts-row" :class="{ 'has-car': many }">
                  <button type="button" class="ts-toggle" @click="toggle(f)" :aria-expanded="!!open[fk(f)]">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6"/></svg>
                    <span class="ts-when">{{ new Date(f.startedAt).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) }}</span>
                    <span class="ts-kind" v-if="f.kind">{{ kindLabel(f.kind) }}</span>
                  </button>
                  <span v-if="many" class="ts-car">{{ f.carName }}</span>
                  <span class="ts-fact"><b>{{ f.laps }}</b> {{ f.laps === 1 ? 'full lap' : 'full laps' }}</span>
                  <span class="ts-fact"><b class="tl-time" :class="{ 'is-best': f.bestMs && f.bestMs === shownBest }">{{ lapTime(f.bestMs) }}</b></span>
                  <span class="ts-fact ts-dur" title="Session length">{{ duration(f.durationMs) }}</span>
                  <span v-if="stints(f).length > 1" class="ts-setup ts-stints" :title="'This session had ' + stints(f).length + ' stints with different setups'">
                    <span>{{ stints(f).map(x => x.name).join(' → ') }}</span>
                    <router-link v-if="stintSession(f)" :to="'/session/' + stintSession(f)" class="ts-setup-open">Change per stint</router-link>
                  </span>
                  <span v-else class="ts-setup">
                    <select class="ts-select" :class="{ unknown: !f.setupId }" :value="f.setupId || 0" @change="setSetup(f, $event.target.value)" :aria-label="'Setup driven in the session at ' + when(f.startedAt)">
                      <option :value="0">Setup not known</option>
                      <option v-for="s in (carOf(f.car) || {}).setups || []" :key="s.id" :value="s.id">{{ s.name }}</option>
                    </select>
                    <router-link v-if="f.setupId" :to="'/setup/' + f.setupId" class="ts-setup-open" title="Open this setup">Open</router-link>
                  </span>
                  <span class="ts-tags">
                    <span v-if="f.hasExtra" class="tag tag-ok" title="Extra telemetry log: car position, fuel, lap validity and more">Extra log</span>
                    <span v-if="f.match === 'session'" class="tag" title="Linked to the session SetupMan recorded">Recorded</span>
                  </span>
                </div>
                <div v-if="open[fk(f)]" class="ts-laps">
                  <div class="ts-note">
                    <input v-if="editing === 'f' + f.id" v-model="draft" class="tt-note-input" maxlength="2000" placeholder="Note for this session" @keyup.enter="$event.target.blur()" @keyup.esc="editing = null" @blur="editing && saveNote(f.id)" aria-label="Session note" v-focus>
                    <button v-else type="button" class="tt-note-btn" @click="editNote('f' + f.id, f.note)">{{ f.note || 'Add a note for this session' }}</button>
                    <span class="tt-muted">{{ bytes(f.size) }}, {{ f.channels }} channels<template v-if="details[fk(f)] && timedLaps(details[fk(f)]) > f.laps">, {{ plural(timedLaps(details[fk(f)]), 'timed lap') }} ({{ f.laps }} with data)</template></span>
                  </div>
                  <p v-if="!details[fk(f)]" class="loading">Loading laps…</p>
                  <div v-else class="table-scroll">
                    <table class="tt-table">
                      <thead><tr><th class="tt-pick"><span class="sr">Compare</span></th><th class="tt-num">Lap</th><th>Time</th><th class="tt-num">Gap</th><th></th><th v-if="anySessionFuel(details[fk(f)])" class="tt-num">Fuel</th><th v-if="anySessionTemps(details[fk(f)])" class="tt-num">Track °C</th><th v-if="stints(f).length > 1">Setup</th><th><span class="sr">Reference</span></th></tr></thead>
                      <tbody>
                        <tr v-for="l in details[fk(f)].laps" :key="l.fileId + ':' + l.n" :class="{ dim: !l.complete || l.valid === false, picked: selIndex(l.fileId, l.n) >= 0 }">
                          <td class="tt-pick">
                            <label v-if="!noData(l)" class="lap-check" :style="selIndex(l.fileId, l.n) >= 0 ? { '--c': colors[selIndex(l.fileId, l.n)] } : {}">
                              <input type="checkbox" :checked="selIndex(l.fileId, l.n) >= 0" @change="pick({ ...l, carName: f.carName, setupName: l.lapSetupName || f.setupName, startedAt: f.startedAt })" :aria-label="'Compare lap ' + l.lap"><span></span>
                            </label>
                          </td>
                          <td class="tt-num tt-rank">{{ l.lap }}</td>
                          <td><button v-if="!noData(l)" type="button" class="tl-time tl-open" :class="{ 'is-best': sessionBest(l, f) }" @click="analyse([l])" :title="(sessionBest(l, f) ? 'Fastest lap of this session. ' : '') + 'Open this lap in Lap analysis'">{{ lapTime(l.lapMs) }}</button>
                            <b v-else class="tl-time">{{ lapTime(l.lapMs) }}</b></td>
                          <td class="tt-num tt-gap">{{ l.complete ? gap(l.lapMs, f.bestMs) : '' }}</td>
                          <td><span v-for="t in flags(l, details[fk(f)].laps)" :key="t" class="lap-flag" :class="{ bad: t === 'Invalid' }"
                            :title="t === 'No data' ? 'ACC kept only the lap time of this lap: it keeps the full data of the last laps of a session only.' : null">{{ t }}</span></td>
                          <td v-if="anySessionFuel(details[fk(f)])" class="tt-num">{{ l.fuelUsed ? l.fuelUsed.toFixed(2) + ' L' : '' }}</td>
                          <td v-if="anySessionTemps(details[fk(f)])" class="tt-num">{{ l.hasTemps ? l.road.toFixed(0) + ' °C' : '' }}</td>
                          <td v-if="stints(f).length > 1" class="tt-setup">{{ l.lapSetupName || '' }}</td>
                          <td><button v-if="l.complete" type="button" class="ref-star" :class="{ on: l.reference }" @click="setRef(l)" :aria-pressed="l.reference"
                            :title="l.reference ? 'Reference lap for this car on this track. Click to clear.' : 'Make this the reference lap for this car on this track'">★</button></td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                  <TeleStint v-if="details[fk(f)]" :file-id="f.id" :session="f.part || 0" />
                </div>
              </li>
            </ul>
          </div>
          <p v-if="emptyCount" class="tt-muted tt-empty-note">
            <button type="button" class="btn-link" @click="showEmpty = !showEmpty">{{ showEmpty ? 'Hide' : 'Show' }} {{ plural(emptyCount, 'session') }} without a full lap</button>
          </p>
        </section>
      </template>

      <div v-if="sel.length || (tab === 'setups' && setupSel.length)" class="tele-selbar" role="region" aria-label="Selected laps">
        <div class="selbar-in">
          <template v-if="tab === 'setups' && setupSel.length">
            <span class="selbar-count">{{ plural(setupSel.length, 'setup') }}</span>
            <span v-for="(k, i) in setupSel" :key="k" class="sel-lap" :style="{ '--c': colors[i] }" :title="setupBy(k) ? (many ? setupBy(k).carName + ': ' : '') + setupBy(k).name : ''"><span class="sel-text">{{ setupBy(k) ? (many ? setupBy(k).carName + ': ' : '') + setupBy(k).name : '' }}</span></span>
            <span class="selbar-gap"></span>
            <button type="button" class="btn btn-quiet" @click="setupSel = []">Clear</button>
            <button type="button" class="btn btn-primary" :disabled="setupSel.length < 2" @click="compareSetups">Compare fastest laps</button>
          </template>
          <template v-else>
            <span class="selbar-count">{{ plural(sel.length, 'lap') }}</span>
            <span v-for="(l, i) in sel" :key="l.fileId + ':' + l.n" class="sel-lap" :style="{ '--c': colors[i] }" :title="selInfo(l).time + ' ' + selInfo(l).label"><b>{{ selInfo(l).time }}</b><span class="sel-text">{{ selInfo(l).label }}</span></span>
            <span class="selbar-gap"></span>
            <button type="button" class="btn btn-quiet" @click="clearSel">Clear</button>
            <button type="button" class="btn btn-primary" @click="analyse(sel)">{{ sel.length === 1 ? 'Analyse lap' : 'Compare laps' }}</button>
          </template>
        </div>
      </div>
    </main>`,
};
