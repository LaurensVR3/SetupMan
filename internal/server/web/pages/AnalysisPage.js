// Lap analysis (spec D-159): up to four laps on top of each other along the distance, with the
// time difference to the first lap, any channel, the track map, the car view and a corner table.
// Only the channels on screen are loaded, and each only once.
import { ref, reactive, computed, watch, watchEffect, markRaw, onBeforeUnmount } from "vue";
import { useRoute, useRouter } from "vue-router";
import { get, put, enc } from "../api.js";
import { Loading, setCrumbs } from "../components/common.js";
import { TeleChart } from "../components/TeleChart.js";
import { TeleMap } from "../components/TeleMap.js";
import { TeleCar } from "../components/TeleCar.js";
import { Tele3D, PEDALS, LAPCOL } from "../components/Tele3D.js";
import { lapTime } from "../components/LapTimes.js";
import { when, kindLabel } from "../telemetry.js";
import { lapTrack, series, delta, valueAt, signed, lapColors, parseLaps, lapKey, aligner, sameLap, PRESETS, CAR_FAMILIES, WHEELS } from "../telemetry.js";
import { showToast } from "../store.js";

const ALWAYS = ["SPEED", "THROTTLE", "BRAKE"]; // corner table
const CAR = CAR_FAMILIES.flatMap((f) => WHEELS.map(([, ld]) => f.base + "_" + ld));
const CUSTOM_KEY = "setupman.telemetry.custom";
const readCustom = () => { try { return JSON.parse(localStorage.getItem(CUSTOM_KEY)) || []; } catch { return []; } };
const saveCustom = (list) => { try { localStorage.setItem(CUSTOM_KEY, JSON.stringify(list)); } catch { /* private window */ } };
// 3D view (D-190): 2D or 3D map and the wall's channels, remembered per browser
const MAP3D_KEY = "setupman.telemetry.map3d";
const readMap3D = () => {
  const def = { v: 2, mode: "2d", h: "SPEED", c: LAPCOL };
  try {
    const st = JSON.parse(localStorage.getItem(MAP3D_KEY)) || {};
    if (st.v !== 2) { delete st.c; delete st.size; } // from before: the wall colour now starts as the lap colour
    return { ...def, ...st, v: 2 };
  } catch { return def; }
};
const saveMap3D = (v) => { try { localStorage.setItem(MAP3D_KEY, JSON.stringify(v)); } catch { /* private window */ } };

// corners: speed minima that drop and rise by at least 12 km/h; each corner runs from the speed
// peak before it to the peak after it.
function findCorners(s, total) {
  const H = 12;
  const out = [];
  if (!s || s.x.length < 10) return out;
  let state = 0, maxV = s.y[0], maxX = s.x[0], minV = 0, minX = 0, start = 0;
  for (let i = 0; i < s.x.length; i++) {
    const v = s.y[i], x = s.x[i];
    if (state === 0) {
      if (v > maxV) { maxV = v; maxX = x; }
      if (v < maxV - H) { state = 1; minV = v; minX = x; }
    } else {
      if (v < minV) { minV = v; minX = x; }
      if (v > minV + H) {
        out.push({ start, apex: minX, entry: maxX, min: minV });
        state = 0; maxV = v; maxX = x;
      }
    }
    if (state === 0 && out.length && out[out.length - 1].end === undefined && v >= maxV) out[out.length - 1].peak = x;
  }
  for (let i = 0; i < out.length; i++) {
    const end = i + 1 < out.length ? (out[i].peak ?? (out[i].apex + out[i + 1].apex) / 2) : total;
    out[i].end = end;
    if (i + 1 < out.length) out[i + 1].start = end;
  }
  return out;
}

// turnsFrom: the track's turns ({ n, at, name }: official numbers where SetupMan has them, D-171,
// else its own count, D-167) on the first lap: each turn runs from the fastest point before it to
// the fastest point after it.
function turnsFrom(turns, s, total) {
  if (!s || s.x.length < 10) return [];
  const apex = turns.map((t) => t.at * total);
  const peak = (a, b) => { // the fastest point between a and b
    let best = -Infinity, at = (a + b) / 2;
    for (let i = 0; i < s.x.length; i++) if (s.x[i] > a && s.x[i] < b && s.y[i] > best) { best = s.y[i]; at = s.x[i]; }
    return at;
  };
  return apex.map((x, i) => ({
    label: turns[i].n, name: turns[i].name || "",
    apex: x,
    start: i === 0 ? 0 : peak(apex[i - 1], x),
    end: i === apex.length - 1 ? total : peak(x, apex[i + 1]),
  }));
}

function minIn(s, a, b) {
  let m = Infinity;
  for (let i = 0; i < s.x.length; i++) if (s.x[i] >= a && s.x[i] <= b && s.y[i] < m) m = s.y[i];
  return isFinite(m) ? m : null;
}
function maxIn(s, a, b) {
  let m = -Infinity;
  for (let i = 0; i < s.x.length; i++) if (s.x[i] >= a && s.x[i] <= b && s.y[i] > m) m = s.y[i];
  return isFinite(m) ? m : null;
}
// brakePoint: where the braking for a turn began, found back from its apex (up to 500 m before
// it): the start of the last stretch with the brake above 10 %. Not from the turn's start, which
// is where the first lap brakes: a lap braking earlier would otherwise never show it.
function brakePoint(s, a, apex) {
  if (!s || !s.x.length) return null;
  let i = s.x.length - 1;
  while (i > 0 && s.x[i] > apex) i--;
  while (i > 0 && s.x[i] >= apex - 500 && s.y[i] <= 10) i--; // to the braking nearest the apex
  if (s.y[i] <= 10 || s.x[i] < apex - 500) return null;
  // back to where that braking began; a short release (under 50 m, e.g. a dab before the apex)
  // is part of the same braking
  for (;;) {
    while (i > 0 && s.y[i - 1] > 10) i--;
    let j = i - 1;
    while (j > 0 && s.y[j] <= 10 && s.x[i] - s.x[j] < 50) j--;
    if (j > 0 && s.y[j] > 10 && s.x[j] >= apex - 500) i = j;
    else break;
  }
  return s.x[i];
}

export const AnalysisPage = {
  components: { Loading, TeleChart, TeleMap, TeleCar, Tele3D },
  setup() {
    const route = useRoute(), router = useRouter();
    const req = ref(parseLaps(route.query.laps));
    const files = reactive({}); // fileId → file detail
    const error = ref("");
    const ver = ref(0);         // goes up when lap data arrives
    const cache = markRaw({});  // lapKey → { base, track, ch: { name: channel }, map }
    const memo = markRaw({});   // series per lap, channel and scale
    const colors = ref(lapColors());
    const mo = new MutationObserver(() => (colors.value = lapColors()));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    onBeforeUnmount(() => mo.disconnect());

    async function loadFiles() {
      try {
        for (const l of req.value) if (!files[l.fileId]) files[l.fileId] = await get(`/api/telemetry/files/${l.fileId}`);
        error.value = "";
      } catch (e) { error.value = e.message; }
    }
    watch(req, loadFiles, { immediate: true, deep: true });

    // names that tell laps apart: the car when the cars differ; then "Lap 12" within one session,
    // the setup when comparing setups, else the day ("12 Oct"), with the time or lap number added
    // only when needed
    const sessionOf = (x) => x.file.keptBy || x.fileId; // the saves of one session are one session (D-167)
    function lapName(l, all) {
      const cars = new Set(all.map((x) => x.file.car)).size > 1;
      const sameCar = all.filter((x) => x.file.car === l.file.car);
      let name;
      if (new Set(sameCar.map(sessionOf)).size === 1) name = `Lap ${l.meta.lap}`;
      else {
        const setups = sameCar.map((x) => x.setupName || "");
        if (setups.every((s) => s) && new Set(setups).size === sameCar.length) name = l.setupName;
        else {
          const day = (x) => new Date(x.file.startedAt).toLocaleDateString(undefined, { day: "numeric", month: "short" });
          const time = (x) => new Date(x.file.startedAt).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
          name = day(l);
          if (sameCar.filter((x) => day(x) === name && sessionOf(x) !== sessionOf(l)).length) name += " " + time(l);
          if (sameCar.filter((x) => sessionOf(x) === sessionOf(l)).length > 1) name += ", lap " + l.meta.lap;
        }
      }
      if (!cars) return name;
      return sameCar.length === 1 ? l.file.carName : `${l.file.carName}, ${name.startsWith("Lap ") ? "lap " + name.slice(4) : name}`;
    }
    const laps = computed(() => req.value.map((l, i) => {
      const f = files[l.fileId];
      const meta = f?.laps.find((x) => x.fileId === l.fileId && x.n === l.n);
      const setupName = meta?.lapSetupName || f?.file.setupName || ""; // the lap's own stint setup (D-177)
      return f && meta ? { ...l, key: lapKey(l), file: f.file, meta, color: colors.value[i], label: `Lap ${meta.lap}`, setupName,
        setup: setupName || "Setup not known", when: when(f.file.startedAt), kind: f.file.kind } : null;
    }).filter(Boolean).map((l, i, all) => ({ ...l, label: lapName(l, all) })));
    const ready = computed(() => laps.value.length === req.value.length && laps.value.length > 0 && !badLap.value);
    // a lap the page can't show: not in its session, or without data (ACC keeps the data of its last
    // laps only, D-167)
    const badLap = computed(() => {
      for (const l of req.value) {
        const f = files[l.fileId];
        if (!f) continue;
        const meta = f.laps.find((x) => x.fileId === l.fileId && x.n === l.n);
        if (!meta) return req.value.length === 1 ? "This lap isn't in SetupMan's telemetry (the link may be out of date). Choose the lap again." : "One of these laps isn't in SetupMan's telemetry (the link may be out of date). Choose the laps again.";
        if (meta.distM < 100) {
          const own = f.laps.filter((x) => x.fileId === l.fileId);
          if (!meta.complete && own[own.length - 1] === meta) return `Lap ${meta.lap} is the unfinished last lap of its session: there is no data for it. Choose another lap.`;
          return `Lap ${meta.lap} has no telemetry data: ACC kept only its lap time. Choose another lap.`;
        }
      }
      const tracks = new Set(req.value.map((l) => files[l.fileId]?.file.track.toLowerCase()).filter(Boolean));
      if (tracks.size > 1) return "These laps are on different tracks. Choose laps of one track to compare them.";
      return "";
    });
    const sameTrack = computed(() => new Set(laps.value.map((l) => l.file.track.toLowerCase())).size <= 1);
    const sameCar = computed(() => new Set(laps.value.map((l) => l.file.car)).size <= 1);


    // channels: what the files hold, the preset's, and the custom set
    const available = computed(() => {
      const m = new Map();
      for (const l of laps.value) for (const c of files[l.fileId].channels) if (!m.has(c.name)) m.set(c.name, c);
      return m;
    });
    const preset = ref(PRESETS.some((p) => p.id === route.query.view) ? route.query.view : "driver");
    const custom = ref(readCustom());
    const shownChannels = computed(() => {
      const list = preset.value === "custom" ? custom.value : PRESETS.find((p) => p.id === preset.value).channels;
      return list.filter((n) => available.value.has(n));
    });
    // the page stays when only the address changes (back button, links): follow it
    watch(() => route.query.laps, (q) => { const next = parseLaps(q); if (next.map(lapKey).join() !== req.value.map(lapKey).join()) req.value = next; });
    watch(() => route.query.view, (v) => { preset.value = PRESETS.some((p) => p.id === v) ? v : "driver"; });
    function setPreset(id) {
      preset.value = id;
      router.replace({ query: { ...route.query, view: id === "driver" ? undefined : id } });
    }
    function toCustom(list) {
      custom.value = list;
      saveCustom(list);
      if (preset.value !== "custom") setPreset("custom");
    }
    const removeChart = (name) => toCustom(shownChannels.value.filter((n) => n !== name));
    function toggleChannel(name) {
      const cur = shownChannels.value;
      toCustom(cur.includes(name) ? cur.filter((n) => n !== name) : [...cur, name]);
    }
    const picker = ref(false);
    const pickQ = ref("");
    const groups = computed(() => {
      const q = pickQ.value.trim().toLowerCase();
      const out = [];
      for (const c of available.value.values()) {
        if (q && !(c.label + " " + c.name).toLowerCase().includes(q)) continue;
        let g = out.find((x) => x.name === c.group);
        if (!g) out.push((g = { name: c.group, items: [] }));
        g.items.push(c);
      }
      return out;
    });

    // 3D map (D-190): the wall's channels are loaded like the graphs'
    const map3d = reactive(readMap3D());
    watch(map3d, () => saveMap3D({ ...map3d }), { deep: true });
    const wallChannels = computed(() => (map3d.mode === "3d" ? [map3d.h, map3d.c].filter((n) => n && n !== PEDALS && n !== LAPCOL) : []));

    // loading: only the channels that are missing, per lap
    let loading = false, failed = false; // failed: stop asking again until the laps or channels change
    async function fetchMissing() {
      if (!ready.value || loading) return;
      loading = true;
      try {
        const wanted = [...new Set([...ALWAYS, ...CAR, ...shownChannels.value, ...wallChannels.value])].filter((n) => available.value.has(n));
        for (let i = 0; i < laps.value.length; i++) {
          const l = laps.value[i];
          const c = cache[l.key] || (cache[l.key] = { base: null, track: null, ch: {}, map: null });
          const missing = wanted.filter((n) => !(n in c.ch));
          const needMap = i === 0 && !c.map;
          if (!missing.length && c.base && !needMap) continue;
          const d = await get(`/api/telemetry/files/${l.fileId}/laps/${l.n}?ch=${missing.map(enc).join(",")}${needMap ? "&map=1" : ""}`);
          if (!c.base) { c.base = markRaw({ dist: d.dist, distHz: d.distHz, distT0: d.distT0, lapMs: d.lapMs }); c.track = markRaw(lapTrack(c.base)); }
          for (const cd of d.channels) c.ch[cd.name] = markRaw(cd);
          for (const n of missing) if (!(n in c.ch)) c.ch[n] = null; // not in this lap (e.g. no extra log)
          if (d.map) c.map = markRaw(d.map);
          ver.value++;
        }
      } catch (e) { failed = true; showToast(e.message); }
      finally { loading = false; }
      // the set may have changed while loading
      if (!failed && laps.value.some((l) => !cache[l.key] || [...shownChannels.value, ...wallChannels.value].some((n) => available.value.has(n) && !(n in cache[l.key].ch)))) fetchMissing();
    }
    watch([ready, shownChannels, wallChannels, () => laps.value.map((l) => l.key).join()], () => { failed = false; fetchMissing(); }, { immediate: true });

    // the shared view of all graphs and the map
    const view = reactive({ from: 0, to: 1, total: 1, cursor: null, sel: null });
    const ref0 = computed(() => { ver.value; const l = laps.value[0]; return l && cache[l.key]?.track; });
    watch(() => ref0.value?.total, (t) => { if (t) { view.total = t; view.from = 0; view.to = t; } }, { immediate: true });
    // every lap is stretched to the length of the first one (telemetry.js, aligner)
    function alignOf(l) {
      const r = laps.value[0];
      const c = cache[l.key], rc = r && cache[r.key];
      if (!c?.track || !rc?.track) return null;
      return l.key === r.key ? sameLap : aligner(rc.track, c.track);
    }
    function seriesOf(l, name) {
      const c = cache[l.key];
      const al = alignOf(l);
      if (!c || !c.ch[name] || !c.track || !al) return null;
      const k = l.key + "|" + name + "|" + laps.value[0].key;
      return memo[k] || (memo[k] = markRaw(series(c.ch[name], c.track, al.toRef)));
    }
    const deltas = computed(() => {
      ver.value;
      if (!ref0.value || laps.value.length < 2) return [];
      return laps.value.slice(1).map((l) => {
        const c = cache[l.key], al = alignOf(l);
        return c?.track && al ? { s: markRaw(delta(ref0.value, c.track, al)), color: l.color, name: l.label } : null;
      }).filter(Boolean);
    });
    const charts = computed(() => {
      ver.value;
      return shownChannels.value.map((name) => {
        const info = available.value.get(name);
        const lines = laps.value.map((l) => { const s = seriesOf(l, name); return s ? { s, color: l.color, name: l.label } : null; }).filter(Boolean);
        return { name, info, lines, wheel: !!info.wheel };
      });
    });
    const carLaps = computed(() => {
      ver.value;
      return laps.value.map((l) => ({ color: l.color, label: l.label, series: Object.fromEntries(CAR.map((n) => [n, seriesOf(l, n)]).filter(([, s]) => s)) }));
    });
    const map = computed(() => { ver.value; const l = laps.value[0]; return l && cache[l.key]?.map; });
    const laps3d = computed(() => {
      ver.value;
      return laps.value.map((l) => {
        const al = alignOf(l);
        return { key: l.key, fileId: l.fileId, n: l.n, color: l.color, label: l.label, fromRef: al ? al.fromRef : (v) => v,
          h: seriesOf(l, map3d.h), c: map3d.c === PEDALS || map3d.c === LAPCOL ? null : seriesOf(l, map3d.c), th: seriesOf(l, "THROTTLE"), br: seriesOf(l, "BRAKE") };
      });
    });
    const channels3d = computed(() => [...available.value.values()].map((c) => ({ name: c.name, label: c.label, unit: c.unit })));

    // cursor read-out: distance and lap time at the cursor
    const atCursor = computed(() => {
      ver.value;
      if (view.cursor === null) return null;
      return { km: (view.cursor / 1000).toFixed(3), times: laps.value.map((l) => {
        const t = cache[l.key]?.track;
        const al = alignOf(l);
        return { color: l.color, t: t && al ? t.timeAt(al.fromRef(view.cursor)) : null };
      }) };
    });

    // corner table (with two laps or more)
    const corners = computed(() => {
      ver.value;
      if (laps.value.length < 2 || !ref0.value) return [];
      const refSpeed = seriesOf(laps.value[0], "SPEED");
      const stored = sameTrack.value ? files[laps.value[0].fileId]?.turns : null;
      const list = stored && stored.length ? turnsFrom(stored, refSpeed, ref0.value.total) : findCorners(refSpeed, ref0.value.total);
      return list.map((c, k) => {
        const refMin = minIn(refSpeed, c.start, c.end), refTop = maxIn(refSpeed, c.apex, c.end);
        const refBrake = brakePoint(seriesOf(laps.value[0], "BRAKE"), c.start, c.apex);
        const cmp = laps.value.slice(1).map((l, i) => {
          const d = deltas.value[i]?.s;
          const dt = d ? valueAt(d, c.end) - valueAt(d, c.start) : null;
          const sp = seriesOf(l, "SPEED");
          const mn = sp ? minIn(sp, c.start, c.end) : null, top = sp ? maxIn(sp, c.apex, c.end) : null;
          const br = brakePoint(seriesOf(l, "BRAKE"), c.start, c.apex);
          // reasons that explain the result: only those pointing the same way as the time (a lap that
          // lost time braked earlier, was slower at the apex or slower out of the turn)
          const why = [];
          const lost = dt !== null && dt > 0;
          if (dt !== null && Math.abs(dt) >= 0.01) {
            if (br !== null && refBrake !== null && Math.abs(br - refBrake) >= 8 && (br < refBrake) === lost) why.push(`Braking ${Math.round(Math.abs(br - refBrake))} m ${br > refBrake ? "later" : "earlier"}`);
            if (mn !== null && Math.abs(mn - refMin) >= 2 && (mn < refMin) === lost) why.push(`Minimum speed ${mn > refMin ? "+" : "−"}${Math.abs(mn - refMin).toFixed(0)} km/h`);
            if (top !== null && refTop !== null && Math.abs(top - refTop) >= 3 && (top < refTop) === lost) why.push(`Exit speed ${top > refTop ? "+" : "−"}${Math.abs(top - refTop).toFixed(0)} km/h`);
          }
          return { color: l.color, dt, why: why.length ? why.slice(0, 2).join(" · ") : dt !== null && Math.abs(dt) >= 0.05 ? "No single cause: see the graphs" : "" };
        });
        return { n: c.label || String(k + 1), ...c, refMin, cmp };
      });
    });
    const zoomTo = (a, b) => { view.from = Math.max(0, a); view.to = Math.min(view.total, b); };
    const wholeLap = () => zoomTo(0, view.total);

    function makeFirst(i) {
      const list = [...req.value];
      const [l] = list.splice(i, 1);
      list.unshift(l);
      setLaps(list);
    }
    // reference lap: one per car on a track; set or cleared straight from a lap card
    async function toggleRef(l) {
      const on = !l.meta.reference;
      try {
        await put(`/api/telemetry/files/${l.fileId}/laps/${l.n}/reference`, { on });
        for (const f of Object.values(files)) {
          if (f.file.car !== l.file.car || f.file.track.toLowerCase() !== l.file.track.toLowerCase()) continue;
          for (const m of f.laps) if (m.reference && on) m.reference = false; // the car's earlier reference
        }
        l.meta.reference = on;
        showToast(on ? `Reference lap for the ${l.file.carName} at ${l.file.trackName}` : "Reference lap cleared");
      } catch (e) { showToast(e.message); }
    }
    const removeLap = (i) => setLaps(req.value.filter((_, j) => j !== i));
    function setLaps(list) {
      req.value = list;
      router.replace({ query: { ...route.query, laps: list.map(lapKey).join(",") } });
    }
    const finalDelta = (i) => {
      if (i === 0 || !laps.value[0]) return "";
      return signed((laps.value[i].meta.lapMs - laps.value[0].meta.lapMs) / 1000);
    };
    const backTo = computed(() => {
      const f = laps.value[0]?.file;
      if (!f) return "/telemetry";
      const cars = [...new Set(laps.value.map((l) => l.file.car))];
      return `/telemetry/${f.game}/${enc(f.track)}?car=${cars.map(enc).join(",")}&sel=${req.value.map(lapKey).join(",")}`;
    });
    watchEffect(() => {
      const f = laps.value[0]?.file;
      setCrumbs([{ label: "Telemetry", to: "/telemetry" },
        ...(f ? [{ label: f.trackName, to: backTo.value }] : []),
        { label: "Lap analysis" }]);
    });

    // result strip: what a driver wants to know first
    function share(s, test) {
      if (!s || !s.y.length) return null;
      let n = 0;
      for (let i = 0; i < s.y.length; i++) if (test(s.y[i])) n++;
      return (n / s.y.length) * 100;
    }
    const facts = computed(() => {
      ver.value;
      return laps.value.map((l) => {
        const sp = seriesOf(l, "SPEED"), th = seriesOf(l, "THROTTLE"), br = seriesOf(l, "BRAKE");
        let top = null;
        if (sp) for (let i = 0; i < sp.y.length; i++) if (top === null || sp.y[i] > top) top = sp.y[i];
        return { color: l.color, top, full: share(th, (v) => v >= 98), brake: share(br, (v) => v > 5) };
      });
    });
    const extremes = computed(() => {
      const list = corners.value.filter((c) => c.cmp[0] && c.cmp[0].dt !== null);
      if (!list.length) return null;
      const loss = list.reduce((a, b) => (b.cmp[0].dt > a.cmp[0].dt ? b : a));
      const gain = list.reduce((a, b) => (b.cmp[0].dt < a.cmp[0].dt ? b : a));
      return { loss: loss.cmp[0].dt >= 0.05 ? loss : null, gain: gain.cmp[0].dt <= -0.05 ? gain : null };
    });
    const total = computed(() => (laps.value.length > 1 ? (laps.value[1].meta.lapMs - laps.value[0].meta.lapMs) / 1000 : null));
    const barMax = computed(() => Math.max(0.05, ...corners.value.flatMap((c) => c.cmp.map((x) => Math.abs(x.dt || 0)))));
    const bar = (dt) => ({ width: Math.min(100, (Math.abs(dt || 0) / barMax.value) * 100) + "%" });
    const mapCorners = computed(() => corners.value.map((c) => ({ n: c.n, dist: c.apex, name: c.name })));
    const officialTurns = computed(() => !!(sameTrack.value && files[laps.value[0]?.fileId]?.officialTurns));
    // a time on the lap clock: "38.28 s", or "1:05.43" from a minute on
    const clock = (t) => { const m = Math.floor(t / 60); return m > 0 ? `${m}:${(t - m * 60).toFixed(2).padStart(5, "0")}` : t.toFixed(2) + " s"; };
    const pct = (v) => (v === null ? "–" : v.toFixed(0) + "%");
    const kmh = (v) => (v === null ? "–" : v.toFixed(0));

    return { req, laps, ready, badLap, sameTrack, sameCar, error, colors, view, charts, deltas, carLaps, map, atCursor, corners, PRESETS, preset, setPreset,
      picker, pickQ, groups, shownChannels, toggleChannel, removeChart, zoomTo, wholeLap, makeFirst, removeLap, finalDelta, backTo,
      facts, extremes, total, bar, mapCorners, toggleRef, officialTurns, pct, kmh, lapTime, signed, clock, kindLabel, map3d, laps3d, channels3d };
  },
  template: `
    <main class="page page-wide ta-page">
      <div v-if="!req.length" class="empty">
        <h2>No laps selected</h2>
        <div class="empty-body"><p>Choose laps on a track's page in <router-link to="/telemetry">Telemetry</router-link>.</p></div>
      </div>
      <Loading v-else-if="!ready" :error="error || badLap" back="/telemetry" back-label="Back to Telemetry" />
      <template v-else>
        <header class="ta-head">
          <div>
            <h1 class="page-title">{{ laps[0].file.trackName }}</h1>
            <p class="ta-head-sub">{{ sameCar ? laps[0].file.carName : laps.map(l => l.file.carName).filter((c, i, a) => a.indexOf(c) === i).join(' · ') }}<span v-if="map && map.source === 'position'" class="tag tag-ok" title="Track map from the car position in SetupMan's extra log">Map from car position</span></p>
          </div>
          <router-link :to="backTo" class="btn">{{ laps.length < 4 ? 'Add or change laps' : 'Change laps' }}</router-link>
        </header>
        <p v-if="!sameTrack" class="notice">These laps are not all on the same track; the comparison lines them up by distance only.</p>

        <ol class="ta-laps">
          <li v-for="(l, i) in laps" :key="l.key" class="ta-lap" :style="{ '--c': l.color }">
            <span class="ta-lap-top">
              <span class="ta-lap-name">{{ l.label }}</span>
              <span v-if="i === 0 && laps.length > 1" class="ta-lap-role">compared against</span>
            </span>
            <span class="ta-lap-time">{{ lapTime(l.meta.lapMs) }}</span>
            <span v-if="l.label !== l.setup" class="ta-lap-setup">{{ l.setup }}</span>
            <span class="ta-lap-when">{{ l.when }}<template v-if="!l.label.startsWith('Lap ') && !l.label.includes(', lap ')">, lap {{ l.meta.lap }}</template><template v-if="l.kind">, {{ kindLabel(l.kind).toLowerCase() }}</template></span>
            <span v-if="i" class="ta-lap-delta" :class="l.meta.lapMs > laps[0].meta.lapMs ? 'is-loss' : l.meta.lapMs < laps[0].meta.lapMs ? 'is-gain' : ''">{{ finalDelta(i) }}</span>
            <span class="ta-lap-tools">
              <button v-if="i" type="button" class="btn-link" @click="makeFirst(i)" title="Compare the other laps with this one">Compare against this</button>
              <button v-if="l.meta.complete" type="button" class="btn-link ta-ref" :class="{ on: l.meta.reference }" @click="toggleRef(l)" :aria-pressed="!!l.meta.reference"
                :title="l.meta.reference ? 'Reference lap for the ' + l.file.carName + ' on this track. Click to clear.' : 'Make this the reference lap for the ' + l.file.carName + ' on this track (one per car)'">{{ l.meta.reference ? '★ Reference lap' : '☆ Make reference' }}</button>
              <button v-if="laps.length > 1" type="button" class="btn-link" @click="removeLap(i)">Remove</button>
            </span>
          </li>
        </ol>

        <section class="ta-results" aria-label="Result">
          <div v-if="total !== null" class="ta-result ta-hero" :class="total > 0 ? 'is-loss' : total < 0 ? 'is-gain' : ''">
            <span class="ta-result-label">{{ laps[1].label }} against {{ laps[0].label }}</span>
            <b class="ta-hero-value">{{ signed(total) }} s</b>
            <span class="ta-result-sub">{{ total > 0 ? 'slower' : total < 0 ? 'faster' : 'equal' }} over the lap</span>
          </div>
          <button v-if="extremes && extremes.loss" type="button" class="ta-result ta-click is-loss" @click="zoomTo(extremes.loss.start - 50, extremes.loss.end + 50)">
            <span class="ta-result-label">Most time lost<template v-if="laps.length > 2">, {{ laps[1].label }}</template></span>
            <b class="ta-result-big">{{ signed(extremes.loss.cmp[0].dt) }} s</b>
            <span class="ta-result-sub" :title="extremes.loss.name || null">T{{ extremes.loss.n }}{{ extremes.loss.cmp[0].why ? ': ' + extremes.loss.cmp[0].why.toLowerCase() : '' }}</span>
          </button>
          <button v-if="extremes && extremes.gain" type="button" class="ta-result ta-click is-gain" @click="zoomTo(extremes.gain.start - 50, extremes.gain.end + 50)">
            <span class="ta-result-label">Most time gained<template v-if="laps.length > 2">, {{ laps[1].label }}</template></span>
            <b class="ta-result-big">{{ signed(extremes.gain.cmp[0].dt) }} s</b>
            <span class="ta-result-sub" :title="extremes.gain.name || null">T{{ extremes.gain.n }}{{ extremes.gain.cmp[0].why ? ': ' + extremes.gain.cmp[0].why.toLowerCase() : '' }}</span>
          </button>
          <div class="ta-result">
            <span class="ta-result-label">Top speed, km/h</span>
            <span class="ta-multi"><b v-for="(f, i) in facts" :key="i" :style="{ color: f.color }">{{ kmh(f.top) }}</b></span>
          </div>
          <div class="ta-result">
            <span class="ta-result-label">Full throttle</span>
            <span class="ta-multi"><b v-for="(f, i) in facts" :key="i" :style="{ color: f.color }">{{ pct(f.full) }}</b></span>
          </div>
          <div class="ta-result">
            <span class="ta-result-label">On the brakes</span>
            <span class="ta-multi"><b v-for="(f, i) in facts" :key="i" :style="{ color: f.color }">{{ pct(f.brake) }}</b></span>
          </div>
        </section>

        <section v-if="map3d.mode === '3d'" class="ta-card ta-3d-card">
          <Tele3D :game="laps[0].file.game" :track="laps[0].file.track" :view="view" :laps="laps3d" :channels="channels3d" :wall="map3d" :corners="mapCorners">
            <template #lead>
              <h2 class="ta-card-title">Track</h2>
              <div class="segmented seg-sm" role="radiogroup" aria-label="Track view">
                <button type="button" role="radio" aria-checked="false" @click="map3d.mode = '2d'">2D</button>
                <button type="button" role="radio" aria-checked="true" class="on">3D</button>
              </div>
            </template>
          </Tele3D>
        </section>

        <div class="ta-bench" :class="{ 'is-3d': map3d.mode === '3d' }">
          <aside class="ta-side">
            <section v-if="map3d.mode !== '3d'" class="ta-card ta-map-card">
              <div class="ta-card-head">
                <h2 class="ta-card-title">Track</h2>
                <div class="segmented seg-sm" role="radiogroup" aria-label="Track view">
                  <button type="button" role="radio" aria-checked="true" class="on">2D</button>
                  <button type="button" role="radio" aria-checked="false" @click="map3d.mode = '3d'">3D</button>
                </div>
                <button type="button" class="btn btn-sm" @click="wholeLap" :disabled="view.from <= 0.5 && view.to >= view.total - 0.5">Whole lap</button>
              </div>
              <TeleMap v-if="map" :map="map" :view="view" :gain="deltas[0] ? deltas[0].s : null" :corners="mapCorners" />
              <p v-else class="panel-hint">Loading the map…</p>
              <p v-if="deltas.length" class="ta-legend"><i class="lg-gain"></i>{{ laps[1].label }} faster <i class="lg-loss"></i>{{ laps[1].label }} slower</p>
            </section>
            <section class="ta-card">
              <div class="ta-card-head"><h2 class="ta-card-title">Car</h2></div>
              <TeleCar :laps="carLaps" :view="view" />
            </section>
          </aside>

          <section class="ta-card ta-charts">
            <div class="ta-toolbar">
              <div class="segmented" role="radiogroup" aria-label="Graphs">
                <button v-for="p in PRESETS" :key="p.id" type="button" role="radio" :aria-checked="preset === p.id" :class="{ on: preset === p.id }" @click="setPreset(p.id)">{{ p.label }}</button>
              </div>
              <button type="button" class="btn btn-sm" @click="picker = !picker" :aria-expanded="picker">Channels</button>
              <span class="ta-cursor">
                <template v-if="atCursor"><b>{{ atCursor.km }} km</b><span v-for="(t, i) in atCursor.times" :key="i" :style="{ color: t.color }">{{ t.t !== null ? clock(t.t) : '' }}</span></template>
                <span v-else class="ta-hint">Point at a graph or the map; drag or Ctrl + wheel to zoom, Shift + wheel to move, double-click for the whole lap</span>
              </span>
            </div>
            <div v-if="picker" class="ta-picker">
              <div class="ta-picker-head">
                <input v-model="pickQ" type="search" class="search" placeholder="Find a channel" aria-label="Find a channel" v-focus>
                <span class="ta-picker-note">Ticked channels are shown as graphs. Changing them makes your Custom set.</span>
                <button type="button" class="btn btn-sm" @click="picker = false">Done</button>
              </div>
              <div class="ta-picker-groups">
                <fieldset v-for="g in groups" :key="g.name" class="ta-group">
                  <legend>{{ g.name }}</legend>
                  <label v-for="c in g.items" :key="c.name" class="ta-chan">
                    <input type="checkbox" :checked="shownChannels.includes(c.name)" @change="toggleChannel(c.name)">
                    <span>{{ c.label }}</span><span v-if="c.source === 'extra'" class="tag tag-ok">extra</span>
                  </label>
                </fieldset>
              </div>
            </div>
            <TeleChart v-if="deltas.length" label="Time difference" :unit="'s, against ' + laps[0].label" :lines="deltas" :view="view" :height="130" is-delta fixed class="tc-delta" />
            <TeleChart v-for="(c, i) in charts" :key="c.name" :label="c.info.label" :unit="c.info.unit" :lines="c.lines" :view="view"
              :step="c.info.step" :height="c.wheel ? 96 : 132" :axis="i === charts.length - 1" @remove="removeChart(c.name)" />
            <p v-if="!charts.length" class="panel-hint">No graphs shown. Select Channels to choose some.</p>
          </section>
        </div>

        <section v-if="corners.length" class="ta-card ta-corners">
          <div class="ta-card-head">
            <h2 class="ta-card-title">Turns</h2>
            <span class="ta-card-note">Time gained or lost per turn against {{ laps[0].label }}. Click a turn to zoom in. <template v-if="officialTurns">Official turn numbers; point at one for its name where known.</template><template v-else>Turns are SetupMan's own count of the track's slow points (no official numbers for this track yet).</template></span>
          </div>
          <div class="table-scroll">
            <table class="tt-table ta-corner-table">
              <thead><tr><th>Turn</th><th class="tt-num">At</th><th class="tt-num">Min. speed ({{ laps[0].label }})</th>
                <th v-for="(l, i) in laps.slice(1)" :key="i" class="ta-bar-head"><i class="lap-dot" :style="{ background: l.color }"></i>{{ l.label }}</th>
                <th>Why ({{ laps[1].label }})</th></tr></thead>
              <tbody>
                <tr v-for="c in corners" :key="c.n" class="ta-corner" tabindex="0" @click="zoomTo(c.start - 50, c.end + 50)" @keyup.enter="zoomTo(c.start - 50, c.end + 50)">
                  <td><span class="ta-corner-n" :title="c.name || null">T{{ c.n }}</span></td>
                  <td class="tt-num">{{ (c.apex / 1000).toFixed(2) }} km</td>
                  <td class="tt-num">{{ c.refMin !== null ? c.refMin.toFixed(0) + ' km/h' : '' }}</td>
                  <td v-for="(x, i) in c.cmp" :key="i" class="ta-bar-cell"><span class="ta-bar-wrap">
                    <span class="ta-divbar">
                      <span class="ta-divbar-half left"><i v-if="x.dt <= -0.01" class="is-gain" :style="bar(x.dt)"></i></span>
                      <span class="ta-divbar-zero"></span>
                      <span class="ta-divbar-half right"><i v-if="x.dt >= 0.01" class="is-loss" :style="bar(x.dt)"></i></span>
                    </span>
                    <b class="ta-dt" :class="x.dt >= 0.01 ? 'is-loss' : x.dt <= -0.01 ? 'is-gain' : ''">{{ signed(x.dt) }}</b></span>
                  </td>
                  <td class="ta-why">{{ c.cmp[0].why }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </main>`,
};
