// Per-browser conveniences remembered in localStorage: which columns a
// collection's table shows, and whether the body editor is in rich-text mode.
// Storage can be missing or blocked (private windows), so every access is guarded
// and the defaults win when it is.

const COLUMNS = "friendo.admin.columns.";
const RICHTEXT = "friendo.admin.richtext";

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    /* storage unavailable; the choice just doesn't stick */
  }
}

// The saved column keys for a collection, or null when the user never chose.
export function columnsFor(collection: string): string[] | null {
  const raw = read(COLUMNS + collection);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((k) => typeof k === "string") : null;
  } catch {
    return null;
  }
}

export function setColumnsFor(collection: string, keys: string[]) {
  write(COLUMNS + collection, JSON.stringify(keys));
}

export function richTextOn(): boolean {
  return read(RICHTEXT) === "1";
}

export function setRichTextOn(on: boolean) {
  write(RICHTEXT, on ? "1" : null);
}
