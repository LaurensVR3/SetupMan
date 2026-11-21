// Car page: one tile per track that has setups for this car (spec D-28).
import { watchEffect } from "vue";
import { get, enc } from "../api.js";
import { classColors, plural } from "../format.js";
import { usePageData } from "./load.js";
import { BrandLogo, DupBadge, Loading, setCrumbs } from "../components/common.js";

export const CarPage = {
  components: { BrandLogo, DupBadge, Loading },
  setup() {
    const { data, error } = usePageData((p) => get(`/api/games/${enc(p.game)}/cars/${enc(p.car)}`));
    watchEffect(() => {
      if (!data.value) return;
      const g = data.value.game, c = data.value.car;
      setCrumbs([{ label: g.name, to: "/" + g.id }, { label: c.name, to: `/${g.id}/${c.id}` }]);
    });
    return { data, error, classColors, plural };
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
        <div class="track-grid">
          <router-link v-for="t in data.tracks" :key="t.id" :to="'/' + data.game.id + '/' + data.car.id + '/' + t.id" class="track-tile">
            <span class="track-name">{{ t.name }}</span>
            <span class="track-count"><b>{{ t.setups }}</b> {{ t.setups === 1 ? 'setup' : 'setups' }}</span>
            <DupBadge :count="t.duplicates" />
          </router-link>
        </div>
      </template>
    </main>`,
};
