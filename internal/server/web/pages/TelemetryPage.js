// Telemetry comparison page (spec D-156): compare ACC laps against a configurable reference lap
// using live shared-memory recordings, imported MoTeC (.ld / .ldx) logs, or Nürburgring 24h samples.
import { ref, computed, watch, onMounted } from "vue";
import { useRoute } from "vue-router";
import { get, post, put, del } from "../api.js";
import { store, showToast } from "../store.js";
import { setCrumbs } from "../components/common.js";
import { lapTime } from "../components/LapTimes.js";
import { dateTime } from "../format.js";
import { TrackMap } from "../components/TrackMap.js";
import { TelemetryCharts } from "../components/TelemetryCharts.js";

function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const s = String(reader.result || "");
      const idx = s.indexOf(",");
      resolve(idx >= 0 ? s.slice(idx + 1) : s);
    };
    reader.onerror = () => reject(new Error("Could not read " + file.name));
    reader.readAsDataURL(file);
  });
}

export const TelemetryPage = {
  components: { TrackMap, TelemetryCharts },
  setup() {
    const route = useRoute();
    const loading = ref(true);
    const error = ref("");
    const allLaps = ref([]);
    const liveStatus = ref({ active: false });
    const motecFolder = ref("");

    const selectedTrack = ref(String(route.query.track || "").toLowerCase());
    const selectedCar = ref(String(route.query.car || ""));
    const refId = ref(0);
    const cmpId = ref(0);

    const comp = ref(null);
    const compLoading = ref(false);
    const compError = ref("");

    const cursorIdx = ref(-1);
    const zoomRange = ref([0, 1]);
    const activeSectionIdx = ref(-1);
    const mapExpanded = ref(false);

    const busy = ref(false);
    const showLibrary = ref(false);
    const editingId = ref(0);
    const editingLabel = ref("");

    // MoTeC import dialog state
    const motecDlg = ref(null);
    const motecFiles = ref([]);
    const motecLoading = ref(false);
    const motecBusy = ref("");
    const uploadMsg = ref("");
    const dragOver = ref(false);

    setCrumbs([{ label: "Telemetry" }]);

    // Unique tracks & cars available in stored telemetry laps.
    const tracks = computed(() => {
      const m = new Map();
      for (const l of allLaps.value) {
        if (!m.has(l.track)) m.set(l.track, { id: l.track, name: l.trackName || l.track, count: 0 });
        m.get(l.track).count++;
      }
      return [...m.values()].sort((a, b) => a.name.localeCompare(b.name));
    });

    const trackLaps = computed(() => {
      let list = allLaps.value;
      if (selectedTrack.value) list = list.filter((l) => l.track === selectedTrack.value);
      if (selectedCar.value) list = list.filter((l) => l.car === selectedCar.value);
      return list;
    });

    const carsForTrack = computed(() => {
      const m = new Map();
      for (const l of allLaps.value) {
        if (selectedTrack.value && l.track !== selectedTrack.value) continue;
        if (!m.has(l.car)) m.set(l.car, { id: l.car, name: l.carName || l.car });
      }
      return [...m.values()].sort((a, b) => a.name.localeCompare(b.name));
    });

    const refLap = computed(() => trackLaps.value.find((l) => l.id === refId.value) || null);
    const cmpLap = computed(() => trackLaps.value.find((l) => l.id === cmpId.value) || null);

    // Max absolute section delta for proportional diverging bars in the breakdown table.
    const maxSecDelta = computed(() => {
      const secs = comp.value?.sections || [];
      let m = 0.25;
      for (const s of secs) {
        if (Math.abs(s.deltaS) > m) m = Math.abs(s.deltaS);
      }
      return m;
    });

    function lapDisplayName(l) {
      if (!l) return "";
      const parts = [lapTime(l.lapMs)];
      if (l.label) parts.push(l.label);
      else if (l.setupName) parts.push(l.setupName);
      else if (l.source === "live") parts.push(`Live lap ${l.lapNumber}`);
      else if (l.source === "motec") parts.push(`MoTeC lap ${l.lapNumber}`);
      return parts.join(" · ");
    }

    function pickDefaultLaps() {
      const list = trackLaps.value;
      if (!list.length) {
        refId.value = 0;
        cmpId.value = 0;
        comp.value = null;
        return;
      }
      // Keep current selection if still valid.
      const hasRef = list.some((l) => l.id === refId.value);
      const hasCmp = list.some((l) => l.id === cmpId.value);
      if (!hasRef) {
        const pinned = list.find((l) => l.isReference);
        const fastest = [...list].sort((a, b) => a.lapMs - b.lapMs)[0];
        refId.value = (pinned || fastest).id;
      }
      if (!hasCmp || cmpId.value === refId.value) {
        const other = list.find((l) => l.id !== refId.value);
        cmpId.value = other ? other.id : refId.value;
      }
    }

    async function loadLaps() {
      loading.value = true;
      error.value = "";
      try {
        const res = await get("/api/telemetry/laps");
        allLaps.value = res.laps || [];
        liveStatus.value = res.live || { active: false };
        motecFolder.value = res.motecFolder || "";

        if (allLaps.value.length) {
          const trackExists = allLaps.value.some((l) => l.track === selectedTrack.value);
          if (!selectedTrack.value || !trackExists) {
            const hasNords = allLaps.value.some((l) => l.track === "nurburgring_24h");
            selectedTrack.value = hasNords ? "nurburgring_24h" : allLaps.value[0].track;
          }
          if (selectedCar.value && !allLaps.value.some((l) => l.track === selectedTrack.value && l.car === selectedCar.value)) {
            selectedCar.value = "";
          }
        }
        pickDefaultLaps();
      } catch (e) {
        error.value = e.message;
      } finally {
        loading.value = false;
      }
    }

    async function loadComparison() {
      if (!refId.value || !cmpId.value) {
        comp.value = null;
        return;
      }
      compLoading.value = true;
      compError.value = "";
      try {
        comp.value = await get(`/api/telemetry/compare?ref=${refId.value}&cmp=${cmpId.value}`);
        zoomRange.value = [0, 1];
        cursorIdx.value = -1;
        activeSectionIdx.value = -1;
      } catch (e) {
        compError.value = e.message;
        comp.value = null;
      } finally {
        compLoading.value = false;
      }
    }

    watch([selectedTrack, selectedCar], () => pickDefaultLaps());
    watch([refId, cmpId], () => loadComparison());
    watch(zoomRange, ([z0, z1]) => {
      if (z0 <= 0.002 && z1 >= 0.998) activeSectionIdx.value = -1;
    });
    watch(() => store.telemetryVersion, () => loadLaps());

    onMounted(loadLaps);

    function swapLaps() {
      const a = refId.value;
      refId.value = cmpId.value;
      cmpId.value = a;
    }

    async function setAsReference(id) {
      if (!id) return;
      busy.value = true;
      try {
        await put(`/api/telemetry/laps/${id}/reference`, {});
        showToast("Reference lap updated for this track.");
        refId.value = id;
        await loadLaps();
      } catch (e) {
        showToast(e.message, 6000);
      } finally {
        busy.value = false;
      }
    }

    function startRename(l) {
      editingId.value = l.id;
      editingLabel.value = l.label || "";
    }

    async function saveRename(id) {
      busy.value = true;
      try {
        await put(`/api/telemetry/laps/${id}`, { label: editingLabel.value });
        editingId.value = 0;
        await loadLaps();
        showToast("Lap label saved.");
      } catch (e) {
        showToast(e.message, 6000);
      } finally {
        busy.value = false;
      }
    }

    async function removeLap(l) {
      busy.value = true;
      try {
        await del(`/api/telemetry/laps/${l.id}`);
        showToast("Telemetry lap deleted.");
        await loadLaps();
      } catch (e) {
        showToast(e.message, 6000);
      } finally {
        busy.value = false;
      }
    }

    async function loadNordsSample() {
      busy.value = true;
      try {
        const res = await post("/api/telemetry/sample/nords24", {});
        selectedTrack.value = "nurburgring_24h";
        selectedCar.value = "";
        await loadLaps();
        if (res.refId) refId.value = res.refId;
        if (res.cmpId) cmpId.value = res.cmpId;
        showToast("Nürburgring 24h sample laps loaded.");
      } catch (e) {
        showToast(e.message, 6000);
      } finally {
        busy.value = false;
      }
    }

    function zoomToSection(sec) {
      if (!sec) return;
      // Add slight padding (~0.4%) around the section for cleaner braking entry visibility.
      const pad = Math.min(0.006, (sec.endNorm - sec.startNorm) * 0.08);
      const a = Math.max(0, sec.startNorm - pad);
      const b = Math.min(1, sec.endNorm + pad);
      if (activeSectionIdx.value === sec.index && (zoomRange.value[0] > 0.002 || zoomRange.value[1] < 0.998)) {
        zoomRange.value = [0, 1];
        activeSectionIdx.value = -1;
      } else {
        zoomRange.value = [a, b];
        activeSectionIdx.value = sec.index;
      }
    }

    // MoTeC Import dialog actions
    async function openMotecDialog() {
      uploadMsg.value = "";
      motecDlg.value?.showModal();
      motecLoading.value = true;
      try {
        const res = await get("/api/telemetry/motec");
        motecFolder.value = res.folder || "";
        motecFiles.value = res.files || [];
      } catch (e) {
        uploadMsg.value = e.message;
      } finally {
        motecLoading.value = false;
      }
    }

    async function importMotecFromFolder(file) {
      motecBusy.value = file.name;
      uploadMsg.value = "";
      try {
        const res = await post("/api/telemetry/import-motec", { fileName: file.name });
        showToast(`Imported ${res.count} lap${res.count === 1 ? "" : "s"} from ${file.name}.`);
        motecDlg.value?.close();
        if (res.laps?.length) {
          selectedTrack.value = res.laps[0].track;
          selectedCar.value = "";
        }
        await loadLaps();
      } catch (e) {
        uploadMsg.value = e.message;
      } finally {
        motecBusy.value = "";
      }
    }

    async function handleFileUpload(fileList) {
      const files = Array.from(fileList || []);
      const ldFile = files.find((f) => f.name.toLowerCase().endsWith(".ld"));
      if (!ldFile) {
        uploadMsg.value = "Please drop or select a MoTeC .ld file (optionally together with its matching .ldx file).";
        return;
      }
      const ldxFile = files.find((f) => f.name.toLowerCase().endsWith(".ldx"));
      motecBusy.value = ldFile.name;
      uploadMsg.value = "";
      try {
        const ldData = await fileToBase64(ldFile);
        const ldxData = ldxFile ? await fileToBase64(ldxFile) : "";
        const res = await post("/api/telemetry/import-motec", { ldName: ldFile.name, ldData, ldxData });
        showToast(`Imported ${res.count} lap${res.count === 1 ? "" : "s"} from ${ldFile.name}.`);
        motecDlg.value?.close();
        if (res.laps?.length) {
          selectedTrack.value = res.laps[0].track;
          selectedCar.value = "";
        }
        await loadLaps();
      } catch (e) {
        uploadMsg.value = e.message;
      } finally {
        motecBusy.value = "";
      }
    }

    function onDropFiles(e) {
      dragOver.value = false;
      handleFileUpload(e.dataTransfer?.files);
    }

    function fmtDelta(v) {
      if (v === undefined || v === null) return "–";
      const sign = v > 0.0005 ? "+" : v < -0.0005 ? "−" : "";
      return sign + Math.abs(v).toFixed(3) + " s";
    }

    function fmtSigned(v, unit = "") {
      if (v === undefined || v === null) return "–";
      const sign = v > 0.05 ? "+" : v < -0.05 ? "−" : "";
      return sign + Math.abs(v).toFixed(1) + unit;
    }

    function fmtBytes(n) {
      if (!n) return "0 B";
      if (n >= 1048576) return (n / 1048576).toFixed(1) + " MB";
      return Math.round(n / 1024) + " KB";
    }

    return {
      loading, error, allLaps, liveStatus, motecFolder,
      tracks, carsForTrack, trackLaps, selectedTrack, selectedCar,
      refId, cmpId, refLap, cmpLap, comp, compLoading, compError,
      cursorIdx, zoomRange, activeSectionIdx, mapExpanded, maxSecDelta,
      busy, showLibrary, editingId, editingLabel,
      motecDlg, motecFiles, motecLoading, motecBusy, uploadMsg, dragOver,
      lapTime, dateTime, lapDisplayName, swapLaps, setAsReference,
      startRename, saveRename, removeLap, loadNordsSample, zoomToSection,
      openMotecDialog, importMotecFromFolder, handleFileUpload, onDropFiles,
      fmtDelta, fmtSigned, fmtBytes,
    };
  },
  template: `
    <main class="page page-wide tele-page">
      <!-- Page header & top actions -->
      <div class="page-head">
        <div>
          <h1 class="page-title">Lap telemetry</h1>
          <p class="page-sub">
            Compare ACC laps against a reference lap — spot braking points, speed differences, and where you gain or lose time on track.
            <span v-if="liveStatus.active" class="tele-live-pill"><i></i> Live capture: {{ liveStatus.trackName || liveStatus.track }} (Lap {{ liveStatus.lapNumber }}, {{ liveStatus.samples }} samples)</span>
          </p>
        </div>
        <div class="page-actions">
          <button type="button" class="btn btn-primary" :disabled="busy" @click="openMotecDialog">Import MoTeC (.ld)…</button>
          <button v-if="allLaps.length" type="button" class="btn" @click="showLibrary = !showLibrary">{{ showLibrary ? 'Hide lap list' : 'Manage laps (' + trackLaps.length + ')' }}</button>
        </div>
      </div>

      <div v-if="error" class="notice notice-error" role="alert">{{ error }}</div>
      <div v-else-if="loading" class="loading">Loading telemetry…</div>

      <!-- Empty state -->
      <section v-else-if="!allLaps.length" class="panel tele-empty">
        <div class="tele-empty-grid">
          <div>
            <h2>No telemetry laps stored yet</h2>
            <p>SetupMan can record and compare ACC lap telemetry in two ways:</p>
            <ul class="tele-empty-list">
              <li><strong>Automatic live recording from ACC</strong> — keep SetupMan running while you drive. Every completed lap is captured at 20 Hz with exact 2D track coordinates, speed, throttle, brake, steering, gear, TC and ABS.</li>
              <li><strong>Import MoTeC logs (.ld / .ldx)</strong> — load reference laps or sessions from your ACC <code>MoTeC</code> folder or drop any <code>.ld</code> file here.</li>
            </ul>
            <div class="tele-empty-actions">
              <button type="button" class="btn btn-primary" :disabled="busy" @click="openMotecDialog">Import MoTeC (.ld)…</button>
            </div>
          </div>
          <div class="tele-empty-tip">
            <strong>Tip for ACC MoTeC logs</strong>
            <p>If your ACC <code>.ld</code> files contain no speed/pedal data, open your car setup in ACC → <strong>Electronics</strong> → set <strong>Telemetry Laps</strong> above <code>0</code> and complete a flying lap.</p>
          </div>
        </div>
      </section>

      <!-- Main telemetry workbench -->
      <template v-else>
        <!-- Track & car filter + Reference vs Comparison lap selector bar -->
        <section class="panel tele-selector-bar" aria-label="Select laps to compare">
          <div class="tele-filters">
            <label class="tele-field">
              <span>Track</span>
              <select v-model="selectedTrack">
                <option v-for="t in tracks" :key="t.id" :value="t.id">{{ t.name }} ({{ t.count }})</option>
              </select>
            </label>
            <label class="tele-field" v-if="carsForTrack.length > 1">
              <span>Car</span>
              <select v-model="selectedCar">
                <option value="">All cars</option>
                <option v-for="c in carsForTrack" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
            </label>
          </div>

          <div class="tele-pickers">
            <!-- Reference Lap Card -->
            <div class="tele-pick-card tele-pick-ref">
              <div class="tele-pick-head">
                <span class="tele-pick-tag"><i class="sw sw-ref"></i> Reference lap</span>
                <span v-if="refLap?.isReference" class="tele-ref-star" title="Pinned as default reference lap for this track">★ Default ref</span>
                <button v-else-if="refLap" type="button" class="tele-pin-btn" :disabled="busy" @click="setAsReference(refLap.id)">☆ Set as default ref</button>
              </div>
              <select v-model.number="refId" aria-label="Reference lap">
                <option v-for="l in trackLaps" :key="l.id" :value="l.id">
                  {{ l.isReference ? '★ ' : '' }}{{ lapDisplayName(l) }} [{{ l.source.toUpperCase() }}]
                </option>
              </select>
            </div>

            <button type="button" class="btn tele-swap-btn" @click="swapLaps" title="Swap reference and comparison lap" aria-label="Swap laps">⇄</button>

            <!-- Comparison Lap Card -->
            <div class="tele-pick-card tele-pick-cmp">
              <div class="tele-pick-head">
                <span class="tele-pick-tag"><i class="sw sw-cmp"></i> Comparison lap</span>
                <button v-if="cmpLap && !cmpLap.isReference" type="button" class="tele-pin-btn" :disabled="busy" @click="setAsReference(cmpLap.id)">☆ Make ref</button>
              </div>
              <select v-model.number="cmpId" aria-label="Comparison lap">
                <option v-for="l in trackLaps" :key="l.id" :value="l.id">
                  {{ lapDisplayName(l) }} [{{ l.source.toUpperCase() }}]
                </option>
              </select>
            </div>
          </div>
        </section>

        <!-- Optional collapsible Lap Library manager -->
        <section v-if="showLibrary" class="panel tele-lib-panel" aria-label="Stored telemetry laps">
          <div class="tele-card-head">
            <h2 class="panel-title">Stored laps for {{ tracks.find(t => t.id === selectedTrack)?.name || selectedTrack }}</h2>
            <button type="button" class="btn btn-sm" @click="showLibrary = false">Close</button>
          </div>
          <div class="table-scroll">
            <table class="tele-table tele-lib-table">
              <thead>
                <tr>
                  <th class="tl">Lap / Label</th>
                  <th class="tr">Time</th>
                  <th class="tl">Car</th>
                  <th class="tl">Source</th>
                  <th class="tr">Top speed</th>
                  <th class="tl">Recorded</th>
                  <th class="tl">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="l in trackLaps" :key="l.id" :class="{ 'tele-row-ref': l.id === refId, 'tele-row-cmp': l.id === cmpId && l.id !== refId }">
                  <td class="tl">
                    <template v-if="editingId === l.id">
                      <form class="tele-rename-form" @submit.prevent="saveRename(l.id)">
                        <input v-model="editingLabel" class="tele-rename-input" placeholder="e.g. Dry 23C Q lap" autofocus>
                        <button type="submit" class="btn btn-sm btn-primary">Save</button>
                        <button type="button" class="btn btn-sm" @click="editingId = 0">Cancel</button>
                      </form>
                    </template>
                    <template v-else>
                      <span v-if="l.isReference" class="tele-ref-star" title="Default reference">★</span>
                      <strong>{{ l.label || ('Lap ' + l.lapNumber) }}</strong>
                      <span v-if="!l.valid" class="tele-badge tele-badge-bad">Invalid</span>
                      <router-link v-if="l.setupId" :to="'/setup/' + l.setupId" class="muted-small"> · {{ l.setupName }}</router-link>
                    </template>
                  </td>
                  <td class="tr"><strong>{{ lapTime(l.lapMs) }}</strong></td>
                  <td class="tl">{{ l.carName || l.car }}</td>
                  <td class="tl"><span class="tele-badge" :class="'tele-src-' + l.source">{{ l.source.toUpperCase() }}</span></td>
                  <td class="tr">{{ l.stats?.maxSpeedKmh ? l.stats.maxSpeedKmh.toFixed(1) + ' km/h' : '–' }}</td>
                  <td class="tl muted-small">{{ dateTime(l.recordedAt) }}</td>
                  <td class="tl tele-row-actions">
                    <button type="button" class="btn btn-sm" @click="refId = l.id" :disabled="refId === l.id">Ref</button>
                    <button type="button" class="btn btn-sm" @click="cmpId = l.id" :disabled="cmpId === l.id">Cmp</button>
                    <button type="button" class="btn btn-sm" @click="setAsReference(l.id)" :disabled="l.isReference || busy" title="Pin as default reference">★ Pin</button>
                    <button type="button" class="btn btn-sm" @click="startRename(l)">Rename</button>
                    <button type="button" class="btn btn-sm btn-danger" :disabled="busy" @click="removeLap(l)">Delete</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <div v-if="compError" class="notice notice-error">{{ compError }}</div>
        <div v-else-if="compLoading && !comp" class="loading">Computing lap comparison…</div>

        <template v-else-if="comp">
          <!-- Hero KPI strip -->
          <section class="tele-kpis" aria-label="Comparison summary">
            <div class="tele-kpi tele-kpi-hero" :class="comp.totalDeltaS < -0.005 ? 'is-gain' : comp.totalDeltaS > 0.005 ? 'is-loss' : ''">
              <span class="tele-kpi-lbl">Total delta (Cmp vs Ref)</span>
              <span class="tele-kpi-val">{{ fmtDelta(comp.totalDeltaS) }}</span>
              <span class="tele-kpi-sub">{{ comp.cmp.carName || comp.cmp.car }} · {{ (comp.trackLengthM / 1000).toFixed(2) }} km</span>
            </div>
            <div class="tele-kpi">
              <span class="tele-kpi-lbl">Lap times</span>
              <span class="tele-kpi-val"><span class="c-cmp">{{ lapTime(comp.cmp.lapMs) }}</span></span>
              <span class="tele-kpi-sub">Ref: <span class="c-ref">{{ lapTime(comp.ref.lapMs) }}</span></span>
            </div>
            <div class="tele-kpi">
              <span class="tele-kpi-lbl">Top speed</span>
              <span class="tele-kpi-val">{{ comp.cmp.stats.maxSpeedKmh.toFixed(1) }} <small>km/h</small></span>
              <span class="tele-kpi-sub">Ref {{ comp.ref.stats.maxSpeedKmh.toFixed(1) }} ({{ fmtSigned(comp.cmp.stats.maxSpeedKmh - comp.ref.stats.maxSpeedKmh, ' km/h') }})</span>
            </div>
            <div class="tele-kpi">
              <span class="tele-kpi-lbl">Full throttle</span>
              <span class="tele-kpi-val">{{ comp.cmp.stats.fullThrottlePct.toFixed(1) }}%</span>
              <span class="tele-kpi-sub">Ref {{ comp.ref.stats.fullThrottlePct.toFixed(1) }}% ({{ fmtSigned(comp.cmp.stats.fullThrottlePct - comp.ref.stats.fullThrottlePct, '%') }})</span>
            </div>
            <div class="tele-kpi">
              <span class="tele-kpi-lbl">Braking / Coast</span>
              <span class="tele-kpi-val">{{ comp.cmp.stats.brakePct.toFixed(1) }}% <small>/ {{ comp.cmp.stats.coastPct.toFixed(1) }}%</small></span>
              <span class="tele-kpi-sub">Ref {{ comp.ref.stats.brakePct.toFixed(1) }}% / {{ comp.ref.stats.coastPct.toFixed(1) }}%</span>
            </div>
            <div class="tele-kpi">
              <span class="tele-kpi-lbl">TC / ABS activity</span>
              <span class="tele-kpi-val">{{ comp.cmp.stats.tcActivePct.toFixed(1) }}% <small>/ {{ comp.cmp.stats.absActivePct.toFixed(1) }}%</small></span>
              <span class="tele-kpi-sub">Ref {{ comp.ref.stats.tcActivePct.toFixed(1) }}% / {{ comp.ref.stats.absActivePct.toFixed(1) }}%</span>
            </div>
          </section>

          <!-- Top Losses & Gains clickable quick-zoom coaching strip -->
          <section v-if="comp.topLosses?.length || comp.topGains?.length" class="tele-highlights" aria-label="Key gains and losses">
            <div v-if="comp.topLosses?.length" class="tele-hl-group">
              <span class="tele-hl-title is-loss">Biggest losses</span>
              <button v-for="s in comp.topLosses" :key="'l' + s.index" type="button"
                class="tele-hl-pill is-loss" :class="{ active: activeSectionIdx === s.index }"
                @click="zoomToSection(s)" :title="s.hint || 'Click to zoom into this section'">
                <strong>{{ s.name }}</strong>
                <span>{{ fmtDelta(s.deltaS) }}</span>
              </button>
            </div>
            <div v-if="comp.topGains?.length" class="tele-hl-group">
              <span class="tele-hl-title is-gain">Biggest gains</span>
              <button v-for="s in comp.topGains" :key="'g' + s.index" type="button"
                class="tele-hl-pill is-gain" :class="{ active: activeSectionIdx === s.index }"
                @click="zoomToSection(s)" :title="s.hint || 'Click to zoom into this section'">
                <strong>{{ s.name }}</strong>
                <span>{{ fmtDelta(s.deltaS) }}</span>
              </button>
            </div>
          </section>

          <!-- Split main workbench: Track Map (left, or expanded full-width on top) + Synchronized Telemetry Charts -->
          <div class="tele-workbench" :class="{ 'map-expanded': mapExpanded }">
            <TrackMap :comp="comp" v-model:cursorIdx="cursorIdx" :zoomRange="zoomRange" v-model:expanded="mapExpanded" @select-section="zoomToSection" />
            <TelemetryCharts :comp="comp" v-model:cursorIdx="cursorIdx" v-model:zoomRange="zoomRange" />
          </div>

          <!-- Section-by-section breakdown table -->
          <section class="panel tele-sections-card" aria-labelledby="sec-h">
            <div class="tele-card-head">
              <div>
                <h2 id="sec-h" class="panel-title">Section breakdown</h2>
                <p class="tele-card-sub">Click any section to zoom the telemetry charts and highlight it on the track map.</p>
              </div>
              <button v-if="zoomRange[0] > 0.002 || zoomRange[1] < 0.998" type="button" class="btn btn-sm" @click="zoomRange = [0, 1]; activeSectionIdx = -1">Show full lap</button>
            </div>
            <div class="table-scroll">
              <table class="tele-table tele-sec-table">
                <thead>
                  <tr>
                    <th class="tl">Section</th>
                    <th class="tr">Dist</th>
                    <th class="tr">Time (Cmp / Ref)</th>
                    <th class="tr">Delta</th>
                    <th class="tele-bar-col tl">Gain / Loss</th>
                    <th class="tr">Min km/h</th>
                    <th class="tr">Max km/h</th>
                    <th class="tl">Insight</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="s in comp.sections" :key="s.index"
                    class="tele-sec-row" :class="{ active: activeSectionIdx === s.index }"
                    @click="zoomToSection(s)">
                    <td class="tl tele-wrap"><strong>{{ s.name }}</strong></td>
                    <td class="tr muted-small">{{ (s.startM / 1000).toFixed(1) }}–{{ (s.endM / 1000).toFixed(1) }} km</td>
                    <td class="tr">{{ s.cmpTimeS.toFixed(2) }}s <span class="muted-small">/ {{ s.refTimeS.toFixed(2) }}s</span></td>
                    <td class="tr"><strong :class="s.deltaS < -0.02 ? 'is-gain' : s.deltaS > 0.02 ? 'is-loss' : ''">{{ fmtDelta(s.deltaS) }}</strong></td>
                    <td class="tele-bar-col">
                      <div class="tele-divbar" aria-hidden="true">
                        <div class="tele-divbar-half left">
                          <div v-if="s.deltaS < 0" class="tele-divbar-fill is-gain" :style="{ width: Math.min(100, (Math.abs(s.deltaS) / maxSecDelta) * 100) + '%' }"></div>
                        </div>
                        <div class="tele-divbar-zero"></div>
                        <div class="tele-divbar-half right">
                          <div v-if="s.deltaS > 0" class="tele-divbar-fill is-loss" :style="{ width: Math.min(100, (s.deltaS / maxSecDelta) * 100) + '%' }"></div>
                        </div>
                      </div>
                    </td>
                    <td class="tr">
                      {{ s.cmpMinSpeed.toFixed(0) }} <span class="muted-small">/ {{ s.refMinSpeed.toFixed(0) }}</span>
                    </td>
                    <td class="tr">
                      {{ s.cmpMaxSpeed.toFixed(0) }} <span class="muted-small">/ {{ s.refMaxSpeed.toFixed(0) }}</span>
                    </td>
                    <td class="tl tele-wrap muted-small">{{ s.hint || '–' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </template>
      </template>

      <!-- MoTeC Import Dialog -->
      <dialog ref="motecDlg" class="confirm tele-motec-dlg">
        <div class="tele-card-head">
          <h2>Import MoTeC telemetry (.ld / .ldx)</h2>
          <button type="button" class="btn btn-sm" @click="motecDlg.close()">Close</button>
        </div>

        <p v-if="uploadMsg" class="notice notice-error">{{ uploadMsg }}</p>

        <!-- Drag & drop / manual file upload -->
        <label class="tele-dropzone" :class="{ over: dragOver }"
          @dragover.prevent="dragOver = true" @dragleave="dragOver = false" @drop.prevent="onDropFiles">
          <input type="file" accept=".ld,.ldx" multiple hidden @change="handleFileUpload($event.target.files); $event.target.value = ''">
          <strong>Drop a MoTeC <code>.ld</code> (+ optional <code>.ldx</code>) file here, or click to browse</strong>
          <span class="muted-small">Great for importing reference laps from coaches, teammates, or other folders.</span>
        </label>

        <!-- ACC MoTeC folder scanner -->
        <div class="tele-motec-section">
          <h3>ACC MoTeC folder <span v-if="motecFolder" class="muted-small">({{ motecFolder }})</span></h3>
          <div v-if="motecLoading" class="loading">Scanning MoTeC files…</div>
          <p v-else-if="!motecFiles.length" class="muted-small">No <code>.ld</code> files found in ACC's MoTeC folder yet.</p>
          <div v-else class="table-scroll tele-motec-list">
            <table class="tele-table">
              <thead>
                <tr>
                  <th class="tl">File</th>
                  <th class="tl">Track / Car</th>
                  <th class="tr">Laps / Data</th>
                  <th class="tr">Size</th>
                  <th class="tr">Action</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="f in motecFiles" :key="f.name">
                  <td class="tl">
                    <div class="tele-motec-fname">{{ f.name }}</div>
                    <div class="muted-small">{{ dateTime(f.modified) }}</div>
                  </td>
                  <td class="tl tele-wrap">
                    <div>{{ f.track || '–' }}</div>
                    <div class="muted-small">{{ f.car || '–' }}</div>
                  </td>
                  <td class="tr">
                    <template v-if="f.hasData">
                      <span class="tele-badge tele-src-live">{{ f.lapCount > 0 ? f.lapCount + ' laps' : Math.round(f.drivingSecs) + 's data' }}</span>
                    </template>
                    <span v-else class="tele-badge tele-badge-bad" :title="f.note">Empty</span>
                  </td>
                  <td class="tr muted-small">{{ fmtBytes(f.size) }}</td>
                  <td class="tr">
                    <button type="button" class="btn btn-sm btn-primary"
                      :disabled="!f.hasData || !!motecBusy"
                      @click="importMotecFromFolder(f)">
                      {{ motecBusy === f.name ? 'Importing…' : f.imported ? 'Re-import' : 'Import' }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="panel-hint" style="margin-top:10px">Files marked <em>Empty (0 samples)</em> were saved by ACC without flying-lap driving channels. Set <strong>Telemetry Laps &gt; 0</strong> in ACC's Electronics setup tab and complete a lap — or just let SetupMan record live automatically.</p>
        </div>
      </dialog>
    </main>
  `,
};
