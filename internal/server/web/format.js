// Formatting helpers shared by pages.

export function plural(n, one, many = one + "s") {
  return `${typeof n === "number" ? n.toLocaleString() : n} ${n === 1 ? one : many}`;
}

// A message as one sentence ending in a single full stop (server messages may end in one already).
export const sentence = (s) => String(s || "").replace(/[.\s]+$/, "") + ".";

// Round down so 99.99% never shows as 100%.
export function pct(score) {
  return Math.floor(score * 10) / 10 + "%";
}

export function dateTime(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function ago(iso) {
  const s = (Date.now() - new Date(iso)) / 1000;
  if (!isFinite(s)) return "";
  if (s < 60) return "just now";
  if (s < 3600) return plural(Math.floor(s / 60), "minute") + " ago";
  if (s < 86400) return plural(Math.floor(s / 3600), "hour") + " ago";
  return dateTime(iso);
}

// Floats with many decimals (e.g. -4.2949256896972656) become readable.
// A setting as the game shows it (spec D-101, D-141): real values where known (e.g. 25.3 psi,
// −3.6°, caster from the car's table), else clicks.
const decimals = (step) => (step && step < 1 ? (String(step).split(".")[1] || "").length : 0);
const decimalsOf = (f) => (f.table && f.table.length ? Math.max(...f.table.map((x) => (String(x).split(".")[1] || "").length)) : decimals(Number(f.step || 0)));
// Stored in real units already (step 1, no offset, no table, e.g. AC EVO): keep the decimals the file has.
const direct = (f) => !(f.table && f.table.length) && Number(f.step || 1) === 1 && !Number(f.offset || 0);
const trim = (n) => String(Math.round(n * 1000) / 1000);
const isReal = (f) => !!f && (f.kind === "int" || f.kind === "float") && (!!f.real || !!f.unit);
const suffix = (u) => (!u ? "" : u === "°" || u === "%" ? u : " " + u);
// realNum is the number the game shows for a setting (the clicks when there is no real value).
export function realNum(f) {
  const n = Number(f.value);
  if (!isReal(f)) return n;
  if (f.table && f.table.length) return f.table[Math.max(0, Math.min(f.table.length - 1, Math.round(n)))];
  if (direct(f)) return n;
  return Number(f.offset || 0) + n * Number(f.step || 1);
}
export function shown(f) {
  if (!f) return "";
  // Named values (D-120), e.g. tyre compound "Dry" / "Wet".
  if (f.choices && f.choices.length && f.kind === "int" && f.choices[Number(f.value)] !== undefined) return f.choices[Number(f.value)];
  if (isReal(f)) return (direct(f) ? trim(realNum(f)) : realNum(f).toFixed(decimalsOf(f))) + suffix(f.unit);
  return value(f.value, f.kind);
}
// shownBare is shown without the unit, for tables that name the unit once in the row label.
export function shownBare(f) {
  if (!f || !isReal(f) || (f.choices && f.choices.length)) return shown(f);
  return direct(f) ? trim(realNum(f)) : realNum(f).toFixed(decimalsOf(f));
}
// The change from setting a to setting b (same setting, two setups), in real values where known.
export function shownDelta(a, b) {
  const d = realNum(b) - realNum(a);
  const t = isReal(b) && !direct(b) ? d.toFixed(decimalsOf(b)) : trim(d);
  return (d > 0 ? "+" : "") + t + (isReal(b) ? suffix(b.unit) : "");
}
// Whether a setting has a real value (shown next to the clicks while changing values).
export const hasReal = (f) => isReal(f);

export function value(v, kind) {
  if (v === undefined) return "";
  if (kind === "float") {
    const n = Number(v);
    if (isFinite(n)) return String(Math.round(n * 1000) / 1000);
  }
  return v;
}

export function shortDate(iso) {
  const d = new Date(iso);
  return isNaN(d) || d.getFullYear() < 2000 ? "" : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

// Plain-language changelog text (spec D-18).
const SHORT = { acc: "ACC", ac: "AC", ace: "AC EVO", acr: "AC Rally" };
export function eventText(e) {
  const d = e.details || {};
  const G = SHORT[e.game] || "the game";
  switch (e.kind) {
    case "imported": return `Stored ${d.count === 1 ? "1 setup" : d.count + " setups"} in SetupMan and worked out where they came from.`;
    case "new-setup": return `New setup: ${d.name}.`;
    case "new-version": return `${d.name} changed (version ${d.seq})${d.changes ? ": " + d.changes : ""}.`;
    case "branch": return `${d.name} was made from ${d.parent} (${Math.floor(d.score)}% alike).`;
    case "renamed": return `${d.from} was renamed to ${d.to}.`;
    case "removed": return `${d.name} is no longer in ${G}'s folder. Its history is kept.`;
    case "relinked": return d.parent ? `You linked ${d.name} to ${d.parent}.` : `You made ${d.name} the start of its own family.`;
    case "rebuilt": return `History rebuilt: ${d.families} families, ${d.linked} linked setups.`;
    case "staged": return `${d.name} is shown in ${G}${d.seq ? " (version " + d.seq + ")" : ""}.`;
    case "moved-to-library": return `${d.name} was hidden from ${G}.`;
    case "returned": return `${d.name} is back in ${G}'s folder.`;
    case "edited": return `You changed ${d.name} (version ${d.seq})${d.changes ? ": " + d.changes : ""}.`;
    case "edited-new": return `You made ${d.name} from ${d.from}${d.changes ? ": " + d.changes : ""}.`;
    case "renamed-by-you": return `You renamed ${d.from} to ${d.to}.`;
    case "imported-file": return `You imported ${d.name}.`;
    case "deleted": return `You deleted ${d.name} (it is in the bin for 30 days).`;
    case "undeleted": return `You restored ${d.name} from the bin.`;
    case "queued-failed": {
      const what = d.change === "stage" ? `Showing ${d.name || "a setup"} in ${G}` : d.change === "unstage" ? `Hiding ${d.name || "a setup"} from ${G}` : `Changing which setups ${G} shows`;
      return `${what} was queued while you were driving, but could not be applied: ${d.error}.`;
    }
    default: return e.kind;
  }
}

export const classColors ={ GT3: "gt3", GT4: "gt4", GT2: "gt2", Cup: "cup", TCX: "tcx" };
export const classTitles = {
  GT3: "GT3", GT4: "GT4", GT2: "GT2", Cup: "Cup, Super Trofeo & Challenge", TCX: "TCX", Other: "Other cars",
};
