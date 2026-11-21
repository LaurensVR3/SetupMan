// Remove duplicates (spec D-106): every duplicate set with the setup to keep selected; the
// others go to the bin (30 days, D-102). Used on the track page (one track) and the game page.
import { ref, reactive, computed, watch } from "vue";
import { get, post, enc } from "../api.js";
import { showToast } from "../store.js";
import { dateTime, plural } from "../format.js";
import { SessionBadge } from "./FamilyTree.js";
import { ManageNotice } from "./ManageNotice.js";

export const DuplicatesDialog = {
  components: { SessionBadge, ManageNotice },
  props: { game: String, car: String, track: String, managed: Boolean, threshold: Number, short: { type: String, default: "the game" } },
  setup(props, { expose }) {
    const dlg = ref(null);
    const st = reactive({ near: false, sets: null, keep: {}, busy: false, switchedOn: false });
    // Removing files requires folder management (D-73); ManageNotice can enable it in place.
    const isManaged = computed(() => props.managed || st.switchedOn);
    async function load() {
      st.sets = null;
      const q = new URLSearchParams({ near: st.near ? "1" : "0" });
      if (props.car) { q.set("car", props.car); q.set("track", props.track); }
      const r = await get(`/api/games/${enc(props.game)}/duplicates?${q}`).catch(() => ({ sets: [] }));
      st.keep = Object.fromEntries(r.sets.map((s, i) => [i, s.keep]));
      st.sets = r.sets;
    }
    watch(() => st.near, () => dlg.value?.open && load());
    function open() { dlg.value.showModal(); load(); }
    expose({ open });
    const remove = computed(() => (st.sets || []).flatMap((s, i) => s.setups.filter((x) => x.id !== st.keep[i]).map((x) => x.id)));
    async function run() {
      st.busy = true;
      try {
        const res = (await post("/api/setups/delete", { ids: remove.value })).results;
        const ok = res.filter((r) => !r.error), bad = res.filter((r) => r.error);
        showToast(`${plural(ok.length, "duplicate")} removed (in the bin for 30 days).` + (bad.length ? ` ${bad.length} not removed: ${bad[0].error}.` : ""), 9000);
        dlg.value.close();
      } catch (e) { showToast(e.message, 7000); } finally { st.busy = false; }
    }
    return { dlg, st, remove, run, dateTime, plural, props, isManaged };
  },
  template: `
    <dialog ref="dlg" class="confirm dup-dialog">
      <h2>Remove duplicates</h2>
      <p>Per set, select the setup to keep; the others are removed from {{ short }} and from SetupMan's lists. They stay in the bin for 30 days (Settings → Bin).</p>
      <div class="segmented" role="radiogroup" aria-label="Which duplicates">
        <button type="button" role="radio" :aria-checked="!st.near" :class="{ on: !st.near }" @click="st.near = false">Identical only</button>
        <button type="button" role="radio" :aria-checked="st.near" :class="{ on: st.near }" @click="st.near = true">Also near-duplicates (≥ {{ threshold }}% alike)</button>
      </div>
      <ManageNotice v-if="!isManaged" :game="game" :short="short" action="removes files" @on="st.switchedOn = true" />
      <p v-if="!st.sets" class="panel-hint">Searching…</p>
      <p v-else-if="!st.sets.length" class="panel-hint">No {{ st.near ? 'duplicates or near-duplicates' : 'identical duplicates' }} found.</p>
      <div v-else class="dup-sets">
        <fieldset v-for="(s, i) in st.sets" :key="i" class="dup-set">
          <legend>
            <template v-if="!car">{{ s.carName }}, {{ s.trackName }}: </template>
            {{ s.identical ? 'identical' : Math.floor(s.minScore) + '% or more alike' }}
          </legend>
          <label v-for="x in s.setups" :key="x.id" class="dup-row" :class="{ keep: st.keep[i] === x.id }">
            <input type="radio" :name="'keep' + i" :value="x.id" v-model="st.keep[i]">
            <span class="dup-name">{{ x.name }} <SessionBadge :session="x.session" small /></span>
            <span class="dup-date">{{ dateTime(x.modified) }}</span>
            <span class="dup-tag">{{ st.keep[i] === x.id ? 'Keep' : 'Remove' }}</span>
          </label>
        </fieldset>
      </div>
      <div class="picker-actions">
        <button type="button" class="btn" @click="dlg.close()">Cancel</button>
        <button type="button" class="btn btn-danger-solid" :disabled="st.busy || !remove.length || !isManaged" @click="run">{{ st.busy ? 'Removing ' + plural(remove.length, 'duplicate') + '…' : 'Remove ' + plural(remove.length, 'duplicate') }}</button>
      </div>
    </dialog>`,
};
