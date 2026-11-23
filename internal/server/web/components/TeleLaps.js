// Set telemetry laps to 99 in the setups in the game folder (D-176): every setup that has fewer
// is listed first (grouped by car and track, each one can be unticked), a backup of the folder
// is made, and nothing changes until you confirm.
import { ref, reactive, computed } from "vue";
import { get, post, enc } from "../api.js";
import { showToast } from "../store.js";
import { plural } from "../format.js";

export const TeleLapsDialog = {
  props: { game: String, short: String },
  setup(props, { expose }) {
    const dlg = ref(null);
    const st = reactive({ list: null, skip: {}, busy: false, error: "" });
    async function load() {
      st.list = null; st.skip = {}; st.error = "";
      try { st.list = (await get(`/api/games/${enc(props.game)}/telemetry-laps`)).setups; }
      catch (e) { st.list = []; st.error = e.message; }
    }
    function open() { dlg.value.showModal(); load(); }
    expose({ open });
    const groups = computed(() => {
      const out = [];
      for (const r of st.list || []) {
        const key = r.carName + ", " + r.trackName;
        if (!out.length || out[out.length - 1].key !== key) out.push({ key, items: [] });
        out[out.length - 1].items.push(r);
      }
      return out;
    });
    const chosen = computed(() => (st.list || []).filter((r) => !st.skip[r.id]).map((r) => r.id));
    function toggleAll(on) { for (const r of st.list || []) st.skip[r.id] = !on; }
    async function run() {
      st.busy = true;
      try {
        const res = (await post(`/api/games/${enc(props.game)}/telemetry-laps`, { ids: chosen.value })).results;
        const bad = res.filter((r) => r.error);
        showToast(`Telemetry laps set to 99 in ${plural(res.length - bad.length, "setup")}. A backup of the folder was made first.`
          + (bad.length ? ` ${bad.length} could not be changed: ${bad[0].name}: ${bad[0].error}` : ""), 9000);
        dlg.value.close();
      } catch (e) { showToast(e.message, 8000); } finally { st.busy = false; }
    }
    return { dlg, st, groups, chosen, toggleAll, run, plural };
  },
  template: `
    <dialog ref="dlg" class="confirm dup-dialog">
      <h2>Set telemetry laps to 99</h2>
      <p>{{ short }} keeps telemetry for as many laps as the setup's Telemetry laps value (Electronics), up to 99. These setups in the {{ short }} folder have fewer. SetupMan makes a backup of the folder first, then changes only this value in each file. The setups get no new version.</p>
      <p v-if="!st.list" class="panel-hint">Reading the setups…</p>
      <p v-else-if="st.error" class="field-error">{{ st.error }}</p>
      <p v-else-if="!st.list.length" class="panel-hint">Every setup in the {{ short }} folder already has 99 telemetry laps.</p>
      <template v-else>
        <div class="tidy-tools">
          <span>{{ chosen.length }} of {{ plural(st.list.length, 'setup') }} selected</span>
          <button type="button" class="btn-link" @click="toggleAll(true)">Select all</button>
          <button type="button" class="btn-link" @click="toggleAll(false)">Select none</button>
        </div>
        <div class="dup-sets">
          <fieldset v-for="g in groups" :key="g.key" class="dup-set">
            <legend>{{ g.key }}</legend>
            <label v-for="r in g.items" :key="r.id" class="tidy-row" :class="{ off: st.skip[r.id] }">
              <input type="checkbox" :checked="!st.skip[r.id]" @change="st.skip[r.id] = !$event.target.checked">
              <span class="tidy-from">{{ r.name }}</span>
              <span class="tidy-arrow" aria-label="from">{{ r.laps }}</span>
              <span class="tidy-to">→ 99 laps</span>
            </label>
          </fieldset>
        </div>
      </template>
      <div class="picker-actions">
        <button type="button" class="btn" @click="dlg.close()">Cancel</button>
        <button type="button" class="btn btn-primary" :disabled="st.busy || !chosen.length" @click="run">{{ st.busy ? 'Changing ' + plural(chosen.length, 'setup') + '…' : 'Set 99 laps in ' + plural(chosen.length, 'setup') }}</button>
      </div>
    </dialog>`,
};
