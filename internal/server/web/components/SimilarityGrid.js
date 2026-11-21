// Heatmap of every setup against every other (spec D-30). Click a cell to compare that pair.
import { computed } from "vue";
import { pct } from "../format.js";

export const SimilarityGrid = {
  props: {
    setups: Array,     // all setups of the track
    order: Array,      // display order (indexes into setups)
    pairs: Array,      // compare.Pair list
    threshold: Number,
    selected: Array,   // selected setup indexes
  },
  emits: ["pick"],
  setup(props, { emit }) {
    const lookup = computed(() => {
      const m = new Map();
      props.pairs.forEach((p) => { m.set(p.a + ":" + p.b, p); m.set(p.b + ":" + p.a, p); });
      return m;
    });
    const pos = computed(() => new Map(props.order.map((s, i) => [s, i + 1])));

    function cell(a, b) {
      if (a === b) return { self: true };
      const p = lookup.value.get(a + ":" + b);
      // Colour intensity: 60% or less = none, 100% = full.
      const heat = Math.max(0, Math.min(1, (p.score - 60) / 40));
      const near = p.identical || p.score >= props.threshold;
      const summary = p.identical ? "Identical"
        : `${pct(p.score)} the same, ${p.differ} setting${p.differ === 1 ? "" : "s"} differ: ${p.summary}`;
      return {
        label: p.identical ? "=" : Math.floor(p.score),
        style: { "--heat": Math.round(heat * 100) + "%" },
        dark: heat > 0.55,
        near,
        summary,
      };
    }
    const isSelected = (a, b) => props.selected.length === 2 && props.selected.includes(a) && props.selected.includes(b);
    return { cell, pos, isSelected, emit };
  },
  template: `
    <div class="grid-wrap">
      <table class="heat" role="grid" aria-label="Similarity of every setup with every other setup">
        <thead>
          <tr><th></th><th v-for="b in order" :key="b" scope="col" :title="setups[b].name">{{ pos.get(b) }}</th></tr>
        </thead>
        <tbody>
          <tr v-for="a in order" :key="a">
            <th scope="row" :title="setups[a].name"><span class="heat-num">{{ pos.get(a) }}</span>{{ setups[a].name }}</th>
            <template v-for="b in order" :key="b">
              <td v-if="a === b" class="heat-self" aria-hidden="true"></td>
              <td v-else>
                <button type="button" class="heat-cell"
                  :class="{ 'heat-dark': cell(a,b).dark, 'heat-near': cell(a,b).near, 'heat-selected': isSelected(a,b) }"
                  :style="cell(a,b).style" :title="setups[a].name + ' vs ' + setups[b].name + '\\n' + cell(a,b).summary"
                  @click="emit('pick', [a, b])">{{ cell(a,b).label }}</button>
              </td>
            </template>
          </tr>
        </tbody>
      </table>
    </div>`,
};
