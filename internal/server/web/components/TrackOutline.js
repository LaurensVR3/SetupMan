// A track's shape (spec D-164), drawn from the user's own fastest lap there. The start/finish line
// is marked; an optional highlight colours the drawing (e.g. the class colour on a tile).
import { computed } from "vue";
import { outlinePath } from "../telemetry.js";

export const TrackOutline = {
  props: { outline: Object, label: String },
  setup(props) {
    const d = computed(() => outlinePath(props.outline));
    const box = computed(() => {
      const o = props.outline, pad = 60;
      return o ? `${-pad} ${-pad} ${o.w + 2 * pad} ${o.h + 2 * pad}` : "0 0 100 100";
    });
    const start = computed(() => (props.outline ? { x: props.outline.x[0], y: props.outline.y[0] } : null));
    return { d, box, start };
  },
  template: `
    <svg class="outline" :viewBox="box" role="img" :aria-label="label ? label + ' track layout' : 'Track layout'" preserveAspectRatio="xMidYMid meet">
      <template v-if="d">
        <path class="outline-halo" :d="d" />
        <path class="outline-line" :d="d" />
        <circle v-if="start" class="outline-start" :cx="start.x" :cy="start.y" r="34" />
      </template>
      <g v-else class="outline-none"><circle cx="50" cy="50" r="34" /></g>
    </svg>`,
};
