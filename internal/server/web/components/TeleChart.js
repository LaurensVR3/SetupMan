// One telemetry graph (spec D-159): the laps of one channel along the distance. All graphs on the
// analysis page share one view (zoom range and cursor). The traces are drawn on one canvas only
// when the data or zoom changes; the cursor lives on a second canvas, so moving it costs little.
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { valueAt, fmt, signed } from "../telemetry.js";

const DPR = () => Math.min(window.devicePixelRatio || 1, 2);

function lowerBound(a, v) {
  let lo = 0, hi = a.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (a[mid] < v) lo = mid + 1; else hi = mid;
  }
  return lo;
}

// niceStep: a round step for about n ticks over span.
export function niceStep(span, n) {
  const raw = span / Math.max(1, n);
  const p = Math.pow(10, Math.floor(Math.log10(raw)));
  const m = raw / p;
  return (m < 1.5 ? 1 : m < 3 ? 2 : m < 7 ? 5 : 10) * p;
}

// drawLine draws one series between x0 and x1, keeping the lowest and highest point of every
// pixel column (fast with 200 samples per second, and no peak is lost).
function drawLine(ctx, s, x0, x1, w, h, y0, y1, step) {
  if (!s.x.length) return;
  const sx = w / (x1 - x0), sy = h / (y1 - y0 || 1);
  const Y = (v) => h - (v - y0) * sy;
  let i = Math.max(0, lowerBound(s.x, x0) - 1);
  ctx.beginPath();
  let col = null, mn = 0, mx = 0, last = 0, started = false;
  for (; i < s.x.length; i++) {
    const px = (s.x[i] - x0) * sx, py = Y(s.y[i]);
    const c = Math.floor(px);
    if (c === col) {
      if (py < mn) mn = py;
      if (py > mx) mx = py;
      last = py;
    } else {
      if (col !== null && mx - mn > 1) { ctx.lineTo(col + 0.5, mn); ctx.lineTo(col + 0.5, mx); ctx.lineTo(col + 0.5, last); }
      if (!started) { ctx.moveTo(px, py); started = true; }
      else if (step) { ctx.lineTo(px, last); ctx.lineTo(px, py); }
      else ctx.lineTo(px, py);
      col = c; mn = mx = last = py;
    }
    if (s.x[i] > x1) break;
  }
  ctx.stroke();
}

export const TeleChart = {
  props: {
    label: String,
    unit: String,
    lines: Array,     // [{ s: {x, y}, color, name }]
    view: Object,     // shared: { from, to, total, cursor, sel }
    height: { type: Number, default: 130 },
    step: Boolean,    // values that jump (gear, levels)
    isDelta: Boolean, // time difference: centred on zero, values as ±s
    axis: Boolean,    // distance labels under the graph
    fixed: Boolean,   // no remove button
  },
  emits: ["remove"],
  setup(props) {
    const box = ref(null), base = ref(null), over = ref(null);
    let w = 0, h = 0, raf = 0, rafOver = 0, ro = null, drag = null;
    const PAD_T = 6, PAD_B = () => (props.axis ? 22 : 6);

    const range = () => {
      let lo = Infinity, hi = -Infinity;
      const { from, to } = props.view;
      for (const l of props.lines || []) {
        const s = l.s;
        let i = Math.max(0, lowerBound(s.x, from) - 1);
        for (; i < s.x.length && s.x[i] <= to; i++) {
          const v = s.y[i];
          if (v < lo) lo = v;
          if (v > hi) hi = v;
        }
      }
      if (!isFinite(lo)) return [0, 1];
      if (props.isDelta) { const m = Math.max(Math.abs(lo), Math.abs(hi), 0.05); return [-m * 1.1, m * 1.1]; }
      if (hi - lo < 1e-6) { lo -= 1; hi += 1; }
      const pad = (hi - lo) * 0.07;
      return [lo - pad, hi + pad];
    };
    let yr = [0, 1];

    function colors() {
      const cs = getComputedStyle(box.value);
      return { grid: cs.getPropertyValue("--line").trim(), text: cs.getPropertyValue("--ink-3").trim(), zero: cs.getPropertyValue("--line-strong").trim(),
        sel: cs.getPropertyValue("--focus").trim(), ink: cs.getPropertyValue("--ink-2").trim(),
        gain: cs.getPropertyValue("--t-gain").trim(), loss: cs.getPropertyValue("--t-loss").trim() };
    }

    function drawBase() {
      raf = 0;
      const c = base.value;
      if (!c || !w) return;
      const ctx = c.getContext("2d"), dpr = DPR();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const ph = h - PAD_T - PAD_B();
      const { from, to } = props.view;
      const col = colors();
      yr = range();
      // grid: distance
      ctx.font = "11px Bahnschrift, 'Segoe UI', sans-serif";
      ctx.lineWidth = 1;
      const xs = niceStep(to - from, Math.max(2, w / 110));
      ctx.strokeStyle = col.grid;
      ctx.fillStyle = col.text;
      ctx.beginPath();
      for (let x = Math.ceil(from / xs) * xs; x <= to; x += xs) {
        const px = Math.round(((x - from) / (to - from)) * w) + 0.5;
        ctx.moveTo(px, PAD_T); ctx.lineTo(px, PAD_T + ph);
        if (props.axis) ctx.fillText(x >= 1000 ? (x / 1000).toFixed(xs < 10 ? 3 : xs < 100 ? 2 : xs < 1000 ? 1 : 0) + " km" : Math.round(x) + " m", px + 3, h - 6);
      }
      // grid: values
      // a time difference gets ticks on both sides of zero (±2, ±4 …), not only "±0"
      const ys = props.step ? Math.max(1, niceStep(yr[1] - yr[0], 3)) : niceStep(yr[1] - yr[0], props.isDelta ? 5 : 3);
      const Y = (v) => PAD_T + ph - ((v - yr[0]) / (yr[1] - yr[0])) * ph;
      for (let v = Math.ceil(yr[0] / ys) * ys; v <= yr[1]; v += ys) {
        const py = Math.round(Y(v)) + 0.5;
        ctx.moveTo(0, py); ctx.lineTo(w, py);
      }
      ctx.stroke();
      ctx.textBaseline = "middle";
      for (let v = Math.ceil(yr[0] / ys) * ys; v <= yr[1]; v += ys) {
        // as many decimals as the grid step needs: 0, 50, 100 or 0.25, 0.50
        const d = ys >= 1 ? 0 : Math.min(3, Math.ceil(-Math.log10(ys) - 1e-9));
        const t = Math.abs(v) < ys / 1000 ? (props.isDelta ? "±0" : "0") : (props.isDelta && v > 0 ? "+" : "") + v.toFixed(d).replace("-", "−");
        ctx.fillText(t, 4, Math.max(PAD_T + 6, Math.round(Y(v)) - 6)); // never cut off at the top
      }
      ctx.textBaseline = "alphabetic";
      if (props.isDelta) {
        // shade the first compared lap: red above zero (losing time), green below (gaining)
        const s = props.lines?.[0]?.s;
        if (s && s.x.length) {
          const y0 = Y(0);
          ctx.beginPath();
          ctx.moveTo(0, y0);
          const step = Math.max(1, Math.floor(s.x.length / (w * 2)));
          for (let i = 0; i < s.x.length; i += step) {
            if (s.x[i] < from || s.x[i] > to) continue;
            ctx.lineTo(((s.x[i] - from) / (to - from)) * w, Y(s.y[i]));
          }
          ctx.lineTo(w, y0);
          ctx.closePath();
          for (const [top, color] of [[true, col.loss], [false, col.gain]]) {
            ctx.save();
            ctx.beginPath();
            if (top) ctx.rect(0, 0, w, y0); else ctx.rect(0, y0, w, h);
            ctx.clip();
            ctx.globalAlpha = 0.22;
            ctx.fillStyle = color;
            ctx.beginPath();
            ctx.moveTo(0, y0);
            for (let i = 0; i < s.x.length; i += step) {
              if (s.x[i] < from || s.x[i] > to) continue;
              ctx.lineTo(((s.x[i] - from) / (to - from)) * w, Y(s.y[i]));
            }
            ctx.lineTo(w, y0);
            ctx.closePath();
            ctx.fill();
            ctx.restore();
          }
        }
        ctx.strokeStyle = col.zero;
        ctx.beginPath(); ctx.moveTo(0, Math.round(Y(0)) + 0.5); ctx.lineTo(w, Math.round(Y(0)) + 0.5); ctx.stroke();
      }
      // traces, reference lap on top
      ctx.save();
      ctx.translate(0, PAD_T);
      ctx.lineJoin = "round";
      const lines = [...(props.lines || [])].reverse();
      for (const l of lines) {
        ctx.strokeStyle = l.color;
        ctx.lineWidth = 1.8;
        drawLine(ctx, l.s, from, to, w, ph, yr[0], yr[1], props.step);
      }
      ctx.restore();
      drawOver();
    }

    function drawOver() {
      rafOver = 0;
      const c = over.value;
      if (!c || !w) return;
      const ctx = c.getContext("2d"), dpr = DPR();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const { from, to, cursor, sel } = props.view;
      const col = colors();
      const ph = h - PAD_T - PAD_B();
      const X = (x) => ((x - from) / (to - from)) * w;
      if (sel) {
        const a = X(Math.min(sel[0], sel[1])), b = X(Math.max(sel[0], sel[1]));
        ctx.fillStyle = col.sel;
        ctx.globalAlpha = 0.12;
        ctx.fillRect(a, PAD_T, b - a, ph);
        ctx.globalAlpha = 1;
      }
      if (cursor === null || cursor < from || cursor > to) return;
      const px = Math.round(X(cursor)) + 0.5;
      ctx.strokeStyle = col.ink;
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.beginPath(); ctx.moveTo(px, PAD_T); ctx.lineTo(px, PAD_T + ph); ctx.stroke();
      ctx.setLineDash([]);
      for (const l of props.lines || []) {
        const v = valueAt(l.s, cursor, props.step);
        if (v === null) continue;
        const py = PAD_T + ph - ((v - yr[0]) / (yr[1] - yr[0])) * ph;
        ctx.fillStyle = l.color;
        ctx.beginPath(); ctx.arc(px, py, 3, 0, Math.PI * 2); ctx.fill();
      }
    }

    const redraw = () => { if (!raf) raf = requestAnimationFrame(drawBase); };
    const redrawOver = () => { if (!rafOver) rafOver = requestAnimationFrame(drawOver); };

    function resize() {
      const r = box.value.getBoundingClientRect();
      w = Math.max(10, Math.floor(r.width));
      h = props.height;
      const dpr = DPR();
      for (const c of [base.value, over.value]) {
        c.width = w * dpr; c.height = h * dpr;
        c.style.width = w + "px"; c.style.height = h + "px";
      }
      drawBase();
    }

    // pointer: move = cursor, drag = zoom to the dragged part, double-click = whole lap,
    // Ctrl + wheel = zoom, Shift + wheel = move along the lap
    const toX = (e) => {
      const r = over.value.getBoundingClientRect();
      const f = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width));
      return props.view.from + f * (props.view.to - props.view.from);
    };
    function onMove(e) {
      const x = toX(e);
      props.view.cursor = x;
      if (drag) props.view.sel = [drag, x];
    }
    function onDown(e) {
      if (e.button !== 0) return;
      over.value.setPointerCapture(e.pointerId);
      drag = toX(e);
    }
    function onUp() {
      const sel = props.view.sel;
      drag = null;
      props.view.sel = null;
      if (!sel) return;
      const a = Math.min(sel[0], sel[1]), b = Math.max(sel[0], sel[1]);
      const pxSpan = ((b - a) / (props.view.to - props.view.from)) * w;
      if (pxSpan > 6 && b - a > 5) { props.view.from = a; props.view.to = b; }
    }
    function onDbl() { props.view.from = 0; props.view.to = props.view.total; }
    // keyboard: arrows move the cursor (with Shift: further), + and − zoom around it, 0 or Escape
    // shows the whole lap
    function zoomAt(x, k) {
      const v = props.view, span = v.to - v.from;
      const ns = Math.min(v.total, Math.max(20, span * k));
      const a = Math.max(0, Math.min(v.total - ns, x - (x - v.from) * (ns / span)));
      v.from = a; v.to = a + ns;
    }
    function onKey(e) {
      const v = props.view, span = v.to - v.from;
      const cur = v.cursor === null ? (v.from + v.to) / 2 : v.cursor;
      const step = span * (e.shiftKey ? 0.1 : 0.01);
      if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
        const x = Math.max(0, Math.min(v.total, cur + (e.key === "ArrowRight" ? step : -step)));
        v.cursor = x;
        if (x > v.to) { const a = Math.min(v.total - span, x - span * 0.1); v.from = a; v.to = a + span; }
        if (x < v.from) { const a = Math.max(0, x - span * 0.9); v.from = a; v.to = a + span; }
      } else if (e.key === "+" || e.key === "=") zoomAt(cur, 0.8);
      else if (e.key === "-" || e.key === "_") zoomAt(cur, 1.25);
      else if (e.key === "0" || e.key === "Escape") onDbl();
      else return;
      e.preventDefault();
    }
    function onWheel(e) {
      if (!e.ctrlKey && !e.shiftKey) return;
      e.preventDefault();
      const v = props.view, span = v.to - v.from;
      if (e.ctrlKey) {
        const x = toX(e), k = e.deltaY > 0 ? 1.25 : 0.8;
        const ns = Math.min(v.total, Math.max(20, span * k));
        let a = x - (x - v.from) * (ns / span);
        a = Math.max(0, Math.min(v.total - ns, a));
        v.from = a; v.to = a + ns;
      } else {
        const d = (e.deltaY || e.deltaX) > 0 ? span * 0.15 : -span * 0.15;
        const a = Math.max(0, Math.min(v.total - span, v.from + d));
        v.from = a; v.to = a + span;
      }
    }

    onMounted(() => {
      ro = new ResizeObserver(resize);
      ro.observe(box.value);
      over.value.addEventListener("wheel", onWheel, { passive: false });
    });
    onBeforeUnmount(() => { ro && ro.disconnect(); cancelAnimationFrame(raf); cancelAnimationFrame(rafOver); });
    watch(() => [props.lines, props.view.from, props.view.to, props.height], redraw);
    watch(() => [props.view.cursor, props.view.sel], redrawOver);
    // repaint when the theme changes
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    mq.addEventListener?.("change", redraw);
    const mo = new MutationObserver(redraw);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    onBeforeUnmount(() => { mq.removeEventListener?.("change", redraw); mo.disconnect(); });

    const readouts = computed(() => {
      const x = props.view.cursor;
      if (x === null) return [];
      return (props.lines || []).map((l) => {
        const v = valueAt(l.s, x, props.step);
        return { color: l.color, text: props.isDelta ? signed(v) : fmt(v, props.unit, props.step) };
      });
    });
    return { box, base, over, readouts, onMove, onDown, onUp, onDbl, onKey };
  },
  template: `
    <div class="tc">
      <div class="tc-head">
        <span class="tc-label">{{ label }}<span v-if="unit" class="tc-unit">{{ unit }}</span></span>
        <span class="tc-values"><b v-for="(r, i) in readouts" :key="i" :style="{ color: r.color }">{{ r.text }}</b></span>
        <button v-if="!fixed" type="button" class="tc-x" @click="$emit('remove')" :aria-label="'Remove ' + label" title="Remove this graph">×</button>
      </div>
      <div class="tc-plot" ref="box" :style="{ height: height + 'px' }">
        <canvas ref="base" aria-hidden="true"></canvas>
        <canvas ref="over" class="tc-over" role="img" tabindex="0" :aria-label="label + ' along the lap. Arrow keys move the cursor, plus and minus zoom, 0 shows the whole lap.'"
          @pointermove="onMove" @pointerdown="onDown" @pointerup="onUp" @dblclick="onDbl" @keydown="onKey"></canvas>
      </div>
    </div>`,
};
