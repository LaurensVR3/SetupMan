// Small shared components.
import { ref, computed, watch } from "vue";
import { store } from "../store.js";
import { get } from "../api.js";
import { SetupSelect } from "./SetupSelect.js";

export const TopBar = {
  components: { SetupSelect },
  setup() {
    const live = computed(() => store.live);
    // What is being recorded (laps per setup, D-44); refreshed whenever something changes.
    const rec = ref({ active: false });
    const loadRec = () => get("/api/recording").then((r) => (rec.value = r)).catch(() => {});
    watch(() => [store.version, store.lapVersion, store.live], loadRec, { immediate: true });
    return { store, live, rec };
  },
  template: `
    <header class="topbar">
      <div class="topbar-inner">
        <router-link to="/" class="wordmark" aria-label="SetupMan home">Setup<b>Man</b></router-link>
        <nav class="crumbs" aria-label="You are here">
          <template v-for="(c, i) in store.crumbs" :key="i">
            <span class="crumb-sep" aria-hidden="true">/</span>
            <router-link v-if="c.to && i < store.crumbs.length - 1" :to="c.to">{{ c.label }}</router-link>
            <span v-else aria-current="page">{{ c.label }}</span>
          </template>
        </nav>
        <router-link v-if="rec.active" :to="rec.setupId ? '/setup/' + rec.setupId : '/'" class="live live-rec" :title="rec.setupName ? 'Lap times are recorded for ' + rec.setupName : 'Lap times are recorded; SetupMan is not sure which setup you are using'">
          <i></i><span class="live-text">Recording laps<template v-if="rec.setupName">: {{ rec.setupName }}</template><template v-else> (setup unknown)</template></span>
        </router-link>
        <span v-if="live === 'driving'" class="live live-driving" :title="'You are driving in ' + (store.liveGame || 'the game') + '. SetupMan waits with changes until you stop.'">
          <i></i><span class="live-text">Driving in {{ store.liveGame || 'the game' }}</span>
        </span>
        <span v-else-if="live === 'not-driving'" class="live live-ready" :title="(store.liveGame || 'The game') + ' is running; you are not driving'">
          <i></i><span class="live-text">{{ store.liveGame || 'Game' }} running</span>
        </span>
        <nav class="mainnav" aria-label="SetupMan">
          <router-link to="/prep" class="nav-link" :class="{ active: $route.path === '/prep' }" title="Race prep">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 22V3h2v1h12l-2.5 4.5L19 13H7v9H5Zm2-11h8.6l-1.4-2.5L15.6 6H7v5Z"/></svg>
            Race prep
          </router-link>
          <router-link to="/fuel" class="nav-link" :class="{ active: $route.path === '/fuel' }">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 21V5a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v16H5Zm2-11h6V5H7v5Zm10 11v-8h1a1 1 0 0 1 1 1v3.5a1.5 1.5 0 0 0 3 0V9l-2.5-2.5L18 7l1.5 1.5V17.5a.5.5 0 0 1-1 0V14a3 3 0 0 0-3-3H15"/></svg>
            Fuel calculator
          </router-link>
          <router-link to="/pit" class="nav-link" :class="{ active: $route.path === '/pit' }">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path fill-rule="evenodd" d="M12 2.5a9.5 9.5 0 1 1 0 19 9.5 9.5 0 0 1 0-19Zm0 4.5a5 5 0 1 0 0 10 5 5 0 0 0 0-10Zm0 3a2 2 0 1 1 0 4 2 2 0 0 1 0-4Z"/></svg>
            Pit stop
          </router-link>
          <router-link to="/telemetry" class="nav-link" :class="{ active: $route.path.startsWith('/telemetry') }">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 20V4h2v14h16v2H3Zm4-4.6 3.6-5.8 3.3 3.1L18.3 6l1.7 1-5.7 9.1-3.3-3.1-3.3 5.4L7 15.4Z"/></svg>
            Telemetry
          </router-link>
          <router-link to="/manual" class="nav-link" :class="{ active: $route.path === '/manual' }">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 4.5A2.5 2.5 0 0 1 6.5 2H20v17H6.5a.5.5 0 0 0 0 1H20v2H6.5A2.5 2.5 0 0 1 4 19.5v-15ZM8 6v2h8V6H8Zm0 4v2h6v-2H8Z"/></svg>
            Manual
          </router-link>
          <router-link to="/settings" class="nav-link" :class="{ active: $route.path === '/settings' }">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 15.5a3.5 3.5 0 1 1 0-7 3.5 3.5 0 0 1 0 7Zm7.4-2.5a7.6 7.6 0 0 0 0-2l2-1.6-2-3.4-2.4 1a7.6 7.6 0 0 0-1.7-1L15 3.5h-4l-.4 2.5a7.6 7.6 0 0 0-1.7 1l-2.4-1-2 3.4L6.6 11a7.6 7.6 0 0 0 0 2l-2 1.6 2 3.4 2.4-1a7.6 7.6 0 0 0 1.7 1l.4 2.5h4l.4-2.5a7.6 7.6 0 0 0 1.7-1l2.4 1 2-3.4-2-1.6Z"/></svg>
            Settings
          </router-link>
        </nav>
      </div>
      <!-- manual setup select (D-149) on every page while SetupMan doesn't know the setup -->
      <div v-if="rec.active && !rec.setupId && (rec.choices || []).length" class="ss-strip">
        <div class="topbar-inner"><SetupSelect :rec="rec" compact /></div>
      </div>
      <div v-if="store.offline" class="offline" role="alert">SetupMan isn't running. Start SetupMan.exe again; this page reloads by itself.</div>
    </header>`,
};

// Brand logo from the local cache (downloaded from Wikimedia Commons), or a monogram.
export const BrandLogo = {
  props: { brand: String, size: { type: String, default: "md" } },
  setup(props) {
    const failed = ref(false);
    const initials = computed(() =>
      (props.brand || "?").split(/[\s-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join("").toUpperCase());
    return { failed, initials };
  },
  template: `
    <span class="logo" :class="'logo-' + size">
      <img v-if="!failed" :src="'/api/logos/' + encodeURIComponent(brand)" :alt="brand + ' logo'" @error="failed = true" loading="lazy">
      <span v-else class="monogram" aria-hidden="true">{{ initials }}</span>
    </span>`,
};

export const Stat = {
  props: { value: [Number, String], label: String, tone: String },
  template: `<div class="stat" :class="tone ? 'stat-' + tone : ''"><span class="stat-value">{{ value }}</span><span class="stat-label">{{ label }}</span></div>`,
};

export const DupBadge = {
  props: { count: Number },
  template: `<span v-if="count" class="dup-badge" :title="'Sets of identical or near-identical setups'">{{ count === 1 ? '1 duplicate set' : count + ' duplicate sets' }}</span>`,
};

export const EmptyState = {
  props: { title: String },
  template: `<section class="empty"><h2>{{ title }}</h2><div class="empty-body"><slot></slot></div></section>`,
};

export const Loading = {
  props: { error: String, back: { type: String, default: "/" }, backLabel: { type: String, default: "Go to the start page" } },
  template: `
    <div v-if="error" class="notice notice-error" role="alert">{{ error }} <router-link :to="back">{{ backLabel }}</router-link></div>
    <div v-else class="loading" aria-live="polite">Loading…</div>`,
};

// Setting a page's breadcrumbs.
export function setCrumbs(list) {
  store.crumbs = list;
  document.title = list.length ? list[list.length - 1].label + " – SetupMan" : "SetupMan";
}
