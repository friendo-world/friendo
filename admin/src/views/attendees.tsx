import { useEffect, useState } from "preact/hooks";
import { api, type Attendee, type Record } from "../api";
import { formatWhen } from "../when";

const LABEL = { going: "Going", maybe: "Maybe", not_going: "Can't go" } as const;

// Who answered an event's RSVP, grouped by occurrence (a repeating event has one
// group per date), with a CSV download. Reachable from a collection's list for
// any record that has a time.
export function Attendees({ id }: { id?: string }) {
  const [record, setRecord] = useState<Record | null>(null);
  const [rows, setRows] = useState<Attendee[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!id) return;
    Promise.all([api.record(id), api.attendees(id)])
      .then(([r, a]) => {
        setRecord(r.record);
        setRows(a.attendees);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load attendees."));
  }, [id]);

  const groups: { key: string; text: string; scheduled: boolean; rows: Attendee[] }[] = [];
  for (const r of rows || []) {
    let g = groups.find((x) => x.key === r.occurrence);
    if (!g) {
      g = { key: r.occurrence, text: r.occurrence_text || r.occurrence, scheduled: r.scheduled, rows: [] };
      groups.push(g);
    }
    g.rows.push(r);
  }
  const tally = (list: Attendee[], answer: Attendee["answer"]) => list.filter((r) => r.answer === answer).length;

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
        <span class="text-ink">Attendees</span>
      </div>
      <div class="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-bold">{record?.title || "Attendees"}</h1>
          {record?.when && <p class="text-sm text-dim">{formatWhen(record.when)}</p>}
        </div>
        {id && (
          <a
            href={`/_/api/posts/${encodeURIComponent(id)}/attendees?format=csv`}
            class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link"
            download
          >
            Download CSV
          </a>
        )}
      </div>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

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
                  {tally(g.rows, "going")} going · {tally(g.rows, "maybe")} maybe · {tally(g.rows, "not_going")} can't go
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
