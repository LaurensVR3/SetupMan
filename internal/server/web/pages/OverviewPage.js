// Overview: one tile per supported, installed (or forced visible) game (spec D-26, D-28, D-31).
import { ref, reactive, computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { showToast } from "../store.js";
import { useDrop, readFiles } from "../drop.js";
import { get, post } from "../api.js";
import { ago, eventText } from "../format.js";
import { usePageData } from "./load.js";
import { Stat, DupBadge, EmptyState, Loading, setCrumbs } from "../components/common.js";
import { LastOpenedNotice } from "../components/LastOpenedNotice.js";

export const OverviewPage = {
  components: { Stat, DupBadge, EmptyState, Loading, LastOpenedNotice },
  setup() {
    setCrumbs([]);
    const { data, error } = usePageData(async () => {
      const [games, changes] = await Promise.all([get("/api/games"), get("/api/changes")]);
      return { ...games, changes };
    });
    const games = computed(() => (data.value ? data.value.games.filter((g) => g.visible) : []));
    const hidden = computed(() => (data.value ? data.value.games.filter((g) => !g.visible).length : 0));
    const unseen = computed(() => (data.value ? data.value.changes.events.filter((e) => !e.seen) : []));
    const markSeen = () => post("/api/changes/seen");
    const noLogo = reactive({}); // game logo not available (offline, or downloads off): show the short name
    // The panel takes the colour of the logo's own background (its top-left pixel), so the logo
    // fills the panel without a visible box (ACC's logo is on black, AC's on dark grey).
    const artBg = reactive({});
    function logoLoaded(id, e) {
      try {
        const c = document.createElement("canvas");
        c.width = c.height = 1;
        const ctx = c.getContext("2d");
        ctx.drawImage(e.target, 0, 0, 1, 1, 0, 0, 1, 1);
        const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
        if (a > 200) artBg[id] = `rgb(${r} ${g} ${b})`;
      } catch { /* keep the default */ }
    }
    const shown = ref(8);
    const recent = computed(() => (data.value ? data.value.changes.events.slice(0, shown.value) : []));
    // The guide is a per-browser convenience; if storage is unavailable it simply shows again.
    // The welcome pop-up shows once per browser (a convenience; if storage is blocked it shows again).
    const welcomeDlg = ref(null);
    let seen = false;
    try { seen = localStorage.getItem("setupman.guideHidden") === "1"; } catch {}
    onMounted(() => { if (!seen) welcomeDlg.value?.showModal(); });
    function hideGuide() {
      try { localStorage.setItem("setupman.guideHidden", "1"); } catch {}
    }
    // Open setups someone shared with you (.setupman files, D-94).
    const router = useRouter();
    const opening = ref(false);
    const shared = ref(null);
    async function openShared(ev) {
      const list = [...ev.target.files];
      ev.target.value = "";
      await openList(list);
    }
    useDrop("Drop shared setups (.setupman) to add them to your library", openList);
    async function openList(all) {
      const list = all.filter((f) => f.name.toLowerCase().endsWith(".setupman"));
      if (list.length < all.length) showToast("Setup files (.json or .zip) don't say which track they are for: open the car's track page and drop them there.", 9000);
      if (!list.length || opening.value) return;
      opening.value = true;
      try {
        const files = await readFiles(list);
        const res = await post("/api/shared/import", { files });
        const one = res.items.length === 1 ? res.items[0] : null;
        if (one && one.setupId) router.push("/setup/" + one.setupId); // imported, or already there
        if (one) showToast(one.result === "imported" ? `${one.name} added to your library.` : `${one.name}: ${one.reason}.`, 7000);
        else shared.value = res;
      } catch (e) { showToast(e.message, 7000); } finally { opening.value = false; }
    }
    const lapGames = computed(() => games.value.filter((g) => g.byFile).map((g) => g.short).join(" and "));
    return { lapGames, data, error, games, hidden, unseen, markSeen, noLogo, artBg, logoLoaded, shown, recent, ago, eventText, welcomeDlg, hideGuide, opening, shared, openShared };
  },
  template: `
    <main class="page">
      <div class="page-head-row">
        <h1 class="page-title">Games</h1>
        <label class="btn btn-file">{{ opening ? 'Opening…' : 'Open a shared setup…' }}<input type="file" accept=".setupman" multiple :disabled="opening" @change="openShared"></label>
      </div>
      <div v-if="shared" class="import-result" role="status">
        <p><b>{{ shared.imported }} shared setup{{ shared.imported === 1 ? '' : 's' }} added to your library</b><template v-if="shared.skipped">, {{ shared.skipped }} skipped</template>.</p>
        <ul><li v-for="(it, k) in shared.items" :key="k"><router-link v-if="it.setupId" :to="'/setup/' + it.setupId">{{ it.name }}</router-link><template v-else>{{ it.name }}</template><template v-if="it.reason">: {{ it.reason }}</template></li></ul>
        <button type="button" class="btn btn-quiet btn-sm" @click="shared = null">Close</button>
      </div>
      <dialog ref="welcomeDlg" class="confirm welcome" @close="hideGuide">
        <h2>Welcome to SetupMan</h2>
        <p>SetupMan keeps your sim racing setups organised. What it does:</p>
        <ul class="features">
          <li><b>Every setup, every version.</b> Each change is kept, with what changed and which setup it was made from.</li>
          <li><b>Compare</b> setups side by side, and find and remove duplicates.</li>
          <li><b>Change values</b> with − and +, and save as a new version or a new setup.</li>
          <li><b>Lap times per setup</b>, recorded while you drive, with fuel use and track temperature.</li>
          <li><b>Race prep:</b> which of your setups to use for a session and temperature, with its fuel plan.</li>
          <li><b>Fuel calculator</b> for races and qualifying.</li>
          <li><b>Control the game's setup list:</b> with folder management enabled, select which setups the game lists; the rest remain stored in SetupMan.</li>
          <li><b>Naming standard, sharing and backups.</b></li>
        </ul>
        <p>Start by clicking a game below. The <router-link to="/manual" @click="welcomeDlg.close()">Manual</router-link> in the top bar explains everything.</p>
        <div class="picker-actions"><button type="button" class="btn btn-primary" @click="welcomeDlg.close()">Get started</button></div>
      </dialog>
      <!-- the notice stands apart from the Loading / EmptyState / games chain below, so it never hides the games -->
      <LastOpenedNotice v-if="data && lapGames" :games="lapGames" class="overview-notice" />
      <Loading v-if="!data" :error="error" />
      <EmptyState v-else-if="!games.length" title="No supported games found">
        <p>SetupMan didn't find a supported game on this PC. If your setups are stored elsewhere, set the setups folder in Settings.</p>
        <router-link to="/settings?tab=games" class="btn btn-primary">Open settings</router-link>
      </EmptyState>
      <div v-else class="overview-layout">
      <div class="game-grid">
        <component :is="g.status === 'ok' ? 'router-link' : 'div'" v-for="g in games" :key="g.id"
          :to="g.status === 'ok' ? '/' + g.id : undefined" class="game-tile" :class="'game-' + g.id">
          <div class="game-art" :class="{ 'has-logo': !noLogo[g.id] }" :style="artBg[g.id] ? { background: artBg[g.id] } : null" aria-hidden="true">
            <img v-if="!noLogo[g.id]" :src="'/api/logos/game:' + g.id" alt="" @load="logoLoaded(g.id, $event)" @error="noLogo[g.id] = true">
            <span v-else>{{ g.short }}</span>
          </div>
          <div class="game-body">
            <h2 class="game-name">{{ g.name }}</h2>
            <template v-if="g.status === 'ok'">
              <div class="stats">
                <Stat :value="g.cars" label="cars" />
                <Stat :value="g.tracks" label="tracks" />
                <Stat :value="g.setups" label="setups" />
                <Stat :value="g.duplicates" label="duplicate sets" :tone="g.duplicates ? 'dup' : ''" />
              </div>
              <p class="game-meta">Scanned {{ ago(g.scannedAt) }} from {{ g.folder }}<template v-if="g.managed">. Folder management enabled</template></p>
              <p v-if="g.freshCount" class="tile-note">{{ g.freshCount === 1 ? '1 new setup' : g.freshCount + ' new setups' }} saved in {{ g.short }}. Review them on the track pages.</p>
              <p v-if="g.queued" class="tile-note">{{ g.queued === 1 ? '1 queued change' : g.queued + ' queued changes' }} will be applied when you stop driving.</p>
            </template>
            <p v-else-if="g.status === 'scanning'" class="game-meta">Reading setups…</p>
            <div v-else class="notice" :class="g.status === 'error' ? 'notice-error' : ''">
              {{ g.message }}
              <router-link :to="'/settings?tab=games&game=' + g.id">Open settings</router-link>
            </div>
          </div>
        </component>
      </div>
      <section class="panel changes" aria-labelledby="changes-h">
        <div class="changes-head">
          <h2 id="changes-h" class="panel-title">Recent changes</h2>
          <button v-if="unseen.length" type="button" class="btn btn-sm" @click="markSeen">Mark as read</button>
        </div>
        <p v-if="!recent.length" class="panel-hint">No recent changes.</p>
        <ul>
          <li v-for="e in recent" :key="e.id" :class="{ unseen: !e.seen }">
            <component :is="e.setupId ? 'router-link' : 'span'" :to="e.setupId ? '/setup/' + e.setupId : undefined" class="ch-text">{{ eventText(e) }}</component>
            <span class="ch-when">{{ ago(e.at) }}</span>
          </li>
        </ul>
        <button v-if="data.changes.events.length > shown" type="button" class="btn btn-quiet btn-sm" @click="shown += 15">Show more</button>
      </section>
      </div>

      <p v-if="data && hidden" class="footnote">{{ hidden }} supported {{ hidden === 1 ? 'game is' : 'games are' }} hidden. To show {{ hidden === 1 ? 'it' : 'them' }}, open <router-link to="/settings?tab=games">Settings</router-link>.</p>
    </main>`,
};
