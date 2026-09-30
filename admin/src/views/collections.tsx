import { useEffect, useMemo, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api, can, type Collection, type Record as Rec } from "../api";
import { useAuth } from "../auth";
import { useToast } from "../components/toast";
import { ContentShell } from "../components/content-shell";
import { Card, ErrorBox, LinkButton } from "../components/ui";
import { inferFields } from "../record/fields";
import { RecordsTable, type BulkAction } from "./records-table";
import { RecordPanel } from "./record-panel";

// The content area: collections down the left, the chosen collection's records
// in the middle, and — when the URL names a record (or "new") — the editor as a
// panel over the list. One component for all three, so the table keeps its
// search, sort and selection while a record is open.
export function CollectionsArea({ collection, id }: { collection?: string; id?: string }) {
  const { user } = useAuth();
  const { route } = useLocation();
  const toast = useToast();
  const name = collection || "";

  const [collections, setCollections] = useState<Collection[] | null>(null);
  const [declared, setDeclared] = useState(false);
  const [records, setRecords] = useState<Rec[] | null>(null);
  const [error, setError] = useState("");
  const [bulkProgress, setBulkProgress] = useState("");


  useEffect(() => {
    api
      .collections()
      .then((r) => {
        setCollections(r.collections);
        setDeclared(!!r.declared);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load collections."));
  }, []);

  function load(quiet = false) {
    if (!name) return;
    if (!quiet) setRecords(null);
    setError("");
    api
      .records(name)
      .then((r) => setRecords(r.posts.map((x) => ({ ...x, collection: x.collection || name }))))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load posts."));
  }
  useEffect(() => load(), [name]);

  const declaredFields = useMemo(() => collections?.find((c) => c.name === name)?.fields || [], [collections, name]);
  const fields = useMemo(() => inferFields(records || [], declaredFields), [records, declaredFields]);

  function bumpCount(delta: number) {
    setCollections((cs) => {
      if (!cs) return cs;
      const known = cs.some((c) => c.name === name);
      const next = known ? cs.map((c) => (c.name === name ? { ...c, count: Math.max(0, c.count + delta) } : c)) : [...cs, { name, count: Math.max(0, delta), declared: false, fields: [] }];
      return next;
    });
  }

  function saved(record: Rec, created: boolean) {
    setRecords((rs) => {
      const list = rs || [];
      return created ? [record, ...list] : list.map((r) => (r.id === record.id ? record : r));
    });
    if (created) bumpCount(1);
    toast(created ? "Created" : "Saved");
    route(`/_/collections/${encodeURIComponent(name)}`);
  }

  function deleted(recordId: string) {
    setRecords((rs) => (rs || []).filter((r) => r.id !== recordId));
    bumpCount(-1);
    toast("Deleted");
    route(`/_/collections/${encodeURIComponent(name)}`);
  }

  // Bulk actions are one call per record; failures are collected, not fatal.
  async function bulk(action: BulkAction, ids: string[]) {
    const failures: string[] = [];
    let n = 0;
    const done: { [id: string]: Rec | null } = {};
    for (const recordId of ids) {
      n++;
      setBulkProgress(`${n} of ${ids.length}…`);
      try {
        if (action === "delete") {
          await api.deleteRecord(recordId);
          done[recordId] = null;
        } else {
          const r = await api.setRecordStatus(recordId, action === "publish" ? "published" : "draft");
          done[recordId] = { ...r.post, collection: r.post.collection || name };
        }
      } catch (e) {
        failures.push(e instanceof Error ? e.message : "failed");
      }
    }
    setBulkProgress("");
    setRecords((rs) =>
      (rs || [])
        .filter((r) => !(r.id in done && done[r.id] === null))
        .map((r) => (done[r.id] ? { ...r, ...done[r.id] } : r))
    );
    const ok = ids.length - failures.length;
    if (action === "delete") bumpCount(-Object.values(done).filter((v) => v === null).length);
    const verb = action === "delete" ? "deleted" : action === "publish" ? "published" : "unpublished";
    if (failures.length) setError(`${ok} ${verb}, ${failures.length} failed: ${failures[0]}`);
    else toast(`${ok} post${ok === 1 ? "" : "s"} ${verb}`);
    load(true);
  }

  const initial = id && id !== "new" ? records?.find((r) => r.id === id) || null : null;
  // friendo.toml names its collections, and this one isn't among them yet.
  const unsynced =
    declared && can(user.role, "site.configure") && collections !== null && !collections.find((c) => c.name === name)?.declared;

  return (
    <ContentShell collection={name} collections={collections} declared={declared}>
      <>
        {!name ? (
          <Card>
            <h1 class="mb-2 text-xl font-bold">Content</h1>
            <p class="text-sm text-dim">
              Pick a collection on the left to see its posts, or start writing:
            </p>
            <div class="mt-4 flex flex-wrap gap-2">
              {(collections || []).slice(0, 3).map((c) => (
                <LinkButton key={c.name} href={`/_/collections/${encodeURIComponent(c.name)}/new`} size="sm">
                  New in {c.name}
                </LinkButton>
              ))}
            </div>
          </Card>
        ) : (
          <>
            <div class="mb-4 flex items-start justify-between gap-3">
              <div>
                <h1 class="text-xl font-bold">{name}</h1>
                {collections?.find((c) => c.name === name)?.drop_box && (
                  <p class="mt-1 text-xs text-dim" data-drop-box>
                    A drop box: anyone can post here, and no one's name is kept, even for posts made
                    from the admin. Everything waits in Review.
                  </p>
                )}
                {collections?.find((c) => c.name === name)?.drop_box_closed && (
                  <p class="mt-1 border border-crimson px-2 py-1 text-xs text-crimson" data-drop-box-closed>
                    This was a drop box until its <code>drop-box</code> form went away. Posts sent here now keep
                    their sender's name. Put <code>drop-box</code> back on the form to make it one again.{" "}
                    {can(user.role, "site.configure") && (
                      <button
                        class="font-bold underline"
                        onClick={async () => {
                          await api.dismissDropBoxNotice(name);
                          setCollections((cs) => cs && cs.map((c) => (c.name === name ? { ...c, drop_box_closed: false } : c)));
                        }}
                      >
                        Dismiss
                      </button>
                    )}
                  </p>
                )}
                {unsynced && (
                  <p class="mt-1 text-xs text-dim" data-unsynced>
                    ✱ Not in your <code>friendo.toml</code> yet.{" "}
                    <a href="/_/settings#content" class="text-link hover:underline" data-toml-link>
                      Copy the [content] block
                    </a>{" "}
                    from Settings, or run <code>friendo pull</code>.
                  </p>
                )}
              </div>
              <LinkButton href={`/_/collections/${encodeURIComponent(name)}/new`} variant="primary">
                New post
              </LinkButton>
            </div>
            {error && <ErrorBox class="mb-4">{error}</ErrorBox>}
            {records === null && !error ? (
              <p class="text-sm text-dim">Loading…</p>
            ) : records && records.length > 0 ? (
              <RecordsTable
                collection={name}
                records={records}
                fields={fields}
                canPublish={can(user.role, "content.publish")}
                bulkProgress={bulkProgress}
                onOpen={(recordId) => route(`/_/collections/${encodeURIComponent(name)}/${encodeURIComponent(recordId)}`)}
                onBulk={bulk}
              />
            ) : (
              records && (
                <Card class="text-center">
                  <p class="text-sm text-dim">
                    No posts yet.{" "}
                    <a href={`/_/collections/${encodeURIComponent(name)}/new`} class="text-link hover:underline">
                      Create one
                    </a>
                    .
                  </p>
                </Card>
              )
            )}
          </>
        )}
      </>

      {name && id && (
        <RecordPanel
          key={id}
          collection={name}
          id={id}
          initial={initial}
          fields={fields}
          onSaved={saved}
          onDeleted={deleted}
          onClose={() => route(`/_/collections/${encodeURIComponent(name)}`)}
        />
      )}
    </ContentShell>
  );
}
