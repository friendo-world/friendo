import { useEffect, useState } from "preact/hooks";
import { api, type RSVPName, type Record } from "../api";
import { formatWhen } from "../when";

const LABEL = { going: "Going", maybe: "Maybe", not_going: "Can't go", invited: "Invited (no answer yet)" } as const;

// Who answered an event's RSVP, grouped by date (a repeating event has one
// group per date), with a CSV download. Reachable from a collection's list for
// any post that has a time.
export function RSVPs({ id }: { id?: string }) {
  const [record, setRecord] = useState<Record | null>(null);
  const [rows, setRows] = useState<RSVPName[] | null>(null);
  const [error, setError] = useState("");
  const [who, setWho] = useState("");
  const [group, setGroup] = useState("");
  const [followers, setFollowers] = useState(false);
  const [result, setResult] = useState("");

  function load() {
    if (!id) return;
    Promise.all([api.record(id), api.rsvpNames(id)])
      .then(([r, a]) => {
        setRecord(r.post);
        setRows(a.names);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load RSVPs."));
  }
  useEffect(load, [id]);

  async function invite() {
    if (!id) return;
    setResult("");
    try {
      const slugs = who.split(/[\s,]+/).map((s) => s.trim().replace(/^\/profiles\//, "")).filter(Boolean);
      const r = await api.invite(id, { slugs, group: group.trim() || undefined, followers });
      setResult(`Invited ${r.invited}` + (r.skipped ? `, ${r.skipped} had already answered` : "") + (r.unknown.length ? `; no profile called ${r.unknown.join(", ")}` : "") + ".");
      setWho("");
      load();
    } catch (e) {
      setResult(e instanceof Error ? e.message : "Inviting failed.");
    }
  }

  const groups: { key: string; text: string; scheduled: boolean; rows: RSVPName[] }[] = [];
  for (const r of rows || []) {
    let g = groups.find((x) => x.key === r.date);
    if (!g) {
      g = { key: r.date, text: r.date_text || r.date, scheduled: r.scheduled, rows: [] };
      groups.push(g);
    }
    g.rows.push(r);
  }
  const tally = (list: RSVPName[], answer: RSVPName["answer"]) => list.filter((r) => r.answer === answer).length;

  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <div class="mb-6 flex items-center gap-3 text-sm text-dim">
        {record?.collection && (
          <>
            <a href={`/_/collections/${encodeURIComponent(record.collection)}`} class="hover:text-ink">
              {record.collection}
            </a>
            <span>/</span>
          </>
        )}
        <span class="text-ink">RSVPs</span>
      </div>
      <div class="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-bold">{record?.title || "RSVPs"}</h1>
          {record?.when && <p class="text-sm text-dim">{formatWhen(record.when)}</p>}
        </div>
        {id && (
          <a
            href={`/_/api/posts/${encodeURIComponent(id)}/rsvps/names?format=csv`}
            class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link"
            download
          >
            Download CSV
          </a>
        )}
      </div>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

      <section class="mb-6 bg-white border border-ink p-4" data-invite>
        <h2 class="mb-2 text-sm font-bold">Invite people</h2>
        <p class="mb-3 text-xs text-dim">
          Each person gets an RSVP waiting for their answer, and a note in their inbox. Profile names (pat, sam),
          a group's members, or everyone who follows you.
        </p>
        <div class="grid gap-3 sm:grid-cols-[1fr_auto_auto_auto] sm:items-end">
          <label class="block text-xs font-bold text-dim">
            Profile names
            <input type="text" name="invite-who" value={who} placeholder="pat, sam" onInput={(e) => setWho((e.target as HTMLInputElement).value)}
              class="mt-1 block w-full border border-ink px-2 py-1 text-sm font-normal text-ink" />
          </label>
          <label class="block text-xs font-bold text-dim">
            Group
            <input type="text" name="invite-group" value={group} placeholder="board" onInput={(e) => setGroup((e.target as HTMLInputElement).value)}
              class="mt-1 block w-32 border border-ink px-2 py-1 text-sm font-normal text-ink" />
          </label>
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" name="invite-followers" checked={followers} onChange={(e) => setFollowers((e.target as HTMLInputElement).checked)} />
            my followers
          </label>
          <button type="button" onClick={invite} disabled={!who.trim() && !group.trim() && !followers}
            class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50">
            Invite
          </button>
        </div>
        {result && <p class="mt-2 text-xs text-dim" data-invite-result>{result}</p>}
      </section>

      {rows === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : groups.length === 0 ? (
        <div class="bg-white p-6 text-center border border-ink">
          <p class="text-sm text-dim">Nobody has answered yet.</p>
        </div>
      ) : (
        <div class="space-y-6">
          {groups.map((g) => (
            <section key={g.key} class="bg-white border border-ink">
              <header class="flex flex-wrap items-baseline justify-between gap-2 border-b border-ink px-4 py-3">
                <span class="font-bold">
                  {g.text}
                  {!g.scheduled && (
                    <span class="ml-2 bg-tint px-1.5 py-0.5 text-xs font-normal text-crimson">no longer scheduled</span>
                  )}
                </span>
                <span class="text-xs text-dim">
                  {tally(g.rows, "going")} going · {tally(g.rows, "maybe")} maybe · {tally(g.rows, "not_going")} can't go · {tally(g.rows, "invited")} invited
                </span>
              </header>
              <table class="w-full">
                <thead>
                  <tr class="border-b border-ink">
                    <th class="px-4 py-2 text-left text-xs font-bold text-dim">Name</th>
                    <th class="px-4 py-2 text-left text-xs font-bold text-dim">Email</th>
                    <th class="px-4 py-2 text-left text-xs font-bold text-dim">Answer</th>
                    <th class="px-4 py-2 text-left text-xs font-bold text-dim">Answered</th>
                  </tr>
                </thead>
                <tbody>
                  {g.rows.map((r) => (
                    <tr key={r.id} class="border-b border-ink last:border-b-0">
                      <td class="px-4 py-2 text-sm font-bold">{r.author_name || "Anonymous"}</td>
                      <td class="px-4 py-2 text-sm">{r.author_email}</td>
                      <td class="px-4 py-2 text-sm">{LABEL[r.answer] || r.answer}</td>
                      <td class="px-4 py-2 text-xs text-dim">{r.updated?.replace("T", " ").replace("Z", "")}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
