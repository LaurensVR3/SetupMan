// Session report (spec D-127): one practice, qualifying or race session on one page: summary,
// stints between pit stops, and every lap with fuel, temperatures and hot tyre pressures.
import { computed, ref, watchEffect } from "vue";
import { useRoute } from "vue-router";
import { get, put, enc } from "../api.js";
import { showToast } from "../store.js";
import { usePageData } from "./load.js";
import { Loading, setCrumbs } from "../components/common.js";
import { lapTime } from "../components/LapTimes.js";
import { TeleLinks } from "../components/TeleLinks.js";
import { dateTime } from "../format.js";

const KINDS = { race: "Race", qualifying: "Qualifying", practice: "Practice", hotlap: "Hotlap", hotstint: "Hotstint", superpole: "Superpole", "time attack": "Time attack" };
const NOTES = { out: "Out-lap", in: "In-lap", stop: "Pit stop" };
const WHEELS = ["FL", "FR", "RL", "RR"];

export const SessionPage = {
  components: { Loading, TeleLinks },
  setup() {
    const route = useRoute();
    const { data, error, reload } = usePageData(() => get(`/api/sessions/${route.params.id}`), { laps: true });
    // A stint's setup (D-177): laps of two setups in one session (swapped in the pits) are split here.
    const busy = ref(false);
    async function setStint(n, id) {
      if (busy.value) return;
      busy.value = true;
      try {
        await put(`/api/sessions/${enc(data.value.session.id)}/stints/${n}/setup`, { setupId: Number(id) || 0 });
        await reload?.();
      } catch (e) { showToast(e.message, 6000); } finally { busy.value = false; }
    }
    const kind = (k) => KINDS[k] || "Session";
    watchEffect(() => {
      const d = data.value;
      if (!d) return;
      const s = d.session;
      setCrumbs([{ label: d.short || s.game, to: "/" + s.game }, { label: d.carName, to: `/${s.game}/${s.car}` },
        { label: d.trackName, to: `/${s.game}/${s.car}/${s.track}` }, { label: kind(s.kind) + ", " + dateTime(s.startedAt) }]);
    });
    const best = computed(() => data.value?.summary.bestMs || 0);
    const hasPressures = computed(() => !!data.value?.laps.some((l) => l.hasPressures));
    const hasTemps = computed(() => !!data.value?.laps.some((l) => l.hasTemps));
    const versions = computed(() => new Set((data.value?.laps || []).map((l) => l.seq).filter(Boolean)).size > 1);
    const gap = (l) => (!best.value || l.best ? "" : (l.lapMs >= best.value ? "+" : "−") + (Math.abs(l.lapMs - best.value) / 1000).toFixed(3));
    const note = (l) => [NOTES[l.kind], l.valid ? "" : "Invalid"].filter(Boolean).join(", ");
    const psi = (v) => Number(v).toFixed(1);
    const temp = (v) => Number(v).toFixed(1);
    const range = (a, b) => (Math.abs(a - b) < 0.05 ? temp(a) : temp(a) + "–" + temp(b)) + " °C";
    function duration(ms) {
      const s = Math.round(ms / 1000);
      const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
      return h ? `${h}:${String(m).padStart(2, "0")}:${String(r).padStart(2, "0")}` : `${m}:${String(r).padStart(2, "0")}`;
    }
    return { data, error, busy, setStint, kind, lapTime, dateTime, gap, note, psi, range, temp, duration, hasPressures, hasTemps, versions, WHEELS };
  },
  template: `
    <main class="page page-wide">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <div class="page-head">
          <div>
            <h1 class="page-title">{{ kind(data.session.kind) }} at {{ data.trackName }}</h1>
            <p class="page-sub"><span>{{ data.carName }}. {{ dateTime(data.session.startedAt) }}.
              <template v-if="data.setupName">Setup <router-link :to="'/setup/' + data.session.setupId">{{ data.setupName }}</router-link><template v-if="data.session.match === 'auto'"> (recognised by SetupMan)</template>.</template>
              <template v-else>Setup not known: select it under Unassigned sessions on the <router-link :to="'/' + data.session.game + '/' + data.session.car + '/' + data.session.track">track page</router-link>.</template></span>
            </p>
          </div>
        </div>

        <TeleLinks :url="'/api/sessions/' + data.session.id + '/telemetry'" title="Telemetry of this session" />
        <p v-if="!data.laps.length" class="panel-hint">No laps were completed in this session.</p>
        <template v-else>
          <div class="stats session-stats">
            <div class="stat"><span class="stat-value">{{ data.summary.laps }}</span><span class="stat-label">laps ({{ data.summary.clean }} clean)</span></div>
            <div class="stat"><span class="stat-value">{{ data.summary.bestMs ? lapTime(data.summary.bestMs) : '–' }}</span><span class="stat-label">best lap</span></div>
            <div class="stat"><span class="stat-value">{{ data.summary.avgMs ? lapTime(data.summary.avgMs) : '–' }}</span><span class="stat-label">average of clean laps</span></div>
            <div v-if="data.summary.consistencyMs" class="stat"><span class="stat-value">±{{ (data.summary.consistencyMs / 1000).toFixed(3) }}</span><span class="stat-label">consistency (standard deviation)</span></div>
            <div v-if="data.summary.fuelLaps" class="stat"><span class="stat-value">{{ data.summary.fuelPerLap.toFixed(2) }} L</span><span class="stat-label">fuel per lap</span></div>
            <div class="stat"><span class="stat-value">{{ duration(data.summary.drivingMs) }}</span><span class="stat-label">driving time</span></div>
            <div v-if="data.summary.tempsKnown" class="stat"><span class="stat-value">{{ range(data.summary.roadMin, data.summary.roadMax) }}</span><span class="stat-label">track (air {{ range(data.summary.airMin, data.summary.airMax) }})</span></div>
          </div>
          <p v-if="data.summary.hasPressures" class="panel-hint">Average hot tyre pressures (out-laps and in-laps not counted): <template v-for="(w, i) in WHEELS" :key="w">{{ w }} {{ psi(data.summary.pressures[i]) }}<template v-if="i < 3">, </template></template> psi.</p>

          <section class="panel session-panel" aria-labelledby="stints-h">
            <h2 id="stints-h" class="panel-title">Stints</h2>
            <p class="panel-hint">Swapped setups in the pits? Choose each stint's setup here; its laps move along.</p>
            <div class="table-scroll">
            <table class="compare-table lap-table">
              <thead><tr><th class="ct-label">Stint</th><th class="ct-label">Setup</th><th class="ct-val">Laps</th><th class="ct-val">Best</th><th class="ct-val">Average</th><th class="ct-val">Fuel / lap</th><th class="ct-val">Time</th>
                <template v-if="hasPressures"><th v-for="w in WHEELS" :key="w" class="ct-val">{{ w }}</th></template></tr></thead>
              <tbody>
                <tr v-for="s in data.stints" :key="s.n">
                  <td class="ct-label">Stint {{ s.n }}<span v-if="s.wet" class="muted-small"> (wet tyres)</span></td>
                  <td class="ct-label"><select class="stint-setup" :value="s.setupId || ''" :disabled="busy" :aria-label="'Setup of stint ' + s.n" @change="setStint(s.n, $event.target.value)">
                    <option value="">{{ s.setupId ? 'Not known' : 'Not known or mixed' }}</option>
                    <option v-for="c in data.choices" :key="c.id" :value="c.id">{{ c.name }}</option>
                  </select></td>
                  <td class="ct-val">{{ s.fromLap }}–{{ s.toLap }}</td>
                  <td class="ct-val">{{ s.bestMs ? lapTime(s.bestMs) : '–' }}</td>
                  <td class="ct-val">{{ s.avgMs ? lapTime(s.avgMs) : '–' }}</td>
                  <td class="ct-val">{{ s.fuelLaps ? s.fuelPerLap.toFixed(2) + ' L' : '–' }}</td>
                  <td class="ct-val">{{ duration(s.drivingMs) }}</td>
                  <template v-if="hasPressures"><td v-for="(w, i) in WHEELS" :key="w" class="ct-val">{{ s.hasPressures ? psi(s.pressures[i]) : '–' }}</td></template>
                </tr>
              </tbody>
            </table>
            </div>
          </section>

          <section class="panel session-panel" aria-labelledby="laps-h">
            <h2 id="laps-h" class="panel-title">Laps</h2>
            <p class="panel-hint">Fuel is the amount used during the lap. Tyre pressures are hot averages over the lap, in psi.</p>
            <div class="table-scroll">
            <table class="compare-table lap-table session-laps">
              <thead><tr><th class="ct-label">Lap</th><th class="ct-val">Time</th><th class="ct-val">Gap to best</th><th class="ct-val">Fuel</th>
                <template v-if="hasTemps"><th class="ct-val">Track</th><th class="ct-val">Air</th></template>
                <template v-if="hasPressures"><th v-for="w in WHEELS" :key="w" class="ct-val">{{ w }}</th></template>
                <th v-if="versions" class="ct-val">Version</th><th class="ct-label">Note</th></tr></thead>
              <tbody>
                <tr v-for="l in data.laps" :key="l.n" :class="{ 'lap-best': l.best, 'lap-invalid': !l.valid, 'lap-pit': l.kind }">
                  <td class="ct-label">{{ l.n }}</td>
                  <td class="ct-val">{{ lapTime(l.lapMs) }}</td>
                  <td class="ct-val">{{ l.best ? 'Best' : gap(l) }}</td>
                  <td class="ct-val">{{ l.fuelUsed > 0 ? l.fuelUsed.toFixed(2) + ' L' : '–' }}</td>
                  <template v-if="hasTemps"><td class="ct-val">{{ l.hasTemps ? temp(l.road) + ' °C' : '–' }}</td><td class="ct-val">{{ l.hasTemps ? temp(l.air) + ' °C' : '–' }}</td></template>
                  <template v-if="hasPressures"><td v-for="(w, i) in WHEELS" :key="w" class="ct-val">{{ l.hasPressures ? psi(l.pressures[i]) : '–' }}</td></template>
                  <td v-if="versions" class="ct-val">{{ l.seq || '–' }}</td>
                  <td class="ct-label">{{ note(l) }}</td>
                </tr>
              </tbody>
            </table>
            </div>
          </section>
        </template>
      </template>
    </main>`,
};
