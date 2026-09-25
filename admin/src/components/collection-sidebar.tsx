import { useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import type { Collection } from "../api";
import { Button, Input } from "./ui";
import { slugify } from "../record/fields";

// The column down the left of the content area: every collection with how many
// records it holds. A collection exists by having records, so "New collection"
// just opens a new record in a collection of that name; saving it creates both.
export function CollectionSidebar({
  collections,
  active,
  declared,
}: {
  collections: Collection[] | null;
  active: string; // the open collection, if any
  declared: boolean; // friendo.toml names its types, so an unlisted one stands out
}) {
  const { route } = useLocation();
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("");

  const shown = collections || [];
  const href = (n: string) => `/_/collections/${encodeURIComponent(n)}`;

  function startNew(e: Event) {
    e.preventDefault();
    const n = slugify(name);
    if (!name.trim()) return;
    setNaming(false);
    setName("");
    route(`${href(n)}/new`);
  }

  return (
    <aside class="border-b border-ink md:w-[200px] md:shrink-0 md:border-r md:border-b-0" data-sidebar>
      {/* Narrow screens: one menu instead of a column. */}
      <div class="p-3 md:hidden">
        <select
          class="block w-full border border-ink bg-white px-2 py-1 text-sm"
          value={active}
          onChange={(e) => {
            const v = (e.target as HTMLSelectElement).value;
            if (v) route(href(v));
          }}
        >
          <option value="">Collections…</option>
          {(collections || []).map((c) => (
            <option key={c.name} value={c.name}>
              {c.name} ({c.count})
            </option>
          ))}
        </select>
      </div>

      <div class="hidden md:block">
        <ul>
          {collections === null && <li class="px-3 py-2 text-xs text-dim">Loading…</li>}
          {shown.map((c) => (
            <li key={c.name}>
              <a
                href={href(c.name)}
                aria-current={c.name === active ? "page" : undefined}
                class={
                  "flex items-baseline justify-between gap-2 border-b border-ink px-3 py-2 text-sm no-underline hover:bg-tint " +
                  (c.name === active ? "bg-tint font-bold text-link" : "text-ink")
                }
              >
                <span class="truncate">
                  {c.name}
                  {declared && !c.declared && (
                    <span class="ml-1 text-xs font-normal text-dim" title="Not yet in friendo.toml's [content] types" data-adhoc>
                      ✱
                    </span>
                  )}
                </span>
                <span class="text-xs text-dim" data-count={c.name}>
                  {c.count}
                </span>
              </a>
            </li>
          ))}
          <li>
            {naming ? (
              <form onSubmit={startNew} class="px-3 py-2">
                <Input
                  name="new-collection"
                  placeholder="name, e.g. recipes"
                  value={name}
                  autofocus
                  onInput={(e) => setName((e.target as HTMLInputElement).value)}
                  class="mt-0 px-2 py-1 text-xs"
                />
                <p class="mt-1 text-xs text-dim">It appears once its first record is saved.</p>
                <div class="mt-2 flex gap-2">
                  <Button type="submit" variant="primary" size="sm">
                    Start
                  </Button>
                  <Button size="sm" onClick={() => setNaming(false)}>
                    Cancel
                  </Button>
                </div>
              </form>
            ) : (
              <button
                type="button"
                onClick={() => setNaming(true)}
                class="block w-full px-3 py-2 text-left text-sm text-dim hover:bg-tint hover:text-ink"
              >
                + New collection
              </button>
            )}
          </li>
        </ul>

      </div>
    </aside>
  );
}
