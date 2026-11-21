// Setup recognition needs Windows to record when a file was last opened (spec D-147). When that is
// disabled on this PC, explain how to enable it. Shown on the Games overview and in Settings → Games
// for games whose laps SetupMan records. Shows nothing when it is enabled or unknown.
import { ref, computed } from "vue";
import { store } from "../store.js";

export const LastOpenedNotice = {
  props: { games: { type: String, default: "" } }, // e.g. "ACC and AC", for the text
  setup() {
    const st = computed(() => store.app?.lastOpened);
    const show = computed(() => st.value && st.value.known && !st.value.enabled);
    const copied = ref(false);
    const canCopy = !!(navigator.clipboard && window.isSecureContext);
    async function copy() {
      try {
        await navigator.clipboard.writeText(st.value.command);
        copied.value = true;
        setTimeout(() => (copied.value = false), 2500);
      } catch { /* the command can still be selected by hand */ }
    }
    return { st, show, copied, canCopy, copy };
  },
  template: `
    <div v-if="show" class="notice notice-error lastopened-notice" role="note">
      <p><b>Setup recognition is limited: Windows does not record when files are opened.</b>
        SetupMan recognises the setup you drive{{ games ? ' in ' + games : '' }} from the moment the game opened its file. On this PC Windows does not record that moment, so SetupMan can only compare the car's values (fuel, TC, ABS, engine map) with your setups.</p>
      <p>To enable it:</p>
      <ol>
        <li>Open the Start menu, type <b>cmd</b>, right-click <b>Command Prompt</b> and choose <b>Run as administrator</b>.</li>
        <li>Enter this command:
          <span class="cmd-row"><code class="cmd">{{ st.command }}</code><button v-if="canCopy" type="button" class="btn btn-sm" @click="copy">{{ copied ? 'Copied' : 'Copy' }}</button></span></li>
        <li>Reload this page. Only if the command says a restart is needed (older Windows versions), restart Windows first.</li>
      </ol>
      <p class="muted-small">This setting is part of Windows and applies to all files. It only records when a file was last read; the content of files is never changed.</p>
    </div>`,
};
