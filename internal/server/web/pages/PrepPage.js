// Race prep (spec D-141): choose car, track, session and the expected temperature; SetupMan ranks
// your setups for it, shows the fuel plan for the chosen one and puts it in the game in one click.
import { ref, reactive, computed, watch, onMounted, nextTick } from "vue";
import { useRoute } from "vue-router";
import { get, post, put, enc } from "../api.js";
import { store, stagingDone, showToast } from "../store.js";
import { setCrumbs, EmptyState } from "../components/common.js";
import { SessionBadge } from "../components/FamilyTree.js";
import { lapTime } from "../components/LapTimes.js";
import { PlanView } from "./FuelPage.js";

const SESSIONS = [["Q", "Qualifying"], ["R", "Race"], ["Endurance", "Endurance"]];

export const PrepPage = {
  components: { EmptyState, SessionBadge, PlanView },
  setup() {
    setCrumbs([{ label: "Race prep" }]);
    const route = useRoute();
    const inp = reactive({ game: "", car: "", track: "", session: "R", minutes: "", temp: "", tempKind: "air", stops: "" });
    const games = ref([]);
    const cars = ref([]);   // [{id, name, cls}]
    const tracks = ref([]); // [{id, name}]
    const game = computed(() => games.value.find((g) => g.id === inp.game));
    const list = ref(null);
    const error = ref("");
    const chosenId = ref(0);
    const showAll = ref(false);
    const busy = ref(false);
    let loaded = false, saveTimer;

    // While you drive a session, Race prep starts from it (rig 2026-11-21, D-151): game, car, track,
    // session, the temperature now and the setup being driven. A track page link still wins.
    const liveNote = ref("");
    onMounted(async () => {
      let rec = null;
      try {
        games.value = (await get("/api/games")).games.filter((g) => g.visible && g.status === "ok");
        const saved = await get("/api/prep/inputs").catch(() => ({}));
        Object.assign(inp, saved);
        const q = route.query; // opened from a track page: that car and track
        if (q.game) Object.assign(inp, { game: q.game, car: q.car || "", track: q.track || "" });
        else {
          rec = await get("/api/recording").catch(() => null);
          if (rec && rec.active && games.value.some((g) => g.id === rec.game)) Object.assign(inp, { game: rec.game, car: rec.car });
          else rec = null;
        }
        if (!games.value.some((g) => g.id === inp.game)) inp.game = games.value[0]?.id || "";
      } catch (e) { error.value = e.message; }
      await loadCars();
      await loadTracks();
      if (rec) {
        const t = matchTrack(rec.track);
        if (t && inp.car === rec.car) {
          inp.track = t.id;
          if (rec.kind === "race") inp.session = "R";
          else if (rec.kind === "qualifying" || rec.kind === "superpole") inp.session = "Q";
          if (rec.hasTemps) inp.temp = String(Math.round(inp.tempKind === "track" ? rec.road : rec.air));
          if (rec.setupId) chosenId.value = rec.setupId;
          const g = games.value.find((x) => x.id === rec.game);
          liveNote.value = `Filled in from your ${g ? g.short : ""} session` + (rec.hasTemps ? `: air ${Math.round(rec.air)} °C, track ${Math.round(rec.road)} °C` : "") + (rec.setupName ? `, setup ${rec.setupName}` : "") + ".";
        } else rec = null;
      }
      await nextTick(); // let the input watchers see the restored inputs before they react
      loaded = true;
      rank(!!(rec && rec.setupId));
    });

    async function loadCars() {
      cars.value = [];
      if (!inp.game) return;
      const d = await get("/api/games/" + enc(inp.game)).catch(() => null);
      cars.value = (d?.classes || []).flatMap((c) => c.cars.map((x) => ({ id: x.id, name: x.name, cls: c.class })));
      if (!cars.value.some((c) => c.id === inp.car)) inp.car = "";
    }
    let tracksFor = ""; // the car the track list belongs to
    async function loadTracks() {
      tracks.value = [];
      tracksFor = inp.car;
      if (!inp.game || !inp.car) return;
      const d = await get(`/api/games/${enc(inp.game)}/cars/${enc(inp.car)}`).catch(() => null);
      tracks.value = (d?.tracks || []).map((t) => ({ id: t.id, name: t.name }));
      if (!tracks.value.some((t) => t.id === inp.track)) inp.track = "";
    }
    // Only a change made on this page clears the car (watchers run after start-up has restored the inputs).
    let preparing = false; // "Prepare for this race" sets game, car and track itself
    watch(() => inp.game, async (now, before) => { if (loaded && !preparing && before && now !== before) { inp.car = ""; await loadCars(); } });
    watch(() => inp.car, async (now, before) => { if (loaded && !preparing && now !== before && tracksFor !== now) await loadTracks(); });

    // Saved fuel inputs of this car and track (pit loss, safety margin, tank) and the track's lap stats.
    const fuelSaved = ref({});
    const trackStats = ref(null);
    let req = 0; // only the answer to the latest inputs is shown
    // keepChoice: a refresh (a setup shown or hidden, a new lap) keeps the setup you picked; new inputs pick the best match.
    async function rank(keepChoice = false) {
      const my = ++req;
      if (!inp.game || !inp.car || !inp.track) { list.value = null; return; }
      const t = parseFloat(String(inp.temp).replace(",", "."));
      try {
        const [r, f, l] = await Promise.all([
          post("/api/prep", { game: inp.game, car: inp.car, track: inp.track, session: inp.session, temp: isFinite(t) ? t : 0, hasTemp: isFinite(t), tempKind: inp.tempKind }),
          get(`/api/fuel/${enc(inp.game)}/${enc(inp.car)}/${enc(inp.track)}`).catch(() => ({})),
          get(`/api/laps/${enc(inp.game)}/${enc(inp.car)}/${enc(inp.track)}`).catch(() => ({})),
        ]);
        if (my !== req) return;
        const keep = keepChoice && r.setups.some((s) => s.id === chosenId.value);
        list.value = r.setups;
        fuelSaved.value = f.race || {};
        trackStats.value = l.stats && l.stats.laps ? l.stats : null;
        if (!keep) chosenId.value = r.setups[0]?.id || 0;
        error.value = "";
      } catch (e) { error.value = e.message; }
    }
    watch(inp, () => {
      if (!loaded) return;
      rank();
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => put("/api/prep/inputs", { ...inp }).catch(() => {}), 600);
    }, { deep: true });
    watch([() => store.version, () => store.lapVersion], () => { if (loaded) rank(true); }); // setups shown / hidden, new laps …

    const shown = computed(() => (list.value ? (showAll.value ? list.value : list.value.slice(0, 5)) : []));
    const chosen = computed(() => list.value?.find((s) => s.id === chosenId.value) || null);

    // Fuel plan for the chosen setup: its own laps first (at this temperature, then all), else the
    // track's, else what the fuel calculator remembers.
    const plan = reactive({ result: null, error: "", basis: [], missing: "" });
    const num = (v) => { const n = parseFloat(String(v ?? "").replace(",", ".")); return isFinite(n) ? n : 0; };
    const defaultMinutes = () => (inp.session === "Q" ? 15 : 60);
    watch([chosen, () => inp.minutes, () => inp.session, fuelSaved, trackStats], async () => {
      Object.assign(plan, { result: null, error: "", basis: [], missing: "" });
      const s = chosen.value, minutes = num(inp.minutes) || defaultMinutes(); // empty: the length shown in the box (rig 2026-11-21)
      if (!s) return;
      if (!minutes) { plan.missing = "Enter the session length to see the fuel plan."; return; }
      const fs = fuelSaved.value, ts = trackStats.value;
      const pick = (...opts) => opts.find((o) => o && o[0] > 0) || [0, ""];
      const fuelPerLap = pick([s.all.fuelLaps ? s.all.fuelPerLap : 0, "this setup's laps"], [ts?.fuelLaps ? ts.fuelPerLap : 0, "your laps on this track"], [num(fs.fuelPerLap), "the fuel calculator"]);
      try {
        if (inp.session === "Q") {
          const best = pick([s.near.bestMs / 1000, "your best lap at this temperature"], [s.all.bestMs / 1000, "your best lap with this setup"], [(ts?.bestMs || 0) / 1000, "your best lap on this track"]);
          if (!best[0] || !fuelPerLap[0]) { plan.missing = "SetupMan needs a recorded lap time and fuel use for this car and track first. Enter them in the fuel calculator."; return; }
          plan.result = { kind: "q", plan: await post("/api/fuel/qualifying", { minutes, bestLap: best[0], fuelPerLap: fuelPerLap[0], tank: num(fs.tank) }) };
          plan.basis = [`Best lap ${lapTime(best[0] * 1000)} (${best[1]})`, `${fuelPerLap[0].toFixed(2)} L per lap (${fuelPerLap[1]})`];
        } else {
          const avg = pick([s.near.avgMs / 1000, "your average at this temperature"], [s.all.avgMs / 1000, "your average with this setup"], [(ts?.avgMs || 0) / 1000, "your average on this track"]);
          if (!avg[0] || !fuelPerLap[0]) { plan.missing = "SetupMan needs a recorded lap time and fuel use for this car and track first. Enter them in the fuel calculator."; return; }
          const stops = String(inp.stops).trim() !== "" ? num(inp.stops) : s.stops || num(fs.stops);
          plan.result = { kind: "r", plan: await post("/api/fuel/race", { minutes, avgLap: avg[0], fuelPerLap: fuelPerLap[0], stops, pitLoss: num(fs.pitLoss),
            safety: fs.safety !== undefined && fs.safety !== "" ? num(fs.safety) : 2, tank: num(fs.tank), windowOpen: num(fs.windowOpen), windowClose: num(fs.windowClose) }) };
          plan.basis = [`Average lap ${lapTime(avg[0] * 1000)} (${avg[1]})`, `${fuelPerLap[0].toFixed(2)} L per lap (${fuelPerLap[1]})`,
            stops ? `${stops} mandatory ${stops === 1 ? "stop" : "stops"}` : "no mandatory stop"];
        }
      } catch (e) { plan.error = e.message; }
    }, { immediate: true });

    // Put the chosen setup in the game (needs folder management, D-73).
    async function useIt(only) {
      const s = chosen.value;
      if (!s) return;
      busy.value = true;
      try {
        const res = only
          ? await post(`/api/games/${enc(inp.game)}/cars/${enc(inp.car)}/tracks/${enc(inp.track)}/keep-only`, { setupIds: [s.id] })
          : await post(`/api/setups/${s.id}/stage`, {});
        stagingDone(res, only ? `${game.value.short} now lists only ${s.name} for this car and track.` : `${s.name} is shown in ${game.value.short}.`);
      } catch (e) { showToast(e.message, 8000); } finally { busy.value = false; }
    }
    // Low Fuel Motorsport (D-142): the next race for the LFM ID in Settings, or any race by its number.
    // The list (D-143): your next races from LFM (a race you signed off from is left out), races you
    // asked for by number on top; "Next race in series" replaces that card; Remove takes a card away.
    const lfm = reactive({ configured: true, races: [], loading: false, error: "", number: "", removed: false });
    async function loadNext() {
      lfm.loading = true;
      try {
        const r = await get("/api/lfm/next");
        lfm.configured = r.configured;
        lfm.races = r.races;
        lfm.removed = false;
        lfm.error = "";
      } catch (e) { lfm.error = e.message; } finally { lfm.loading = false; }
    }
    // replace: the race number of the card to swap for this race (0 = add it on top).
    async function loadRace(id, replace = 0) {
      const n = Number(String(id).trim());
      if (!n) return;
      lfm.loading = true;
      try {
        const r = await get("/api/lfm/race/" + n);
        const rest = lfm.races.filter((x) => x.id !== r.id);
        const i = rest.findIndex((x) => x.id === replace);
        if (i >= 0) rest.splice(i, 1, r); else rest.unshift(r);
        lfm.races = rest;
        if (!replace) lfm.number = "";
        lfm.error = "";
      } catch (e) { lfm.error = e.message; } finally { lfm.loading = false; }
    }
    function removeRace(id) {
      lfm.races = lfm.races.filter((x) => x.id !== id);
      lfm.removed = true;
      prepNote.value = "";
    }
    onMounted(loadNext);
    const fmtStart = (iso) => { const d = new Date(iso); return isNaN(d) || d.getFullYear() < 2000 ? "" : d.toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }); };
    const sessionText = (r) => r.sessions.filter((x) => x.minutes > 0).map((x) => `${{ P: "Practice", Q: "Qualifying", R: "Race" }[x.type] || x.type} ${x.minutes} min at ${String(x.hour).padStart(2, "0")}:00`).join(" · ");
    const raceMinutes = (r) => (r.sessions.find((x) => x.type === "R") || {}).minutes || 0;
    const carLabel = (r) => (r.car ? (cars.value.find((c) => c.id === r.car)?.name || r.car) : "");
    // A server track name ("paul_ricard_2024") to the track as SetupMan knows it ("paul_ricard").
    const matchTrack = (server) => {
      const norm = (x) => String(x || "").toLowerCase().replace(/_20\d\d$/, "").replace(/[^a-z0-9]/g, "");
      return tracks.value.find((t) => t.id === String(server).toLowerCase()) || tracks.value.find((t) => norm(t.id) === norm(server));
    };
    const prepNote = ref("");
    async function prepare(r) {
      prepNote.value = "";
      if (!r.game || !games.value.some((g) => g.id === r.game)) { prepNote.value = "SetupMan can show this race, but can't fill in Race prep for its game yet."; return; }
      preparing = true;
      try {
        if (inp.game !== r.game) { inp.game = r.game; await loadCars(); }
        if (r.car && cars.value.some((c) => c.id === r.car)) inp.car = r.car;
        if (!inp.car) { prepNote.value = "Choose your car: SetupMan didn't find you in this race's entry list."; return; }
        await loadTracks();
        const t = matchTrack(r.serverTrack);
        if (!t) { prepNote.value = `You have no setups for ${r.track} with this car yet.`; inp.track = ""; return; }
        inp.track = t.id;
        inp.session = "R";
        inp.minutes = raceMinutes(r) || inp.minutes;
        inp.stops = r.stops >= 0 ? String(r.stops) : "";
        if (r.weather.known) {
          if (inp.tempKind === "air") inp.temp = String(r.weather.ambientTemp);
          else { inp.temp = ""; prepNote.value = "LFM gives the air temperature only. Enter the track temperature you expect (for example from the weather simulator)."; }
        }
      } finally { preparing = false; }
    }
    const trackPage = computed(() => `/${inp.game}/${inp.car}/${inp.track}`);
    const carName = computed(() => cars.value.find((c) => c.id === inp.car)?.name || "");
    const trackName = computed(() => tracks.value.find((t) => t.id === inp.track)?.name || "");
    return { liveNote, defaultMinutes, lfm, loadNext, loadRace, removeRace, fmtStart, sessionText, carLabel, prepare, prepNote, SESSIONS, inp, games, cars, tracks, game, list, error, shown, showAll, chosen, chosenId, plan, busy, useIt, trackPage, carName, trackName, lapTime };
  },
  template: `
    <main class="page">
      <div class="page-head">
        <div>
          <h1 class="page-title">Race prep</h1>
          <p class="page-sub">Which of your setups to use, and how much fuel. Your choices are remembered.</p>
        </div>
      </div>
      <p v-if="error" class="notice notice-error">{{ error }}</p>

      <section class="panel prep-lfm" aria-labelledby="lfm-h">
        <div class="prep-lfm-head">
          <h2 id="lfm-h" class="panel-title">Low Fuel Motorsport</h2>
          <form class="prep-lfm-number" @submit.prevent="loadRace(lfm.number)">
            <input v-model="lfm.number" inputmode="numeric" placeholder="Race number" aria-label="LFM race number">
            <button type="submit" class="btn btn-sm" :disabled="!lfm.number || lfm.loading">Show race</button>
            <button v-if="lfm.configured" type="button" class="btn btn-sm btn-quiet" :disabled="lfm.loading" @click="loadNext" title="Ask LFM again for your next race (after signing up or off)">Refresh</button>
          </form>
        </div>
        <p v-if="!lfm.configured && !lfm.races.length" class="panel-hint">Enter your LFM ID in <router-link to="/settings">Settings → General</router-link> to see your next LFM race here, or enter a race number.</p>
        <p v-else-if="lfm.loading && !lfm.races.length" class="panel-hint">Asking LFM…</p>
        <p v-else-if="lfm.configured && !lfm.races.length && !lfm.error && lfm.removed" class="panel-hint">No race shown. Select Refresh to see your next LFM race, or enter a race number.</p>
        <p v-else-if="lfm.configured && !lfm.races.length && !lfm.error" class="panel-hint">LFM shows no upcoming race for you. Enter a race number to see any race.</p>
        <p v-if="lfm.error" class="notice notice-error notice-tight">{{ lfm.error }}</p>
        <article v-for="r in lfm.races" :key="r.id" class="prep-race">
          <div class="prep-race-head">
            <div>
              <h3>{{ r.event || 'LFM race' }}: {{ r.track }}</h3>
              <p class="prep-why">Race {{ r.id }}<template v-if="fmtStart(r.start)"> · starts {{ fmtStart(r.start) }}</template><template v-if="r.hasCar"> · your car: {{ carLabel(r) || 'number ' + r.carModel }}</template></p>
            </div>
            <div class="prep-race-actions">
              <button type="button" class="btn btn-sm btn-primary" @click="prepare(r)">Prepare for this race</button>
              <button type="button" class="btn btn-sm btn-quiet" :disabled="lfm.loading" @click="loadRace(r.id + 1, r.id)" title="Replaces this race with the next race of its series (the next race number)">Next race in series</button>
              <button type="button" class="btn btn-sm btn-quiet" @click="removeRace(r.id)" title="Remove this race from the list">Remove</button>
            </div>
          </div>
          <p v-if="r.sessions.length" class="prep-why">{{ sessionText(r) }}<template v-if="r.stops > 0"> · {{ r.stops }} mandatory {{ r.stops === 1 ? 'stop' : 'stops' }}</template></p>
          <dl v-if="r.weather.known" class="prep-weather">
            <div><dt>Air temperature</dt><dd>{{ r.weather.ambientTemp }} °C</dd></div>
            <div><dt>Cloud level</dt><dd>{{ r.weather.cloudLevel }}</dd></div>
            <div><dt>Rain</dt><dd>{{ r.weather.rain }}</dd></div>
            <div><dt>Randomness</dt><dd>{{ r.weather.randomness }}</dd></div>
          </dl>
          <dl v-else-if="r.weather.other && Object.keys(r.weather.other).length" class="prep-weather">
            <div v-for="(v, k) in r.weather.other" :key="k"><dt>{{ k }}</dt><dd>{{ v }}</dd></div>
          </dl>
          <p v-else class="prep-why">LFM gives no weather settings for this race yet.</p>
          <p v-if="r.simId === 1 && r.weather.known" class="prep-why">These are the server's settings, not a forecast. To see what weather they can give, enter them in the <a href="https://www.lennyslounge.net/ACC%20Weather%20sim/" target="_blank" rel="noopener noreferrer">ACC weather simulator</a> (Lenny's Lounge).</p>
        </article>
        <p v-if="prepNote" class="notice notice-tight">{{ prepNote }}</p>
      </section>

      <p v-if="liveNote" class="notice notice-tight prep-live">{{ liveNote }}</p>
      <section class="panel prep-form" aria-label="Session">
        <div class="prep-grid">
          <label v-if="games.length > 1">Game
            <select v-model="inp.game"><option v-for="g in games" :key="g.id" :value="g.id">{{ g.name }}</option></select></label>
          <label>Car
            <select v-model="inp.car"><option value="">Choose a car…</option><option v-for="c in cars" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
          <label>Track
            <select v-model="inp.track" :disabled="!inp.car"><option value="">Choose a track…</option><option v-for="t in tracks" :key="t.id" :value="t.id">{{ t.name }}</option></select></label>
          <div class="prep-field"><span>Session</span>
            <div class="segmented" role="group" aria-label="Session"><button v-for="s in SESSIONS" :key="s[0]" type="button" :class="{ on: inp.session === s[0] }" :aria-pressed="inp.session === s[0]" @click="inp.session = s[0]">{{ s[1] }}</button></div></div>
          <label>Length<span class="unit-input"><input v-model="inp.minutes" inputmode="decimal" :placeholder="String(defaultMinutes())"> min</span></label>
          <label v-if="inp.session !== 'Q'">Mandatory stops<span class="unit-input"><input v-model="inp.stops" inputmode="numeric" placeholder="from the setup"></span></label>
          <div class="prep-field"><span>Expected temperature</span>
            <span class="prep-temp"><span class="unit-input"><input v-model="inp.temp" inputmode="decimal" placeholder="optional" aria-label="Expected temperature"> °C</span>
              <span class="segmented" role="group" aria-label="Air or track temperature"><button type="button" :class="{ on: inp.tempKind === 'air' }" :aria-pressed="inp.tempKind === 'air'" @click="inp.tempKind = 'air'">Air</button><button type="button" :class="{ on: inp.tempKind === 'track' }" :aria-pressed="inp.tempKind === 'track'" @click="inp.tempKind = 'track'">Track</button></span></span>
            <small>Say whether this is the air or the track temperature; SetupMan compares it with the same temperature in your setup names (3040 = air 30 °C, track 40 °C, as the game shows them).</small></div>
        </div>
      </section>

      <EmptyState v-if="!games.length && !error" title="No setups yet">
        <p>Race prep ranks the setups SetupMan knows. Open a game on the start page first.</p>
      </EmptyState>
      <p v-else-if="!inp.car || !inp.track" class="panel-hint">Choose a car and a track to see your setups for this session.</p>
      <template v-else-if="list">
        <EmptyState v-if="!list.length" title="No setups for this car and track">
          <p>SetupMan has no setups for {{ carName }} at {{ trackName }}.</p>
        </EmptyState>
        <div v-else class="prep-layout">
          <section class="panel" aria-labelledby="rank-h">
            <h2 id="rank-h" class="panel-title">Your setups, best match first</h2>
            <ul class="prep-list" role="radiogroup" aria-label="Setup to use">
              <li v-for="(s, i) in shown" :key="s.id" :class="{ on: s.id === chosenId }">
                <label class="prep-item">
                  <input type="radio" name="prep-setup" :value="s.id" v-model="chosenId">
                  <span class="prep-rank">{{ i + 1 }}</span>
                  <span class="prep-main">
                    <span class="prep-name">{{ s.name }} <SessionBadge :session="s.session" small /><span v-if="s.top" class="prep-top">Recommended</span></span>
                    <span class="prep-why">{{ s.reasons.join(' · ') }}</span>
                  </span>
                </label>
              </li>
            </ul>
            <button v-if="list.length > 5" type="button" class="btn btn-quiet btn-sm" @click="showAll = !showAll">{{ showAll ? 'Show the top 5' : 'Show all ' + list.length }}</button>
          </section>

          <section v-if="chosen" class="panel" aria-labelledby="use-h">
            <h2 id="use-h" class="panel-title">{{ chosen.name }}</h2>
            <p class="panel-hint">{{ chosen.inFolder ? 'Shown in ' + game.short + '.' : 'Hidden from ' + game.short + ' (stored in SetupMan).' }}
              <router-link :to="'/setup/' + chosen.id">Open setup</router-link> · <router-link :to="trackPage + '?tab=compare'">Compare on the track page</router-link></p>
            <div v-if="game && game.managed" class="prep-actions">
              <button v-if="!chosen.inFolder" type="button" class="btn btn-primary" :disabled="busy" @click="useIt(false)">Show in {{ game.short }}</button>
              <button type="button" class="btn" :class="{ 'btn-primary': chosen.inFolder }" :disabled="busy" @click="useIt(true)" :title="'Every other setup for this car and track is hidden from ' + game.short + ' (they stay in SetupMan)'">Show only this one in {{ game.short }}</button>
            </div>
            <p v-else-if="game && !game.readOnly" class="notice notice-tight">To put it in {{ game.short }} from here, <router-link :to="'/settings?tab=games&game=' + inp.game">enable folder management</router-link>.</p>

            <h3 class="sub-title">Fuel</h3>
            <p v-if="plan.missing" class="panel-hint">{{ plan.missing }} <router-link :to="trackPage + '/fuel'">Fuel calculator</router-link></p>
            <p v-else-if="plan.error" class="notice notice-error">{{ plan.error }}</p>
            <template v-else-if="plan.result">
              <p class="panel-hint">Based on {{ plan.basis.join(', ') }}. Pit loss, safety margin and tank come from the <router-link :to="trackPage + '/fuel'">fuel calculator</router-link>.</p>
              <PlanView :plan="plan.result.plan" :kind="plan.result.kind" />
            </template>
          </section>
        </div>
      </template>
    </main>`,
};
