// Manual setup select (spec D-149): say which setup you are driving in the session being recorded,
// in as few clicks as possible. ACC doesn't tell which setup is loaded, so when SetupMan can't tell
// either, the setups that fit equally well are shown as buttons (one click). Up to four setups for
// this car and track are always shown as buttons; more go in a list (two clicks). Shown under the
// top bar for as long as a session is recorded (D-177): a known setup can be changed with Change….
// A choice applies to the current stint (on track) or from now on (in the pits); earlier stints are
// changed on the session page.
import { ref, computed } from "vue";
import { put } from "../api.js";
import { showToast } from "../store.js";

const MAX_BUTTONS = 4;

export const SetupSelect = {
  props: { rec: { type: Object, required: true }, compact: Boolean },
  setup(props) {
    const busy = ref(false);
    const unknown = computed(() => !props.rec.setupId);
    const choices = computed(() => props.rec.choices || []);
    // Buttons: the equally likely setups, or every setup when there are only a few.
    const buttons = computed(() => {
      if (!unknown.value) return [];
      const likely = props.rec.likely || [];
      if (likely.length) return likely.slice(0, MAX_BUTTONS);
      return choices.value.length <= MAX_BUTTONS ? choices.value : [];
    });
    const rest = computed(() => {
      const shown = new Set(buttons.value.map((b) => b.id));
      return choices.value.filter((c) => !shown.has(c.id) && c.id !== props.rec.setupId);
    });
    async function pick(id) {
      if (!id || busy.value) return;
      busy.value = true;
      try {
        await put("/api/recording/setup", { setupId: Number(id) });
      } catch (e) {
        showToast(e.message, 6000);
      } finally {
        busy.value = false;
      }
    }
    return { busy, unknown, buttons, rest, pick };
  },
  template: `
    <div v-if="rec.active && rec.sessionId && (rec.choices || []).length" class="setup-select" :class="{ 'setup-select-unknown': unknown, 'setup-select-compact': compact }" role="group" aria-label="Manual setup select">
      <span class="ss-label">{{ unknown ? 'Manual setup select' : 'Driving' }}</span>
      <b v-if="!unknown" class="ss-current">{{ rec.setupName }}</b>
      <button v-for="b in buttons" :key="b.id" type="button" class="btn btn-sm ss-btn" :disabled="busy" @click="pick(b.id)">{{ b.name }}</button>
      <select v-if="rest.length" class="ss-list" :disabled="busy" :aria-label="unknown ? 'Other setup' : 'Change setup'" @change="pick($event.target.value); $event.target.value = ''">
        <option value="">{{ unknown ? (buttons.length ? 'Other setup…' : 'Select the setup…') : 'Change setup…' }}</option>
        <option v-for="c in rest" :key="c.id" :value="c.id">{{ c.name }}</option>
      </select>
    </div>`,
};
