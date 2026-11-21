// Lap times of a setup (spec D-44, D-48): best lap ever, average of clean laps, fuel per lap,
// per version; a graph of lap time against track temperature; the sessions it was driven in.
import { computed } from "vue";
import { dateTime } from "../format.js";

export function lapTime(ms) {
  if (!ms) return "–";
  const m = Math.floor(ms / 60000);
  const s = (ms - m * 60000) / 1000;
  return `${m}:${s.toFixed(3).padStart(6, "0")}`;
}

const TempGraph = {
  props: { points: Array, tempMin: Number, tempMax: Number },
  setup(props) {
    // On a phone the graph is drawn narrower rather than shrunk, so its labels stay readable (D-140).
    const narrow = typeof window !== "undefined" && window.matchMedia && window.matchMedia("(max-width: 640px)").matches;
    const W = narrow ? 330 : 560, H = narrow ? 200 : 220, P = { l: 58, r: 12, t: 12, b: 34 };
    const g = computed(() => {
      const pts = props.points;
      let t0 = Math.min(...pts.map((p) => p.roadTemp)), t1 = Math.max(...pts.map((p) => p.roadTemp));
      if (props.tempMin) t0 = Math.min(t0, props.tempMin);
      if (props.tempMax) t1 = Math.max(t1, props.tempMax);
      if (t1 - t0 < 4) { t0 -= 2; t1 += 2; }
      let l0 = Math.min(...pts.map((p) => p.lapMs)), l1 = Math.max(...pts.map((p) => p.lapMs));
      if (l1 - l0 < 1000) { l0 -= 500; l1 += 500; }
      const x = (t) => P.l + ((t - t0) / (t1 - t0)) * (W - P.l - P.r);
      const y = (ms) => P.t + ((ms - l0) / (l1 - l0)) * (H - P.t - P.b); // faster = higher up
      const ticksT = [t0, (t0 + t1) / 2, t1].map((t) => ({ x: x(t), label: Math.round(t) + " °C" }));
      const ticksL = [l0, (l0 + l1) / 2, l1].map((ms) => ({ y: y(ms), label: lapTime(Math.round(ms)) }));
      const band = props.tempMin && props.tempMax ? { x: x(props.tempMin), w: Math.max(2, x(props.tempMax) - x(props.tempMin)) } : null;
      return { dots: pts.map((p) => ({ cx: x(p.roadTemp), cy: y(p.lapMs), title: `${lapTime(p.lapMs)} at ${p.roadTemp.toFixed(1)} °C track, ${p.airTemp.toFixed(1)} °C air (version ${p.seq})` })), ticksT, ticksL, band };
    });
    return { W, H, P, g };
  },
  template: `
    <svg :viewBox="'0 0 ' + W + ' ' + H" class="temp-graph" role="img" aria-label="Lap time against track temperature">
      <rect v-if="g.band" :x="g.band.x" :y="P.t" :width="g.band.w" :height="H - P.t - P.b" class="tg-band"><title>Temperature range in the setup's name</title></rect>
      <line :x1="P.l" :y1="H - P.b" :x2="W - P.r" :y2="H - P.b" class="tg-axis" />
      <line :x1="P.l" :y1="P.t" :x2="P.l" :y2="H - P.b" class="tg-axis" />
      <text v-for="t in g.ticksT" :key="'t' + t.x" :x="t.x" :y="H - 12" class="tg-label" text-anchor="middle">{{ t.label }}</text>
      <text v-for="t in g.ticksL" :key="'l' + t.y" :x="P.l - 6" :y="t.y + 4" class="tg-label" text-anchor="end">{{ t.label }}</text>
      <circle v-for="(d, i) in g.dots" :key="i" :cx="d.cx" :cy="d.cy" r="4.5" class="tg-dot"><title>{{ d.title }}</title></circle>
    </svg>`,
};

export const LapTimes = {
  components: { TempGraph },
  props: { laps: Object, meta: Object, setupId: Number, options: Array },
  emits: ["assign"],
  setup: () => ({ lapTime, dateTime }),
  template: `
    <div>
      <template v-if="laps && laps.overall.laps">
        <div class="stats lap-stats">
          <div class="stat"><span class="stat-value">{{ lapTime(laps.overall.bestMs) }}</span><span class="stat-label">best lap ever</span></div>
          <div class="stat"><span class="stat-value">{{ lapTime(laps.overall.avgMs) }}</span><span class="stat-label">average lap ({{ laps.overall.clean }} clean laps)</span></div>
          <div class="stat"><span class="stat-value">{{ laps.overall.fuelLaps ? laps.overall.fuelPerLap.toFixed(2) + ' L' : '–' }}</span><span class="stat-label">fuel per lap</span></div>
          <div class="stat"><span class="stat-value">{{ laps.overall.laps }}</span><span class="stat-label">laps recorded</span></div>
        </div>
        <p class="panel-hint">The average only counts valid laps without a pit stop, out-lap or in-lap.</p>
        <table v-if="laps.versions.length > 1" class="compare-table lap-table">
          <thead><tr><th class="ct-label">Version</th><th class="ct-val">Best</th><th class="ct-val">Average</th><th class="ct-val">Fuel / lap</th><th class="ct-val">Laps</th></tr></thead>
          <tbody>
            <tr v-for="v in laps.versions" :key="v.versionId">
              <td class="ct-label">Version {{ v.seq }}</td>
              <td class="ct-val">{{ lapTime(v.bestMs) }}</td><td class="ct-val">{{ lapTime(v.avgMs) }}</td>
              <td class="ct-val">{{ v.fuelLaps ? v.fuelPerLap.toFixed(2) + ' L' : '–' }}</td><td class="ct-val">{{ v.laps }}</td>
            </tr>
          </tbody>
        </table>
        <template v-if="laps.points.length >= 2">
          <h3 class="lap-sub">Lap time and track temperature</h3>
          <p class="panel-hint">Each dot is a clean lap; higher is faster. <template v-if="meta && meta.track">The shaded line is the track temperature in the setup's name.</template><template v-else-if="meta && meta.tempMin">The shaded band is the temperature range in the setup's name.</template></p>
          <TempGraph :points="laps.points" :temp-min="meta && (meta.track || meta.tempMin)" :temp-max="meta && (meta.track || meta.tempMax)" />
        </template>
        <h3 class="lap-sub">Sessions</h3>
        <ul class="sessions">
          <li v-for="s in laps.sessions" :key="s.id">
            <router-link :to="'/session/' + s.id" title="Session report: stints, every lap, fuel and tyre pressures">{{ dateTime(s.startedAt) }}, {{ s.kind || 'session' }}, {{ s.laps }} laps</router-link>
            <span class="muted-small">{{ s.match === 'manual' ? 'set by you' : 'recognised by SetupMan' }}</span>
            <label class="sess-fix">Wrong setup?
              <select @change="$emit('assign', { sessionId: s.id, setupId: Number($event.target.value) })">
                <option :value="setupId" selected>This setup</option>
                <option v-for="o in options" :key="o.setupId" :value="o.setupId">{{ o.name }}</option>
                <option :value="0">Not sure</option>
              </select>
            </label>
          </li>
        </ul>
      </template>
      <p v-else class="panel-hint">Best and average lap times show here once SetupMan has seen you drive with this setup. It recognises the setup when you leave the pits; you can correct it per session.</p>
    </div>`,
};
