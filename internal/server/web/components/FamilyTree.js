// Family tree of setups (spec D-43): each setup with the setups that were made from it.
import { shortDate } from "../format.js";

export const SessionBadge = {
  props: { session: String, small: Boolean },
  template: `<span v-if="session" class="session" :class="['ses-' + session.toLowerCase(), small ? 'session-sm' : '']" :title="{ Q: 'Qualifying', R: 'Race', Endurance: 'Endurance race' }[session]">{{ session === 'Endurance' ? 'Endu' : session }}</span>`,
};

export const FamilyNode = {
  name: "FamilyNode",
  components: { SessionBadge },
  props: { node: Object, current: Number, depth: { type: Number, default: 0 } },
  setup: () => ({ shortDate }),
  template: `
    <li class="ft-item" :class="{ 'ft-probable': node.linkKind === 'probable', 'ft-root': depth === 0 }">
      <router-link :to="'/setup/' + node.setupId" class="ft-node" :class="{ 'ft-current': node.setupId === current, 'ft-gone': !node.inFolder }"
        :title="node.inFolder ? '' : 'Hidden from the game or no longer in its folder (kept in SetupMan)'">
        <SessionBadge :session="node.session" small />
        <span class="ft-name">{{ node.name }}</span>
        <span class="ft-meta">{{ shortDate(node.date) }}<template v-if="node.versions > 1">, {{ node.versions }} versions</template></span>
        <span v-if="depth > 0" class="ft-link" :title="node.linkKind === 'probable' ? 'Guessed from file dates and similarity' : node.linkKind === 'manual' ? 'Linked by you' : 'Seen by SetupMan'">
          {{ node.linkKind === 'probable' ? 'probably' : '' }} from v{{ node.parentSeq }}<template v-if="node.score">, {{ Math.floor(node.score) }}% alike</template>
        </span>
      </router-link>
      <ul v-if="node.children && node.children.length" class="ft-children">
        <FamilyNode v-for="c in node.children" :key="c.setupId" :node="c" :current="current" :depth="depth + 1" />
      </ul>
    </li>`,
};

export const FamilyTree = {
  components: { FamilyNode },
  props: { root: Object, current: Number },
  template: `<ul class="ft"><FamilyNode :node="root" :current="current" /></ul>`,
};
