import { useEffect, useState } from "preact/hooks";
import { api, type Collection } from "../api";
import { useAuth } from "../auth";

export function Dashboard() {
  const { user } = useAuth();
  const [collections, setCollections] = useState<Collection[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api
      .collections()
      .then((r) => setCollections(r.collections))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load collections."));
  }, []);

  return (
    <div class="mx-auto max-w-3xl px-4 py-8">
      <div class="mb-6">
        <h1 class="text-xl font-bold">Welcome back, {user.name}</h1>
        <p class="mt-1 text-sm text-gray-500">
          Signed in as {user.email} · {user.role}
        </p>
      </div>

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Collections</h2>
      {error && (
        <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>
      )}
      {collections === null && !error ? (
        <p class="text-sm text-gray-400">Loading…</p>
      ) : (
        collections?.map((c) => (
          <div
            key={c.name}
            class="mb-3 flex items-center justify-between rounded-lg bg-white p-4 shadow-sm"
          >
            <div>
              <span class="font-medium">{c.name}</span>
              <span class="ml-2 text-xs text-gray-400">
                {c.count} record{c.count === 1 ? "" : "s"}
              </span>
            </div>
            <div class="flex gap-2">
              <a
                href={`/_/collections/${encodeURIComponent(c.name)}`}
                class="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700"
              >
                Browse
              </a>
              <a
                href={`/_/collections/${encodeURIComponent(c.name)}/new`}
                class="rounded border border-blue-600 px-3 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50"
              >
                New
              </a>
            </div>
          </div>
        ))
      )}
    </div>
  );
}
