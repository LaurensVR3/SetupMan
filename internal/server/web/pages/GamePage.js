// Game page: car tiles grouped by class, A–Z within each class (spec D-28, D-29).
import { ref, computed, watchEffect } from "vue";
import { get, post, enc } from "../api.js";
import { ago, classColors, classTitles, plural } from "../format.js";
import { usePageData } from "./load.js";
import { BrandLogo, DupBadge, EmptyState, Loading, setCrumbs } from "../components/common.js";
import { DuplicatesDialog } from "../components/Duplicates.js";
import { TidyAllDialog } from "../components/TidyAll.js";
import { store } from "../store.js";

export const GamePage = {
  components: { BrandLogo, DupBadge, EmptyState, Loading, DuplicatesDialog, TidyAllDialog },
  setup() {
    const { data, error } = usePageData((p) => get("/api/games/" + enc(p.game)));
    const filter = ref("");
    const rescanning = ref(false);

    watchEffect(() => {
      if (data.value) setCrumbs([{ label: data.value.game.name, to: "/" + data.value.game.id }]);
    });

    const classes = computed(() => {
      if (!data.value) return [];
      const f = filter.value.trim().toLowerCase();
      return data.value.classes
        .map((c) => ({ ...c, cars: c.cars.filter((car) => !f || car.name.toLowerCase().includes(f)) }))
        .filter((c) => c.cars.length);
    });

    async function rescan() {
      rescanning.value = true;
      try { await post("/api/games/" + enc(data.value.game.id) + "/rescan"); } finally { rescanning.value = false; }
    }
    const dupDlg = ref(null);
    const tidyDlg = ref(null);
    return { data, error, filter, classes, rescan, rescanning, classColors, classTitles, ago, plural, dupDlg, tidyDlg, store };
  },
  template: `
    <main class="page">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <div class="page-head">
          <div>
            <h1 class="page-title">{{ data.game.name }}</h1>
            <p class="page-sub" v-if="data.game.status === 'ok'">
              {{ plural(data.game.setups, 'setup') }} for {{ plural(data.game.cars, 'car') }}. Scanned {{ ago(data.game.scannedAt) }}.
            </p>
          </div>
          <div class="page-actions">
            <input v-if="data.classes.length" v-model="filter" type="search" class="search" placeholder="Find a car" aria-label="Find a car">
            <button v-if="data.game.status === 'ok' && !data.game.readOnly" type="button" class="btn" @click="dupDlg.open()">Remove duplicates…</button>
            <button v-if="data.game.status === 'ok' && !data.game.readOnly" type="button" class="btn" @click="tidyDlg.open()">Apply naming standard…</button>
            <button type="button" class="btn" :disabled="rescanning" @click="rescan">{{ rescanning ? 'Scanning…' : 'Scan again' }}</button>
          </div>
        </div>

        <TidyAllDialog ref="tidyDlg" :game="data.game.id" :short="data.game.short" :managed="data.game.managed" />
        <DuplicatesDialog ref="dupDlg" :game="data.game.id" :short="data.game.short" :managed="data.game.managed" :threshold="store.settings.threshold" />
        <EmptyState v-if="data.game.status !== 'ok'" title="No setups to show">
          <p>{{ data.game.message || 'SetupMan is still reading your setups.' }}</p>
          <router-link :to="'/settings?tab=games&game=' + data.game.id" class="btn btn-primary">Open settings</router-link>
        </EmptyState>
        <p v-else-if="!classes.length" class="footnote">No car matches “{{ filter }}”.</p>

        <section v-for="c in classes" :key="c.class" class="class-section">
          <h2 class="class-title"><span class="class-chip" :class="'cls-' + (classColors[c.class] || 'other')">{{ c.class }}</span>{{ classTitles[c.class] && classTitles[c.class] !== c.class ? classTitles[c.class] : '' }}</h2>
          <div class="car-grid">
            <router-link v-for="car in c.cars" :key="car.id" :to="'/' + data.game.id + '/' + car.id"
              class="car-tile" :class="'cls-' + (classColors[car.class] || 'other')">
              <BrandLogo :brand="car.brand" />
              <span class="car-name">{{ car.name }}</span>
              <span class="car-year" v-if="car.year">{{ car.year }}</span>
              <span class="car-stats">
                <span><b>{{ car.tracks }}</b> {{ car.tracks === 1 ? 'track' : 'tracks' }}</span>
                <span><b>{{ car.setups }}</b> {{ car.setups === 1 ? 'setup' : 'setups' }}</span>
              </span>
              <DupBadge :count="car.duplicates" />
            </router-link>
          </div>
        </section>

        <details v-if="data.ignored.length || data.warnings.length" class="issues">
          <summary>{{ data.ignored.length }} skipped {{ data.ignored.length === 1 ? 'file' : 'files' }}<template v-if="data.warnings.length">, {{ data.warnings.length }} {{ data.warnings.length === 1 ? 'warning' : 'warnings' }}</template></summary>
          <ul>
            <li v-for="i in data.ignored" :key="'i' + i.relPath"><code>{{ i.relPath }}</code> was skipped: {{ i.reason }}</li>
            <li v-for="w in data.warnings" :key="'w' + w.relPath"><code>{{ w.relPath }}</code>: {{ w.reason }}</li>
          </ul>
        </details>
      </template>
    </main>`,
};
