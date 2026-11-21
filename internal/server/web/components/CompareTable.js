// Side-by-side comparison of 2 or more setups (spec D-30).
import { ref, computed } from "vue";
import { value, shown, shownDelta } from "../format.js";
import { lapTime } from "./LapTimes.js";

export const CompareTable = {
  // setups: the setups to compare (objects with .name and .fields). laps (optional, same order):
  // each setup's recorded lap times ({bestMs, avgMs, clean, roadMin, roadMax, hasTemps}) or null (D-141).
  props: { setups: Array, laps: Array },
  setup(props) {
    const hasLaps = computed(() => (props.laps || []).some((l) => l && l.laps));
    // The fastest best and average lap among the compared setups are marked.
    const fastest = (key) => {
      const v = (props.laps || []).map((l) => (l && l[key]) || 0).filter((x) => x > 0);
      return v.length > 1 ? Math.min(...v) : 0;
    };
    const lapCell = (l) => {
      if (!l || !l.laps) return { none: true };
      return {
        best: l.bestMs ? lapTime(l.bestMs) : "–", avg: l.avgMs ? lapTime(l.avgMs) : "–", clean: l.clean,
        bestTop: l.bestMs && l.bestMs === fastest("bestMs"), avgTop: l.avgMs && l.avgMs === fastest("avgMs"),
        temps: l.hasTemps ? (Math.round(l.roadMin) === Math.round(l.roadMax) ? Math.round(l.roadMin) + " °C" : Math.round(l.roadMin) + "–" + Math.round(l.roadMax) + " °C") : "",
      };
    };
    const diffOnly = ref(true);
    const showCalculated = ref(false);

    // All fields in file order (a field only one setup has is inserted after its predecessor).
    const rows = computed(() => {
      const order = [];
      const info = new Map();
      for (const s of props.setups) {
        let prev = -1;
        for (const f of s.fields) {
          let at = order.indexOf(f.path);
          if (at === -1) { order.splice(prev + 1, 0, f.path); info.set(f.path, f); at = prev + 1; }
          prev = at;
        }
      }
      const maps = props.setups.map((s) => new Map(s.fields.map((f) => [f.path, f])));
      return order.map((path) => {
        const f = info.get(path);
        const cells = maps.map((m) => m.get(path));
        const nums = cells.map((c) => (c && (c.kind === "int" || c.kind === "float") ? Number(c.value) : null));
        const raw = cells.map((c) => (c ? c.value : undefined));
        const differs = raw.some((v, i) => (nums[i] !== null && nums[0] !== null ? nums[i] !== nums[0] : v !== raw[0]));
        // Most common value, so with 3+ setups the odd ones out stand out.
        const counts = new Map();
        raw.forEach((v) => counts.set(String(v), (counts.get(String(v)) || 0) + 1));
        const common = [...counts.entries()].sort((x, y) => y[1] - x[1])[0][0];
        let delta = "";
        if (props.setups.length === 2 && differs && nums[0] !== null && nums[1] !== null) {
          delta = shownDelta(cells[0], cells[1]);
        }
        return { path, label: f.label, section: f.section, excluded: f.excluded, cells, raw, differs, common, delta };
      });
    });

    const visible = computed(() => rows.value.filter((r) => (!diffOnly.value || r.differs) && (showCalculated.value || !r.excluded)));
    const differing = computed(() => rows.value.filter((r) => r.differs && !r.excluded).length);
    const scored = computed(() => rows.value.filter((r) => !r.excluded).length);
    const sectionStart = (i) => i === 0 || visible.value[i - 1].section !== visible.value[i].section;

    return { diffOnly, showCalculated, visible, differing, scored, sectionStart, value, shown, hasLaps, lapCell };
  },
  template: `
    <div class="compare">
      <div class="compare-tools">
        <label class="switch"><input type="checkbox" v-model="diffOnly"><span></span>Only show differences</label>
        <label class="switch" title="Settings SetupMan leaves out when it decides how similar setups are, such as the car name, tyre set and values the game works out itself">
          <input type="checkbox" v-model="showCalculated"><span></span>Also show ignored settings
        </label>
        <span class="compare-count">{{ differing }} of {{ scored }} settings differ</span>
      </div>
      <div class="compare-scroll">
        <table class="compare-table">
          <thead>
            <tr>
              <th class="ct-label">Setting</th>
              <th v-for="s in setups" :key="s.id" class="ct-val" :title="s.relPath">{{ s.name }}</th>
              <th v-if="setups.length === 2" class="ct-delta">Change</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="hasLaps">
              <tr class="ct-section"><th :colspan="setups.length + 2">Lap times (track temperature)</th></tr>
              <tr class="ct-laps">
                <td class="ct-label">Best / average lap</td>
                <td v-for="(s, j) in setups" :key="'lap' + j" class="ct-val">
                  <template v-if="lapCell(laps[j]).none"><span class="ct-note">no laps recorded</span></template>
                  <template v-else>
                    <span :class="{ 'ct-top': lapCell(laps[j]).bestTop }">{{ lapCell(laps[j]).best }}</span> /
                    <span :class="{ 'ct-top': lapCell(laps[j]).avgTop }">{{ lapCell(laps[j]).avg }}</span>
                    <span class="ct-lapinfo">{{ lapCell(laps[j]).clean }} clean laps<template v-if="lapCell(laps[j]).temps">, {{ lapCell(laps[j]).temps }}</template></span>
                  </template>
                </td>
                <td v-if="setups.length === 2" class="ct-delta"></td>
              </tr>
            </template>
            <template v-for="(r, i) in visible" :key="r.path">
              <tr v-if="sectionStart(i)" class="ct-section"><th :colspan="setups.length + 2">{{ r.section }}</th></tr>
              <tr :class="{ 'ct-differs': r.differs, 'ct-excluded': r.excluded }">
                <td class="ct-label" :title="r.path">{{ r.label }}<span v-if="r.excluded" class="ct-note">ignored</span></td>
                <td v-for="(c, j) in r.cells" :key="j" class="ct-val"
                    :class="{ 'ct-odd': r.differs && (setups.length === 2 ? j === 1 : String(r.raw[j]) !== r.common), 'ct-missing': !c }"
                    :title="c && c.kind === 'float' ? c.value : ''">{{ c ? shown(c) : 'not in file' }}</td>
                <td v-if="setups.length === 2" class="ct-delta">{{ r.delta }}</td>
              </tr>
            </template>
            <tr v-if="!visible.length"><td :colspan="setups.length + 2" class="ct-none">These setups are identical in every setting that counts.</td></tr>
          </tbody>
        </table>
      </div>
    </div>`,
};
