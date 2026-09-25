import { useEffect, useState } from "preact/hooks";
import { api, type Record } from "../api";
import { formatWhen } from "../when";

export function CollectionView({ collection }: { collection?: string }) {
  const name = collection || "";
  const [records, setRecords] = useState<Record[] | null>(null);
  const [error, setError] = useState("");

  function load() {
    setRecords(null);
    api
      .records(name)
      .then((r) => setRecords(r.records))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load records."));
  }

  useEffect(load, [name]);

  async function remove(id: string) {
    if (!confirm("Delete this record?")) return;
    try {
      await api.deleteRecord(id);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Delete failed.");
    }
  }

  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">{name}</h1>
        <a
          href={`/_/collections/${encodeURIComponent(name)}/new`}
          class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link"
        >
          New record
        </a>
      </div>

      {error && (
        <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>
      )}

      {records === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : records && records.length > 0 ? (
        <div class="overflow-x-auto bg-white border border-ink">
          <table class="w-full">
            <thead>
              <tr class="border-b border-ink">
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Title</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Slug</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Status</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">When</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Created</th>
                <th class="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {records.map((r) => (
                <tr key={r.id} class="border-b border-ink hover:bg-tint">
                  <td class="px-4 py-3 text-sm font-bold">{r.title}</td>
                  <td class="px-4 py-3 text-sm">
                    <code class="bg-tint px-1.5 py-0.5 text-xs">{r.slug}</code>
                  </td>
                  <td class="px-4 py-3 text-sm">{r.status}</td>
                  <td class="px-4 py-3 text-xs text-dim">
                    {r.when ? (
                      <span title={r.when.repeats ? `Next: ${formatWhen(r.when, { next: true })}` : undefined}>
                        {formatWhen(r.when)}
                      </span>
                    ) : (
                      ""
                    )}
                  </td>
                  <td class="px-4 py-3 text-xs text-dim">{r.created}</td>
                  <td class="px-4 py-3">
                    <div class="flex justify-end gap-2">
                      {r.when && (
                        <a
                          href={`/_/records/${encodeURIComponent(r.id)}/attendees`}
                          class="bg-tint px-2 py-1 text-xs font-bold text-dim hover:bg-manila"
                        >
                          Attendees
                        </a>
                      )}
                      <a
                        href={`/_/records/${encodeURIComponent(r.id)}/edit`}
                        class="bg-ink px-2 py-1 text-xs font-bold text-white hover:bg-link"
                      >
                        Edit
                      </a>
                      <button
                        onClick={() => remove(r.id)}
                        class="bg-crimson px-2 py-1 text-xs font-bold text-white hover:bg-ink"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div class="bg-white p-6 text-center border border-ink">
          <p class="text-sm text-dim">
            No records yet.{" "}
            <a
              href={`/_/collections/${encodeURIComponent(name)}/new`}
              class="text-link hover:underline"
            >
              Create one
            </a>
            .
          </p>
        </div>
      )}
    </div>
  );
}
