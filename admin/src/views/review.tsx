import { useEffect, useState } from "preact/hooks";
import { api, type PendingRecord } from "../api";

export function Review() {
  const [records, setRecords] = useState<PendingRecord[] | null>(null);
  const [error, setError] = useState("");

  function load() {
    setRecords(null);
    setError("");
    api
      .recordsByStatus("pending")
      .then((r) => setRecords(r.records))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load pending posts."));
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
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <h1 class="mb-1 text-xl font-bold">Review</h1>
      <p class="mb-6 text-sm text-dim">
        Posts awaiting approval. Publishing makes them live on the site.
      </p>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

      {records === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : records && records.length > 0 ? (
        <ul class="space-y-3">
          {records.map((r) => (
            <li key={r.id} class="bg-white p-4 border border-ink">
              <div class="mb-2 flex items-baseline justify-between gap-3">
                <span class="font-bold">{r.title || r.slug || "(untitled)"}</span>
                <span class="text-xs text-dim">{r.collection}</span>
              </div>
              <div class="mb-3 text-xs text-dim">
                by {r.author_name || "Anonymous"} · {r.created?.replace("T", " ").replace("Z", "")}
              </div>
              <div class="flex justify-end gap-2">
                <a
                  href={`/_/records/${encodeURIComponent(r.id)}/edit`}
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
    </div>
  );
}
