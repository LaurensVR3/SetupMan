// Brakes over a session (D-170): pad life and brake temperature per lap for the four wheels, with
// how much each wheel used per lap and per hour. Pad and disc life come from SetupMan's extra
// telemetry log; brake temperatures from it or, for its last laps, from ACC's own files.
import { ref, computed } from "vue";
import { get } from "../api.js";
import { lapTime } from "./LapTimes.js";

// a small line chart: one line per wheel, laps along the bottom
const StintChart = {
  props: { laps: Array, field: String, unit: String, digits: { type: Number, default: 1 } },
  setup(props) {
    const W = 640, H = 170, L = 44, R = 10, T = 10, B = 24;
    const pts = computed(() => props.laps.map((l, i) => ({ i, lap: l.lap, pit: l.pit, v: l[props.field] })).filter((p) => p.v));
    const range = computed(() => {
      const all = pts.value.flatMap((p) => p.v);
      if (!all.length) return [0, 1];
      let lo = Math.min(...all), hi = Math.max(...all);
      if (hi - lo < 1e-6) { lo -= 1; hi += 1; }
      const pad = (hi - lo) * 0.08;
      return [lo - pad, hi + pad];
    });
    const n = computed(() => Math.max(1, props.laps.length - 1));
    const X = (i) => L + (i / n.value) * (W - L - R);
    const Y = (v) => T + (1 - (v - range.value[0]) / (range.value[1] - range.value[0])) * (H - T - B);
    const lines = computed(() => [0, 1, 2, 3].map((w) => pts.value.map((p) => `${X(p.i).toFixed(1)},${Y(p.v[w]).toFixed(1)}`).join(" ")));
    const yTicks = computed(() => {
      const [lo, hi] = range.value;
      return [0.1, 0.5, 0.9].map((f) => lo + (hi - lo) * f);
    });
    const xTicks = computed(() => {
      const ls = props.laps;
      const step = Math.max(1, Math.ceil(ls.length / 10));
      return ls.map((l, i) => ({ i, lap: l.lap })).filter((t, k) => k % step === 0 || k === ls.length - 1);
    });
    const pits = computed(() => props.laps.map((l, i) => ({ i, pit: l.pit })).filter((p) => p.pit));
    return { W, H, L, R, T, B, X, Y, lines, yTicks, xTicks, pits };
  },
  template: `
    <svg class="stint-chart" :viewBox="'0 0 ' + W + ' ' + H" role="img" :aria-label="field + ' per lap'">
      <g class="stint-grid">
        <line v-for="(v, k) in yTicks" :key="'y' + k" :x1="L" :x2="W - R" :y1="Y(v)" :y2="Y(v)" />
      </g>
      <g class="stint-pit">
        <line v-for="p in pits" :key="'p' + p.i" :x1="X(p.i)" :x2="X(p.i)" :y1="T" :y2="H - B" />
        <text v-for="p in pits" :key="'pt' + p.i" :x="X(p.i) + 3" :y="T + 10">Pit</text>
      </g>
      <g class="stint-axis">
        <text v-for="(v, k) in yTicks" :key="'yl' + k" :x="L - 6" :y="Y(v) + 4" text-anchor="end">{{ v.toFixed(digits) }}</text>
        <text v-for="t in xTicks" :key="'xl' + t.i" :x="X(t.i)" :y="H - 6" text-anchor="middle">{{ t.lap }}</text>
      </g>
      <polyline v-for="(p, w) in lines" :key="w" :points="p" class="stint-line" :style="{ stroke: 'var(--lap-' + w + ')' }" />
    </svg>`,
};

export const TeleStint = {
  components: { StintChart },
  props: { fileId: Number, session: { type: Number, default: 0 } }, // session: one game session of the file (D-188)
  setup(props) {
    const data = ref(null), error = ref(""), loading = ref(false);
    async function load() {
      loading.value = true;
      error.value = "";
      try { data.value = await get(`/api/telemetry/files/${props.fileId}/stint` + (props.session ? `?session=${props.session}` : "")); } catch (e) { error.value = e.message; }
      finally { loading.value = false; }
    }
    const laps = computed(() => data.value?.laps || []);
    const hasPad = computed(() => laps.value.some((l) => l.pad));
    const hasDisc = computed(() => laps.value.some((l) => l.disc));
    const hasTemp = computed(() => laps.value.some((l) => l.temp));
    const tempLaps = computed(() => laps.value.filter((l) => l.temp).length);
    // per wheel: first and last life, used in total, per normal lap (no pit lane) and per hour driven
    function wearOf(field, usedField) {
      const withV = laps.value.filter((l) => l[field]);
      if (!withV.length) return null;
      const normal = withV.filter((l) => !l.pit);
      const hours = withV.reduce((a, l) => a + l.lapMs, 0) / 3600000;
      return [0, 1, 2, 3].map((w) => {
        const used = withV.reduce((a, l) => a + l[usedField][w], 0);
        const start = withV[0][field][w] + withV[0][usedField][w];
        const perLap = normal.length ? normal.reduce((a, l) => a + l[usedField][w], 0) / normal.length : null;
        return { start, end: withV[withV.length - 1][field][w], used, perLap, perHour: hours > 0 ? used / hours : null };
      });
    }
    const pad = computed(() => wearOf("pad", "padUsed"));
    const disc = computed(() => wearOf("disc", "discUsed"));
    const temps = computed(() => {
      const withT = laps.value.filter((l) => l.temp);
      if (!withT.length) return null;
      return [0, 1, 2, 3].map((w) => ({ avg: withT.reduce((a, l) => a + l.temp[w], 0) / withT.length, max: Math.max(...withT.map((l) => l.tempMax[w])) }));
    });
    const f2 = (v) => (v === null || v === undefined ? "–" : v.toFixed(2));
    const f3 = (v) => (v === null || v === undefined ? "–" : v.toFixed(3));
    const f0 = (v) => (v === null || v === undefined ? "–" : v.toFixed(0));
    return { data, error, loading, load, laps, hasPad, hasDisc, hasTemp, tempLaps, pad, disc, temps, f0, f2, f3, lapTime };
  },
  template: `
    <section class="stint">
      <div class="stint-head">
        <h4 class="stint-title">Brakes over the session</h4>
        <button v-if="!data" type="button" class="btn btn-sm" :disabled="loading" @click="load">{{ loading ? 'Reading…' : 'Show brake wear and temperatures' }}</button>
      </div>
      <p v-if="error" class="notice notice-error">{{ error }}</p>
      <template v-if="data">
        <p v-if="!data.hasExtra" class="panel-hint">Brake wear needs SetupMan's extra telemetry log, which records it while you drive (Settings → Games). This session was driven without it<template v-if="hasTemp">; the brake temperatures below come from ACC's own files<template v-if="tempLaps < laps.length">, which kept them for the last {{ tempLaps === 1 ? 'lap' : tempLaps + ' laps' }} of this session only</template></template>.</p>
        <div v-if="hasPad || hasTemp" class="stint-legend">
          <span v-for="(w, i) in data.wheels" :key="w" :style="{ '--c': 'var(--lap-' + i + ')' }">{{ w }}</span>
        </div>
        <div v-if="hasPad" class="stint-block">
          <h5>Pad life at the end of each lap</h5>
          <StintChart :laps="laps" field="pad" unit="" :digits="2" />
        </div>
        <div v-if="hasTemp" class="stint-block">
          <h5>Brake temperature, lap average (°C)</h5>
          <StintChart :laps="laps" field="temp" unit="°C" :digits="0" />
        </div>
        <div v-if="pad || temps" class="table-scroll">
          <table class="tt-table stint-table">
            <thead><tr><th></th><th v-for="w in data.wheels" :key="w" class="tt-num">{{ w }}</th></tr></thead>
            <tbody>
              <template v-if="pad">
                <tr><th scope="row">Pad life, start → end</th><td v-for="(p, i) in pad" :key="i" class="tt-num">{{ f2(p.start) }} → {{ f2(p.end) }}</td></tr>
                <tr><th scope="row">Pad used per lap</th><td v-for="(p, i) in pad" :key="i" class="tt-num">{{ f3(p.perLap) }}</td></tr>
                <tr><th scope="row">Pad used per hour</th><td v-for="(p, i) in pad" :key="i" class="tt-num">{{ f2(p.perHour) }}</td></tr>
              </template>
              <template v-if="disc">
                <tr><th scope="row">Disc used per lap</th><td v-for="(p, i) in disc" :key="i" class="tt-num">{{ f3(p.perLap) }}</td></tr>
                <tr><th scope="row">Disc used per hour</th><td v-for="(p, i) in disc" :key="i" class="tt-num">{{ f2(p.perHour) }}</td></tr>
              </template>
              <template v-if="temps">
                <tr><th scope="row">Brake temperature, average</th><td v-for="(t, i) in temps" :key="i" class="tt-num">{{ f0(t.avg) }} °C</td></tr>
                <tr><th scope="row">Brake temperature, highest</th><td v-for="(t, i) in temps" :key="i" class="tt-num">{{ f0(t.max) }} °C</td></tr>
              </template>
            </tbody>
          </table>
        </div>
        <p v-if="pad" class="tt-muted stint-note">Pad and disc life as ACC reports them. Per lap leaves out laps through the pit lane; per hour counts all driving.</p>
        <p v-if="!hasPad && !hasTemp && data.hasExtra" class="panel-hint">The extra telemetry log of this session has no brake values.</p>
      </template>
    </section>`,
};
