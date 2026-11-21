// Apply the naming standard to a whole game: every proposed new name is listed first (grouped
// by car and track, each one can be unticked), and nothing is renamed until you confirm.
import { ref, reactive, computed } from "vue";
import { get, post, enc } from "../api.js";
import { showToast } from "../store.js";
import { plural } from "../format.js";
import { ManageNotice } from "./ManageNotice.js";

export const TidyAllDialog = {
  components: { ManageNotice },
  props: { game: String, short: String, managed: Boolean },
  setup(props, { expose }) {
    const dlg = ref(null);
    const st = reactive({ list: null, skip: {}, busy: false, switchedOn: false, error: "" });
    const isManaged = computed(() => props.managed || st.switchedOn);
    async function load() {
      st.list = null; st.skip = {}; st.error = "";
      try { st.list = (await get(`/api/games/${enc(props.game)}/tidy-preview`)).renames; }
      catch (e) { st.list = []; st.error = e.message; }
    }
    function open() { dlg.value.showModal(); load(); }
    expose({ open });
    // Grouped by car and track for reading; the list arrives sorted that way.
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
        const res = (await post("/api/setups/rename-tidy", { ids: chosen.value })).results;
        const bad = res.filter((r) => r.error);
        showToast(`${plural(res.length - bad.length, "setup")} renamed.` + (bad.length ? ` ${bad.length} could not be renamed: ${bad[0].error}` : ""), 9000);
        dlg.value.close();
      } catch (e) { showToast(e.message, 7000); } finally { st.busy = false; }
    }
    return { dlg, st, isManaged, groups, chosen, toggleAll, run, plural };
  },
  template: `
    <dialog ref="dlg" class="confirm dup-dialog">
      <h2>Apply naming standard</h2>
      <p>Proposed names follow the naming standard: initials, session, temperature, game version, codes, then the remainder of the current name. Review the list and deselect any setup that should keep its current name.</p>
      <ManageNotice v-if="!isManaged" :game="game" :short="short" action="renames files" @on="st.switchedOn = true" />
      <p v-if="!st.list" class="panel-hint">Calculating names…</p>
      <p v-else-if="st.error" class="field-error">{{ st.error }}</p>
      <p v-else-if="!st.list.length" class="panel-hint">All setups already follow the naming standard.</p>
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
              <span class="tidy-from">{{ r.from }}</span>
              <span class="tidy-arrow" aria-label="becomes">→</span>
              <span class="tidy-to">{{ r.to }}</span>
            </label>
          </fieldset>
        </div>
      </template>
      <div class="picker-actions">
        <button type="button" class="btn" @click="dlg.close()">Cancel</button>
        <button type="button" class="btn btn-primary" :disabled="st.busy || !chosen.length || !isManaged" @click="run">{{ st.busy ? 'Renaming ' + plural(chosen.length, 'setup') + '…' : 'Rename ' + plural(chosen.length, 'setup') }}</button>
      </div>
    </dialog>`,
};
