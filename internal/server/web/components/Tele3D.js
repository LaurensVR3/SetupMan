// 3D track view (spec D-190): every lap's own driving line on the real, hilly track, with a wall on
// it whose height is one channel and colour another. The graph cursor moves a marker per lap; the
// camera is free (drag to rotate, right-drag to pan, wheel to zoom) and never moves by itself.
// Photos: MapTiler satellite with the user's own key (loaded by the browser, never stored), or the
// offline government aerial photos SetupMan downloaded. Drawn only when something changes (D-13).
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, markRaw } from "vue";
import { get, post, api, enc } from "../api.js";
import { valueAt, bytes } from "../telemetry.js";

const R = 6378137;
const BAND = 300; // photos for this far around the track (as the offline set)
const MAPTILER = "https://api.maptiler.com/tiles/satellite-v2/{z}/{x}/{y}.jpg?key=";
export const PEDALS = "@pedals"; // colour: throttle green / brake red
export const LAPCOL = "@lap";     // colour: the lap's own colour (default)
// wall heights as fractions of the wall size: a low wall for the lowest value, the top bands
const W_MIN = 0.06, W_BAND = 0.07, W_EDGE = 0.025;
const wallTop = (k) => W_MIN + Math.max(0, Math.min(1, k));

// ---- small helpers ----
function css(el, name) { return getComputedStyle(el).getPropertyValue(name).trim(); }
function mercTile(lat, lon, z) {
  const n = 2 ** z, s = Math.sin((lat * Math.PI) / 180);
  return [((lon + 180) / 360) * n, (0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)) * n];
}
function tileLatLon(z, tx, ty) {
  const n = 2 ** z;
  return [(Math.atan(Math.sinh(Math.PI * (1 - (2 * ty) / n))) * 180) / Math.PI, (tx / n) * 360 - 180];
}
// position along a path at distance d (Catmull-Rom through the recorded points)
function pathAt(p, d) {
  const D = p.dist, n = D.length;
  if (!n) return null;
  if (d <= D[0]) return [p.e[0], p.h[0], -p.n[0]];
  if (d >= D[n - 1]) return [p.e[n - 1], p.h[n - 1], -p.n[n - 1]];
  let lo = 0, hi = n - 1;
  while (hi - lo > 1) { const m = (lo + hi) >> 1; if (D[m] <= d) lo = m; else hi = m; }
  const span = D[hi] - D[lo], t = span > 0 ? (d - D[lo]) / span : 0;
  const i0 = Math.max(0, lo - 1), i3 = Math.min(n - 1, hi + 1);
  const cr = (a, b, c, e) => {
    const t2 = t * t, t3 = t2 * t;
    return 0.5 * (2 * b + (-a + c) * t + (2 * a - 5 * b + 4 * c - e) * t2 + (-a + 3 * b - 3 * c + e) * t3);
  };
  return [cr(p.e[i0], p.e[lo], p.e[hi], p.e[i3]), cr(p.h[i0], p.h[lo], p.h[hi], p.h[i3]), -cr(p.n[i0], p.n[lo], p.n[hi], p.n[i3])];
}
// keep only points whose distance goes up, so the search along the lap works
function increasing(p) {
  const keep = [];
  for (let i = 0; i < p.dist.length; i++) if (!keep.length || p.dist[i] >= p.dist[keep[keep.length - 1]]) keep.push(i);
  if (keep.length === p.dist.length) return p;
  const pick = (a) => keep.map((i) => a[i]);
  return { ...p, e: pick(p.e), n: pick(p.n), h: pick(p.h), dist: pick(p.dist) };
}
function terrainAt(t, e, n) {
  const x = Math.max(0, Math.min(t.cols - 1.000001, (e - t.e0) / t.step));
  const y = Math.max(0, Math.min(t.rows - 1.000001, (n - t.n0) / t.step));
  const i = Math.floor(x), j = Math.floor(y), fx = x - i, fy = y - j, H = t.h, c = t.cols;
  return (H[j * c + i] * (1 - fx) + H[j * c + i + 1] * fx) * (1 - fy) + (H[(j + 1) * c + i] * (1 - fx) + H[(j + 1) * c + i + 1] * fx) * fy;
}
// colour ramp for a channel: blue → teal → yellow → orange → red
const RAMP = [[0.15, 0.35, 0.85], [0.1, 0.65, 0.65], [0.95, 0.8, 0.15], [0.95, 0.5, 0.1], [0.85, 0.15, 0.15]];
function ramp(k) {
  k = Math.max(0, Math.min(1, k)) * (RAMP.length - 1);
  const i = Math.min(RAMP.length - 2, Math.floor(k)), f = k - i, a = RAMP[i], b = RAMP[i + 1];
  return [a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f, a[2] + (b[2] - a[2]) * f];
}
function pedalColour(th, br) {
  if (br !== null && br > 3) { const k = Math.min(1, br / 80); return [0.55 + 0.4 * k, 0.2 - 0.1 * k, 0.2 - 0.1 * k]; }
  const k = th === null ? 0 : Math.min(1, th / 100);
  return [0.55 - 0.45 * k, 0.55 + 0.25 * k, 0.55 - 0.35 * k]; // grey when coasting → green on full throttle
}
function range(list) {
  let lo = Infinity, hi = -Infinity;
  for (const s of list) if (s) for (const v of s.y) { if (v < lo) lo = v; if (v > hi) hi = v; }
  if (!isFinite(lo)) return [0, 1];
  if (lo > 0 && lo < hi * 0.5) lo = 0; // speed, pressures …: from zero, so heights compare
  return [lo, hi > lo ? hi : lo + 1];
}

export const Tele3D = {
  props: {
    game: String,
    track: String,
    view: Object,     // shared with the graphs: cursor, from, to, total
    laps: Array,      // [{ key, fileId, n, color, label, fromRef(d) → own distance, h: series, c: series, th, br }]
    channels: Array,  // [{ name, label, unit }] for the pickers
    wall: Object,     // { h, c, size } chosen channels (owned by the page, which loads them)
    corners: Array,   // [{ n, dist, name }] turn labels
  },
  setup(props) {
    const box = ref(null), canvasBox = ref(null);
    const scene = ref(null), error = ref(""), loading = ref(true);
    const paths = reactive({}); // lap key → path (or null while loading)
    const photo = ref("none");  // "maptiler", "offline" or "none"
    const online = ref(navigator.onLine);
    const labels = ref([]);
    let THREE = null, Orbit = null, renderer = null, cam = null, world = null, controls = null, ro = null, raf = 0;
    let groups = { ground: null, laps: null, markers: null };
    let photoTextures = [];
    // wall size in metres, from the camera distance; one uniform shared by every wall
    const wallSize = { value: 40 };
    function liftByWallSize(shader) {
      shader.uniforms.uWall = wallSize;
      shader.vertexShader = ["attribute float wallF;", "uniform float uWall;", shader.vertexShader.replace("#include <begin_vertex>", ["#include <begin_vertex>", "transformed.y += wallF * uWall;"].join(String.fromCharCode(10)))].join(String.fromCharCode(10));
    }
    function sizeWalls() {
      if (!controls) return;
      const s = Math.max(6, Math.min(320, cam.position.distanceTo(controls.target) * 0.1));
      if (Math.abs(s - wallSize.value) / wallSize.value < 0.02) return;
      wallSize.value = s;
      if (props.view.cursor !== null) buildMarkers();
    }
    const disposeList = [];
    const own = (o) => { disposeList.push(o); return o; };

    // ---- data ----
    async function loadScene() {
      loading.value = true;
      try {
        const s = await get(`/api/telemetry/3d/${enc(props.game)}/${enc(props.track)}`);
        scene.value = markRaw(s);
        const done = s.photos?.source && s.photos.have >= s.photos.tiles && s.photos.tiles > 0;
        photo.value = s.mapTilerKey && online.value ? "maptiler" : done ? "offline" : "none";
        error.value = "";
      } catch (e) { error.value = e.message; }
      finally { loading.value = false; }
    }
    async function loadPaths() {
      for (const l of props.laps) {
        if (l.key in paths) continue;
        paths[l.key] = null;
        try { paths[l.key] = markRaw(increasing(await get(`/api/telemetry/files/${l.fileId}/laps/${l.n}/path`))); }
        catch (e) { error.value = e.message; }
      }
    }
    // a lap with the car position can place the track anew (its first such lap): then the scene and
    // every path are fetched again so they share one placement
    async function followPlacement() {
      if (!scene.value || !props.laps.some((l) => paths[l.key] && paths[l.key].scene !== scene.value.source)) return;
      await loadScene();
      for (const k of Object.keys(paths)) delete paths[k];
      await loadPaths();
      if (THREE && renderer) buildGround();
    }
    const exactness = computed(() => props.laps.map((l) => ({ color: l.color, label: l.label, exact: paths[l.key]?.exact })));

    // ---- drawing ----
    function render() {
      raf = 0;
      if (!renderer) return;
      for (const m of groups.markers.children) if (m.userData.marker) m.scale.setScalar(Math.max(1.6, m.position.distanceTo(cam.position) * 0.0125));
      renderer.render(world, cam);
      placeLabels();
    }
    const redraw = () => { if (!raf) raf = requestAnimationFrame(render); };

    function setup3D() {
      const el = canvasBox.value;
      renderer = new THREE.WebGLRenderer({ antialias: true });
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
      el.appendChild(renderer.domElement);
      world = new THREE.Scene();
      cam = new THREE.PerspectiveCamera(45, 1, 2, 40000);
      world.add(new THREE.HemisphereLight(0xffffff, 0x6a7060, 1.6));
      const sun = new THREE.DirectionalLight(0xffffff, 2.2);
      sun.position.set(-0.8, 0.7, 0.3); // low sun from the west: hills cast shade
      world.add(sun);
      controls = new Orbit(cam, renderer.domElement);
      controls.screenSpacePanning = false; // right-drag moves over the ground
      controls.maxPolarAngle = Math.PI * 0.49;
      controls.addEventListener("change", () => { sizeWalls(); redraw(); });
      for (const k of Object.keys(groups)) { groups[k] = new THREE.Group(); world.add(groups[k]); }
      theme();
      resize();
    }

    function theme() {
      if (!world || !box.value) return;
      world.background = new THREE.Color(css(box.value, "--plot") || "#f7f9fb");
      redraw();
    }

    function frame() { // the camera looks at the whole track from the south-west, above
      const L = scene.value.line;
      let e0 = Infinity, e1 = -Infinity, n0 = Infinity, n1 = -Infinity, h = 0;
      for (let i = 0; i < L.e.length; i++) { e0 = Math.min(e0, L.e[i]); e1 = Math.max(e1, L.e[i]); n0 = Math.min(n0, L.n[i]); n1 = Math.max(n1, L.n[i]); h += L.h[i]; }
      h /= L.e.length || 1;
      const cx = (e0 + e1) / 2, cz = -(n0 + n1) / 2, size = Math.max(e1 - e0, n1 - n0);
      controls.target.set(cx, h, cz + (n1 - n0) * 0.08); // a little towards the camera: the near side looks bigger
      // fit the track to the view: its width against the horizontal field of view, its depth
      // (seen at an angle from the south) against the vertical one
      const dir = new THREE.Vector3(-0.2, 0.62, 0.76).normalize();
      const t = Math.tan(((cam.fov / 2) * Math.PI) / 180);
      const dist = 1.15 * Math.max((e1 - e0) / 2 / (t * cam.aspect), ((n1 - n0) * 1.25) / 2 / t);
      cam.position.set(cx + dir.x * dist, h + dir.y * dist, cz + dir.z * dist);
      controls.maxDistance = size * 4;
      controls.minDistance = 15;
      controls.update();
      sizeWalls();
    }

    function clear(g) {
      while (g.children.length) {
        const o = g.children.pop();
        o.geometry?.dispose();
        if (o.material) { o.material.map?.dispose(); o.material.dispose(); }
      }
    }

    // ground: plain shaded terrain, plus photo blocks along the track when a photo source is on
    function buildGround() {
      if (!renderer || !scene.value) return; // not set up yet: onMounted builds it
      clear(groups.ground);
      for (const t of photoTextures) t.dispose();
      photoTextures = [];
      const s = scene.value, T = s.terrain;
      const groundCol = new THREE.Color(css(box.value, "--terrain") || "#cfd5c4");
      if (T) {
        const g = new THREE.PlaneGeometry(1, 1, T.cols - 1, T.rows - 1);
        const pos = g.attributes.position;
        for (let r = 0; r < T.rows; r++) for (let c = 0; c < T.cols; c++) {
          const i = r * T.cols + c;
          pos.setXYZ(i, T.e0 + c * T.step, T.h[i] - (photo.value === "none" ? 0 : 0.6), -(T.n0 + r * T.step));
        }
        g.computeVertexNormals();
        const m = new THREE.MeshLambertMaterial({ color: groundCol, side: THREE.DoubleSide });
        groups.ground.add(new THREE.Mesh(g, m));
      }
      if (photo.value === "none") buildRibbon(s.line, 12, new THREE.Color(css(box.value, "--asphalt") || "#5d636b"), 0.12, groups.ground);
      else buildPhotos();
      redraw();
    }

    function photoPlan() {
      const s = scene.value;
      if (photo.value === "maptiler") return { z: 17, px: 512, url: (z, x, y) => MAPTILER.replace("{z}", z).replace("{x}", x).replace("{y}", y) + enc(s.mapTilerKey) };
      const src = s.photos.source;
      return { z: src.zoom, px: 256, url: (z, x, y) => `/api/telemetry/3d/${enc(props.game)}/${enc(props.track)}/photo/${z}/${x}/${y}.jpg` };
    }

    function buildPhotos() {
      const s = scene.value, T = s.terrain, plan = photoPlan(), z = plan.z;
      const per = Math.max(1, 1024 / plan.px); // tiles per block side (1024 px blocks)
      const toLL = (e, n) => [s.lat + (n / R) * 180 / Math.PI, s.lon + (e / (R * Math.cos((s.lat * Math.PI) / 180))) * 180 / Math.PI];
      const toEN = (lat, lon) => [((lon - s.lon) * Math.PI) / 180 * R * Math.cos((s.lat * Math.PI) / 180), ((lat - s.lat) * Math.PI) / 180 * R];
      // tiles near the line
      const L = s.line, cell = 100, near = new Map();
      for (let i = 0; i < L.e.length; i += 3) near.set(Math.floor(L.e[i] / cell) + ":" + Math.floor(L.n[i] / cell), true);
      const e0 = T ? T.e0 : Math.min(...L.e) - 400, n0 = T ? T.n0 : Math.min(...L.n) - 400;
      const e1 = T ? T.e0 + (T.cols - 1) * T.step : Math.max(...L.e) + 400, n1 = T ? T.n0 + (T.rows - 1) * T.step : Math.max(...L.n) + 400;
      const [la0, lo0] = toLL(e0, n0), [la1, lo1] = toLL(e1, n1);
      const [tx0, ty1] = mercTile(la0, lo0, z), [tx1, ty0] = mercTile(la1, lo1, z);
      const tileM = (40075016.7 * Math.cos((s.lat * Math.PI) / 180)) / 2 ** z;
      const reach = Math.ceil((BAND + tileM * 0.75) / cell);
      const wanted = (tx, ty) => {
        const [lat, lon] = tileLatLon(z, tx + 0.5, ty + 0.5), [e, n] = toEN(lat, lon);
        const ce = Math.floor(e / cell), cn = Math.floor(n / cell);
        for (let a = -reach; a <= reach; a++) for (let b = -reach; b <= reach; b++) if (near.has(ce + a + ":" + (cn + b))) return true;
        return false;
      };
      const blocks = new Map();
      for (let ty = Math.floor(ty0); ty <= Math.floor(ty1); ty++) for (let tx = Math.floor(tx0); tx <= Math.floor(tx1); tx++) {
        if (!wanted(tx, ty)) continue;
        const k = Math.floor(tx / per) + ":" + Math.floor(ty / per);
        if (!blocks.has(k)) blocks.set(k, { bx: Math.floor(tx / per) * per, by: Math.floor(ty / per) * per, tiles: [] });
        blocks.get(k).tiles.push([tx, ty]);
      }
      const fill = css(box.value, "--terrain") || "#cfd5c4";
      const sub = 8; // grid squares per tile
      for (const b of blocks.values()) {
        const cv = document.createElement("canvas");
        cv.width = cv.height = 1024;
        const ctx = cv.getContext("2d");
        ctx.fillStyle = fill;
        ctx.fillRect(0, 0, 1024, 1024);
        const tex = new THREE.CanvasTexture(cv);
        tex.colorSpace = THREE.SRGBColorSpace;
        tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
        photoTextures.push(tex);
        const N = per * sub, g = new THREE.PlaneGeometry(1, 1, N, N), pos = g.attributes.position, uv = g.attributes.uv;
        for (let r = 0; r <= N; r++) for (let c = 0; c <= N; c++) {
          const i = r * (N + 1) + c, tx = b.bx + c / sub, ty = b.by + r / sub;
          const [lat, lon] = tileLatLon(z, tx, ty), [e, n] = toEN(lat, lon);
          const h = T ? terrainAt(T, e, n) : 0;
          pos.setXYZ(i, e, h, -n);
          uv.setXY(i, c / N, 1 - r / N);
        }
        g.computeVertexNormals();
        const m = new THREE.MeshLambertMaterial({ map: tex, polygonOffset: true, polygonOffsetFactor: -2, polygonOffsetUnits: -2 });
        groups.ground.add(new THREE.Mesh(g, m));
        const slot = 1024 / per;
        for (const [tx, ty] of b.tiles) {
          const img = new Image();
          img.crossOrigin = "anonymous";
          img.onload = () => { ctx.drawImage(img, (tx - b.bx) * slot, (ty - b.by) * slot, slot, slot); tex.needsUpdate = true; redraw(); };
          img.src = plan.url(z, tx, ty);
        }
      }
    }

    // a flat band along a path (the track in plain view, each lap's driving line)
    function buildRibbon(p, width, color, lift, group, opacity = 1) {
      const n = p.e.length;
      if (n < 2) return null;
      const g = new THREE.BufferGeometry(), v = new Float32Array(n * 6);
      for (let i = 0; i < n; i++) {
        const a = Math.max(0, i - 1), b = Math.min(n - 1, i + 1);
        let dx = p.e[b] - p.e[a], dy = p.n[b] - p.n[a];
        const len = Math.hypot(dx, dy) || 1;
        dx /= len; dy /= len;
        const w = width / 2;
        v.set([p.e[i] - dy * w, p.h[i] + lift, -(p.n[i] + dx * w), p.e[i] + dy * w, p.h[i] + lift, -(p.n[i] - dx * w)], i * 6);
      }
      const idx = [];
      for (let i = 0; i + 1 < n; i++) idx.push(2 * i, 2 * i + 1, 2 * i + 2, 2 * i + 1, 2 * i + 3, 2 * i + 2);
      g.setAttribute("position", new THREE.BufferAttribute(v, 3));
      g.setIndex(idx);
      const m = new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide, transparent: opacity < 1, opacity, polygonOffset: true, polygonOffsetFactor: -4, polygonOffsetUnits: -4 });
      const mesh = new THREE.Mesh(g, m);
      group.add(mesh);
      return mesh;
    }

    // every lap: its driving line on the ground and its wall
    let pick = []; // each lap's line points and their distances, to click on
    function buildLaps() {
      const total = props.view.total;
      if (!total || !renderer) return; // not set up yet: onMounted builds it
      clear(groups.laps);
      pick = [];
      const hRange = range(props.laps.map((l) => l.h));
      const cRange = props.wall.c === PEDALS || props.wall.c === LAPCOL ? null : range(props.laps.map((l) => l.c));
      const step = 2;
      props.laps.forEach((l, li) => {
        const p = paths[l.key];
        if (!p || !p.dist.length) return;
        const D = [], pts = [];
        for (let d = 0; d <= total; d += step) { const q = pathAt(p, l.fromRef(d)); if (q) { D.push(d); pts.push(q); } }
        const n = pts.length;
        if (n < 2) return;
        // driving line (1.2 m wide, lap colour)
        const line = { e: pts.map((q) => q[0]), n: pts.map((q) => -q[2]), h: pts.map((q) => q[1]) };
        buildRibbon(line, 1.4, new THREE.Color(l.color), 0.15 + li * 0.02, groups.laps);
        pick.push({ D, pts });
        // wall: see-through body (faint at the foot, stronger near the top) so laps can be compared
        // through each other, a solid band in the channel colour along the top, and a thin band in
        // the lap colour above it. Heights are fractions of the wall size, which follows the camera
        // distance (wallSize, applied on the graphics card: zooming rebuilds nothing).
        const own = new THREE.Color(l.color), ownRGB = [own.r, own.g, own.b];
        const body = new Float32Array(n * 6), bodyCol = new Float32Array(n * 8), bodyF = new Float32Array(n * 2);
        const cap = new Float32Array(n * 6), capCol = new Float32Array(n * 6), capF = new Float32Array(n * 2);
        const edge = new Float32Array(n * 6), edgeF = new Float32Array(n * 2);
        for (let i = 0; i < n; i++) {
          const d = D[i];
          const hv = l.h ? valueAt(l.h, d) : null;
          const f = wallTop(hv === null ? 0 : (hv - hRange[0]) / (hRange[1] - hRange[0]));
          const c = props.wall.c === LAPCOL ? ownRGB : props.wall.c === PEDALS
            ? pedalColour(l.th ? valueAt(l.th, d) : null, l.br ? valueAt(l.br, d) : null)
            : l.c ? ramp((valueAt(l.c, d) - cRange[0]) / (cRange[1] - cRange[0])) : [0.6, 0.6, 0.6];
          const [x, y, z] = pts[i], y0 = y + 0.3;
          for (const a of [body, cap, edge]) a.set([x, y0, z, x, y0, z], i * 6);
          bodyF.set([0, f - W_BAND], i * 2);
          capF.set([f - W_BAND, f], i * 2);
          edgeF.set([f, f + W_EDGE], i * 2);
          bodyCol.set([...c, 0.12, ...c, 0.5], i * 8);
          capCol.set([...c, ...c], i * 6);
        }
        const idx = [];
        for (let i = 0; i + 1 < n; i++) idx.push(2 * i, 2 * i + 1, 2 * i + 2, 2 * i + 1, 2 * i + 3, 2 * i + 2);
        const strip = (pos, frac, col, size, mat) => {
          const g = new THREE.BufferGeometry();
          g.setAttribute("position", new THREE.BufferAttribute(pos, 3));
          g.setAttribute("wallF", new THREE.BufferAttribute(frac, 1));
          if (col) g.setAttribute("color", new THREE.BufferAttribute(col, size));
          g.setIndex(idx);
          mat.onBeforeCompile = liftByWallSize;
          const m = new THREE.Mesh(g, mat);
          m.frustumCulled = false; // the real height is only known on the graphics card
          groups.laps.add(m);
        };
        strip(body, bodyF, bodyCol, 4, new THREE.MeshBasicMaterial({ vertexColors: true, side: THREE.DoubleSide, transparent: true, depthWrite: false }));
        strip(cap, capF, capCol, 3, new THREE.MeshBasicMaterial({ vertexColors: true, side: THREE.DoubleSide }));
        strip(edge, edgeF, null, 3, new THREE.MeshBasicMaterial({ color: new THREE.Color(l.color), side: THREE.DoubleSide }));
      });
      buildMarkers();
      redraw();
    }

    // one marker per lap at the cursor: a ball on the driving line and a pin up the wall
    function buildMarkers() {
      if (!renderer) return;
      clear(groups.markers);
      const d = props.view.cursor;
      if (d === null) { redraw(); return; }
      const hRange = range(props.laps.map((l) => l.h));
      props.laps.forEach((l) => {
        const p = paths[l.key];
        const q = p && pathAt(p, l.fromRef(d));
        if (!q) return;
        const color = new THREE.Color(l.color);
        // drawn on top of everything, at the same size on screen however far the camera is (render)
        const ball = new THREE.Mesh(new THREE.SphereGeometry(1, 20, 14), new THREE.MeshBasicMaterial({ color, depthTest: false }));
        const ring = new THREE.Mesh(new THREE.SphereGeometry(1.35, 20, 14), new THREE.MeshBasicMaterial({ color: 0xffffff, depthTest: false }));
        for (const [m, order] of [[ring, 10], [ball, 11]]) {
          m.position.set(q[0], q[1] + 1, q[2]);
          m.renderOrder = order;
          m.userData.marker = true;
          groups.markers.add(m);
        }
        const hv = l.h ? valueAt(l.h, d) : null;
        const top = wallTop(hv === null ? 0 : (hv - hRange[0]) / (hRange[1] - hRange[0])) * wallSize.value;
        const pg = new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(q[0], q[1] + 0.3, q[2]), new THREE.Vector3(q[0], q[1] + 0.3 + top + 3, q[2])]);
        const pin = new THREE.Line(pg, new THREE.LineBasicMaterial({ color, depthTest: false }));
        pin.renderOrder = 9;
        groups.markers.add(pin);
      });
      redraw();
    }

    // turn numbers, kept beside the first lap's line as HTML labels
    function placeLabels() {
      if (!cam || !props.corners?.length || !props.laps.length) { if (labels.value.length) labels.value = []; return; }
      const l = props.laps[0], p = paths[l.key];
      if (!p) return;
      const w = renderer.domElement.clientWidth, h = renderer.domElement.clientHeight, out = [];
      const v = new THREE.Vector3();
      for (const c of props.corners) {
        const q = pathAt(p, l.fromRef(c.dist));
        if (!q) continue;
        v.set(q[0], q[1] + 6, q[2]).project(cam);
        if (v.z > 1 || v.x < -1.1 || v.x > 1.1 || v.y < -1.1 || v.y > 1.1) continue;
        out.push({ n: c.n, name: c.name, x: ((v.x + 1) / 2) * w, y: ((1 - v.y) / 2) * h });
      }
      labels.value = out;
    }

    // click (not a drag) on a driving line or wall: move the cursor there
    let down = null;
    function onDown(e) { down = [e.clientX, e.clientY]; }
    function onUp(e) {
      if (!down || Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 4 || !THREE) return;
      down = null;
      // the lines are thin from afar: take the point of any lap's line closest on screen (25 px)
      const r = renderer.domElement.getBoundingClientRect(), x = e.clientX - r.left, y = e.clientY - r.top;
      const v = new THREE.Vector3();
      let best = 625, at = null;
      for (const pk of pick) for (let i = 0; i < pk.pts.length; i++) {
        const q = pk.pts[i];
        v.set(q[0], q[1], q[2]).project(cam);
        if (v.z > 1) continue;
        const d = (((v.x + 1) / 2) * r.width - x) ** 2 + (((1 - v.y) / 2) * r.height - y) ** 2;
        if (d < best) { best = d; at = pk.D[i]; }
      }
      if (at !== null) props.view.cursor = at;
    }

    function resize() {
      if (!renderer || !canvasBox.value) return;
      const r = canvasBox.value.getBoundingClientRect();
      const w = Math.max(10, Math.floor(r.width)), h = Math.max(10, Math.floor(r.height));
      renderer.setSize(w, h);
      cam.aspect = w / h;
      cam.updateProjectionMatrix();
      redraw();
    }

    // ---- photos ----
    const offline = computed(() => scene.value?.photos || {});
    const offlineReady = computed(() => offline.value.source && offline.value.tiles > 0 && offline.value.have >= offline.value.tiles);
    let poll = 0;
    async function downloadPhotos() {
      try {
        scene.value = markRaw({ ...scene.value, photos: await post(`/api/telemetry/3d/${enc(props.game)}/${enc(props.track)}/photos`) });
        watchDownload();
      } catch (e) { error.value = e.message; }
    }
    function watchDownload() { // only while a download runs (a few minutes at most)
      clearTimeout(poll);
      if (!scene.value?.photos?.busy) return;
      poll = setTimeout(async () => {
        try {
          const s = await get(`/api/telemetry/3d/${enc(props.game)}/${enc(props.track)}`);
          scene.value = markRaw({ ...scene.value, photos: s.photos, credits: s.credits });
          if (!s.photos.busy && s.photos.have >= s.photos.tiles && photo.value !== "maptiler") photo.value = "offline";
        } catch { /* try again */ }
        watchDownload();
      }, 1500);
    }
    async function deletePhotos() {
      try {
        const p = await api("DELETE", `/api/telemetry/3d/${enc(props.game)}/${enc(props.track)}/photos`);
        scene.value = markRaw({ ...scene.value, photos: p });
        if (photo.value === "offline") photo.value = "none";
      } catch (e) { error.value = e.message; }
    }
    const credits = computed(() => {
      const s = scene.value;
      if (!s) return [];
      const out = [...(s.credits || [])];
      if (photo.value === "maptiler") out.unshift("© MapTiler © OpenStreetMap contributors");
      else if (photo.value === "offline" && s.photos?.source && !out.some((c) => c.startsWith(s.photos.source.credit))) out.unshift(`${s.photos.source.credit} (${s.photos.source.licence})`);
      return out;
    });

    // ---- life cycle ----
    const goOnline = () => { online.value = true; }, goOffline = () => { online.value = false; if (photo.value === "maptiler") photo.value = offlineReady.value ? "offline" : "none"; };
    window.addEventListener("online", goOnline);
    window.addEventListener("offline", goOffline);
    const mo = new MutationObserver(() => { theme(); if (scene.value && THREE) buildGround(); });
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    onMounted(async () => {
      try {
        [THREE, Orbit] = await Promise.all([import("three"), import("three/orbit").then((m) => m.OrbitControls)]);
      } catch { error.value = "The 3D view could not start in this browser."; loading.value = false; return; }
      await loadScene();
      if (!scene.value) return;
      try { setup3D(); } catch { error.value = "The 3D view needs WebGL, which this browser or graphics driver doesn't offer."; return; }
      frame();
      buildGround();
      ro = new ResizeObserver(resize);
      ro.observe(canvasBox.value);
      renderer.domElement.addEventListener("pointerdown", onDown);
      renderer.domElement.addEventListener("pointerup", onUp);
      await loadPaths();
      await followPlacement();
      buildLaps();
      watchDownload();
    });
    onBeforeUnmount(() => {
      cancelAnimationFrame(raf);
      clearTimeout(poll);
      ro && ro.disconnect();
      mo.disconnect();
      window.removeEventListener("online", goOnline);
      window.removeEventListener("offline", goOffline);
      if (renderer) {
        for (const g of Object.values(groups)) clear(g);
        for (const t of photoTextures) t.dispose();
        controls.dispose();
        renderer.dispose();
        renderer.domElement.remove();
      }
    });
    watch(() => props.laps.map((l) => l.key).join(), async () => { await loadPaths(); await followPlacement(); buildLaps(); });
    watch(() => [props.laps.map((l) => (l.h ? 1 : 0) + (l.c ? 2 : 0) + (l.th ? 4 : 0) + (l.br ? 8 : 0) + l.color).join(), props.wall.h, props.wall.c, props.view.total], () => buildLaps());
    watch(() => props.view.cursor, () => buildMarkers());
    watch(photo, () => { if (THREE && scene.value) buildGround(); });

    // the wheel over a turn label zooms the view (not the page)
    const toCanvas = (e) => renderer && renderer.domElement.dispatchEvent(new WheelEvent("wheel", e));
    const channelLabel = (n) => (n === LAPCOL ? "Lap colour" : n === PEDALS ? "Throttle / brake" : props.channels.find((c) => c.name === n)?.label || n);
    return { box, canvasBox, scene, error, loading, photo, online, offline, offlineReady, credits, labels, exactness,
      downloadPhotos, deletePhotos, channelLabel, toCanvas, bytes, PEDALS, LAPCOL };
  },
  template: `
    <div class="t3d" ref="box">
      <div class="t3d-bar">
        <slot name="lead"></slot>
        <label class="t3d-field">Height
          <select v-model="wall.h" aria-label="Wall height channel">
            <option v-for="c in channels" :key="c.name" :value="c.name">{{ c.label }}</option>
          </select>
        </label>
        <label class="t3d-field">Colour
          <select v-model="wall.c" aria-label="Wall colour channel">
            <option :value="LAPCOL">Lap colour</option>
            <option :value="PEDALS">Throttle / brake</option>
            <option v-for="c in channels" :key="c.name" :value="c.name">{{ c.label }}</option>
          </select>
        </label>
        <span class="t3d-notes">
          <span v-if="scene && scene.source === 'calculated'" class="tag tag-warn" title="No lap at this track has SetupMan's extra log yet, so the track was placed from a line calculated from speed and yaw rate (± a few metres). Drive a session with SetupMan running for exact positions.">placed ± a few m</span>
          <span v-if="scene && scene.noTerrain" class="tag tag-warn" :title="scene.noTerrain">no terrain</span>
          <template v-if="offline.source">
            <span v-if="offline.busy" class="tag">Aerial photo {{ offline.have }}/{{ offline.tiles }}</span>
            <button v-else-if="offline.error" type="button" class="btn btn-sm" :title="offline.error" @click="downloadPhotos">Retry aerial photo</button>
            <button v-else-if="!offlineReady" type="button" class="btn btn-sm" @click="downloadPhotos" :title="'Keep the aerial photo for offline use (about ' + bytes(offline.estimate) + '). ' + offline.source.credit + ' (' + offline.source.licence + '). Delete it in Settings → Telemetry.'">Download aerial photo</button>
          </template>
          <button type="button" class="t3d-info" aria-label="How to use the 3D view" title="Drag to rotate, right-drag to move, wheel to zoom. Point at the graphs to move the markers; click a line to move the cursor. The wall height follows the zoom.">i</button>
        </span>
        <div v-if="scene" class="segmented seg-sm t3d-photo" role="radiogroup" aria-label="Photo">
          <button type="button" role="radio" :aria-checked="photo === 'maptiler'" :class="{ on: photo === 'maptiler' }" :disabled="!scene.mapTilerKey || !online"
            :title="!scene.mapTilerKey ? 'Add your free MapTiler key in Settings → Telemetry' : !online ? 'Needs an internet connection' : 'Sharp satellite photos from MapTiler (online)'" @click="photo = 'maptiler'">Satellite</button>
          <button v-if="offline.source" type="button" role="radio" :aria-checked="photo === 'offline'" :class="{ on: photo === 'offline' }" :disabled="!offlineReady"
            :title="offlineReady ? offline.source.credit : 'Download the aerial photo first'" @click="photo = 'offline'">Aerial</button>
          <button type="button" role="radio" :aria-checked="photo === 'none'" :class="{ on: photo === 'none' }" @click="photo = 'none'">None</button>
        </div>
      </div>
      <div class="t3d-stage">
        <div class="t3d-canvas" ref="canvasBox"></div>
        <span v-for="t in labels" :key="t.n" class="t3d-turn" @wheel.prevent="toCanvas" :style="{ left: t.x + 'px', top: t.y + 'px' }" :title="t.name ? 'T' + t.n + ': ' + t.name : null">T{{ t.n }}</span>
        <p v-if="loading" class="t3d-msg">Placing the track on the map… (the first time for a track takes a few seconds)</p>
        <p v-else-if="error" class="t3d-msg is-error">{{ error }}</p>
        <div class="t3d-legend">
          <span v-for="(x, i) in exactness" :key="i"><i class="lap-dot" :style="{ background: x.color }"></i>{{ x.label }}
            <span v-if="x.exact === false" class="tag tag-warn" title="This lap has no SetupMan extra log: it is drawn on the track's typical line. The channel values are this lap's own.">approximate position</span></span>
        </div>
        <div v-if="credits.length" class="t3d-credits">
          <a v-if="photo === 'maptiler'" href="https://www.maptiler.com/" target="_blank" rel="noopener" class="t3d-maptiler"><img src="https://api.maptiler.com/resources/logo.svg" alt="MapTiler" height="18"></a>
          <span>{{ credits.join(' · ') }}</span>
        </div>
      </div>
    </div>`,
};
