import { useEffect, useState } from "preact/hooks";
import { api, type PendingRecord } from "../api";
import { formatWhen } from "../when";
import { VisitorsOnly } from "./moderation";

// Posts waiting for a moderator, as a section of the Review page.
export function ReviewQueue() {
  const [records, setRecords] = useState<PendingRecord[] | null>(null);
  const [error, setError] = useState("");
  const [visitorsOnly, setVisitorsOnly] = useState(false);

  function load() {
    setRecords(null);
    setError("");
    api
      .recordsByStatus("pending")
      .then((r) => setRecords(r.posts))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load the posts waiting for review."));
  }

  useEffect(load, []);

  async function publish(id: string) {
    try {
      await api.setRecordStatus(id, "published");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Publish failed.");
    }
  }

  async function remove(id: string) {
    if (!confirm("Delete this post?")) return;
    try {
      await api.deleteRecord(id);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Delete failed.");
    }
  }

  return (
    <section data-section="review">
      <div class="mb-1 flex items-center justify-between">
        <h2 class="text-sm font-bold text-dim">Posts to review</h2>
        <VisitorsOnly on={visitorsOnly} onToggle={setVisitorsOnly} />
      </div>
      <p class="mb-3 text-xs text-dim">
        Posts waiting for review. Publishing makes them live on the site.
      </p>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

      {records === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : records && records.some((r) => !visitorsOnly || r.visitor) ? (
        <ul class="space-y-3">
          {records.filter((r) => !visitorsOnly || r.visitor).map((r) => (
            <li key={r.id} class="bg-white p-4 border border-ink">
              <div class="flex gap-4">
                {r.image && (
                  <a href={r.image} target="_blank" rel="noreferrer" class="shrink-0" title="Open the full image">
                    <img
                      src={r.image}
                      alt=""
                      loading="lazy"
                      class="h-20 w-20 border border-ink object-cover"
                    />
                  </a>
                )}
                <div class="min-w-0 flex-1">
                  <div class="mb-2 flex items-baseline justify-between gap-3">
                    <span class="font-bold">{r.title || r.slug || "(untitled)"}</span>
                    <span class="text-xs text-dim">{r.collection}</span>
                  </div>
                  {r.when && <div class="mb-1 text-sm">📅 {formatWhen(r.when)}</div>}
                  <div class="mb-3 text-xs text-dim">
                    by {r.author_name || "Anonymous"} · {r.created?.replace("T", " ").replace("Z", "")}
                  </div>
                </div>
              </div>
              <div class="flex justify-end gap-2">
                <a
                  href={`/_/collections/${encodeURIComponent(r.collection)}/${encodeURIComponent(r.id)}`}
                  class="bg-tint px-2 py-1 text-xs font-bold text-dim hover:bg-manila"
                >
                  Edit
                </a>
                <button
                  onClick={() => publish(r.id)}
                  class="bg-ink px-2 py-1 text-xs font-bold text-white hover:bg-link"
                >
                  Publish
                </button>
                <button
                  onClick={() => remove(r.id)}
                  class="bg-crimson px-2 py-1 text-xs font-bold text-white hover:bg-ink"
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div class="bg-white p-6 text-center border border-ink">
          <p class="text-sm text-dim">Nothing awaiting review.</p>
        </div>
      )}
    </section>
  );
}
