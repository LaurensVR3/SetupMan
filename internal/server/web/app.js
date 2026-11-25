// SetupMan web UI: Vue 3 without a build step (spec D-35).
// Pages: Overview → game → car → track, plus Settings (spec D-26).
import { createApp, ref } from "vue";
import { post, needPin } from "./api.js";
import { createRouter, createWebHashHistory } from "vue-router";
import { store, loadSettings, listen } from "./store.js";
import { TopBar } from "./components/common.js";
import { DataPackDialog, PackImportDialog, PackChip } from "./components/DataPack.js";
import { OverviewPage } from "./pages/OverviewPage.js";
import { GamePage } from "./pages/GamePage.js";
import { CarPage } from "./pages/CarPage.js";
import { TrackPage } from "./pages/TrackPage.js";
import { SettingsPage } from "./pages/SettingsPage.js";
import { SetupPage } from "./pages/SetupPage.js";
import { FuelPage } from "./pages/FuelPage.js";
import { ManualPage } from "./pages/ManualPage.js";
import { PitPage } from "./pages/PitPage.js";
import { SessionPage } from "./pages/SessionPage.js";
import { PrepPage } from "./pages/PrepPage.js";
import { TelemetryPage } from "./pages/TelemetryPage.js";
import { TelemetryTrackPage } from "./pages/TelemetryTrackPage.js";
import { AnalysisPage } from "./pages/AnalysisPage.js";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", component: OverviewPage },
    { path: "/settings", component: SettingsPage },
    { path: "/setup/:id", component: SetupPage },
    { path: "/:game", component: GamePage },
    { path: "/:game/:car", component: CarPage },
    { path: "/:game/:car/:track", component: TrackPage },
    { path: "/:game/:car/:track/fuel", component: FuelPage },
    { path: "/fuel", component: FuelPage },
    { path: "/pit", component: PitPage },
    { path: "/prep", component: PrepPage },
    { path: "/session/:id", component: SessionPage },
    { path: "/manual", component: ManualPage },
    { path: "/telemetry", component: TelemetryPage },
    { path: "/telemetry/analyse", component: AnalysisPage },
    { path: "/telemetry/:game/:track", component: TelemetryTrackPage },
    // the earlier car-and-track address: the track page with that car chosen
    { path: "/telemetry/:game/:car/:track", redirect: (to) => ({ path: `/telemetry/${to.params.game}/${encodeURIComponent(to.params.track)}`, query: { car: to.params.car, ...to.query } }) },
  ],
  scrollBehavior: () => ({ top: 0 }),
});

const PinScreen = {
  setup() {
    const pin = ref("");
    const err = ref("");
    async function send() {
      try { await post("/api/pin", { pin: pin.value }); location.reload(); } catch (e) { err.value = e.message; }
    }
    return { pin, err, send };
  },
  template: `
    <main class="page pin-page">
      <form class="panel pin-box" @submit.prevent="send">
        <h1 class="page-title">Enter SetupMan's PIN</h1>
        <p class="panel-hint">You are opening SetupMan from another computer. The PIN is shown in SetupMan's Settings, under Network, on the PC SetupMan runs on.</p>
        <input v-model="pin" class="pin-input" inputmode="numeric" autocomplete="off" maxlength="6" aria-label="PIN" autofocus>
        <p v-if="err" class="field-error">{{ err }}</p>
        <button type="submit" class="btn btn-primary" :disabled="pin.length < 6">Open SetupMan</button>
      </form>
    </main>`,
};

const App = {
  components: { TopBar, PinScreen, DataPackDialog, PackImportDialog, PackChip },
  setup: () => ({ store, needPin }),
  template: `
    <PinScreen v-if="needPin" />
    <template v-else>
    <TopBar />
    <!-- Pages are keyed by path, not fullPath: switching tabs (?tab=) keeps the page and its selection. -->
    <router-view v-if="store.settings" v-slot="{ Component, route }"><component :is="Component" :key="route.path" /></router-view>
    <DataPackDialog v-if="store.settings" />
    <PackImportDialog v-if="store.settings" />
    <PackChip />
    <div v-if="store.toast" class="toast" role="status">{{ store.toast }}</div>
    <div v-if="store.dropHint" class="drop-overlay" aria-hidden="true"><div class="drop-box">{{ store.dropHint }}</div></div>
    </template>`,
};
blockStrayDrops();

import { blockStrayDrops } from "./drop.js";
try {
  await loadSettings();
} catch {
  // Opened from another computer without the PIN yet: the PIN screen shows (D-100).
}
if (!needPin.value) listen();
// v-focus: an input that appears (e.g. a note being edited) gets the keyboard focus
// ("autofocus" only works when the page loads)
createApp(App).use(router).directive("focus", { mounted: (el) => el.focus() }).mount("#app");
