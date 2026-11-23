// Telemetry overview (spec D-159 … D-167): the latest sessions, then every track with its circuit
// outline and the cars driven there (track, then car, then setup). SetupMan only reads the game's
// telemetry files.
import { ref, computed, watchEffect } from "vue";
import { get, post, enc } from "../api.js";
import { usePageData } from "./load.js";
import { Loading, EmptyState, setCrumbs } from "../components/common.js";
import { TrackOutline } from "../components/TrackOutline.js";
import { lapTime } from "../components/LapTimes.js";
import { plural } from "../format.js";
import { bytes, when, kindLabel, classCls, dateLabel } from "../telemetry.js";
import { showToast } from "../store.js";

const SORTS = [["name", "Track name"], ["recent", "Recent"], ["laps", "Most laps"]];

export const TelemetryPage = {
  components: { Loading, EmptyState, TrackOutline },
  setup() {
    const { data, error, reload } = usePageData(() => get("/api/telemetry"));
    watchEffect(() => setCrumbs([{ label: "Telemetry" }]));
    const q = ref("");
    const sort = ref("name"); // alphabetical first (owner request 2026-11-23)
    const car = ref("");
    const g = computed(() => data.value?.games?.[0] || null);
    const cars = computed(() => {
      const m = new Map();
      for (const t of g.value?.tracks || []) {
        for (const c of t.cars) {
          const x = m.get(c.car) || { id: c.car, name: c.carName, cls: c.class, laps: 0 };
          x.laps += c.laps;
          m.set(c.car, x);
        }
      }
      return [...m.values()].sort((a, b) => b.laps - a.laps);
    });
    // each track as shown: with a car chosen, that car's numbers
    const tracks = computed(() => {
      const s = q.value.trim().toLowerCase();
      const carHit = (c) => (c.carName + " " + c.car).toLowerCase().includes(s);
      let list = (g.value?.tracks || []).map((t) => {
        let cars = car.value ? t.cars.filter((x) => x.car === car.value) : t.cars;
        if (!cars.length) return null;
        // a search that names a car (and not the track) shows only the cars it names
        if (s && !(t.trackName + " " + t.track).toLowerCase().includes(s)) cars = cars.filter(carHit);
        if (s && !cars.length && !(t.trackName + " " + t.track).toLowerCase().includes(s)) return null;
        const one = cars.length === 1 && cars.length < t.cars.length ? cars[0] : null;
        const sum = (k) => cars.reduce((a, c) => a + c[k], 0);
        const fastest = cars.filter((c) => c.bestMs).sort((a, b) => a.bestMs - b.bestMs)[0];
        return { ...t, shownCars: cars, carLink: one ? one.car : car.value, files: sum("files"), laps: sum("laps"),
          bestMs: fastest ? fastest.bestMs : 0, bestCar: fastest ? fastest.carName : "", last: cars.reduce((a, c) => (c.last > a ? c.last : a), "") };
      }).filter(Boolean);
      if (sort.value === "laps") list.sort((a, b) => b.laps - a.laps);
      else if (sort.value === "recent") list.sort((a, b) => (b.last > a.last ? 1 : b.last < a.last ? -1 : 0));
      else list.sort((a, b) => a.trackName.localeCompare(b.trackName));
      return list;
    });
    // the fastest lap of each car on each track: a session's best in purple when it is that record
    const carBest = computed(() => {
      const m = {};
      for (const t of g.value?.tracks || []) for (const c of t.cars) m[c.car + "|" + t.track.toLowerCase()] = c.bestMs;
      return m;
    });
    const isRecord = (f) => f.bestMs && carBest.value[f.car + "|" + f.track.toLowerCase()] === f.bestMs;
    const period = computed(() => {
      const x = g.value;
      if (!x || !x.files) return "";
      const f = (d) => new Date(d).toLocaleDateString(undefined, { month: "long", year: "numeric" });
      return f(x.first) === f(x.last) ? `in ${f(x.first)}` : `from ${f(x.first)} to ${f(x.last)}`;
    });
    async function reindex() {
      try {
        await post(`/api/telemetry/games/${g.value.game}/reindex`);
        showToast("Checking the telemetry folder for new files.");
        setTimeout(reload, 1500);
      } catch (e) { showToast(e.message); }
    }
    const trackLink = (track, carId) => `/telemetry/${g.value.game}/${enc(track)}` + (carId ? `?car=${enc(carId)}` : "");
    return { data, error, g, q, sort, car, cars, tracks, period, reindex, trackLink, isRecord, SORTS, lapTime, plural, bytes, when, dateLabel,
      kindLabel, classCls };
  },
  template: `
    <main class="page tl-home">
      <Loading v-if="!data" :error="error" />
      <EmptyState v-else-if="!g" title="No game with telemetry files">
        <p>SetupMan reads the MoTeC telemetry ACC writes. Show ACC on the overview (Settings → Games) to use it.</p>
      </EmptyState>
      <template v-else>
        <header class="tl-hero">
          <div class="tl-hero-text">
            <h1 class="page-title">Telemetry</h1>
            <p v-if="g.files" class="tl-hero-sub">{{ plural(g.files, 'session') }} with {{ plural(g.laps, 'full lap') }} {{ period }}, read from {{ g.short }}'s MoTeC files.</p>
            <p v-else-if="g.folder" class="tl-hero-sub">No full laps yet. {{ g.short }} writes a MoTeC file for every session while its telemetry option is on.</p>
            <p v-else class="tl-hero-sub">{{ g.short }}'s MoTeC folder was not found. Choose it in <router-link :to="'/settings?tab=games&game=' + g.game">Settings → Games</router-link>.</p>
          </div>
          <div class="tl-hero-tools">
            <label class="tl-search">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 3a7 7 0 0 1 5.6 11.2l5 5-1.4 1.4-5-5A7 7 0 1 1 10 3Zm0 2a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z"/></svg>
              <input v-model="q" type="search" placeholder="Find a track or car" aria-label="Find a track or car">
            </label>
            <button v-if="g.folder" type="button" class="btn" @click="reindex" :disabled="g.indexing">
              <span v-if="g.indexing" class="tl-busy" aria-hidden="true"></span>{{ g.indexing ? 'Reading new files' : 'Check for new files' }}
            </button>
          </div>
        </header>
        <p v-if="g.error" class="notice notice-error">{{ g.error }}</p>

        <section v-if="g.recent.length && !q" class="tl-section">
          <h2 class="tl-h2">Latest sessions</h2>
          <div class="tl-recent">
            <router-link v-for="f in g.recent" :key="f.id" class="tl-session-card" :class="classCls(f.class)" :to="trackLink(f.track) + '?open=' + f.id">
              <TrackOutline :outline="f.outline" :label="f.trackName" />
              <span class="tl-sc-body">
                <span class="tl-sc-track">{{ f.trackName }}</span>
                <span class="tl-sc-car">{{ f.carName }}</span>
                <span class="tl-sc-when">{{ when(f.startedAt) }}<template v-if="f.kind">, {{ kindLabel(f.kind).toLowerCase() }}</template></span>
                <span class="tl-sc-nums">
                  <b class="tl-time" :class="{ 'is-best': isRecord(f) }" :title="isRecord(f) ? 'Your fastest lap with this car on this track' : 'Best lap of the session'">{{ lapTime(f.bestMs) }}</b>
                  <span>{{ plural(f.laps, 'lap') }}</span>
                </span>
                <span class="tl-sc-setup" :class="{ unknown: !f.setupName }">{{ f.setupName || 'Setup not known' }}</span>
              </span>
            </router-link>
          </div>
        </section>

        <section v-if="g.tracks.length" class="tl-section">
          <div class="tl-section-head">
            <h2 class="tl-h2">Tracks</h2>
            <div class="segmented tl-sort" role="group" aria-label="Sort tracks">
              <button v-for="s in SORTS" :key="s[0]" type="button" :aria-pressed="sort === s[0]" :class="{ on: sort === s[0] }" @click="sort = s[0]">{{ s[1] }}</button>
            </div>
          </div>
          <div v-if="cars.length > 1" class="tl-cars" role="group" aria-label="Show the tracks of one car">
            <button type="button" :aria-pressed="!car" :class="{ on: !car }" @click="car = ''">All cars</button>
            <button v-for="c in cars" :key="c.id" type="button" :aria-pressed="car === c.id" :class="{ on: car === c.id }" @click="car = car === c.id ? '' : c.id" :title="plural(c.laps, 'full lap')">{{ c.name }} <small>{{ plural(c.laps, 'lap') }}</small></button>
          </div>
          <p v-if="!tracks.length" class="panel-hint">No track or car matches “{{ q }}”.</p>
          <div class="tl-grid">
            <router-link v-for="t in tracks" :key="t.track" class="tl-tile" :class="classCls(t.shownCars[0] && t.shownCars[0].class)" :to="trackLink(t.track, t.carLink)">
              <span class="tl-tile-map"><TrackOutline :outline="t.outline" :label="t.trackName" /></span>
              <span class="tl-tile-body">
                <span class="tl-tile-track">{{ t.trackName }}</span>
                <span class="tl-tile-cars">
                  <span v-for="c in t.shownCars.slice(0, 3)" :key="c.car" class="tl-tile-car"><span v-if="c.class" class="class-chip" :class="classCls(c.class)">{{ c.class }}</span>{{ c.carName }}</span>
                  <span v-if="t.shownCars.length > 3" class="tl-tile-more">and {{ plural(t.shownCars.length - 3, 'other car') }}</span>
                </span>
                <span class="tl-tile-stats">
                  <span><b>{{ t.files.toLocaleString() }}</b> {{ t.files === 1 ? 'session' : 'sessions' }}</span>
                  <span><b>{{ t.laps.toLocaleString() }}</b> {{ t.laps === 1 ? 'full lap' : 'full laps' }}</span>
                  <b v-if="t.bestMs" class="tl-time is-best" :title="'Your fastest lap here' + (t.bestCar && t.cars.length > 1 ? ', ' + t.bestCar : '')">{{ lapTime(t.bestMs) }}</b>
                </span>
                <span class="tl-tile-when">Last driven {{ dateLabel(t.last) }}</span>
              </span>
            </router-link>
          </div>
        </section>

        <footer class="tl-foot">
          <p v-if="g.folder">Telemetry folder <code>{{ g.folder }}</code>, {{ bytes(g.diskBytes) }} in {{ g.diskFiles.toLocaleString() }} files. {{ g.short }} saves a session several times while you drive; SetupMan puts the saves of a session together and shows every lap once.<template v-if="g.empty">{{ ' ' + plural(g.empty, 'session') }} without a full lap (only an out-lap, or a file without ACC's lap clock, as in the oldest files) {{ g.empty === 1 ? 'is' : 'are' }} not listed.</template></p>
          <p v-if="g.extraOn">Extra telemetry log enabled: SetupMan adds car position, fuel, lap validity, slip, brake pressure, brake wear and ride height while you drive<template v-if="g.extraFiles"> ({{ plural(g.extraFiles, 'session') }} so far, {{ bytes(g.extraBytes) }})</template>.</p>
          <p v-else>Extra telemetry log disabled. Enable it in <router-link :to="'/settings?tab=games&game=' + g.game">Settings → Games</router-link> for car position, fuel and lap validity.</p>
        </footer>
      </template>
    </main>`,
};
