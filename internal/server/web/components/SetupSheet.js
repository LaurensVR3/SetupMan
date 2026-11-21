// One setup's values, grouped by section (tyres, electronics, …), so a setup can be read on its
// own, and changed (spec D-85): − / + per setting, saved as a new version or a new setup.
// Games that describe their own setup screens (ACC, D-144) give each field a box (group), the
// game's own name (row) and a wheel or axle column (col): per-wheel settings are then shown side
// by side, as on the game's screens. While changing values every setting gets its own row.
import { ref, reactive, computed, watch } from "vue";
import { value, shown, shownBare, hasReal } from "../format.js";

// ACC's tabs (D-144) first, then the sections the other games use; anything else after.
const SECTION_ORDER = ["Tyres", "Alignment", "Electronics", "Fuel & strategy", "Strategy", "Pit strategy",
  "Mechanical grip", "Mechanical balance", "Dampers", "Aero", "Aero balance", "Drivetrain"];

// blocks turns a section's fields into what is drawn: box titles, plain rows and wheel grids.
function blocks(rows, flat) {
  const out = [];
  let group = "";
  for (const f of rows) {
    if ((f.group || "") !== group) {
      group = f.group || "";
      if (group) out.push({ kind: "title", name: group, key: "t" + f.path });
    }
    const last = out[out.length - 1];
    if (f.col && !flat) {
      let grid = last && last.kind === "grid" ? last : null;
      if (!grid) { grid = { kind: "grid", cols: [], rows: [], key: "g" + f.path }; out.push(grid); }
      if (!grid.cols.includes(f.col)) grid.cols.push(f.col);
      let r = grid.rows.find((x) => x.name === (f.row || f.label));
      if (!r) { r = { name: f.row || f.label, unit: f.unit || "", cells: {} }; grid.rows.push(r); }
      r.cells[f.col] = f;
      continue;
    }
    let list = last && last.kind === "list" ? last : null;
    if (!list) { list = { kind: "list", rows: [], key: "l" + f.path }; out.push(list); }
    list.rows.push(f);
  }
  return out;
}

export const SetupSheet = {
  props: {
    fields: Array,
    editable: Boolean, // true when this is the newest version and the game supports editing
    saving: Boolean,
  },
  emits: ["save"],
  setup(props, { emit }) {
    const showAll = ref(false);
    const editing = ref(false);
    const edits = reactive({}); // path → new value (as text)
    const newName = ref("");
    const askName = ref(false);

    watch(() => props.fields, () => { for (const k of Object.keys(edits)) delete edits[k]; editing.value = false; askName.value = false; });

    const canEdit = (f) => editing.value && f.kind === "int" && !f.excluded;
    const sections = computed(() => {
      const out = [];
      const byName = new Map();
      for (const f of props.fields || []) {
        if (f.hidden && !showAll.value) continue;
        let s = byName.get(f.section);
        if (!s) {
          s = { name: f.section, rows: [] };
          byName.set(f.section, s);
          out.push(s);
        }
        s.rows.push(f);
      }
      // The order of ACC's own setup screens; anything else after.
      const rank = (n) => { const i = SECTION_ORDER.indexOf(n); return i < 0 ? 99 : i; };
      for (const s of out) {
        s.blocks = blocks(s.rows, editing.value);
        s.notes = [...new Set(s.rows.map((f) => f.note).filter(Boolean))]; // e.g. ACC ride heights (D-158)
      }
      return out.sort((a, b) => rank(a.name) - rank(b.name));
    });
    // A plain row shows the game's name; a wheel setting listed on its own row (while changing
    // values) needs the wheel too, e.g. "psi FL".
    const rowName = (f) => (f.col ? f.label : f.row || f.label);
    const gridLabel = (r) => (r.unit && r.unit !== r.name ? r.name + " (" + r.unit + ")" : r.name);
    const current = (f) => (f.path in edits ? edits[f.path] : f.value);
    const changed = (f) => f.path in edits && String(edits[f.path]) !== String(f.value);
    function setValue(f, v) {
      // Most settings start at 0; some games also store values below zero (f.signed, e.g. AC camber).
      const r = Math.round(Number(v));
      const n = f.signed ? r : Math.max(0, r);
      if (!isFinite(n)) return;
      if (String(n) === String(f.value)) delete edits[f.path];
      else edits[f.path] = String(n);
    }
    const step = (f, d) => setValue(f, Number(current(f)) + d);
    const changeCount = computed(() => Object.keys(edits).length);
    function discard() {
      for (const k of Object.keys(edits)) delete edits[k];
      editing.value = false;
      askName.value = false;
    }
    function save(asNew) {
      if (asNew && !askName.value) { askName.value = true; return; }
      emit("save", { edits: { ...edits }, newName: asNew ? newName.value.trim() : "" });
    }
    return { showAll, editing, edits, newName, askName, sections, canEdit, current, changed, setValue, step, changeCount, discard, save, value, shown, shownBare, hasReal, rowName, gridLabel };
  },
  template: `
    <div class="sheet">
      <div class="compare-tools">
        <button v-if="editable && !editing" type="button" class="btn btn-primary" @click="editing = true">Change values</button>
        <span v-if="editing" class="sheet-editing">Use − and + or type a number. Numbers are the values stored in the setup file: clicks for most settings, not degrees.</span>
        <label class="switch" title="Settings the game doesn't show on its setup screens, such as the car name and values it works out itself"><input type="checkbox" v-model="showAll"><span></span>Also show hidden settings</label>
        <span v-if="!editing" class="compare-count">Values are shown as in the game where SetupMan knows the car; other numbers are the game's own steps (clicks). Grey settings are not used for Similarity.</span>
      </div>
      <div class="sheet-grid">
        <section v-for="s in sections" :key="s.name" class="sheet-section">
          <h3>{{ s.name }}</h3>
          <template v-for="b in s.blocks" :key="b.key">
            <h4 v-if="b.kind === 'title'" class="sheet-box">{{ b.name }}</h4>
            <table v-else-if="b.kind === 'grid'" class="sheet-wheels">
              <thead><tr><th></th><th v-for="c in b.cols" :key="c" scope="col">{{ c }}</th></tr></thead>
              <tbody>
                <tr v-for="r in b.rows" :key="r.name">
                  <th scope="row" class="ct-label">{{ gridLabel(r) }}</th>
                  <td v-for="c in b.cols" :key="c" class="ct-val" :class="{ 'ct-excluded': r.cells[c] && r.cells[c].excluded }" :title="r.cells[c] ? r.cells[c].label : ''">{{ r.cells[c] ? shownBare(r.cells[c]) : '' }}</td>
                </tr>
              </tbody>
            </table>
            <table v-else>
              <tr v-for="f in b.rows" :key="f.path" :class="{ 'ct-excluded': f.excluded, 'sheet-changed': changed(f) }">
                <td class="ct-label" :title="f.label">{{ rowName(f) }}</td>
                <td class="ct-val" :title="f.kind === 'float' ? f.value : ''">
                  <template v-if="canEdit(f)">
                    <span class="stepper">
                      <button type="button" :aria-label="'Lower ' + f.label" @click="step(f, -1)" :disabled="!f.signed && Number(current(f)) <= 0">−</button>
                      <input :value="current(f)" @change="setValue(f, $event.target.value)" inputmode="numeric" :aria-label="f.label">
                      <button type="button" :aria-label="'Raise ' + f.label" @click="step(f, 1)" :disabled="!!(f.choices && f.choices.length) && Number(current(f)) >= f.choices.length - 1">+</button>
                    </span>
                    <span v-if="hasReal(f) || (f.choices && f.choices.length)" class="was">{{ shown({ ...f, value: current(f) }) }}</span>
                    <span v-if="changed(f)" class="was">was {{ shown(f) }}</span>
                  </template>
                  <template v-else>{{ shown(f) }}</template>
                </td>
              </tr>
            </table>
          </template>
          <p v-for="n in s.notes" :key="n" class="sheet-note">{{ n }}</p>
        </section>
      </div>
      <div v-if="editing" class="edit-bar">
        <span class="edit-count">{{ changeCount ? (changeCount === 1 ? '1 setting changed' : changeCount + ' settings changed') : 'Nothing changed yet' }}</span>
        <template v-if="askName">
          <input v-model="newName" class="edit-name" placeholder="Name for the new setup, e.g. ABC R 2327 TC25" @keyup.enter="newName.trim() && save(true)" aria-label="Name for the new setup">
          <button type="button" class="btn btn-primary" :disabled="!newName.trim() || saving" @click="save(true)">Save new setup</button>
          <button type="button" class="btn btn-quiet" @click="askName = false">Back</button>
        </template>
        <template v-else>
          <button type="button" class="btn" @click="discard">Discard</button>
          <button type="button" class="btn" :disabled="saving" @click="save(true)">Save as new setup…</button>
          <button type="button" class="btn btn-primary" :disabled="!changeCount || saving" @click="save(false)">Save as new version</button>
        </template>
      </div>
    </div>`,
};
