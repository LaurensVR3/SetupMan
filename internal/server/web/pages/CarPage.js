// Car page: one tile per track that has setups for this car (spec D-28).
import { computed, watchEffect } from "vue";
import { get, enc } from "../api.js";
import { classColors, plural } from "../format.js";
import { usePageData } from "./load.js";
import { BrandLogo, DupBadge, Loading, setCrumbs } from "../components/common.js";
import { TrackOutline } from "../components/TrackOutline.js";
import { lapTime } from "../components/LapTimes.js";

export const CarPage = {
  components: { BrandLogo, DupBadge, Loading, TrackOutline },
  setup() {
    const { data, error } = usePageData((p) => get(`/api/games/${enc(p.game)}/cars/${enc(p.car)}`));
    watchEffect(() => {
      if (!data.value) return;
      const g = data.value.game, c = data.value.car;
      setCrumbs([{ label: g.short || g.name, to: "/" + g.id }, { label: c.name, to: `/${g.id}/${c.id}` }]);
    });
    // circuit outlines only when SetupMan has one for a track of this car (from telemetry, D-165)
    const maps = () => (data.value?.tracks || []).some((t) => t.outline);
    const tracks = computed(() => [...(data.value?.tracks || [])].sort((a, b) => a.name.localeCompare(b.name))); // alphabetical
    return { data, error, classColors, plural, maps, tracks, lapTime };
  },
  template: `
    <main class="page">
      <Loading v-if="!data" :error="error" />
      <template v-else>
        <div class="car-head" :class="'cls-' + (classColors[data.car.class] || 'other')">
          <BrandLogo :brand="data.car.brand" size="lg" />
          <div>
            <h1 class="page-title">{{ data.car.name }}</h1>
            <p class="page-sub">
              <span class="class-chip" :class="'cls-' + (classColors[data.car.class] || 'other')">{{ data.car.class }}</span>
              <span v-if="data.car.year">{{ data.car.year }}</span>
              <span>{{ plural(data.car.setups, 'setup') }} on {{ plural(data.car.tracks, 'track') }}</span>
            </p>
          </div>
        </div>
        <div class="track-grid" :class="{ 'track-grid-maps': maps() }">
          <router-link v-for="t in tracks" :key="t.id" :to="'/' + data.game.id + '/' + data.car.id + '/' + t.id" class="track-tile" :class="'cls-' + (classColors[data.car.class] || 'other')">
            <span v-if="maps()" class="track-map"><TrackOutline :outline="t.outline" :label="t.name" /></span>
            <span class="track-name">{{ t.name }}</span>
            <span class="track-count"><span><b>{{ t.setups }}</b> {{ t.setups === 1 ? 'setup' : 'setups' }}</span><b v-if="t.bestMs" class="tl-time is-best" title="Your fastest lap with this car here">{{ lapTime(t.bestMs) }}</b></span>
            <DupBadge :count="t.duplicates" />
          </router-link>
        </div>
      </template>
    </main>`,
};
