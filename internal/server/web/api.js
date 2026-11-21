// Small helpers for talking to SetupMan's local API.

import { ref } from "vue";

// True when this browser (on another computer) still has to enter SetupMan's PIN.
export const needPin = ref(false);

export async function api(method, url, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  let resp;
  try {
    resp = await fetch(url, opt);
  } catch {
    throw new Error("SetupMan isn't running. Start SetupMan.exe again; this page reloads by itself.");
  }
  const data = await resp.json().catch(() => ({}));
  if (resp.status === 401 && data.needPin) needPin.value = true; // opened from another computer (D-100)
  if (!resp.ok) {
    const msg = String(data.error || `${resp.status} ${resp.statusText}`);
    throw new Error(msg.charAt(0).toUpperCase() + msg.slice(1)); // messages read as sentences
  }
  return data;
}

export const get = (url) => api("GET", url);
export const post = (url, body = {}) => api("POST", url, body);
export const put = (url, body) => api("PUT", url, body);

export const enc = encodeURIComponent;
