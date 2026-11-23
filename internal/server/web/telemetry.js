// Telemetry helpers (spec D-159, D-164): lap distance, series along the lap, time difference between
// laps. Laps are compared along the distance from the line; every lap is stretched to the length
// of the first (reference) lap, so the same corner lines up even when one lap took a wider line.

// Colours of the laps on the analysis page, read from the theme (--lap-0 … --lap-3).
export const MAX_LAPS = 4;
export function lapColors() {
  const cs = getComputedStyle(document.documentElement);
  return [0, 1, 2, 3].map((i) => cs.getPropertyValue(`--lap-${i}`).trim() || "#888");
}

// A lap's distance as a function of time, and back. d = lap data from the API.
// The speed samples can stop up to one sample (~17 ms) short of either end of the lap: past the
// first and last sample the line carries on at the speed of the end segment, so the time
// difference at the line equals the lap-time difference exactly.
export function lapTrack(d) {
  const dist = d.dist || [];
  const hz = d.distHz || 1;
  const t0 = d.distT0 || 0;
  const lapS = d.lapMs / 1000;
  const n = dist.length;
  const v0 = n > 1 ? dist[1] - dist[0] : 0; // metres per sample at each end
  const v1 = n > 1 ? dist[n - 1] - dist[n - 2] : 0;
  function distAt(t) {
    if (!n) return 0;
    const f = (t - t0) * hz;
    if (f <= 0) return dist[0] + v0 * f;
    const i = Math.floor(f);
    if (i >= n - 1) return dist[n - 1] + v1 * (f - (n - 1));
    return dist[i] + (dist[i + 1] - dist[i]) * (f - i);
  }
  function timeAt(x) {
    if (!n) return 0;
    let lo = 0, hi = n - 1;
    if (x <= dist[0]) return t0 + (v0 > 0 ? (x - dist[0]) / v0 : 0) / hz;
    if (x >= dist[hi]) return t0 + (hi + (v1 > 0 ? (x - dist[hi]) / v1 : 0)) / hz;
    while (hi - lo > 1) {
      const mid = (lo + hi) >> 1;
      if (dist[mid] <= x) lo = mid; else hi = mid;
    }
    const span = dist[hi] - dist[lo];
    return t0 + (lo + (span > 0 ? (x - dist[lo]) / span : 0)) / hz;
  }
  return { distAt, timeAt, total: Math.max(1, distAt(lapS)), lapS };
}

// series turns one channel of one lap into x (metres along the reference lap) and y arrays.
export function series(cd, track, align) {
  const map = typeof align === "function" ? align : (v) => v * (align || 1);
  const n = cd.data.length;
  const xs = new Float32Array(n), ys = new Float32Array(n);
  let k = 0;
  for (let i = 0; i < n; i++) {
    const t = cd.t ? cd.t[i] : cd.t0 + i / cd.hz;
    if (t < -0.05 || t > track.lapS + 0.05) continue;
    xs[k] = map(track.distAt(t));
    ys[k] = cd.data[i];
    k++;
  }
  return { x: xs.subarray(0, k), y: ys.subarray(0, k) };
}

// delta: time difference of a lap against the reference lap along the distance, every few metres
// (positive = slower than the reference).
export function delta(ref, lap, al, step = 4) {
  const n = Math.max(2, Math.floor(ref.total / step));
  const xs = new Float32Array(n), ys = new Float32Array(n);
  for (let i = 0; i < n; i++) {
    const x = (i / (n - 1)) * ref.total;
    xs[i] = x;
    ys[i] = lap.timeAt(al.fromRef(x)) - ref.timeAt(x);
  }
  return { x: xs, y: ys };
}

// valueAt: the value of a series at distance x, in between samples; step = a value that jumps
// (gear, levels): the sample before x, so a gear never reads 3.34.
export function valueAt(s, x, step = false) {
  if (!s || !s.x.length) return null;
  let lo = 0, hi = s.x.length - 1;
  if (x <= s.x[0]) return s.y[0];
  if (x >= s.x[hi]) return s.y[hi];
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (s.x[mid] <= x) lo = mid; else hi = mid;
  }
  if (step) return s.y[lo];
  const span = s.x[hi] - s.x[lo];
  return s.y[lo] + (span > 0 ? (s.y[hi] - s.y[lo]) * (x - s.x[lo]) / span : 0);
}

// Number formatting for read-outs: enough decimals for the size of the value.
// step: a value that jumps (gear, levels): a whole number. Minus is the typographic "−".
export function fmt(v, unit, step = false) {
  if (v === null || v === undefined || !isFinite(v)) return "–";
  if (step) return String(Math.round(v)).replace("-", "−");
  if (Math.abs(v) < 5e-4) return "0";
  const a = Math.abs(v);
  // decimals by the size the value rounds to (so 9.996 is "10.0", not "10.00")
  const d = unit === "rpm" || unit === "%" || a >= 999.5 ? 0 : a >= 9.995 ? 1 : a >= 0.9995 ? 2 : 3;
  return v.toFixed(d).replace("-", "−");
}

export function signed(s) {
  if (s === null || !isFinite(s)) return "–";
  if (Math.abs(s) < 0.0005) return "±0.000";
  return (s > 0 ? "+" : "−") + Math.abs(s).toFixed(3);
}

// Laps on the analysis page are addressed as "fileId:n".
export const lapKey = (l) => `${l.fileId}:${l.n}`;
export function parseLaps(q) {
  const seen = new Set();
  return String(q || "").split(",").map((p) => p.split(":").map(Number))
    .filter(([f, n]) => Number.isInteger(f) && Number.isInteger(n) && f > 0 && n >= 0)
    .filter(([f, n]) => !seen.has(f + ":" + n) && seen.add(f + ":" + n)) // the same lap once
    .slice(0, MAX_LAPS).map(([fileId, n]) => ({ fileId, n }));
}

export function bytes(n) {
  if (!n) return "0 MB";
  if (n >= 1e9) return (n / 1e9).toFixed(n >= 1e10 ? 0 : 1) + " GB";
  return Math.max(1, Math.round(n / 1e6)) + " MB";
}

// duration: a session's length in words ("8 min", "2 h 06 min"), so it never reads like a lap time.
export function duration(ms) {
  const m = Math.max(1, Math.round(ms / 60000));
  const h = Math.floor(m / 60);
  return h ? `${h} h ${String(m % 60).padStart(2, "0")} min` : `${m} min`;
}

// Channel sets of the analysis presets (D-159: driver, tyres, brakes, suspension, any channel).
export const PRESETS = [
  { id: "driver", label: "Driver", channels: ["SPEED", "THROTTLE", "BRAKE", "STEERANGLE", "GEAR", "RPMS"] },
  { id: "tyres", label: "Tyres", channels: ["SPEED", "TYRE_PRESS_LF", "TYRE_PRESS_RF", "TYRE_PRESS_LR", "TYRE_PRESS_RR", "TYRE_TAIR_LF", "TYRE_TAIR_RF", "TYRE_TAIR_LR", "TYRE_TAIR_RR", "x:SLIP_ANGLE_FL", "x:SLIP_ANGLE_FR", "x:SLIP_ANGLE_RL", "x:SLIP_ANGLE_RR"] },
  { id: "brakes", label: "Brakes", channels: ["SPEED", "BRAKE", "ABS", "x:ABS_ACTIVE", "BRAKE_TEMP_LF", "BRAKE_TEMP_RF", "BRAKE_TEMP_LR", "BRAKE_TEMP_RR", "x:BRAKE_PRESS_FL", "x:BRAKE_PRESS_RL"] },
  { id: "suspension", label: "Suspension", channels: ["SPEED", "SUS_TRAVEL_LF", "SUS_TRAVEL_RF", "SUS_TRAVEL_LR", "SUS_TRAVEL_RR", "x:RIDE_HEIGHT_F", "x:RIDE_HEIGHT_R", "x:PITCH", "x:ROLL"] },
  { id: "custom", label: "Custom", channels: [] },
];

// Car view: per-wheel values at the cursor and averaged over each lap.
export const CAR_FAMILIES = [
  { base: "TYRE_PRESS", label: "Tyre pressure", unit: "psi", lo: 25, hi: 29 }, // blue below, red above the usual hot target of 27 psi
  { base: "TYRE_TAIR", label: "Tyre temperature", unit: "°C", lo: 60, hi: 100 },
  { base: "BRAKE_TEMP", label: "Brake temperature", unit: "°C", lo: 150, hi: 750 },
];
export const WHEELS = [["FL", "LF"], ["FR", "RF"], ["RL", "LR"], ["RR", "RR"]];

// Session kinds as ACC names them.
export const KINDS = { race: "Race", qualifying: "Qualifying", practice: "Practice", hotlap: "Hotlap", hotstint: "Hotstint", superpole: "Superpole", "time attack": "Time attack" };
export const kindLabel = (k) => KINDS[k] || "Session";

// Class colour of a car (GT3, GT4 …), as on the rest of SetupMan.
export const classCls = (c) => "cls-" + (["gt3", "gt4", "gt2", "cup", "tcx"].includes(String(c || "").toLowerCase()) ? String(c).toLowerCase() : "other");

// SVG path of a track outline from the API ({x, y, w, h}, 0 … 1000).
export function outlinePath(o) {
  if (!o || !o.x || o.x.length < 3) return "";
  let d = "M" + o.x[0] + " " + o.y[0];
  for (let i = 1; i < o.x.length; i++) d += "L" + o.x[i] + " " + o.y[i];
  return d + "Z";
}

// When a session took place, in words a driver uses: "Today 20:00", "Yesterday", "Tue 17 Nov".
export function when(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const day = new Date(d); day.setHours(0, 0, 0, 0);
  const diff = Math.round((today - day) / 86400000);
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (diff === 0) return "Today " + time;
  if (diff === 1) return "Yesterday " + time;
  const sameYear = d.getFullYear() === today.getFullYear();
  return d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short", ...(sameYear ? {} : { year: "numeric" }) }) + " " + time;
}
// dateLabel: the day without the time ("Today", "Tue, Oct 13", with the year when not this year).
export function dateLabel(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const day = new Date(d); day.setHours(0, 0, 0, 0);
  const diff = Math.round((today - day) / 86400000);
  if (diff === 0) return "today";
  if (diff === 1) return "yesterday";
  return d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short", year: d.getFullYear() === today.getFullYear() ? undefined : "numeric" });
}

export function dayLabel(iso) {
  const d = new Date(iso);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const day = new Date(d); day.setHours(0, 0, 0, 0);
  const diff = Math.round((today - day) / 86400000);
  if (diff === 0) return "Today";
  if (diff === 1) return "Yesterday";
  return d.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long", year: d.getFullYear() === today.getFullYear() ? undefined : "numeric" });
}

// ---------- lining laps up (D-164) ----------
// A lap is stretched evenly to the length of the first lap. Lining laps up on every corner's apex
// instead was measured on the user's laps (8 laps each at Spa, Donington, Monza and Kyalami): the
// time difference did not get smoother (Spa and Monza rougher, Donington and Kyalami a little
// smoother), because most of its ups and downs are real driving differences. With the extra log's
// car position (D-162) laps could later be lined up by position.
export function aligner(ref, lap) {
  const k = ref.total / lap.total;
  return { toRef: (v) => v * k, fromRef: (v) => v / k, anchors: 0 };
}
export const sameLap = { toRef: (v) => v, fromRef: (v) => v, anchors: 0 };
