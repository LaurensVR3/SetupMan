// Dropping files onto a page (drag and drop import). A page calls useDrop(hint, onFiles);
// while files are dragged over the window, a full-page overlay shows the hint.
import { onMounted, onBeforeUnmount } from "vue";
import { store } from "./store.js";

const hasFiles = (e) => [...(e.dataTransfer?.types || [])].includes("Files");

export function useDrop(hint, onFiles) {
  let depth = 0; // dragenter/dragleave fire for every child element
  const enter = (e) => { if (!hasFiles(e)) return; e.preventDefault(); depth++; store.dropHint = hint; };
  const over = (e) => { if (hasFiles(e)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } };
  const leave = (e) => { if (hasFiles(e) && --depth <= 0) { depth = 0; store.dropHint = ""; } };
  const drop = (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    store.dropHint = "";
    const files = [...e.dataTransfer.files];
    if (files.length) onFiles(files);
  };
  onMounted(() => {
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragover", over);
    window.addEventListener("dragleave", leave);
    window.addEventListener("drop", drop);
  });
  onBeforeUnmount(() => {
    window.removeEventListener("dragenter", enter);
    window.removeEventListener("dragover", over);
    window.removeEventListener("dragleave", leave);
    window.removeEventListener("drop", drop);
    store.dropHint = "";
  });
}

// Pages without a drop target: stop the browser from opening a dropped file instead of SetupMan.
export function blockStrayDrops() {
  window.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
  window.addEventListener("drop", (e) => { if (hasFiles(e)) e.preventDefault(); });
}

// Reads files for an upload: [{ name, data (base64) }].
export const readFiles = (list) => Promise.all(list.map((f) => new Promise((ok, fail) => {
  const r = new FileReader();
  r.onload = () => ok({ name: f.name, data: String(r.result).split(",")[1] || "" });
  r.onerror = () => fail(new Error("Could not read " + f.name));
  r.readAsDataURL(f);
})));
