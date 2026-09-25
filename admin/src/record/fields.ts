import type { DeclaredField, Record as Rec } from "../api";
import { WHEN_KEYS } from "./when-where";

// A collection has no declared schema: its fields are whatever keys its records
// carry in `data`. This module reads those keys back into something a form and a
// table can show — a kind per field, guessed from the values — without ever
// deciding what a record may or may not hold.

export type Data = { [key: string]: unknown };

export type Kind = "text" | "longtext" | "number" | "checkbox" | "tags" | "image" | "json";

export const KINDS: { value: Kind; label: string }[] = [
  { value: "text", label: "Text" },
  { value: "longtext", label: "Paragraph" },
  { value: "number", label: "Number" },
  { value: "checkbox", label: "Checkbox" },
  { value: "tags", label: "Tags" },
  { value: "image", label: "Image" },
  { value: "json", label: "Structured (JSON)" },
];

export type FieldDef = {
  key: string;
  kind: Kind;
  count: number; // how many of the collection's records carry the key
  declared?: boolean; // from friendo.toml, so always shown and never removable
  choices?: string[];
  required?: boolean;
  hint?: string;
};

// kindFromDeclared maps the word friendo.toml uses onto a widget kind.
export function kindFromDeclared(kind: string): Kind {
  if (kind === "paragraph") return "longtext";
  return KINDS.some((k) => k.value === kind) ? (kind as Kind) : "text";
}

// Keys the server lifts out of `data` into the post's event (the When section).
export const RESERVED_KEYS: string[] = WHEN_KEYS;

// A record's own columns, which a data field may not shadow.
const COLUMN_NAMES = ["title", "slug", "body", "status", "id", "collection", "created", "updated", "published_at", "author_id"];

export function isImageUrl(s: string): boolean {
  return /^(\/assets\/|https?:\/\/)\S+\.(png|jpe?g|gif|webp|avif|svg)(\?\S*)?$/i.test(s);
}

// kindOf guesses a value's kind; null means the value casts no vote (null/undefined).
export function kindOf(v: unknown): Kind | null {
  if (v === null || v === undefined) return null;
  if (typeof v === "boolean") return "checkbox";
  if (typeof v === "number") return "number";
  if (typeof v === "string") {
    if (isImageUrl(v)) return "image";
    return v.includes("\n") || v.length > 120 ? "longtext" : "text";
  }
  if (Array.isArray(v)) return v.every((x) => typeof x === "string") ? "tags" : "json";
  return "json";
}

// inferFields reads a collection's fields: the ones friendo.toml declares come
// first, in its order, then every other non-reserved data key its records carry,
// with the kind the values agree on, ordered by how many records carry the key
// and then by name, so the table's columns are stable across reloads.
export function inferFields(records: { data?: Data | null }[], declared: DeclaredField[] = []): FieldDef[] {
  const stats = new Map<string, { count: number; votes: Map<Kind, number> }>();
  for (const r of records) {
    const data = r.data;
    if (!data || typeof data !== "object") continue;
    for (const key of Object.keys(data)) {
      if (RESERVED_KEYS.includes(key)) continue;
      let s = stats.get(key);
      if (!s) {
        s = { count: 0, votes: new Map() };
        stats.set(key, s);
      }
      s.count++;
      const k = kindOf(data[key]);
      if (k) s.votes.set(k, (s.votes.get(k) || 0) + 1);
    }
  }
  const out: FieldDef[] = [];
  for (const d of declared) {
    if (RESERVED_KEYS.includes(d.name) || out.some((f) => f.key === d.name)) continue;
    out.push({
      key: d.name,
      kind: kindFromDeclared(d.kind),
      count: stats.get(d.name)?.count || 0,
      declared: true,
      choices: d.choices && d.choices.length ? d.choices : undefined,
      required: !!d.required,
      hint: d.hint || undefined,
    });
  }
  const extra: FieldDef[] = [];
  for (const [key, s] of stats) {
    if (out.some((f) => f.key === key)) continue;
    extra.push({ key, kind: settle([...s.votes.keys()]), count: s.count });
  }
  extra.sort((a, b) => b.count - a.count || a.key.localeCompare(b.key));
  return [...out, ...extra];
}

// isEmptyValue says whether a required field counts as unfilled.
export function isEmptyValue(v: unknown): boolean {
  if (v === null || v === undefined) return true;
  if (typeof v === "string") return v.trim() === "";
  if (Array.isArray(v)) return v.length === 0;
  return false;
}

// settle picks one kind from the kinds a field's values were seen as.
function settle(kinds: Kind[]): Kind {
  if (kinds.length === 0) return "text";
  if (kinds.length === 1) return kinds[0];
  const only = (...allowed: Kind[]) => kinds.every((k) => allowed.includes(k));
  if (only("text", "longtext")) return "longtext";
  if (only("text", "image")) return "text";
  if (kinds.includes("json") || kinds.includes("tags")) return "json";
  return "text";
}

// widgetKind picks the widget for one record's value: what the value is, or the
// collection's kind when the record has no value yet.
export function widgetKind(value: unknown, fallback: Kind): Kind {
  return kindOf(value) || fallback;
}

// defaultColumns picks the first three fields that read well in a table cell.
export function defaultColumns(fields: FieldDef[]): string[] {
  const good: Kind[] = ["text", "number", "checkbox", "tags", "image"];
  return fields
    .filter((f) => good.includes(f.kind))
    .slice(0, 3)
    .map((f) => f.key);
}

// cellText prints a value for a table cell (images and tags are drawn by the table).
export function cellText(v: unknown, kind: Kind): string {
  if (v === null || v === undefined || v === "") return "";
  switch (kind) {
    case "checkbox":
      return v ? "Yes" : "";
    case "tags":
      return Array.isArray(v) ? v.join(", ") : String(v);
    case "json":
      if (Array.isArray(v)) return v.length ? "[…]" : "[]";
      if (typeof v === "object") return Object.keys(v as object).length ? "{…}" : "{}";
      return String(v);
    case "longtext": {
      const s = String(v).replace(/\s+/g, " ").trim();
      return s.length > 60 ? s.slice(0, 60) + "…" : s;
    }
    default:
      return String(v);
  }
}

// fieldLabel shows a key as words: "cover_image" → "Cover image".
export function fieldLabel(key: string): string {
  const words = key.replace(/[_-]+/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2").trim();
  return words ? words[0].toUpperCase() + words.slice(1).toLowerCase() : key;
}

// slugify turns a title into a URL-safe slug, the same way the SDK and the
// content importer do.
export function slugify(s: string): string {
  return (
    String(s == null ? "" : s)
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 80) || "post"
  );
}

// validateFieldName says why a new field's name won't do, or "" when it will.
export function validateFieldName(name: string, existing: string[]): string {
  const n = name.trim();
  if (!n) return "Give the field a name.";
  if (!/^[A-Za-z][A-Za-z0-9_-]*$/.test(n)) return "Use letters, numbers, dashes or underscores, starting with a letter.";
  if (RESERVED_KEYS.includes(n)) return "That name is used by the When section.";
  if (COLUMN_NAMES.includes(n)) return "That name is one of the record's own fields.";
  if (existing.includes(n)) return "This record already has that field.";
  return "";
}

// emptyValue is what a freshly added field starts as.
export function emptyValue(kind: Kind): unknown {
  switch (kind) {
    case "checkbox":
      return false;
    case "number":
      return 0;
    case "tags":
      return [];
    case "json":
      return {};
    default:
      return "";
  }
}

// --- Table helpers ---

// searchMatches is a plain case-insensitive search over everything a record says.
export function searchMatches(r: Rec, q: string): boolean {
  const needle = q.trim().toLowerCase();
  if (!needle) return true;
  const hay = [r.title, r.slug, r.body, r.status, r.data ? JSON.stringify(r.data) : ""].join("\n").toLowerCase();
  return hay.includes(needle);
}

// A sort key is a record column, or "data:<key>" for an inferred field.
export type SortKey = "title" | "slug" | "status" | "when" | "created" | "updated" | `data:${string}`;

function sortValue(r: Rec, key: SortKey, kind?: Kind): string | number {
  if (key.startsWith("data:")) {
    const v = r.data?.[key.slice(5)];
    if (v === null || v === undefined) return kind === "number" ? Number.NEGATIVE_INFINITY : "";
    if (typeof v === "number") return v;
    if (typeof v === "boolean") return v ? 1 : 0;
    if (Array.isArray(v)) return v.join(", ").toLowerCase();
    if (typeof v === "object") return JSON.stringify(v);
    return String(v).toLowerCase();
  }
  switch (key) {
    case "when":
      return r.when ? (r.when.next ? r.when.next.starts : r.when.starts) : "";
    case "created":
      return r.created || "";
    case "updated":
      return r.updated || "";
    case "status":
      return r.status || "";
    case "slug":
      return r.slug.toLowerCase();
    default:
      return r.title.toLowerCase();
  }
}

export function compareRecords(a: Rec, b: Rec, key: SortKey, dir: 1 | -1, kind?: Kind): number {
  const va = sortValue(a, key, kind);
  const vb = sortValue(b, key, kind);
  let c: number;
  if (typeof va === "number" && typeof vb === "number") c = va - vb;
  else c = String(va).localeCompare(String(vb));
  // Empty values sink to the bottom whichever way the column sorts.
  if (va === "" && vb !== "") return 1;
  if (vb === "" && va !== "") return -1;
  return c * dir;
}
