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
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <div class="mb-6">
        <h1 class="text-xl font-bold">Welcome back, {user.name}</h1>
        <p class="mt-1 text-sm text-dim">
          Signed in as {user.email} · {user.role}
        </p>
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Collections</h2>
      {error && (
        <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>
      )}
      {collections === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : (
        collections?.map((c) => (
          <div
            key={c.name}
            class="mb-3 flex items-center justify-between bg-white p-4 border border-ink"
          >
            <div>
              <span class="font-bold">{c.name}</span>
              <span class="ml-2 text-xs text-dim">
                {c.count} record{c.count === 1 ? "" : "s"}
              </span>
            </div>
            <div class="flex gap-2">
              <a
                href={`/_/collections/${encodeURIComponent(c.name)}`}
                class="bg-ink px-3 py-1 text-xs font-bold text-white hover:bg-link"
              >
                Browse
              </a>
              <a
                href={`/_/collections/${encodeURIComponent(c.name)}/new`}
                class="border border-ink px-3 py-1 text-xs font-bold text-ink hover:bg-tint"
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
