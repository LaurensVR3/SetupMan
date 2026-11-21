// usePageData loads a page's data and reloads it when the page's address changes or when the
// server reports a change (store.version). A change of tab (?tab=) doesn't reload: the data is the
// same. Pages that show lap times pass { laps: true } to reload when a lap is recorded (D-138).
import { ref, watch } from "vue";
import { useRoute } from "vue-router";
import { store } from "../store.js";

export function usePageData(fetcher, opts = {}) {
  const route = useRoute();
  const data = ref(null);
  const error = ref("");
  async function load() {
    try {
      data.value = await fetcher(route.params);
      error.value = "";
    } catch (e) {
      error.value = e.message;
    }
  }
  watch([() => route.path, () => store.version, () => (opts.laps ? store.lapVersion : 0)], load, { immediate: true });
  return { data, error, reload: load };
}
