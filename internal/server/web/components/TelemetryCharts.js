// High-DPI synchronized telemetry charts (Canvas, zero external dependencies).
// Panels: Time Delta, Speed (with Ref/Cmp diff shading), Throttle & Brake, Steering + Gear + TC/ABS.
// Supports drag-to-zoom, wheel zoom, double-click reset, and bidirectional cursor sync with TrackMap.
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from "vue";

const PAD_L = 58;
const PAD_R = 14;
const PAD_T = 10;
const PAD_B = 22;

function clamp(v, lo, hi) {
  return v < lo ? lo : v > hi ? hi : v;
}

function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

function isDark() {
  const dt = document.documentElement.dataset.theme;
  if (dt === "dark") return true;
  if (dt === "light") return false;
  return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export const TelemetryCharts = {
  props: {
    comp: { type: Object, required: true },
    cursorIdx: { type: Number, default: -1 },
    zoomRange: { type: Array, default: () => [0, 1] },
  },
  emits: ["update:cursorIdx", "update:zoomRange"],
  setup(props, { emit }) {
    const wrapRef = ref(null);
    const deltaCv = ref(null);
    const speedCv = ref(null);
    const pedalCv = ref(null);
    const steerCv = ref(null);

    const drag = ref({ active: false, x0: 0, x1: 0, norm0: 0, norm1: 0 });
    const showSteer = ref(true);

    const isZoomed = computed(() => {
      const [z0, z1] = props.zoomRange || [0, 1];
      return z0 > 0.002 || z1 < 0.998;
    });

    const zoomInfo = computed(() => {
      const c = props.comp;
      if (!c) return "";
      const [z0, z1] = props.zoomRange || [0, 1];
      const d0 = ((z0 * c.trackLengthM) / 1000).toFixed(2);
      const d1 = ((z1 * c.trackLengthM) / 1000).toFixed(2);
      const mid = (z0 + z1) / 2;
      const sec = (c.sections || []).find((s) => mid >= s.startNorm && mid <= s.endNorm);
      return `${d0} – ${d1} km` + (sec && isZoomed.value ? ` · ${sec.name}` : "");
    });

    const readout = computed(() => {
      const c = props.comp;
      const i = props.cursorIdx;
      if (!c || i < 0 || i >= c.n) return null;
      const norm = i / Math.max(1, c.n - 1);
      const sec = (c.sections || []).find((s) => norm >= s.startNorm && norm <= s.endNorm);
      const rSpd = c.refSpeed?.[i] || 0;
      const cSpd = c.cmpSpeed?.[i] || 0;
      return {
        distKm: ((c.distM?.[i] || 0) / 1000).toFixed(2),
        section: sec ? sec.name : "",
        delta: c.deltaS?.[i] || 0,
        refSpd: Math.round(rSpd),
        cmpSpd: Math.round(cSpd),
        spdDiff: Math.round(cSpd - rSpd),
        refThr: Math.round(c.refThrottle?.[i] || 0),
        cmpThr: Math.round(c.cmpThrottle?.[i] || 0),
        refBrk: Math.round(c.refBrake?.[i] || 0),
        cmpBrk: Math.round(c.cmpBrake?.[i] || 0),
        refGear: c.refGear?.[i] ?? 0,
        cmpGear: c.cmpGear?.[i] ?? 0,
        refSteer: Math.round(c.refSteer?.[i] || 0),
        cmpSteer: Math.round(c.cmpSteer?.[i] || 0),
        cmpTC: !!(c.cmpTc?.[i]),
        cmpABS: !!(c.cmpAbs?.[i]),
      };
    });

    function visibleIndices() {
      const c = props.comp;
      if (!c || c.n < 2) return [0, 1];
      const [z0, z1] = props.zoomRange || [0, 1];
      const i0 = Math.max(0, Math.floor(z0 * (c.n - 1)));
      const i1 = Math.min(c.n - 1, Math.ceil(z1 * (c.n - 1)));
      return [i0, Math.max(i0 + 1, i1)];
    }

    function palette() {
      const dark = isDark();
      return {
        bg: cssVar("--surface", dark ? "#1c2027" : "#ffffff"),
        grid: dark ? "rgba(255,255,255,0.07)" : "rgba(26,31,38,0.08)",
        axis: dark ? "rgba(255,255,255,0.18)" : "rgba(26,31,38,0.22)",
        text: cssVar("--ink-2", dark ? "#a2abb7" : "#5a6472"),
        ink: cssVar("--ink", dark ? "#e7eaee" : "#1a1f26"),
        ref: dark ? "#38bdf8" : "#1f5bd1",
        cmp: dark ? "#fb923c" : "#e4572e",
        gain: dark ? "#22c55e" : "#15803d",
        gainFill: dark ? "rgba(34,197,94,0.22)" : "rgba(22,163,74,0.16)",
        loss: dark ? "#f43f5e" : "#d7263d",
        lossFill: dark ? "rgba(244,63,94,0.22)" : "rgba(215,38,61,0.16)",
        thr: dark ? "#4ade80" : "#16a34a",
        brk: dark ? "#f87171" : "#dc2626",
        tc: "#f59e0b",
        abs: "#ef4444",
        cursor: dark ? "rgba(255,255,255,0.55)" : "rgba(26,31,38,0.45)",
        dragFill: dark ? "rgba(56,189,248,0.18)" : "rgba(31,91,209,0.14)",
      };
    }

    function setupCanvas(cv, height) {
      if (!cv) return null;
      const rect = cv.getBoundingClientRect();
      const w = Math.max(260, Math.floor(rect.width));
      const h = height;
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      if (cv.width !== Math.floor(w * dpr) || cv.height !== Math.floor(h * dpr)) {
        cv.width = Math.floor(w * dpr);
        cv.height = Math.floor(h * dpr);
      }
      const ctx = cv.getContext("2d");
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      return { ctx, w, h, pw: w - PAD_L - PAD_R, ph: h - PAD_T - PAD_B };
    }

    function drawFrame(ctx, w, h, pw, ph, yTicks, yFmt, i0, i1, pal, showXAxis) {
      const c = props.comp;
      ctx.font = '11px "Bahnschrift", "Segoe UI", sans-serif';
      ctx.textBaseline = "middle";

      // Horizontal gridlines & Y labels
      for (const tk of yTicks) {
        const y = PAD_T + ph * (1 - tk.frac);
        ctx.strokeStyle = tk.zero ? pal.axis : pal.grid;
        ctx.lineWidth = tk.zero ? 1.2 : 1;
        ctx.beginPath();
        ctx.moveTo(PAD_L, Math.round(y) + 0.5);
        ctx.lineTo(PAD_L + pw, Math.round(y) + 0.5);
        ctx.stroke();

        ctx.fillStyle = pal.text;
        ctx.textAlign = "right";
        ctx.fillText(yFmt(tk.val), PAD_L - 7, y);
      }

      // Vertical distance gridlines (5-7 clean ticks)
      const d0 = (c.distM?.[i0] || 0) / 1000;
      const d1 = (c.distM?.[i1] || 0) / 1000;
      const spanKm = Math.max(0.05, d1 - d0);
      const rawStep = spanKm / 6;
      const niceSteps = [0.05, 0.1, 0.2, 0.25, 0.5, 1, 2, 2.5, 5];
      const stepKm = niceSteps.find((s) => s >= rawStep) || 5;
      const firstTick = Math.ceil(d0 / stepKm) * stepKm;

      ctx.strokeStyle = pal.grid;
      ctx.lineWidth = 1;
      for (let km = firstTick; km <= d1 + 1e-6; km += stepKm) {
        const frac = (km - d0) / spanKm;
        const x = PAD_L + frac * pw;
        ctx.beginPath();
        ctx.moveTo(Math.round(x) + 0.5, PAD_T);
        ctx.lineTo(Math.round(x) + 0.5, PAD_T + ph);
        ctx.stroke();

        if (showXAxis) {
          ctx.fillStyle = pal.text;
          ctx.textAlign = "center";
          ctx.textBaseline = "top";
          ctx.fillText(stepKm < 0.5 ? km.toFixed(2) + " km" : km.toFixed(1) + " km", x, PAD_T + ph + 5);
        }
      }

      // Section boundary faint markers when zoomed out or partially zoomed
      if (c.sections && c.sections.length > 1) {
        ctx.strokeStyle = pal.grid;
        ctx.setLineDash([2, 3]);
        for (const s of c.sections) {
          if (s.startNorm <= 0 || s.startNorm >= 1) continue;
          const [z0, z1] = props.zoomRange || [0, 1];
          if (s.startNorm > z0 && s.startNorm < z1) {
            const x = PAD_L + ((s.startNorm - z0) / (z1 - z0)) * pw;
            ctx.beginPath();
            ctx.moveTo(x, PAD_T);
            ctx.lineTo(x, PAD_T + ph);
            ctx.stroke();
          }
        }
        ctx.setLineDash([]);
      }
    }

    function drawCursorAndDrag(ctx, pw, ph, i0, i1, pal) {
      const idx = props.cursorIdx;
      if (idx >= i0 && idx <= i1) {
        const x = PAD_L + ((idx - i0) / Math.max(1, i1 - i0)) * pw;
        ctx.strokeStyle = pal.cursor;
        ctx.lineWidth = 1;
        ctx.setLineDash([4, 3]);
        ctx.beginPath();
        ctx.moveTo(Math.round(x) + 0.5, PAD_T);
        ctx.lineTo(Math.round(x) + 0.5, PAD_T + ph);
        ctx.stroke();
        ctx.setLineDash([]);
      }
      if (drag.value.active) {
        const xA = Math.min(drag.value.x0, drag.value.x1);
        const xB = Math.max(drag.value.x0, drag.value.x1);
        ctx.fillStyle = pal.dragFill;
        ctx.fillRect(xA, PAD_T, xB - xA, ph);
      }
    }

    function drawAll() {
      const c = props.comp;
      if (!c || c.n < 2) return;
      const pal = palette();
      const [i0, i1] = visibleIndices();
      const span = Math.max(1, i1 - i0);
      const xOf = (i, pw) => PAD_L + ((i - i0) / span) * pw;

      // 1. TIME DELTA CHART
      {
        const s = setupCanvas(deltaCv.value, 148);
        if (s) {
          const { ctx, w, h, pw, ph } = s;
          let minV = 0, maxV = 0;
          for (let i = i0; i <= i1; i++) {
            const v = c.deltaS[i] || 0;
            if (v < minV) minV = v;
            if (v > maxV) maxV = v;
          }
          const pad = Math.max(0.15, (maxV - minV) * 0.14);
          minV -= pad;
          maxV += pad;
          const yOf = (v) => PAD_T + ph * (1 - (v - minV) / (maxV - minV));
          const yZero = yOf(0);

          const ticks = [
            { val: maxV, frac: 1 },
            { val: 0, frac: (0 - minV) / (maxV - minV), zero: true },
            { val: minV, frac: 0 },
          ];
          if (maxV > 0.6) ticks.push({ val: maxV / 2, frac: (maxV / 2 - minV) / (maxV - minV) });
          if (minV < -0.6) ticks.push({ val: minV / 2, frac: (minV / 2 - minV) / (maxV - minV) });
          drawFrame(ctx, w, h, pw, ph, ticks, (v) => (v > 0.005 ? "+" : v < -0.005 ? "−" : "") + Math.abs(v).toFixed(2) + "s", i0, i1, pal, false);

          // Fill loss region (>0, red) and gain region (<0, green) using clip rects!
          const tracePath = () => {
            ctx.beginPath();
            ctx.moveTo(xOf(i0, pw), yZero);
            for (let i = i0; i <= i1; i++) ctx.lineTo(xOf(i, pw), yOf(c.deltaS[i] || 0));
            ctx.lineTo(xOf(i1, pw), yZero);
            ctx.closePath();
          };
          ctx.save();
          ctx.beginPath();
          ctx.rect(PAD_L, PAD_T, pw, Math.max(0, yZero - PAD_T));
          ctx.clip();
          tracePath();
          ctx.fillStyle = pal.lossFill;
          ctx.fill();
          ctx.restore();

          ctx.save();
          ctx.beginPath();
          ctx.rect(PAD_L, yZero, pw, Math.max(0, PAD_T + ph - yZero));
          ctx.clip();
          tracePath();
          ctx.fillStyle = pal.gainFill;
          ctx.fill();
          ctx.restore();

          // Delta curve colored per segment (green below 0, red above 0)
          ctx.lineWidth = 2;
          ctx.beginPath();
          for (let i = i0; i <= i1; i++) {
            const x = xOf(i, pw), y = yOf(c.deltaS[i] || 0);
            if (i === i0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          }
          ctx.strokeStyle = (c.deltaS[i1] || 0) >= 0 ? pal.loss : pal.gain;
          ctx.stroke();

          drawCursorAndDrag(ctx, pw, ph, i0, i1, pal);
          if (props.cursorIdx >= i0 && props.cursorIdx <= i1) {
            const v = c.deltaS[props.cursorIdx] || 0;
            ctx.fillStyle = v > 0 ? pal.loss : pal.gain;
            ctx.beginPath();
            ctx.arc(xOf(props.cursorIdx, pw), yOf(v), 4, 0, Math.PI * 2);
            ctx.fill();
          }
        }
      }

      // 2. SPEED CHART
      {
        const s = setupCanvas(speedCv.value, 168);
        if (s) {
          const { ctx, w, h, pw, ph } = s;
          let minV = 999, maxV = 0;
          for (let i = i0; i <= i1; i++) {
            const a = c.refSpeed[i] || 0, b = c.cmpSpeed[i] || 0;
            if (a < minV) minV = a;
            if (b < minV) minV = b;
            if (a > maxV) maxV = a;
            if (b > maxV) maxV = b;
          }
          minV = Math.max(0, Math.floor(minV / 25) * 25 - 10);
          maxV = Math.ceil(maxV / 25) * 25 + 10;
          if (maxV - minV < 40) maxV = minV + 40;
          const yOf = (v) => PAD_T + ph * (1 - (v - minV) / (maxV - minV));

          const ticks = [];
          const step = maxV - minV > 160 ? 50 : 25;
          for (let v = Math.ceil(minV / step) * step; v <= maxV; v += step) {
            ticks.push({ val: v, frac: (v - minV) / (maxV - minV) });
          }
          drawFrame(ctx, w, h, pw, ph, ticks, (v) => Math.round(v) + "", i0, i1, pal, false);

          // Subtle speed advantage shading between Ref and Cmp
          for (let i = i0; i < i1; i++) {
            const r0 = c.refSpeed[i] || 0, r1 = c.refSpeed[i + 1] || 0;
            const c0 = c.cmpSpeed[i] || 0, c1 = c.cmpSpeed[i + 1] || 0;
            if (Math.abs(c0 - r0) < 1.2 && Math.abs(c1 - r1) < 1.2) continue;
            ctx.fillStyle = (c0 + c1) >= (r0 + r1) ? pal.gainFill : pal.lossFill;
            ctx.beginPath();
            ctx.moveTo(xOf(i, pw), yOf(r0));
            ctx.lineTo(xOf(i + 1, pw), yOf(r1));
            ctx.lineTo(xOf(i + 1, pw), yOf(c1));
            ctx.lineTo(xOf(i, pw), yOf(c0));
            ctx.closePath();
            ctx.fill();
          }

          // Ref speed line (blue)
          ctx.strokeStyle = pal.ref;
          ctx.lineWidth = 1.9;
          ctx.beginPath();
          for (let i = i0; i <= i1; i++) {
            const x = xOf(i, pw), y = yOf(c.refSpeed[i] || 0);
            if (i === i0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
          }
          ctx.stroke();

          // Cmp speed line (orange)
          ctx.strokeStyle = pal.cmp;
          ctx.lineWidth = 2.0;
          ctx.beginPath();
          for (let i = i0; i <= i1; i++) {
            const x = xOf(i, pw), y = yOf(c.cmpSpeed[i] || 0);
            if (i === i0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
          }
          ctx.stroke();

          drawCursorAndDrag(ctx, pw, ph, i0, i1, pal);
          if (props.cursorIdx >= i0 && props.cursorIdx <= i1) {
            const x = xOf(props.cursorIdx, pw);
            ctx.fillStyle = pal.ref;
            ctx.beginPath(); ctx.arc(x, yOf(c.refSpeed[props.cursorIdx] || 0), 3.5, 0, Math.PI * 2); ctx.fill();
            ctx.fillStyle = pal.cmp;
            ctx.beginPath(); ctx.arc(x, yOf(c.cmpSpeed[props.cursorIdx] || 0), 3.5, 0, Math.PI * 2); ctx.fill();
          }
        }
      }

      // 3. THROTTLE & BRAKE CHART
      {
        const s = setupCanvas(pedalCv.value, 136);
        if (s) {
          const { ctx, w, h, pw, ph } = s;
          const yOf = (v) => PAD_T + ph * (1 - clamp(v, 0, 100) / 100);
          const ticks = [
            { val: 100, frac: 1 },
            { val: 50, frac: 0.5 },
            { val: 0, frac: 0, zero: true },
          ];
          drawFrame(ctx, w, h, pw, ph, ticks, (v) => v + "%", i0, i1, pal, !showSteer.value);

          const drawLine = (arr, color, width, dash) => {
            ctx.strokeStyle = color;
            ctx.lineWidth = width;
            ctx.setLineDash(dash);
            ctx.beginPath();
            for (let i = i0; i <= i1; i++) {
              const x = xOf(i, pw), y = yOf(arr[i] || 0);
              if (i === i0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            }
            ctx.stroke();
            ctx.setLineDash([]);
          };

          // Reference traces dashed (subtle), Comparison traces crisp solid
          drawLine(c.refThrottle, pal.thr, 1.35, [4, 3]);
          drawLine(c.refBrake, pal.brk, 1.35, [4, 3]);
          drawLine(c.cmpThrottle, pal.thr, 2.0, []);
          drawLine(c.cmpBrake, pal.brk, 2.0, []);

          drawCursorAndDrag(ctx, pw, ph, i0, i1, pal);
        }
      }

      // 4. STEERING + GEAR + TC/ABS CHART
      if (showSteer.value) {
        const s = setupCanvas(steerCv.value, 146);
        if (s) {
          const { ctx, w, h, pw, ph } = s;
          const gearStripH = 22;
          const steerH = ph - gearStripH - 6;

          let maxAng = 45;
          for (let i = i0; i <= i1; i++) {
            const a = Math.abs(c.refSteer[i] || 0);
            const b = Math.abs(c.cmpSteer[i] || 0);
            if (a > maxAng) maxAng = a;
            if (b > maxAng) maxAng = b;
          }
          maxAng = Math.ceil(maxAng / 30) * 30;
          const yOf = (v) => PAD_T + steerH * (1 - (v + maxAng) / (2 * maxAng));

          const ticks = [
            { val: maxAng, frac: 1 - (gearStripH + 6) / ph },
            { val: 0, frac: 1 - (steerH / 2 + gearStripH + 6) / ph, zero: true },
            { val: -maxAng, frac: (gearStripH + 6) / ph },
          ];
          // Draw crisp steering grid & distance X-axis
          ctx.font = '11px "Bahnschrift", "Segoe UI", sans-serif';
          ctx.textBaseline = "middle";
          for (const v of [maxAng, 0, -maxAng]) {
            const y = yOf(v);
            ctx.strokeStyle = v === 0 ? pal.axis : pal.grid;
            ctx.lineWidth = v === 0 ? 1.2 : 1;
            ctx.beginPath(); ctx.moveTo(PAD_L, Math.round(y) + 0.5); ctx.lineTo(PAD_L + pw, Math.round(y) + 0.5); ctx.stroke();
            ctx.fillStyle = pal.text; ctx.textAlign = "right";
            ctx.fillText((v > 0 ? "+" : "") + v + "°", PAD_L - 7, y);
          }

          // Distance X-axis ticks on bottom panel
          drawFrame(ctx, w, h, pw, ph, [], () => "", i0, i1, pal, true);

          // Ref & Cmp steering curves
          const drawSteer = (arr, color, width) => {
            ctx.strokeStyle = color;
            ctx.lineWidth = width;
            ctx.beginPath();
            for (let i = i0; i <= i1; i++) {
              const x = xOf(i, pw), y = yOf(arr[i] || 0);
              if (i === i0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            }
            ctx.stroke();
          };
          drawSteer(c.refSteer, pal.ref, 1.5);
          drawSteer(c.cmpSteer, pal.cmp, 1.9);

          // Gear strip + TC / ABS markers along the bottom band
          const stripY = PAD_T + steerH + 6;
          ctx.fillStyle = pal.grid;
          ctx.fillRect(PAD_L, stripY, pw, gearStripH);
          ctx.fillStyle = pal.text;
          ctx.textAlign = "right";
          ctx.textBaseline = "middle";
          ctx.fillText("Gear", PAD_L - 8, stripY + gearStripH / 2);

          // Draw gear change blocks for comparison lap
          let segStart = i0;
          for (let i = i0 + 1; i <= i1 + 1; i++) {
            const curG = i <= i1 ? (c.cmpGear[i] || 0) : -99;
            const prevG = c.cmpGear[segStart] || 0;
            if (curG !== prevG) {
              const x0 = xOf(segStart, pw);
              const x1 = xOf(Math.min(i, i1), pw);
              if (x1 - x0 > 18 && prevG > 0) {
                ctx.fillStyle = pal.text;
                ctx.textAlign = "center";
                ctx.fillText("G" + prevG, (x0 + x1) / 2, stripY + gearStripH / 2);
              }
              if (i <= i1) {
                ctx.strokeStyle = pal.axis;
                ctx.lineWidth = 1;
                ctx.beginPath(); ctx.moveTo(x1, stripY); ctx.lineTo(x1, stripY + gearStripH); ctx.stroke();
              }
              segStart = i;
            }
          }

          // TC (amber top ticks) & ABS (red bottom ticks) on gear strip
          for (let i = i0; i <= i1; i++) {
            const x = xOf(i, pw);
            if (c.cmpTc?.[i]) {
              ctx.fillStyle = pal.tc;
              ctx.fillRect(x - 1, stripY, 2.5, 5);
            }
            if (c.cmpAbs?.[i]) {
              ctx.fillStyle = pal.abs;
              ctx.fillRect(x - 1, stripY + gearStripH - 5, 2.5, 5);
            }
          }

          drawCursorAndDrag(ctx, pw, ph, i0, i1, pal);
        }
      }
    }

    function eventToIdx(e) {
      const cv = e.currentTarget;
      const c = props.comp;
      if (!cv || !c) return { idx: -1, x: 0, norm: 0 };
      const rect = cv.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const pw = Math.max(1, rect.width - PAD_L - PAD_R);
      const frac = clamp((x - PAD_L) / pw, 0, 1);
      const [z0, z1] = props.zoomRange || [0, 1];
      const norm = z0 + frac * (z1 - z0);
      const idx = Math.round(norm * (c.n - 1));
      return { idx: clamp(idx, 0, c.n - 1), x: clamp(x, PAD_L, rect.width - PAD_R), norm };
    }

    function onMouseDown(e) {
      if (e.button !== 0) return;
      const p = eventToIdx(e);
      drag.value = { active: true, x0: p.x, x1: p.x, norm0: p.norm, norm1: p.norm };
    }

    function onMouseMove(e) {
      const p = eventToIdx(e);
      emit("update:cursorIdx", p.idx);
      if (drag.value.active) {
        drag.value.x1 = p.x;
        drag.value.norm1 = p.norm;
        drawAll();
      }
    }

    function onMouseUp() {
      if (!drag.value.active) return;
      const { x0, x1, norm0, norm1 } = drag.value;
      drag.value.active = false;
      if (Math.abs(x1 - x0) >= 14) {
        const a = Math.min(norm0, norm1);
        const b = Math.max(norm0, norm1);
        if (b - a >= 0.008) emit("update:zoomRange", [a, b]);
      }
      drawAll();
    }

    function onWheel(e) {
      e.preventDefault();
      const p = eventToIdx(e);
      const [z0, z1] = props.zoomRange || [0, 1];
      const span = z1 - z0;
      const factor = e.deltaY > 0 ? 1.32 : 0.72;
      const nextSpan = clamp(span * factor, 0.015, 1.0);
      const anchor = (p.norm - z0) / Math.max(1e-6, span);
      let n0 = p.norm - anchor * nextSpan;
      let n1 = n0 + nextSpan;
      if (n0 < 0) { n1 -= n0; n0 = 0; }
      if (n1 > 1) { n0 -= (n1 - 1); n1 = 1; }
      emit("update:zoomRange", [clamp(n0, 0, 1), clamp(n1, 0, 1)]);
    }

    function resetZoom() {
      emit("update:zoomRange", [0, 1]);
    }

    function fmtDelta(v) {
      const sign = v > 0.0005 ? "+" : v < -0.0005 ? "−" : "";
      return sign + Math.abs(v).toFixed(3) + " s";
    }

    let ro = null;
    onMounted(() => {
      nextTick(drawAll);
      if (window.ResizeObserver && wrapRef.value) {
        ro = new ResizeObserver(() => drawAll());
        ro.observe(wrapRef.value);
      }
      window.addEventListener("mouseup", onMouseUp);
    });
    onBeforeUnmount(() => {
      if (ro) ro.disconnect();
      window.removeEventListener("mouseup", onMouseUp);
    });

    watch(() => [props.comp, props.cursorIdx, props.zoomRange, showSteer.value], () => nextTick(drawAll), { deep: true });

    return {
      wrapRef, deltaCv, speedCv, pedalCv, steerCv, showSteer,
      isZoomed, zoomInfo, readout,
      onMouseDown, onMouseMove, onWheel, resetZoom, fmtDelta,
    };
  },
  template: `
    <section ref="wrapRef" class="panel tele-charts-card" aria-label="Telemetry traces">
      <div class="tele-card-head">
        <div>
          <h2 class="panel-title">Telemetry traces</h2>
          <p class="tele-card-sub">{{ zoomInfo }} · Drag horizontally or scroll to zoom, double-click to reset</p>
        </div>
        <div class="tele-chart-controls">
          <label class="tele-check"><input type="checkbox" v-model="showSteer"> Steer &amp; Gear</label>
          <button v-if="isZoomed" type="button" class="btn btn-sm" @click="resetZoom">Reset full lap</button>
        </div>
      </div>

      <!-- Sticky live cursor readout bar -->
      <div class="tele-readout" :class="{ empty: !readout }">
        <template v-if="readout">
          <div class="tele-ro-item tele-ro-loc">
            <span class="tele-ro-lbl">Distance</span>
            <strong>{{ readout.distKm }} km</strong>
            <span v-if="readout.section" class="tele-ro-sec">{{ readout.section }}</span>
          </div>
          <div class="tele-ro-item">
            <span class="tele-ro-lbl">Delta</span>
            <strong :class="readout.delta < -0.005 ? 'is-gain' : readout.delta > 0.005 ? 'is-loss' : ''">{{ fmtDelta(readout.delta) }}</strong>
          </div>
          <div class="tele-ro-item">
            <span class="tele-ro-lbl">Speed</span>
            <strong><span class="c-cmp">{{ readout.cmpSpd }}</span> <span class="muted">/</span> <span class="c-ref">{{ readout.refSpd }}</span> <small>km/h ({{ readout.spdDiff >= 0 ? '+' : '−' }}{{ Math.abs(readout.spdDiff) }})</small></strong>
          </div>
          <div class="tele-ro-item">
            <span class="tele-ro-lbl">Throttle</span>
            <strong>{{ readout.cmpThr }}% <small class="muted">(ref {{ readout.refThr }}%)</small></strong>
          </div>
          <div class="tele-ro-item">
            <span class="tele-ro-lbl">Brake</span>
            <strong>{{ readout.cmpBrk }}% <small class="muted">(ref {{ readout.refBrk }}%)</small></strong>
          </div>
          <div class="tele-ro-item">
            <span class="tele-ro-lbl">Gear / Steer</span>
            <strong>G{{ readout.cmpGear }} <small class="muted">(G{{ readout.refGear }})</small> · {{ readout.cmpSteer }}°</strong>
          </div>
          <div v-if="readout.cmpTC || readout.cmpABS" class="tele-ro-badges">
            <span v-if="readout.cmpTC" class="tele-flag tele-flag-tc">TC</span>
            <span v-if="readout.cmpABS" class="tele-flag tele-flag-abs">ABS</span>
          </div>
        </template>
        <span v-else class="muted-small">Move cursor over any chart or the track map to inspect exact values at every metre.</span>
      </div>

      <!-- Panel 1: Time Delta -->
      <div class="tele-panel-block">
        <div class="tele-panel-bar">
          <span class="tele-panel-name">Time delta (s)</span>
          <span class="tele-panel-leg">
            <i class="sw sw-gain"></i> Faster than ref (&lt; 0)
            <i class="sw sw-loss"></i> Slower than ref (&gt; 0)
          </span>
        </div>
        <canvas ref="deltaCv" class="tele-cv" style="height:148px"
          @mousedown="onMouseDown" @mousemove="onMouseMove" @mouseleave="$emit('update:cursorIdx', -1)"
          @wheel="onWheel" @dblclick="resetZoom"></canvas>
      </div>

      <!-- Panel 2: Speed -->
      <div class="tele-panel-block">
        <div class="tele-panel-bar">
          <span class="tele-panel-name">Speed (km/h)</span>
          <span class="tele-panel-leg">
            <i class="sw sw-ref"></i> Reference
            <i class="sw sw-cmp"></i> Comparison
          </span>
        </div>
        <canvas ref="speedCv" class="tele-cv" style="height:168px"
          @mousedown="onMouseDown" @mousemove="onMouseMove" @mouseleave="$emit('update:cursorIdx', -1)"
          @wheel="onWheel" @dblclick="resetZoom"></canvas>
      </div>

      <!-- Panel 3: Throttle & Brake -->
      <div class="tele-panel-block">
        <div class="tele-panel-bar">
          <span class="tele-panel-name">Throttle &amp; Brake (%)</span>
          <span class="tele-panel-leg">
            <i class="sw sw-thr"></i> Throttle
            <i class="sw sw-brk"></i> Brake
            <span class="muted-small">(solid = cmp, dashed = ref)</span>
          </span>
        </div>
        <canvas ref="pedalCv" class="tele-cv" style="height:136px"
          @mousedown="onMouseDown" @mousemove="onMouseMove" @mouseleave="$emit('update:cursorIdx', -1)"
          @wheel="onWheel" @dblclick="resetZoom"></canvas>
      </div>

      <!-- Panel 4: Steering, Gear & TC/ABS -->
      <div v-if="showSteer" class="tele-panel-block">
        <div class="tele-panel-bar">
          <span class="tele-panel-name">Steering (°) · Gear · TC / ABS</span>
          <span class="tele-panel-leg">
            <i class="sw sw-ref"></i> Ref steer
            <i class="sw sw-cmp"></i> Cmp steer
            <i class="sw sw-tc"></i> TC
            <i class="sw sw-abs"></i> ABS
          </span>
        </div>
        <canvas ref="steerCv" class="tele-cv" style="height:146px"
          @mousedown="onMouseDown" @mousemove="onMouseMove" @mouseleave="$emit('update:cursorIdx', -1)"
          @wheel="onWheel" @dblclick="resetZoom"></canvas>
      </div>
    </section>
  `,
};
