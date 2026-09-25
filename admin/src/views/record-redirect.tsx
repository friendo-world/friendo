import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api } from "../api";

// The old edit address, /_/records/:id/edit, still works: look the record up and
// go to its place in the collection view.
export function RecordRedirect({ id }: { id?: string }) {
  const { route } = useLocation();
  const [error, setError] = useState("");
  useEffect(() => {
    if (!id) return;
    api
      .record(id)
      .then((r) => route(`/_/collections/${encodeURIComponent(r.record.collection || "posts")}/${encodeURIComponent(id)}`, true))
      .catch((e) => setError(e instanceof Error ? e.message : "Record not found."));
  }, [id]);
  return <div class="mx-auto max-w-[960px] px-4 py-8 text-sm text-dim">{error || "Loading…"}</div>;
}
