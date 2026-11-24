// Fuel calculator (spec D-54 … D-66, D-99). Opened from a track page it is for that car and
// track, and inputs are remembered per car and track (D-59). Opened from the top bar it works
// on its own, with one remembered set of inputs.
import { ref, reactive, watch, watchEffect } from "vue";
import { get, post, put, enc } from "../api.js";
import { usePageData } from "./load.js";
import { Loading, setCrumbs } from "../components/common.js";
import { lapTime } from "../components/LapTimes.js";
import { store } from "../store.js";

// "1:45.0" or "1:45" or "105" → seconds; "" → 0
export function parseLap(text) {
  const t = String(text || "").trim().replace(",", ".");
  if (!t) return 0;
  const m = t.match(/^(\d+):(\d{1,2}(?:\.\d+)?)$/);
  if (m) return Number(m[1]) * 60 + Number(m[2]);
  const n = Number(t);
  return isFinite(n) ? n : NaN;
}

// Planned standing still at a stop (Settings → Games, default 30 s): races are planned without
// repairs (owner 2026-11-24, D-192).
export function pitStill(gameId) {
  const v = Number(store.settings?.games?.[gameId]?.pitStillSeconds);
  return v > 0 ? v : 30;
}
// Time lost per stop for the plan: the measured drive through the pit lane plus the planned
// standing time; without a measured standing time, the measured loss as a whole.
export function pitPlanSeconds(pm, gameId) {
  return pm.timed ? Math.round((pm.laneSeconds + pitStill(gameId)) * 10) / 10 : pm.seconds;
}
export const DAMAGE_NOTE = "Damage repair makes a stop longer: with damage, plan for about 1 lap less.";
// Where a planned pit stop time loss comes from (D-173, D-187, D-192), e.g. "39.8 s in the pit lane + 30 s standing still …".
export function pitSource(pm, gameId) {
  const n = pm.stops === 1 ? "1 stop" : `${pm.stops} stops`;
  const who = pm.class ? `${pm.class} cars` : pm.sameCar ? "this car" : "any car";
  return pm.timed ? `${pm.laneSeconds.toFixed(1)} s pit lane + ${pitStill(gameId)} s standing still (${n}, ${who})` : `measured from ${n}, ${who}`;
}
// The pit stop lines of "How this is calculated" (D-192): what is planned, what was measured, damage.
export function pitLines(pm, gameId, seconds) {
  if (!pm) return [];
  const out = [`Pit stop: ${seconds.toFixed(1)} s lost = ${pitSource(pm, gameId)}`];
  if (pm.timed) out.push(`Standing still planned at ${pitStill(gameId)} s (Settings → Games); you measured ${pm.stillSeconds.toFixed(1)} s, repairs included`);
  out.push(DAMAGE_NOTE);
  return out;
}

export const PlanView = {
  props: { plan: Object, kind: String, basis: { type: Array, default: () => [] } },
  template: `
    <div class="fuel-result">
      <div class="fuel-big">
        <div v-for="(s, i) in plan.stints" :key="i" class="fuel-amount">
          <span class="fa-label">{{ s.fuelLabel }}</span>
          <span class="fa-value">{{ s.fuel }} L</span>
          <span class="fa-sub">{{ kind === 'q' ? 'out-lap + ' + s.laps + ' flying laps' : s.laps + ' laps' + (i === 0 ? ' + out-lap' : '') }}</span>
        </div>
      </div>
      <p class="fuel-total">{{ plan.laps }} laps in total<template v-if="plan.stints.length > 1">, {{ plan.total }} L in total</template>.</p>
      <p v-for="w in plan.warnings" :key="w" class="notice notice-error">{{ w }}</p>
      <details class="fuel-steps">
        <summary>How this is calculated</summary>
        <ul v-if="basis.length" class="fuel-basis"><li v-for="b in basis" :key="b">{{ b }}</li></ul>
        <slot></slot>
        <ol><li v-for="s in plan.steps" :key="s">{{ s }}</li></ol>
      </details>
    </div>`,
};

export const FuelPage = {
  components: { Loading, PlanView },
  setup() {
    // Standalone (/fuel): no car or track; "-" keys keep one remembered set of inputs.
    const { data, error } = usePageData((p) => (p.car ? get(`/api/games/${enc(p.game)}/cars/${enc(p.car)}`) : Promise.resolve({ standalone: true })));
    const trackName = ref("");
    const inputs = reactive({
      race: { minutes: "", avgLap: "", fuelPerLap: "", stops: "0", pitLoss: "", safety: "2", tank: "", windowOpen: "", windowClose: "" },
      q: { minutes: "", bestLap: "", fuelPerLap: "", tank: "" },
    });
    const race = reactive({ plan: null, error: "" });
    const qual = reactive({ plan: null, error: "" });
    const recorded = ref(null); // lap stats recorded on this car and track (D-44)
    const pitMeasured = ref(null); // pit stop time loss from recorded stops (D-173)
    let loadedKey = "";
    let saveTimer;

    watchEffect(() => {
      if (!data.value) return;
      if (data.value.standalone) {
        setCrumbs([{ label: "Fuel calculator" }]);
        if (loadedKey !== "-/-/-") {
          loadedKey = "-/-/-";
          get("/api/fuel/-/-/-").then((saved) => {
            if (saved.race) Object.assign(inputs.race, saved.race);
            if (saved.q) Object.assign(inputs.q, saved.q);
          }).catch(() => {});
        }
        return;
      }
      const p = window.location.hash.split("/");
      const trackId = decodeURIComponent(p[3] || "");
      const t = data.value.tracks.find((x) => x.id === trackId);
      trackName.value = t ? t.name : trackId;
      const g = data.value.game, c = data.value.car;
      setCrumbs([{ label: g.short || g.name, to: "/" + g.id }, { label: c.name, to: `/${g.id}/${c.id}` },
        { label: trackName.value, to: `/${g.id}/${c.id}/${trackId}` }, { label: "Fuel" }]);
      const key = `${g.id}/${c.id}/${trackId}`;
      if (key !== loadedKey) {
        loadedKey = key;
        get(`/api/laps/${enc(g.id)}/${enc(c.id)}/${enc(trackId)}`).then((r) => {
          recorded.value = r.stats.laps ? r.stats : null;
          pitMeasured.value = r.pitLoss && r.pitLoss.seconds > 0 ? r.pitLoss : null;
          if (pitMeasured.value && String(inputs.race.pitLoss).trim() === "") inputs.race.pitLoss = planned().toFixed(1);
        }).catch(() => {});
        get(`/api/fuel/${enc(g.id)}/${enc(c.id)}/${enc(trackId)}`).then((saved) => {
          if (saved.race) Object.assign(inputs.race, saved.race);
          if (saved.q) Object.assign(inputs.q, saved.q);
        });
      }
    });

    async function calc() {
      const r = inputs.race;
      const rq = { minutes: Number(r.minutes), avgLap: parseLap(r.avgLap), fuelPerLap: Number(String(r.fuelPerLap).replace(",", ".")),
        stops: Number(r.stops) || 0, pitLoss: Number(r.pitLoss) || 0, safety: Number(r.safety) || 0, tank: Number(r.tank) || 0,
        windowOpen: Number(r.windowOpen) || 0, windowClose: Number(r.windowClose) || 0 };
      if (rq.minutes && rq.avgLap && rq.fuelPerLap) {
        try { race.plan = await post("/api/fuel/race", rq); race.error = ""; } catch (e) { race.plan = null; race.error = e.message; }
      } else { race.plan = null; race.error = ""; }
      const q = inputs.q;
      const qq = { minutes: Number(q.minutes), bestLap: parseLap(q.bestLap), fuelPerLap: Number(String(q.fuelPerLap || r.fuelPerLap).replace(",", ".")), tank: Number(q.tank || r.tank) || 0 };
      if (qq.minutes && qq.bestLap && qq.fuelPerLap) {
        try { qual.plan = await post("/api/fuel/qualifying", qq); qual.error = ""; } catch (e) { qual.plan = null; qual.error = e.message; }
      } else { qual.plan = null; qual.error = ""; }
    }
    watch(inputs, () => {
      calc();
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => {
        if (!data.value || !loadedKey) return;
        const [g, c, t] = loadedKey.split("/");
        put(`/api/fuel/${enc(g)}/${enc(c)}/${enc(t)}`, { race: inputs.race, q: inputs.q }).catch(() => {});
      }, 600);
    }, { deep: true });

    function useRecorded() {
      const r = recorded.value;
      if (r.avgMs) inputs.race.avgLap = lapTime(r.avgMs);
      if (r.fuelLaps) { inputs.race.fuelPerLap = r.fuelPerLap.toFixed(2); inputs.q.fuelPerLap = ""; }
      if (r.bestMs) inputs.q.bestLap = lapTime(r.bestMs);
      if (pitMeasured.value) inputs.race.pitLoss = planned().toFixed(1);
    }
    const gameId = () => data.value?.game?.id || "acc";
    const planned = () => pitPlanSeconds(pitMeasured.value, gameId());
    const useMeasuredPit = () => { inputs.race.pitLoss = planned().toFixed(1); };
    const pitText = () => pitSource(pitMeasured.value, gameId());
    return { data, error, trackName, inputs, race, qual, recorded, useRecorded, lapTime, pitMeasured, useMeasuredPit, planned, pitText, DAMAGE_NOTE };
  },
  template: `
    <main class="page">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <div class="page-head">
          <div>
            <h1 class="page-title">Fuel calculator</h1>
            <p v-if="data.standalone" class="page-sub">Your numbers are remembered. For numbers per car and track, and your recorded laps, open the calculator from a track page.</p>
            <p v-else class="page-sub">{{ data.car.name }}, {{ trackName }}. Your numbers are remembered for this car and track.</p>
          </div>
        </div>
        <div v-if="recorded" class="banner banner-info">
          <h2>Recorded on this car and track: average {{ lapTime(recorded.avgMs) }}, best {{ lapTime(recorded.bestMs) }}<template v-if="recorded.fuelLaps">, {{ recorded.fuelPerLap.toFixed(2) }} L per lap</template> ({{ recorded.laps }} laps)</h2>
          <div><button type="button" class="btn btn-primary btn-sm" @click="useRecorded">Use my recorded numbers</button></div>
        </div>
        <p v-else-if="!data.standalone" class="panel-hint">Enter lap times and fuel use. Once SetupMan has recorded laps here, they can be filled in with one click.</p>

        <div class="fuel-layout">
          <section class="panel" aria-labelledby="race-h">
            <h2 id="race-h" class="panel-title">Race</h2>
            <div class="fuel-form">
              <label>Race length<span class="unit-input"><input v-model="inputs.race.minutes" inputmode="decimal" placeholder="45"> min</span></label>
              <label>Average lap time<span class="unit-input"><input v-model="inputs.race.avgLap" placeholder="1:45.0"></span></label>
              <label>Fuel per lap<span class="unit-input"><input v-model="inputs.race.fuelPerLap" inputmode="decimal" placeholder="3.2"> L</span></label>
              <label>Mandatory pit stops<span class="unit-input"><input v-model="inputs.race.stops" inputmode="numeric" placeholder="0"></span></label>
              <label v-if="Number(inputs.race.stops) > 0">Time lost per pit stop<span class="unit-input"><input v-model="inputs.race.pitLoss" inputmode="decimal" placeholder="25"> s</span>
                <small v-if="pitMeasured">{{ planned().toFixed(1) }} s = {{ pitText() }}. {{ DAMAGE_NOTE }}
                  <button v-if="Number(String(inputs.race.pitLoss).replace(',', '.')) !== Number(planned().toFixed(1))" type="button" class="btn-link" @click.prevent="useMeasuredPit">Use this</button></small>
                <small v-else>Measured automatically once you drive a full pit stop on this track with SetupMan running.</small></label>
              <label>Safety margin<span class="unit-input"><input v-model="inputs.race.safety" inputmode="decimal" placeholder="2"> L</span></label>
              <label>Tank size (optional)<span class="unit-input"><input v-model="inputs.race.tank" inputmode="decimal" placeholder="e.g. 120"> L</span></label>
              <label v-if="Number(inputs.race.stops) > 0" class="fuel-window">Pit window (optional)
                <span class="unit-input">from <input v-model="inputs.race.windowOpen" inputmode="decimal" placeholder="20"> to <input v-model="inputs.race.windowClose" inputmode="decimal" placeholder="40"> min</span>
                <small>Minutes into the race when the stop is allowed, as the race rules say. Leave empty if the stop is allowed at any time.</small></label>
            </div>
            <PlanView v-if="race.plan" :plan="race.plan" kind="r" />
            <p v-else-if="race.error" class="notice notice-error">{{ race.error }}</p>
            <p v-else class="panel-hint">Fill in race length, average lap time and fuel per lap.</p>
          </section>

          <section class="panel" aria-labelledby="q-h">
            <h2 id="q-h" class="panel-title">Qualifying</h2>
            <div class="fuel-form">
              <label>Qualifying length<span class="unit-input"><input v-model="inputs.q.minutes" inputmode="decimal" placeholder="15"> min</span></label>
              <label>Best lap time<span class="unit-input"><input v-model="inputs.q.bestLap" placeholder="1:44.0"></span></label>
              <label>Fuel per lap<span class="unit-input"><input v-model="inputs.q.fuelPerLap" inputmode="decimal" :placeholder="inputs.race.fuelPerLap || '3.2'"> L</span></label>
            </div>
            <p class="panel-hint">Shorter than 12 minutes: one run. Longer: two runs, each with an out-lap; you return to the garage after the last flying lap.</p>
            <PlanView v-if="qual.plan" :plan="qual.plan" kind="q" />
            <p v-else-if="qual.error" class="notice notice-error">{{ qual.error }}</p>
            <p v-else class="panel-hint">Fill in qualifying length and best lap time.</p>
          </section>
        </div>
      </template>
    </main>`,
};
