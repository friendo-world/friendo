import { useEffect, useState } from "preact/hooks";
import { api, type Record } from "../api";

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
    <div class="mx-auto max-w-3xl px-4 py-8">
      <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">{name}</h1>
        <a
          href={`/_/collections/${encodeURIComponent(name)}/new`}
          class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
        >
          New record
        </a>
      </div>

      {error && (
        <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>
      )}

      {records === null && !error ? (
        <p class="text-sm text-gray-400">Loading…</p>
      ) : records && records.length > 0 ? (
        <div class="overflow-x-auto rounded-lg bg-white shadow-sm">
          <table class="w-full">
            <thead>
              <tr class="border-b border-gray-200">
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Title</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Slug</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Status</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Created</th>
                <th class="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {records.map((r) => (
                <tr key={r.id} class="border-b border-gray-100 hover:bg-gray-50">
                  <td class="px-4 py-3 text-sm font-medium">{r.title}</td>
                  <td class="px-4 py-3 text-sm">
                    <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs">{r.slug}</code>
                  </td>
                  <td class="px-4 py-3 text-sm">{r.status}</td>
                  <td class="px-4 py-3 text-xs text-gray-400">{r.created}</td>
                  <td class="px-4 py-3">
                    <div class="flex justify-end gap-2">
                      <a
                        href={`/_/records/${encodeURIComponent(r.id)}/edit`}
                        class="rounded bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700"
                      >
                        Edit
                      </a>
                      <button
                        onClick={() => remove(r.id)}
                        class="rounded bg-red-600 px-2 py-1 text-xs font-medium text-white hover:bg-red-700"
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
        <div class="rounded-lg bg-white p-6 text-center shadow-sm">
          <p class="text-sm text-gray-500">
            No records yet.{" "}
            <a
              href={`/_/collections/${encodeURIComponent(name)}/new`}
              class="text-blue-600 hover:underline"
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
