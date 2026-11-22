// Interactive color-coded 2D track map (SVG) synced with telemetry charts.
// Supports mouse-wheel zoom (1x–12x), drag-to-pan, automatic zoom to the active
// chart/section range, expandable large view, and bidirectional hover cursor sync.
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";

function clamp(v, lo, hi) {
  return v < lo ? lo : v > hi ? hi : v;
}

// Smooth motorsport color scale: negative (gaining) -> emerald green, 0 -> neutral slate, positive (losing) -> crimson red.
function deltaColor(val, scale) {
  const t = clamp(val / scale, -1, 1);
  if (Math.abs(t) < 0.08) return "#7c8899";
  if (t < 0) {
    const k = Math.pow(-t, 0.72);
    const r = Math.round(110 * (1 - k) + 22 * k);
    const g = Math.round(135 * (1 - k) + 197 * k);
    const b = Math.round(145 * (1 - k) + 94 * k);
    return `rgb(${r},${g},${b})`;
  }
  const k = Math.pow(t, 0.72);
  const r = Math.round(135 * (1 - k) + 232 * k);
  const g = Math.round(125 * (1 - k) + 52 * k);
  const b = Math.round(130 * (1 - k) + 64 * k);
  return `rgb(${r},${g},${b})`;
}

export const TrackMap = {
  props: {
    comp: { type: Object, required: true },
    cursorIdx: { type: Number, default: -1 },
    zoomRange: { type: Array, default: () => [0, 1] }, // [startNorm, endNorm]
    expanded: { type: Boolean, default: false },
  },
  emits: ["update:cursorIdx", "select-section", "update:expanded"],
  setup(props, { emit }) {
    const mode = ref("rate"); // "rate" (local gain/loss) | "delta" (cumulative delta)
    const svgRef = ref(null);
    const W = 440;
    const H = 440;
    const PAD = 30;

    // Camera state in base SVG space (0..440, 0..440).
    const cam = ref({ cx: W / 2, cy: H / 2, zoom: 1 });
    const pan = ref({ active: false, moved: false, startX: 0, startY: 0, origCx: W / 2, origCy: H / 2 });

    const aspect = computed(() => (props.expanded ? 1.85 : 1.0));

    const isRangeZoomed = computed(() => {
      const [z0, z1] = props.zoomRange || [0, 1];
      return z0 > 0.004 || z1 < 0.996;
    });

    const isMapZoomed = computed(() => cam.value.zoom > 1.02 || Math.abs(cam.value.cx - W / 2) > 2 || Math.abs(cam.value.cy - H / 2) > 2);

    // Dynamic SVG viewBox from camera state + expanded aspect ratio.
    const viewBox = computed(() => {
      const vbH = H / cam.value.zoom;
      const vbW = (W * aspect.value) / cam.value.zoom;
      const x = cam.value.cx - vbW / 2;
      const y = cam.value.cy - vbH / 2;
      return `${x.toFixed(2)} ${y.toFixed(2)} ${vbW.toFixed(2)} ${vbH.toFixed(2)}`;
    });

    // Stroke/marker inverse zoom scale so lines stay razor-sharp even at 10x zoom.
    const invZ = computed(() => 1 / Math.max(1, cam.value.zoom));

    // Project raw (x, y) into base 440x440 SVG space (North = up).
    const projected = computed(() => {
      const c = props.comp;
      if (!c || !c.hasMap || !c.mapX || !c.mapY || c.mapX.length < 2) return null;
      const xs = c.mapX;
      const ys = c.mapY;
      let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
      for (let i = 0; i < xs.length; i++) {
        if (xs[i] < minX) minX = xs[i];
        if (xs[i] > maxX) maxX = xs[i];
        if (ys[i] < minY) minY = ys[i];
        if (ys[i] > maxY) maxY = ys[i];
      }
      const spanX = Math.max(1, maxX - minX);
      const spanY = Math.max(1, maxY - minY);
      const scale = Math.min((W - 2 * PAD) / spanX, (H - 2 * PAD) / spanY);
      const ox = (W - spanX * scale) / 2 - minX * scale;
      const oy = (H - spanY * scale) / 2;

      const pts = new Array(xs.length);
      for (let i = 0; i < xs.length; i++) {
        pts[i] = {
          x: ox + xs[i] * scale,
          y: oy + (maxY - ys[i]) * scale,
        };
      }
      return pts;
    });

    // Fit camera cleanly to any [z0, z1] normalized distance range.
    function fitRange(range) {
      const pts = projected.value;
      const [z0, z1] = range || [0, 1];
      if (!pts || pts.length < 2 || (z0 <= 0.004 && z1 >= 0.996)) {
        cam.value = { cx: W / 2, cy: H / 2, zoom: 1 };
        return;
      }
      const i0 = Math.max(0, Math.floor(z0 * (pts.length - 1)));
      const i1 = Math.min(pts.length - 1, Math.ceil(z1 * (pts.length - 1)));
      let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
      for (let i = i0; i <= i1; i++) {
        const p = pts[i];
        if (p.x < minX) minX = p.x;
        if (p.x > maxX) maxX = p.x;
        if (p.y < minY) minY = p.y;
        if (p.y > maxY) maxY = p.y;
      }
      const bw = Math.max(18, maxX - minX);
      const bh = Math.max(18, maxY - minY);
      const padFactor = 1.38;
      const zX = (W * aspect.value) / (bw * padFactor);
      const zY = H / (bh * padFactor);
      const zoom = clamp(Math.min(zX, zY), 1.15, 11);
      cam.value = {
        cx: (minX + maxX) / 2,
        cy: (minY + maxY) / 2,
        zoom,
      };
    }

    // Auto-zoom the map whenever the user selects/zooms a section on charts or breakdown table.
    watch(() => props.zoomRange, (r) => fitRange(r), { deep: true });
    watch(() => props.comp, () => { cam.value = { cx: W / 2, cy: H / 2, zoom: 1 }; });
    watch(() => props.expanded, () => {
      if (isRangeZoomed.value) fitRange(props.zoomRange);
    });

    function zoomBy(factor, anchorX = cam.value.cx, anchorY = cam.value.cy) {
      const prevZ = cam.value.zoom;
      const nextZ = clamp(prevZ * factor, 1, 12);
      if (Math.abs(nextZ - prevZ) < 1e-4) return;
      if (nextZ <= 1.01) {
        cam.value = { cx: W / 2, cy: H / 2, zoom: 1 };
        return;
      }
      const ratio = prevZ / nextZ;
      const cx = clamp(anchorX + (cam.value.cx - anchorX) * ratio, 0, W);
      const cy = clamp(anchorY + (cam.value.cy - anchorY) * ratio, 0, H);
      cam.value = { cx, cy, zoom: nextZ };
    }

    function resetCamera() {
      cam.value = { cx: W / 2, cy: H / 2, zoom: 1 };
    }

    // Convert mouse client coordinates to exact base SVG coordinates using the live CTM.
    function clientToSvg(clientX, clientY) {
      const svg = svgRef.value;
      if (!svg) return null;
      const ctm = svg.getScreenCTM();
      if (!ctm) return null;
      const pt = svg.createSVGPoint();
      pt.x = clientX;
      pt.y = clientY;
      const res = pt.matrixTransform(ctm.inverse());
      return { x: res.x, y: res.y };
    }

    // Base smooth track outline path for the dark asphalt halo underneath colored segments.
    const basePath = computed(() => {
      const pts = projected.value;
      if (!pts) return "";
      let d = `M${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)}`;
      for (let i = 1; i < pts.length; i += 2) {
        d += ` L${pts[i].x.toFixed(1)},${pts[i].y.toFixed(1)}`;
      }
      d += " Z";
      return d;
    });

    // Color-coded segments with crisp non-bloating stroke width as zoom increases.
    const segments = computed(() => {
      const pts = projected.value;
      const c = props.comp;
      if (!pts || !c) return [];
      const n = pts.length;
      const step = n > 700 ? 3 : 2;
      const [z0, z1] = props.zoomRange || [0, 1];
      const zoomed = isRangeZoomed.value;
      const sScale = invZ.value;

      const arr = mode.value === "rate" ? c.deltaRate : c.deltaS;
      let maxAbs = 0.01;
      if (arr && arr.length) {
        const absVals = [];
        for (let i = 0; i < arr.length; i += step) absVals.push(Math.abs(arr[i] || 0));
        absVals.sort((a, b) => a - b);
        const p90 = absVals[Math.floor(absVals.length * 0.9)] || 0.01;
        maxAbs = Math.max(mode.value === "rate" ? 0.035 : 0.35, p90 * 1.15);
      }

      const out = [];
      for (let i = 0; i < n - 1; i += step) {
        const j = Math.min(n - 1, i + step);
        const mid = (i + j) >> 1;
        const frac = mid / (n - 1);
        const val = arr ? (arr[mid] || 0) : 0;
        const inZoom = !zoomed || (frac >= z0 && frac <= z1);
        let d = `M${pts[i].x.toFixed(1)},${pts[i].y.toFixed(1)}`;
        for (let k = i + 1; k <= j; k++) {
          d += ` L${pts[k].x.toFixed(1)},${pts[k].y.toFixed(1)}`;
        }
        const baseW = inZoom ? (zoomed ? 5.6 : 4.4) : 2.6;
        out.push({
          d,
          color: deltaColor(val, maxAbs),
          inZoom,
          width: baseW * sScale,
          opacity: inZoom ? 1 : 0.24,
        });
      }
      return out;
    });

    // Subtle section landmark labels when zoomed in or expanded.
    const sectionLabels = computed(() => {
      const pts = projected.value;
      const c = props.comp;
      if (!pts || !c || !c.sections?.length) return [];
      if (cam.value.zoom < 1.8 && !props.expanded) return [];
      const [z0, z1] = props.zoomRange || [0, 1];
      const zoomed = isRangeZoomed.value;
      return c.sections
        .filter((s) => !zoomed || (s.endNorm >= z0 && s.startNorm <= z1))
        .map((s) => {
          const mid = clamp(Math.round(((s.startNorm + s.endNorm) / 2) * (pts.length - 1)), 0, pts.length - 1);
          // Short readable label (first name before '&' when moderately zoomed)
          const shortName = cam.value.zoom < 3.2 && s.name.includes(" & ") ? s.name.split(" & ")[0] : s.name;
          return {
            index: s.index,
            name: shortName,
            x: pts[mid].x,
            y: pts[mid].y,
            deltaS: s.deltaS,
          };
        });
    });

    // Start/finish line marker.
    const startFinish = computed(() => {
      const pts = projected.value;
      if (!pts || pts.length < 10) return null;
      const p0 = pts[0];
      const p1 = pts[Math.min(8, pts.length - 1)];
      const dx = p1.x - p0.x;
      const dy = p1.y - p0.y;
      const len = Math.hypot(dx, dy) || 1;
      const nx = -dy / len;
      const ny = dx / len;
      const s = invZ.value;
      return {
        x: p0.x,
        y: p0.y,
        x1: p0.x - nx * 9 * s,
        y1: p0.y - ny * 9 * s,
        x2: p0.x + nx * 9 * s,
        y2: p0.y + ny * 9 * s,
        r: 4 * s,
        sw: 2.5 * s,
        tx: p0.x + 10 * s,
        ty: p0.y - 7 * s,
      };
    });

    // Active hovered point from either the map or any synced telemetry chart.
    const activePoint = computed(() => {
      const pts = projected.value;
      const idx = props.cursorIdx;
      const c = props.comp;
      if (!pts || idx < 0 || idx >= pts.length) return null;
      const norm = idx / (pts.length - 1);
      const sec = (c.sections || []).find((s) => norm >= s.startNorm && norm <= s.endNorm);
      return {
        x: pts[idx].x,
        y: pts[idx].y,
        distKm: ((c.distM?.[idx] || 0) / 1000).toFixed(2),
        delta: c.deltaS?.[idx] || 0,
        refSpd: Math.round(c.refSpeed?.[idx] || 0),
        cmpSpd: Math.round(c.cmpSpeed?.[idx] || 0),
        section: sec ? sec.name : "",
      };
    });

    function onWheel(e) {
      const pt = clientToSvg(e.clientX, e.clientY);
      if (!pt) return;
      const factor = e.deltaY < 0 ? 1.28 : 1 / 1.28;
      zoomBy(factor, pt.x, pt.y);
    }

    function onMouseDown(e) {
      if (e.button !== 0) return;
      pan.value = {
        active: true,
        moved: false,
        startX: e.clientX,
        startY: e.clientY,
        origCx: cam.value.cx,
        origCy: cam.value.cy,
      };
    }

    function onMouseMove(e) {
      if (pan.value.active) {
        const dxPx = e.clientX - pan.value.startX;
        const dyPx = e.clientY - pan.value.startY;
        if (Math.hypot(dxPx, dyPx) > 4) {
          pan.value.moved = true;
        }
        if (pan.value.moved && svgRef.value) {
          const rect = svgRef.value.getBoundingClientRect();
          const vbH = H / cam.value.zoom;
          const vbW = (W * aspect.value) / cam.value.zoom;
          const scaleX = vbW / Math.max(1, rect.width);
          const scaleY = vbH / Math.max(1, rect.height);
          const s = Math.max(scaleX, scaleY);
          cam.value = {
            ...cam.value,
            cx: clamp(pan.value.origCx - dxPx * s, 0, W),
            cy: clamp(pan.value.origCy - dyPx * s, 0, H),
          };
          return;
        }
      }

      const pts = projected.value;
      const pt = clientToSvg(e.clientX, e.clientY);
      if (!pts || !pt) return;
      const snapR = 32 * invZ.value;
      let best = -1;
      let bestD2 = snapR * snapR;
      for (let i = 0; i < pts.length; i++) {
        const dx = pts[i].x - pt.x;
        const dy = pts[i].y - pt.y;
        const d2 = dx * dx + dy * dy;
        if (d2 < bestD2) {
          bestD2 = d2;
          best = i;
        }
      }
      if (best >= 0) emit("update:cursorIdx", best);
    }

    function onMouseUp() {
      if (!pan.value.active) return;
      const wasMoved = pan.value.moved;
      pan.value.active = false;
      // Delay clearing moved briefly so click handler knows if it was a drag pan.
      if (wasMoved) {
        setTimeout(() => { pan.value.moved = false; }, 0);
      }
    }

    function onClick() {
      if (pan.value.moved) return;
      const idx = props.cursorIdx;
      const c = props.comp;
      if (idx < 0 || !c || !c.sections?.length) return;
      const norm = idx / Math.max(1, c.n - 1);
      const sec = c.sections.find((s) => norm >= s.startNorm && norm <= s.endNorm);
      if (sec) emit("select-section", sec);
    }

    function fmtDelta(v) {
      const sign = v > 0.0005 ? "+" : v < -0.0005 ? "−" : "";
      return sign + Math.abs(v).toFixed(3) + " s";
    }

    onMounted(() => window.addEventListener("mouseup", onMouseUp));
    onBeforeUnmount(() => window.removeEventListener("mouseup", onMouseUp));

    return {
      mode, svgRef, cam, pan, viewBox, invZ,
      isRangeZoomed, isMapZoomed,
      projected, basePath, segments, sectionLabels, startFinish, activePoint,
      zoomBy, resetCamera, fitRange, onWheel, onMouseDown, onMouseMove, onClick, fmtDelta,
    };
  },
  template: `
    <section class="panel tele-map-card" :class="{ 'is-expanded': expanded }" aria-label="Track map">
      <div class="tele-map-head">
        <div class="tele-map-title-row">
          <div class="tele-map-title-bar">
            <h2 class="panel-title">Track map</h2>
            <button type="button" class="btn btn-sm tele-expand-btn"
              @click="$emit('update:expanded', !expanded)"
              :title="expanded ? 'Switch back to compact side-by-side map' : 'Expand track map to full width'">
              {{ expanded ? '⤡ Compact' : '⤢ Expand' }}
            </button>
          </div>
          <p class="tele-card-sub tele-map-sub" v-if="activePoint">
            <strong>{{ activePoint.section || (activePoint.distKm + ' km') }}</strong> · {{ activePoint.distKm }} km
          </p>
          <p class="tele-card-sub tele-map-sub" v-else>Scroll to zoom, drag to pan, click a corner to zoom section</p>
        </div>
        <div class="tele-map-controls">
          <div class="tele-seg-toggle tele-map-toggle" role="group" aria-label="Map coloring">
            <button type="button" :class="{ on: mode === 'rate' }" @click="mode = 'rate'" title="Green where you actively gain time, red where you actively lose time">Local gain/loss</button>
            <button type="button" :class="{ on: mode === 'delta' }" @click="mode = 'delta'" title="Color by total cumulative lap delta">Cumulative</button>
          </div>
          <div class="tele-zoom-tools" role="group" aria-label="Map zoom controls">
            <button type="button" class="tele-zbtn" @click="zoomBy(1.4)" title="Zoom in">+</button>
            <button type="button" class="tele-zbtn" @click="zoomBy(1 / 1.4)" :disabled="cam.zoom <= 1.02" title="Zoom out">−</button>
            <button v-if="isRangeZoomed" type="button" class="tele-zbtn tele-zbtn-txt" @click="fitRange(zoomRange)" title="Center and zoom map to the active chart section">Fit section</button>
            <button v-if="isMapZoomed" type="button" class="tele-zbtn tele-zbtn-txt" @click="resetCamera" title="Reset map zoom to full circuit">1:1</button>
          </div>
        </div>
      </div>

      <div v-if="!projected" class="tele-map-empty">
        No 2D track coordinates in these laps. Live ACC laps include exact coordinates automatically.
      </div>
      <div v-else class="tele-map-wrap">
        <div class="tele-map-stage">
          <svg ref="svgRef" class="tele-map-svg" :class="{ panning: pan.active && pan.moved }"
            :viewBox="viewBox" preserveAspectRatio="xMidYMid meet"
            @wheel.prevent="onWheel" @mousedown="onMouseDown" @mousemove="onMouseMove"
            @mouseleave="$emit('update:cursorIdx', -1)" @click="onClick" @dblclick="resetCamera">
            <!-- Subtle asphalt base halo -->
            <path :d="basePath" class="tele-track-halo" fill="none" :stroke-width="10 * invZ" stroke-linejoin="round" stroke-linecap="round" />
            <!-- Color-coded telemetry segments -->
            <path v-for="(s, i) in segments" :key="i" :d="s.d" fill="none"
              :stroke="s.color" :stroke-width="s.width" :stroke-opacity="s.opacity"
              stroke-linecap="round" stroke-linejoin="round" />
            <!-- Section landmark labels when zoomed in or expanded -->
            <g v-if="sectionLabels.length" class="tele-sec-labels">
              <text v-for="l in sectionLabels" :key="l.index"
                :x="l.x + 6 * invZ" :y="l.y - 6 * invZ"
                :font-size="10.5 * invZ" :stroke-width="3 * invZ"
                class="tele-sec-label">{{ l.name }}</text>
            </g>
            <!-- Start / Finish marker -->
            <g v-if="startFinish" class="tele-sf">
              <line :x1="startFinish.x1" :y1="startFinish.y1" :x2="startFinish.x2" :y2="startFinish.y2" stroke="var(--ink)" :stroke-width="startFinish.sw" stroke-linecap="round" />
              <circle :cx="startFinish.x" :cy="startFinish.y" :r="startFinish.r" fill="var(--surface)" stroke="var(--kerb)" :stroke-width="2 * invZ" />
              <text :x="startFinish.tx" :y="startFinish.ty" :font-size="10 * invZ" class="tele-sf-label">S/F</text>
            </g>
            <!-- Live synced cursor marker -->
            <g v-if="activePoint" class="tele-cursor-dot">
              <circle :cx="activePoint.x" :cy="activePoint.y" :r="10 * invZ" class="tele-cursor-pulse" />
              <circle :cx="activePoint.x" :cy="activePoint.y" :r="5 * invZ" :stroke-width="2.5 * invZ" class="tele-cursor-core" />
            </g>
          </svg>
          <span v-if="cam.zoom > 1.05" class="tele-zoom-badge">{{ cam.zoom.toFixed(1) }}×</span>
        </div>

        <div class="tele-map-footer">
          <div class="tele-map-legend">
            <span class="tele-leg-item"><i style="background:#16c55e"></i> Faster than ref</span>
            <span class="tele-leg-item"><i style="background:#7c8899"></i> Even</span>
            <span class="tele-leg-item"><i style="background:#e83440"></i> Slower than ref</span>
          </div>
          <div class="tele-map-pill-slot">
            <div v-if="activePoint" class="tele-map-pill" :class="activePoint.delta < -0.005 ? 'is-gain' : activePoint.delta > 0.005 ? 'is-loss' : ''">
              <span>{{ fmtDelta(activePoint.delta) }}</span>
              <span class="tele-map-pill-spd">{{ activePoint.cmpSpd }} vs {{ activePoint.refSpd }} km/h</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  `,
};
