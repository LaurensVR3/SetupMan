// Track map (spec D-159 T3): the reference lap's line, coloured by where the compared lap gains
// (green) or loses (red) time, with the zoomed part and the cursor. Clicking the map moves the
// cursor; the graphs follow.
import { ref, watch, onMounted, onBeforeUnmount } from "vue";

const DPR = () => Math.min(window.devicePixelRatio || 1, 2);

export const TeleMap = {
  props: {
    map: Object,   // { x, y, dist, source }
    view: Object,  // shared with the graphs
    gain: Object,  // time difference of the compared lap: { x, y } (optional)
    corners: Array, // [{ n, dist, name }]: turn numbers drawn at the turn; the name shows on mouseover
  },
  setup(props) {
    const box = ref(null), base = ref(null), over = ref(null);
    let w = 0, h = 0, fit = null, ro = null, raf = 0;

    function layout() {
      const m = props.map;
      if (!m || !m.x.length) return null;
      let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
      for (let i = 0; i < m.x.length; i++) {
        x0 = Math.min(x0, m.x[i]); x1 = Math.max(x1, m.x[i]);
        y0 = Math.min(y0, m.y[i]); y1 = Math.max(y1, m.y[i]);
      }
      const pad = 30, k = Math.min((w - 2 * pad) / (x1 - x0 || 1), (h - 2 * pad) / (y1 - y0 || 1));
      const ox = (w - (x1 - x0) * k) / 2, oy = (h - (y1 - y0) * k) / 2;
      return { X: (x) => ox + (x - x0) * k, Y: (y) => h - oy - (y - y0) * k }; // y up on screen
    }

    function cssColors() {
      const cs = getComputedStyle(box.value);
      const g = (n) => cs.getPropertyValue(n).trim();
      return { track: g("--line-strong"), ink: g("--ink"), ink2: g("--ink-2"), gain: g("--t-gain"), loss: g("--t-loss"), start: g("--lap-0"), surface: g("--surface"), focus: g("--focus") };
    }

    // gain rate per metre between two distances of the time difference
    function rate(a, b) {
      const s = props.gain;
      if (!s || !s.x.length) return 0;
      const step = s.x[1] - s.x[0] || 1;
      const ia = Math.max(0, Math.min(s.x.length - 1, Math.round(a / step)));
      const ib = Math.max(0, Math.min(s.x.length - 1, Math.round(b / step)));
      return ib === ia ? 0 : (s.y[ib] - s.y[ia]) / (s.x[ib] - s.x[ia]);
    }

    function drawBase() {
      raf = 0;
      if (!base.value || !w) return;
      const ctx = base.value.getContext("2d"), dpr = DPR();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      fit = layout();
      if (!fit) return;
      const m = props.map, col = cssColors(), { from, to } = props.view;
      ctx.lineCap = ctx.lineJoin = "round";
      ctx.strokeStyle = col.track;
      ctx.lineWidth = 9;
      ctx.beginPath();
      for (let i = 0; i < m.x.length; i++) (i ? ctx.lineTo : ctx.moveTo).call(ctx, fit.X(m.x[i]), fit.Y(m.y[i]));
      ctx.stroke();
      // gain / loss of the compared lap, every ~25 m, measured over 80 m so small ups and downs of
      // the time difference don't make the line flicker
      if (props.gain) {
        ctx.lineWidth = 5;
        ctx.lineCap = "butt";
        const stride = Math.max(1, Math.round(m.x.length / 160));
        for (let i = 0; i + stride < m.x.length; i += stride) {
          const r = rate(m.dist[i] - 30, m.dist[i + stride] + 30) * 100; // s per 100 m
          const a = Math.min(1, Math.abs(r) / 0.06);
          if (a < 0.08) continue;
          ctx.strokeStyle = r > 0 ? col.loss : col.gain;
          ctx.globalAlpha = 0.25 + 0.75 * a;
          ctx.beginPath();
          ctx.moveTo(fit.X(m.x[i]), fit.Y(m.y[i]));
          for (let j = i + 1; j <= i + stride; j++) ctx.lineTo(fit.X(m.x[j]), fit.Y(m.y[j]));
          ctx.stroke();
        }
        ctx.globalAlpha = 1;
        ctx.lineCap = "round";
      }
      // zoomed part
      if (from > 0.5 || to < props.view.total - 0.5) {
        ctx.strokeStyle = col.ink;
        ctx.lineWidth = 2;
        ctx.beginPath();
        let on = false;
        for (let i = 0; i < m.x.length; i++) {
          if (m.dist[i] < from || m.dist[i] > to) { on = false; continue; }
          (on ? ctx.lineTo : ctx.moveTo).call(ctx, fit.X(m.x[i]), fit.Y(m.y[i]));
          on = true;
        }
        ctx.stroke();
      }
      // start / finish line
      const i1 = Math.min(3, m.x.length - 1);
      const dx = m.x[i1] - m.x[0], dy = m.y[i1] - m.y[0], len = Math.hypot(dx, dy) || 1;
      const nx = (-dy / len) * 9, ny = (dx / len) * 9;
      ctx.strokeStyle = col.ink;
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.moveTo(fit.X(m.x[0]) - nx, fit.Y(m.y[0]) + ny);
      ctx.lineTo(fit.X(m.x[0]) + nx, fit.Y(m.y[0]) - ny);
      ctx.stroke();
      // turn numbers (T1, T2 …), pushed outwards from the middle of the track so they sit beside the line
      if (props.corners && props.corners.length) {
        let cx = 0, cy = 0;
        for (let i = 0; i < m.x.length; i++) { cx += fit.X(m.x[i]); cy += fit.Y(m.y[i]); }
        cx /= m.x.length; cy /= m.x.length;
        ctx.font = "600 11px Bahnschrift, 'Segoe UI', sans-serif";
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        boxes = [];
        for (const c of props.corners) {
          const i = nearestByDist(c.dist);
          const px = fit.X(m.x[i]), py = fit.Y(m.y[i]);
          const vx = px - cx, vy = py - cy, vl = Math.hypot(vx, vy) || 1;
          const lx = px + (vx / vl) * 17, ly = py + (vy / vl) * 17;
          ctx.fillStyle = col.surface;
          ctx.strokeStyle = col.track;
          ctx.lineWidth = 1;
          const label = "T" + c.n;
          const w = Math.max(18, ctx.measureText(label).width + 9), h = 17;
          ctx.beginPath(); ctx.roundRect(lx - w / 2, ly - h / 2, w, h, h / 2); ctx.fill(); ctx.stroke();
          boxes.push({ x: lx - w / 2, y: ly - h / 2, w, h, text: c.name ? `T${c.n}: ${c.name}` : `T${c.n}` });
          ctx.fillStyle = col.ink2;
          ctx.fillText(label, lx, ly + 0.5);
        }
        ctx.textAlign = "start";
        ctx.textBaseline = "alphabetic";
      }
      drawOver();
    }

    function nearestByDist(d) {
      const a = props.map.dist;
      let lo = 0, hi = a.length - 1;
      while (hi - lo > 1) {
        const mid = (lo + hi) >> 1;
        if (a[mid] <= d) lo = mid; else hi = mid;
      }
      return lo;
    }

    function drawOver() {
      if (!over.value || !w) return;
      const ctx = over.value.getContext("2d"), dpr = DPR();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const c = props.view.cursor;
      if (!fit || c === null) return;
      const m = props.map, i = nearestByDist(c), col = cssColors();
      const px = fit.X(m.x[i]), py = fit.Y(m.y[i]);
      ctx.fillStyle = col.focus;
      ctx.globalAlpha = 0.25;
      ctx.beginPath(); ctx.arc(px, py, 13, 0, Math.PI * 2); ctx.fill();
      ctx.globalAlpha = 1;
      ctx.fillStyle = col.ink;
      ctx.strokeStyle = col.surface;
      ctx.lineWidth = 2.5;
      ctx.beginPath(); ctx.arc(px, py, 6, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
    }

    function resize() {
      const r = box.value.getBoundingClientRect();
      w = Math.max(10, Math.floor(r.width));
      h = Math.max(10, Math.floor(r.height));
      const dpr = DPR();
      for (const c of [base.value, over.value]) {
        c.width = w * dpr; c.height = h * dpr;
        c.style.width = w + "px"; c.style.height = h + "px";
      }
      drawBase();
    }

    function pick(e) {
      if (!fit || !props.map) return;
      const r = over.value.getBoundingClientRect();
      const px = e.clientX - r.left, py = e.clientY - r.top, m = props.map;
      let best = 0, bd = Infinity;
      for (let i = 0; i < m.x.length; i++) {
        const d = (fit.X(m.x[i]) - px) ** 2 + (fit.Y(m.y[i]) - py) ** 2;
        if (d < bd) { bd = d; best = i; }
      }
      if (bd < 900) props.view.cursor = m.dist[best];
    }
    // mouseover a turn number: its name (as the canvas's tooltip)
    let boxes = [];
    const hover = ref("");
    function onMove(e) {
      if (e.buttons & 1) { pick(e); return; }
      const r = over.value.getBoundingClientRect();
      const px = e.clientX - r.left, py = e.clientY - r.top;
      const b = boxes.find((b) => px >= b.x && px <= b.x + b.w && py >= b.y && py <= b.y + b.h);
      hover.value = b ? b.text : "";
    }

    onMounted(() => { ro = new ResizeObserver(resize); ro.observe(box.value); });
    onBeforeUnmount(() => { ro && ro.disconnect(); cancelAnimationFrame(raf); });
    const redraw = () => { if (!raf) raf = requestAnimationFrame(drawBase); };
    watch(() => [props.map, props.gain, props.corners, props.view.from, props.view.to], redraw);
    watch(() => props.view.cursor, () => requestAnimationFrame(drawOver));
    const mo = new MutationObserver(redraw);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    onBeforeUnmount(() => mo.disconnect());
    return { box, base, over, pick, onMove, hover };
  },
  template: `
    <div class="tm" ref="box">
      <canvas ref="base" aria-hidden="true"></canvas>
      <canvas ref="over" class="tm-over" role="img" aria-label="Track map; click to move the cursor" :title="hover || null" @pointerdown="pick" @pointermove="onMove" @pointerleave="hover = ''"></canvas>
    </div>`,
};
