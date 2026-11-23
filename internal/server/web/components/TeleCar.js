// Car view (spec D-159 T3): the four wheels with the value at the cursor for every lap, coloured
// from cool to hot, and the lap averages per wheel underneath.
import { ref, computed } from "vue";
import { valueAt, fmt, CAR_FAMILIES, WHEELS } from "../telemetry.js";

export const TeleCar = {
  props: {
    laps: Array,  // [{ color, label, series: { "TYRE_PRESS_LF": {x, y}, … } }]
    view: Object,
  },
  setup(props) {
    const chosen = ref(CAR_FAMILIES[0].base);
    const available = computed(() => CAR_FAMILIES.filter((f) => props.laps.some((l) => l.series[f.base + "_LF"])));
    // the chosen kind of value, or the first these laps have
    const fam = computed({
      get: () => (available.value.some((f) => f.base === chosen.value) ? chosen.value : available.value[0]?.base || chosen.value),
      set: (v) => (chosen.value = v),
    });
    const family = computed(() => CAR_FAMILIES.find((f) => f.base === fam.value));
    // heat: 0 = cool, 1 = hot, within the family's usual range
    const heat = (v) => (v === null ? null : Math.max(0, Math.min(1, (v - family.value.lo) / (family.value.hi - family.value.lo))));
    const tone = (v) => {
      const h = heat(v);
      if (h === null) return {};
      return { "--heat": h.toFixed(3) };
    };
    const wheels = computed(() => WHEELS.map(([name, ld]) => {
      const key = family.value.base + "_" + ld;
      // at the cursor; before pointing anywhere, the lap average
      const avg = (s) => { let t = 0; for (let i = 0; i < s.y.length; i++) t += s.y[i]; return s.y.length ? t / s.y.length : null; };
      const vals = props.laps.map((l) => (!l.series[key] ? null : props.view.cursor !== null ? valueAt(l.series[key], props.view.cursor) : avg(l.series[key])));
      return { name, vals, main: vals[0] };
    }));
    const averages = computed(() => props.laps.map((l) => ({
      label: l.label, color: l.color,
      vals: WHEELS.map(([, ld]) => {
        const s = l.series[family.value.base + "_" + ld];
        if (!s || !s.y.length) return null;
        let sum = 0;
        for (let i = 0; i < s.y.length; i++) sum += s.y[i];
        return sum / s.y.length;
      }),
    })));
    return { fam, family, available, wheels, averages, tone, fmt, WHEELS };
  },
  template: `
    <section class="tcar" v-if="available.length">
      <div class="segmented tcar-fam" role="radiogroup" aria-label="Car view">
        <button v-for="f in available" :key="f.base" type="button" role="radio" :aria-checked="fam === f.base" :class="{ on: fam === f.base }" @click="fam = f.base">{{ f.label }}</button>
      </div>
      <div class="tcar-grid">
        <div class="tcar-body" aria-hidden="true"></div>
        <div v-for="(w, i) in wheels" :key="w.name" class="tcar-wheel" :class="'tcar-' + w.name.toLowerCase()" :style="tone(w.main)">
          <span class="tcar-name">{{ w.name }}</span>
          <span class="tcar-main">{{ fmt(w.main, family.unit) }}</span>
          <span class="tcar-others"><b v-for="(v, j) in w.vals.slice(1)" :key="j" :style="{ color: laps[j + 1].color }">{{ fmt(v, family.unit) }}</b></span>
        </div>
      </div>
      <p class="tcar-unit">{{ view.cursor === null ? 'Lap average' : 'At the cursor' }}, {{ family.label.toLowerCase() }} ({{ family.unit }})<template v-if="laps.length > 1">: the first lap large, the other laps below</template>.</p>
      <table class="tcar-avg">
        <caption>Lap average</caption>
        <thead><tr><th></th><th v-for="w in WHEELS" :key="w[0]">{{ w[0] }}</th></tr></thead>
        <tbody>
          <tr v-for="(a, i) in averages" :key="i">
            <th><i class="lap-dot" :style="{ background: a.color }"></i>{{ a.label }}</th>
            <td v-for="(v, j) in a.vals" :key="j">{{ fmt(v, family.unit) }}</td>
          </tr>
        </tbody>
      </table>
    </section>`,
};
