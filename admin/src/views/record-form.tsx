import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api, type RecordInput } from "../api";

// Handles both "new" (has collection, no id) and "edit" (has id) routes.
export function RecordForm({ collection, id }: { collection?: string; id?: string }) {
  const { route } = useLocation();
  const editing = !!id;

  const [coll, setColl] = useState(collection || "");
  const [form, setForm] = useState<RecordInput>({ slug: "", title: "", body: "", status: "draft" });
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(editing);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!editing) return;
    api
      .record(id!)
      .then((r) => {
        setColl(r.record.collection || "");
        setForm({
          slug: r.record.slug,
          title: r.record.title,
          body: r.record.body,
          status: r.record.status || "draft",
        });
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load record."))
      .finally(() => setLoading(false));
  }, [id]);

  function set<K extends keyof RecordInput>(key: K, value: RecordInput[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (editing) await api.updateRecord(id!, form);
      else await api.createRecord(coll, form);
      route(`/_/collections/${encodeURIComponent(coll)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed.");
      setBusy(false);
    }
  }

  if (loading) {
    return <div class="mx-auto max-w-3xl px-4 py-8 text-sm text-gray-400">Loading…</div>;
  }

  const field =
    "mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none";

  return (
    <div class="mx-auto max-w-3xl px-4 py-8">
      <div class="mb-6 flex items-center gap-3 text-sm text-gray-500">
        <a href={`/_/collections/${encodeURIComponent(coll)}`} class="hover:text-gray-900">
          {coll}
        </a>
        <span>/</span>
        <span class="text-gray-900">{editing ? "Edit" : "New"}</span>
      </div>
      <h1 class="mb-6 text-xl font-bold">
        {editing ? "Edit record" : `New ${coll} record`}
      </h1>
      {error && (
        <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>
      )}
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <form onSubmit={submit}>
          <label class="mb-4 block text-sm font-medium">
            Title
            <input
              type="text"
              required
              value={form.title}
              onInput={(e) => set("title", (e.target as HTMLInputElement).value)}
              class={field}
            />
          </label>
          <label class="mb-4 block text-sm font-medium">
            Slug
            <input
              type="text"
              required
              value={form.slug}
              onInput={(e) => set("slug", (e.target as HTMLInputElement).value)}
              class={field}
            />
          </label>
          <label class="mb-4 block text-sm font-medium">
            Body
            <textarea
              value={form.body}
              onInput={(e) => set("body", (e.target as HTMLTextAreaElement).value)}
              class={field + " min-h-40 resize-y"}
            />
          </label>
          <label class="mb-5 block text-sm font-medium">
            Status
            <select
              value={form.status}
              onChange={(e) => set("status", (e.target as HTMLSelectElement).value)}
              class={field}
            >
              <option value="draft">Draft</option>
              <option value="published">Published</option>
            </select>
          </label>
          <button
            type="submit"
            disabled={busy}
            class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {busy ? "Saving…" : editing ? "Save" : "Create"}
          </button>
        </form>
      </div>
    </div>
  );
}
