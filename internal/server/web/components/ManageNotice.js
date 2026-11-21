// Shown where an action needs SetupMan to manage the game folder (D-73): explains why, and
// enables it in place (the same action as in Settings, with the backup and check first, D-74).
import { reactive } from "vue";
import { post, enc } from "../api.js";
import { showToast } from "../store.js";
import { plural } from "../format.js";

export const ManageNotice = {
  props: { game: String, short: String, action: { type: String, default: "change files" } },
  emits: ["on"],
  setup(props, { emit }) {
    const st = reactive({ busy: false, problems: null, error: "" });
    async function enable() {
      st.busy = true; st.problems = null; st.error = "";
      try {
        const r = await post(`/api/games/${enc(props.game)}/manage`, { on: true });
        if (r.managed) {
          showToast(`Folder management enabled. Backup created and ${plural(r.checked, "setup")} verified.`, 6000);
          emit("on");
        } else st.problems = r.problems || [];
      } catch (e) { st.error = e.message; } finally { st.busy = false; }
    }
    return { st, enable };
  },
  template: `
    <div class="notice notice-error manage-notice">
      <p><b>Folder management is disabled for {{ short }}.</b> SetupMan {{ action }} in the {{ short }} setups folder only when folder management is enabled. Enabling it first creates a full backup of the folder, which can be restored at any time (Settings → Backups).</p>
      <button type="button" class="btn btn-sm" :disabled="st.busy" @click="enable">{{ st.busy ? 'Creating backup…' : 'Enable folder management' }}</button>
      <template v-if="st.problems">
        <p>Folder management was not enabled: some setups differ from the copy stored in SetupMan.</p>
        <ul><li v-for="x in st.problems.slice(0, 5)" :key="x">{{ x }}</li></ul>
      </template>
      <p v-if="st.error" class="field-error">{{ st.error }}</p>
    </div>`,
};
