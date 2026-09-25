import { useEffect, useMemo, useState } from "preact/hooks";
import type { Record as Rec } from "../api";
import { formatWhen } from "../when";
import { Button, Chip, Confirm, Input, StatusChip } from "../components/ui";
import { cellText, compareRecords, isImageUrl, searchMatches, type FieldDef, type SortKey } from "../record/fields";
import { columnsFor, setColumnsFor } from "../record/prefs";

export type BulkAction = "publish" | "unpublish" | "delete";

const PAGE = 50;

// The built-in columns a table can show besides the record's own fields.
const BUILTIN: { key: string; label: string; sort: SortKey }[] = [
  { key: "status", label: "Status", sort: "status" },
  { key: "slug", label: "Slug", sort: "slug" },
  { key: "when", label: "When", sort: "when" },
  { key: "created", label: "Created", sort: "created" },
  { key: "updated", label: "Updated", sort: "updated" },
];

const stamp = (s?: string) => (s ? s.replace("T", " ").replace("Z", "").slice(0, 16) : "");

// A collection's records: search, sortable columns (the record's own fields as
// columns too), pages of fifty, and a bar of actions for whatever's ticked.
export function RecordsTable({
  collection,
  records,
  fields,
  canPublish,
  bulkProgress,
  onOpen,
  onBulk,
}: {
  collection: string;
  records: Rec[];
  fields: FieldDef[];
  canPublish: boolean;
  bulkProgress: string;
  onOpen: (id: string) => void;
  onBulk: (action: BulkAction, ids: string[]) => Promise<void>;
}) {
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "created", dir: -1 });
  const [page, setPage] = useState(0);
  const [chosen, setChosen] = useState<string[] | null>(null);
  const [menu, setMenu] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirming, setConfirming] = useState<BulkAction | null>(null);

  // A new collection starts the table over.
  useEffect(() => {
    setQ("");
    setSort({ key: "created", dir: -1 });
    setPage(0);
    setChosen(columnsFor(collection));
    setSelected(new Set());
    setConfirming(null);
    setMenu(false);
  }, [collection]);

  const fieldKinds = useMemo(() => Object.fromEntries(fields.map((f) => [f.key, f.kind])), [fields]);

  // Columns: the ones the user picked for this collection, else a sensible default.
  const visible = useMemo(() => {
    if (chosen) return chosen;
    const good = fields.filter((f) => ["text", "number", "checkbox", "tags", "image"].includes(f.kind)).slice(0, 3);
    return ["status", ...good.map((f) => "data:" + f.key), "when", "updated"];
  }, [chosen, fields]);

  function toggleColumn(key: string) {
    const next = visible.includes(key) ? visible.filter((k) => k !== key) : [...visible, key];
    setChosen(next);
    setColumnsFor(collection, next);
  }

  const shown = useMemo(() => {
    const kind = sort.key.startsWith("data:") ? fieldKinds[sort.key.slice(5)] : undefined;
    return records.filter((r) => searchMatches(r, q)).sort((a, b) => compareRecords(a, b, sort.key, sort.dir, kind));
  }, [records, q, sort, fieldKinds]);

  const pages = Math.max(1, Math.ceil(shown.length / PAGE));
  const current = Math.min(page, pages - 1);
  const rows = shown.slice(current * PAGE, current * PAGE + PAGE);

  function sortBy(key: SortKey) {
    setPage(0);
    setSort((s) => (s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: key === "created" || key === "updated" ? -1 : 1 }));
  }

  function toggleRow(id: string) {
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  }
  const allOnPage = rows.length > 0 && rows.every((r) => selected.has(r.id));
  function togglePage() {
    setSelected((s) => {
      const n = new Set(s);
      if (allOnPage) rows.forEach((r) => n.delete(r.id));
      else rows.forEach((r) => n.add(r.id));
      return n;
    });
  }

  async function runBulk(action: BulkAction) {
    const ids = [...selected];
    setConfirming(null);
    await onBulk(action, ids);
    setSelected(new Set());
  }

  // Column order: title first, then in the order the user (or default) listed.
  const columns = visible
    .map((key) => {
      if (key.startsWith("data:")) {
        const k = key.slice(5);
        return fieldKinds[k] ? { key, col: k, label: k, sort: key as SortKey } : null;
      }
      const b = BUILTIN.find((x) => x.key === key);
      return b ? { key, col: b.key, label: b.label, sort: b.sort } : null;
    })
    .filter((c): c is { key: string; col: string; label: string; sort: SortKey } => c !== null);

  const arrow = (key: SortKey) => (sort.key === key ? (sort.dir === 1 ? " ↑" : " ↓") : "");
  const th = "px-3 py-2 text-left text-xs font-bold text-dim";
  const count = selected.size;
  const bulkMessage = {
    publish: `Publish ${count} record${count === 1 ? "" : "s"}?`,
    unpublish: `Unpublish ${count} record${count === 1 ? "" : "s"}? They go back to drafts.`,
    delete: `Delete ${count} record${count === 1 ? "" : "s"}? This can't be undone.`,
  };

  return (
    <div>
      <div class="mb-3 flex flex-wrap items-center gap-2">
        <Input
          type="search"
          name="q"
          placeholder="Search records…"
          aria-label="Search records"
          value={q}
          onInput={(e) => {
            setQ((e.target as HTMLInputElement).value);
            setPage(0);
          }}
          class="mt-0 w-64 max-w-full"
        />
        <span class="text-xs text-dim">
          {shown.length === records.length ? `${records.length} record${records.length === 1 ? "" : "s"}` : `${shown.length} of ${records.length}`}
        </span>
        <div class="relative ml-auto">
          <Button size="sm" onClick={() => setMenu((m) => !m)} aria-expanded={menu ? "true" : "false"}>
            Columns
          </Button>
          {menu && (
            <div class="absolute right-0 z-10 mt-1 w-56 border border-ink bg-white p-2" data-columns-menu>
              {[...BUILTIN.map((b) => ({ key: b.key, label: b.label })), ...fields.map((f) => ({ key: "data:" + f.key, label: f.key }))].map((c) => (
                <label key={c.key} class="flex items-center gap-2 px-1 py-0.5 text-sm">
                  <input type="checkbox" checked={visible.includes(c.key)} onChange={() => toggleColumn(c.key)} />
                  <span class={c.key.startsWith("data:") ? "font-mono text-xs" : ""}>{c.label}</span>
                </label>
              ))}
              {fields.length === 0 && <p class="px-1 py-1 text-xs text-dim">Fields appear here once records have some.</p>}
            </div>
          )}
        </div>
      </div>

      {count > 0 && (
        <div class="mb-3" data-bulk-bar>
          {confirming ? (
            <Confirm
              message={bulkMessage[confirming]}
              confirmLabel={confirming === "delete" ? "Delete" : confirming === "publish" ? "Publish" : "Unpublish"}
              danger={confirming === "delete"}
              onConfirm={() => runBulk(confirming)}
              onCancel={() => setConfirming(null)}
            />
          ) : (
            <div class="flex flex-wrap items-center gap-3 border border-ink bg-manila px-3 py-2 text-sm">
              <span class="font-bold">{bulkProgress || `${count} selected`}</span>
              {!bulkProgress && canPublish && (
                <Button size="sm" onClick={() => setConfirming("publish")}>
                  Publish
                </Button>
              )}
              {!bulkProgress && canPublish && (
                <Button size="sm" onClick={() => setConfirming("unpublish")}>
                  Unpublish
                </Button>
              )}
              {!bulkProgress && (
                <Button size="sm" variant="danger" onClick={() => setConfirming("delete")}>
                  Delete
                </Button>
              )}
              <Button size="sm" variant="quiet" class="ml-auto" disabled={!!bulkProgress} onClick={() => setSelected(new Set())}>
                Clear
              </Button>
            </div>
          )}
        </div>
      )}

      <div class="overflow-x-auto border border-ink bg-white">
        <table class="w-full">
          <thead>
            <tr class="border-b border-ink">
              <th class="w-8 px-3 py-2">
                <input type="checkbox" aria-label="Select all on this page" checked={allOnPage} onChange={togglePage} />
              </th>
              <th class={th} data-col="title">
                <button type="button" onClick={() => sortBy("title")} class="hover:text-ink">
                  Title{arrow("title")}
                </button>
              </th>
              {columns.map((c) => (
                <th key={c.key} class={th} data-col={c.col}>
                  <button type="button" onClick={() => sortBy(c.sort)} class={"hover:text-ink " + (c.key.startsWith("data:") ? "font-mono" : "")}>
                    {c.label}
                    {arrow(c.sort)}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr
                key={r.id}
                data-id={r.id}
                onClick={() => onOpen(r.id)}
                class={"cursor-pointer border-b border-ink last:border-b-0 hover:bg-tint " + (selected.has(r.id) ? "bg-tint" : "")}
              >
                <td class="px-3 py-2" onClick={(e) => e.stopPropagation()}>
                  <input type="checkbox" aria-label={`Select ${r.title || r.slug}`} checked={selected.has(r.id)} onChange={() => toggleRow(r.id)} />
                </td>
                <td class="px-3 py-2 text-sm font-bold" data-col="title">
                  {r.title || <span class="font-normal text-dim">(untitled)</span>}
                </td>
                {columns.map((c) => (
                  <td key={c.key} class="px-3 py-2 text-sm" data-col={c.col}>
                    <Cell record={r} colKey={c.key} kind={c.key.startsWith("data:") ? fieldKinds[c.col] : undefined} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {pages > 1 && (
        <div class="mt-3 flex items-center gap-3 text-xs text-dim">
          <span>
            {current * PAGE + 1}–{Math.min(shown.length, (current + 1) * PAGE)} of {shown.length}
          </span>
          <Button size="sm" disabled={current === 0} onClick={() => setPage(current - 1)}>
            Previous
          </Button>
          <Button size="sm" disabled={current >= pages - 1} onClick={() => setPage(current + 1)}>
            Next
          </Button>
        </div>
      )}
    </div>
  );
}

function Cell({ record: r, colKey, kind }: { record: Rec; colKey: string; kind?: FieldDef["kind"] }) {
  if (colKey.startsWith("data:")) {
    const v = r.data?.[colKey.slice(5)];
    if (v === null || v === undefined || v === "") return null;
    if (kind === "image" && typeof v === "string" && isImageUrl(v)) return <img src={v} alt="" class="h-6 w-6 border border-ink object-cover" />;
    if (kind === "tags" && Array.isArray(v))
      return (
        <span class="flex flex-wrap gap-1">
          {v.map((t) => (
            <Chip key={String(t)} class="bg-tint">
              {String(t)}
            </Chip>
          ))}
        </span>
      );
    return <span class={kind === "json" ? "font-mono text-xs text-dim" : ""}>{cellText(v, kind || "text")}</span>;
  }
  switch (colKey) {
    case "status":
      return <StatusChip status={r.status} />;
    case "slug":
      return <code class="bg-tint px-1.5 py-0.5 text-xs">{r.slug}</code>;
    case "when":
      return r.when ? (
        <span class="text-xs text-dim" title={r.when.repeats ? `Next: ${formatWhen(r.when, { next: true })}` : undefined}>
          {formatWhen(r.when)}
        </span>
      ) : null;
    case "created":
      return <span class="text-xs text-dim">{stamp(r.created)}</span>;
    case "updated":
      return <span class="text-xs text-dim">{stamp(r.updated)}</span>;
    default:
      return null;
  }
}
