// In-page folder browser (a browser can't hand out real folder paths, so the server lists folders).
import { ref, watch, nextTick } from "vue";
import { get } from "../api.js";

export const FolderPicker = {
  props: { open: Boolean, start: String, title: { type: String, default: "Choose a folder" } },
  emits: ["pick", "close"],
  setup(props, { emit }) {
    const dlg = ref(null);
    const path = ref("");
    const parent = ref(null);
    const dirs = ref([]);
    const shortcuts = ref([]);
    const error = ref("");

    async function browse(p) {
      try {
        const res = await get("/api/browse?path=" + encodeURIComponent(p || ""));
        error.value = "";
        path.value = res.path;
        parent.value = res.parent ?? null;
        dirs.value = res.dirs;
        shortcuts.value = res.shortcuts;
      } catch (e) {
        error.value = e.message;
        if (p) browse("");
      }
    }

    watch(() => props.open, async (o) => {
      await nextTick();
      if (o) { dlg.value.showModal(); browse(props.start); }
      else if (dlg.value?.open) dlg.value.close();
    });

    return { dlg, path, parent, dirs, shortcuts, error, browse, emit };
  },
  template: `
    <dialog ref="dlg" class="picker" @close="emit('close')" @cancel.prevent="emit('close')">
      <h2>{{ title }}</h2>
      <div class="picker-shortcuts">
        <button v-for="s in shortcuts" :key="s.path" type="button" class="chip" @click="browse(s.path)">{{ s.name }}</button>
      </div>
      <div class="picker-path">{{ path || 'This PC' }}</div>
      <ul class="picker-list">
        <li v-if="parent !== null"><button type="button" class="picker-up" @click="browse(parent)">Up one level</button></li>
        <li v-for="d in dirs" :key="d.path"><button type="button" @click="browse(d.path)">{{ d.name }}</button></li>
        <li v-if="!dirs.length" class="picker-none">No folders here</li>
      </ul>
      <p v-if="error" class="field-error">{{ error }}</p>
      <div class="picker-actions">
        <button type="button" class="btn" @click="emit('close')">Cancel</button>
        <button type="button" class="btn btn-primary" :disabled="!path" @click="emit('pick', path)">Use this folder</button>
      </div>
    </dialog>`,
};
