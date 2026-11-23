// Telemetry of a setup or a recorded session (spec D-159), as links to the Telemetry pages.
import { ref, watch } from "vue";
import { get, enc } from "../api.js";
import { store } from "../store.js";
import { lapTime } from "./LapTimes.js";
import { dateTime, plural } from "../format.js";

export const TeleLinks = {
  props: { url: String, title: { type: String, default: "Telemetry" } },
  setup(props) {
    const files = ref([]);
    const load = () => get(props.url).then((f) => (files.value = f.filter((x) => x.laps > 0))).catch(() => (files.value = []));
    watch(() => [props.url, store.version], load, { immediate: true });
    const to = (f) => `/telemetry/${f.game}/${enc(f.track)}?car=${enc(f.car)}&open=${f.id}`;
    return { files, to, lapTime, dateTime, plural };
  },
  template: `
    <section v-if="files.length" class="tele-links">
      <h3 class="lap-sub">{{ title }}</h3>
      <ul>
        <li v-for="f in files.slice(0, 8)" :key="f.id">
          <router-link :to="to(f)">{{ dateTime(f.startedAt) }}</router-link>
          <span>{{ plural(f.laps, 'full lap') }}</span>
          <b>{{ lapTime(f.bestMs) }}</b>
          <span v-if="f.hasExtra" class="tag tag-ok" title="Extra telemetry log: car position, fuel, lap validity and more">Extra log</span>
        </li>
      </ul>
      <p v-if="files.length > 8" class="muted-small">And {{ files.length - 8 }} more on the <router-link :to="to(files[0])">telemetry page</router-link>.</p>
    </section>`,
};
