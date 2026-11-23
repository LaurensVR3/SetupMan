// Pit stop advice (spec D-124): fuel to add and tyre pressures for the next stop, from the laps
// driven in this session. Made for a phone or tablet next to the rig: everything fits on one
// phone screen without scrolling (rig test R-12, D-144): big values, clear labels, the
// explanations folded away. It refreshes itself every few seconds while it is open.
import { ref, computed, onMounted, onUnmounted } from "vue";
import { get } from "../api.js";
import { setCrumbs, EmptyState } from "../components/common.js";
import { lapTime } from "../components/LapTimes.js";

// The game's own names for the wheels (ACC's pit strategy: left front, right front, …).
const WHEELS = ["Left front", "Right front", "Left rear", "Right rear"];
const REFRESH_MS = 3000;

export const PitPage = {
  components: { EmptyState },
  setup() {
    const data = ref(null);
    const error = ref("");
    let timer = null;
    async function load() {
      try { data.value = await get("/api/pit"); error.value = ""; }
      catch (e) { error.value = e.message; }
    }
    onMounted(() => { setCrumbs([{ label: "Pit stop" }]); load(); timer = setInterval(load, REFRESH_MS); });
    onUnmounted(() => clearInterval(timer));

    const psi = (v) => Number(v).toFixed(1);
    const signed = (v) => (v > 0 ? "+" : v < 0 ? "−" : "±") + Math.abs(v).toFixed(1);
    const litres = (v) => Math.round(Number(v)) + " L";
    // A change under 0.05 psi shows as ±0.0 and needs nothing; up and down get their own colour.
    const deltaClass = (v) => (v >= 0.05 ? "up" : v <= -0.05 ? "down" : "same");
    const arrow = (v) => (v >= 0.05 ? "▲ " : v <= -0.05 ? "▼ " : "");
    function clock(ms) {
      if (ms <= 0) return "0:00";
      const s = Math.floor(ms / 1000);
      const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
      return (h ? h + ":" + String(m).padStart(2, "0") : m) + ":" + String(r).padStart(2, "0");
    }
    const sessionName = (s) => ({ race: "Race", qualifying: "Qualifying", practice: "Practice", hotstint: "Hotstint", hotlap: "Hotlap", superpole: "Superpole", "time attack": "Time attack", drift: "Drift", drag: "Drag" }[s] || "Session");
    // The pit menu already holds the advised fuel: shown in green.
    const fuelSet = computed(() => data.value?.fuel?.pitMenuKnown && Math.round(data.value.fuel.pitMenuFuel) === data.value.fuel.fuelToAdd);
    // No stop left: show the fuel left over or short at the flag instead of fuel to add (D-178).
    const finish = computed(() => { const f = data.value?.fuel; return !!f && !f.reason && f.stopsPlanned === 0; });
    const spare = (v) => (v >= 0 ? "+" : "−") + Math.abs(v).toFixed(1);
    const tyreSet = (i) => { const t = data.value?.tyres; return t && t.pitMenuKnown && Math.abs(t.pitMenu[i] - t.advice[i]) < 0.05; };
    return { data, error, WHEELS, psi, signed, litres, deltaClass, arrow, clock, lapTime, sessionName, fuelSet, tyreSet, finish, spare };
  },
  template: `
    <main class="page pit-page">
      <h1 class="sr">Pit stop</h1>
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>

      <EmptyState v-if="data && !data.active" title="No session running">
        <p>Pit stop advice appears while you drive a session in ACC or AC: fuel to add and tyre pressures for the next stop, based on your last laps. It updates by itself; this page can stay open on a phone, tablet or second screen.</p>
      </EmptyState>

      <template v-else-if="data">
        <p class="pit-head">
          <b>{{ sessionName(data.session) }}</b> · {{ data.carName }} · {{ data.trackName }}
          <span v-if="data.fuel && data.fuel.timed && !data.fuel.reason" class="pit-clock">{{ clock(data.fuel.timeLeftMs) }} left</span>
        </p>

        <section class="pit-block" aria-labelledby="pit-fuel-h">
          <h2 id="pit-fuel-h" class="pit-label">{{ finish ? 'Fuel at the finish' : 'Fuel to add' }}
            <span v-if="data.fuel && !data.fuel.reason && !data.fuel.firm" class="pit-tag">provisional, {{ data.fuel.laps }} lap{{ data.fuel.laps === 1 ? '' : 's' }}</span></h2>
          <template v-if="data.fuel && !data.fuel.reason">
            <!-- no stop left: what is left over (green) or short (red) at the flag, to 0.1 L (D-178) -->
            <div v-if="finish" class="pit-fuel">
              <span class="pit-fuel-value" :class="data.fuel.spare >= 0 ? 'pit-spare' : 'pit-short'">{{ spare(data.fuel.spare) }}<small> L</small></span>
              <span class="pit-menu">{{ data.fuel.spare >= 0 ? 'to spare' : 'short: save fuel or stop' }}</span>
            </div>
            <div v-else class="pit-fuel">
              <span class="pit-fuel-value" :class="{ 'pit-ok': fuelSet }">{{ data.fuel.fuelToAdd }}<small> L</small></span>
              <span v-if="data.fuel.pitMenuKnown" class="pit-menu" :class="{ 'pit-ok': fuelSet }">pit menu<br><b>{{ Math.round(data.fuel.pitMenuFuel) }} L</b></span>
            </div>
            <p v-if="!finish && data.fuel.lastStop" class="pit-min">Last stop: the minimum to the flag ({{ data.fuel.minToAdd.toFixed(1) }} L), no safety margin.</p>
          </template>
          <p v-else class="pit-reason">{{ data.fuel ? data.fuel.reason : '' }}</p>
        </section>

        <section class="pit-block" aria-labelledby="pit-tyres-h">
          <h2 id="pit-tyres-h" class="pit-label">{{ data.tyres.pitMenuKnown ? 'Pressures to set' : 'Pressure change' }}
            <span class="pit-target">target {{ psi(data.tyres.target) }} hot · {{ data.tyres.wet ? 'wet' : 'dry' }}</span></h2>
          <!-- per tyre: the pressure to set and, as big, the change from the pit menu (rig 2026-11-21:
               "I want to see the +-0.x psi more clearly") -->
          <div v-if="!data.tyres.reason" class="pit-tyres">
            <div v-for="(w, i) in WHEELS" :key="w" class="pit-tyre">
              <span class="pit-tyre-name">{{ w }}</span>
              <span class="pit-tyre-main">
                <span v-if="data.tyres.pitMenuKnown" class="pit-tyre-value" :class="{ 'pit-ok': tyreSet(i) }">{{ psi(data.tyres.advice[i]) }}</span>
                <span class="pit-delta" :class="deltaClass(data.tyres.change[i])">{{ arrow(data.tyres.change[i]) }}{{ signed(data.tyres.change[i]) }}</span>
              </span>
              <span class="pit-tyre-sub"><template v-if="data.tyres.pitMenuKnown">menu {{ psi(data.tyres.pitMenu[i]) }} · </template>hot {{ psi(data.tyres.average[i]) }}</span>
            </div>
          </div>
          <p v-else class="pit-reason">{{ data.tyres.reason }}</p>
        </section>

        <dl v-if="data.fuel && !data.fuel.reason" class="pit-facts">
          <div><dt>Laps to go</dt><dd>{{ data.fuel.lapsToGo.toFixed(1) }}</dd></div>
          <div><dt>Fuel / lap</dt><dd>{{ data.fuel.fuelPerLap.toFixed(2) }} L</dd></div>
          <div><dt>In the tank</dt><dd>{{ litres(data.fuel.fuelNow) }}</dd></div>
          <div><dt>Needed</dt><dd>{{ litres(data.fuel.minNeeded) }}</dd></div>
          <div><dt>Stops</dt><dd>{{ data.fuel.stopsPlanned }}<small v-if="data.fuel.stopsKnown"> ({{ data.fuel.stopsLeft }} mand.)</small></dd></div>
          <div><dt>Average lap</dt><dd>{{ lapTime(data.fuel.lapMs) }}</dd></div>
          <div v-if="data.fuel.tankSize"><dt>Tank</dt><dd>{{ Math.round(data.fuel.tankSize) }} L</dd></div>
          <div v-if="data.fuel.maxStintMs"><dt>Max stint</dt><dd>{{ clock(data.fuel.maxStintMs) }}</dd></div>
          <div><dt>Plan</dt><dd>{{ data.fuel.endurance ? 'Long stints' : 'Equal stints' }}</dd></div>
        </dl>
        <p v-if="data.fuel && !data.fuel.reason && !data.fuel.setupName" class="pit-note">Setup not recognised yet: equal stints are planned.</p>

        <details class="pit-how">
          <summary>How this is worked out</summary>
          <p v-if="data.fuel && !data.fuel.reason">Fuel: {{ data.fuel.fuelPerLap.toFixed(2) }} L per lap over the last {{ data.fuel.laps }} valid laps (out-laps, in-laps and stops not counted), {{ data.fuel.lapsToGo.toFixed(1) }} laps to go<template v-if="data.fuel.timed"> from the time left</template>. Stops before the last one add a {{ data.fuel.margin }} L safety margin (fuel calculator); the last stop adds the minimum to the flag, rounded up to whole litres for the pit menu. With no stop left, the big number is the fuel left over at the flag (green) or short (red). Stint plan: {{ data.fuel.endurance ? 'longest stints, the last two equal' : 'equal stints' }}<template v-if="data.fuel.setupName">, from the setup {{ data.fuel.setupName }}</template><template v-if="data.fuel.maxStintMs">; the server allows {{ clock(data.fuel.maxStintMs) }} per stint</template>.<template v-if="!data.fuel.setupName"> SetupMan recognises the setup when you leave the pits, by comparing what the car reports (fuel; in ACC also the pit-menu tyre pressures, TC, ABS and engine map) with your setups for this car and track. When it can't tell, Manual setup select under the top bar sets it with one click.</template></p>
          <p v-if="!data.tyres.reason">Tyres: the average hot pressure of each tyre over the last {{ data.tyres.laps }} lap{{ data.tyres.laps === 1 ? '' : 's' }} is compared with the target (Settings → Games); the difference is added to the pressure now set in the pit menu.</p>
          <p v-if="data.tyres.liveKnown">Hot pressures now: <template v-for="(w, i) in WHEELS" :key="w">{{ w.toLowerCase() }} {{ psi(data.tyres.live[i]) }}<template v-if="i < 3">, </template></template> psi.</p>
        </details>
      </template>
      <p v-else-if="!error" class="loading">Loading…</p>
    </main>`,
};
