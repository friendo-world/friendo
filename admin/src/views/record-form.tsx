import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api, type Location, type RecordInput, type When } from "../api";
import { toInput } from "../when";

// Handles both "new" (has collection, no id) and "edit" (has id) routes.
//
// Besides the post's own fields the form has two front-matter sections that any
// post may use: When (the calendar — start, end, all day, repeats, skipped dates)
// and Where (a map pin). When is sent as the reserved keys inside `data`, which
// the API lifts into the post's event; Where talks to the locations API.
export function RecordForm({ collection, id }: { collection?: string; id?: string }) {
  const { route } = useLocation();
  const editing = !!id;

  const [coll, setColl] = useState(collection || "");
  const [form, setForm] = useState<RecordInput>({ slug: "", title: "", body: "", status: "draft" });
  // The record's other front-matter fields, kept as-is so saving never drops them.
  const [extra, setExtra] = useState<{ [key: string]: unknown }>({});
  const [when, setWhen] = useState<WhenForm>(emptyWhen);
  const [where, setWhere] = useState<WhereForm>({ lat: "", lng: "", label: "" });
  const [existingPin, setExistingPin] = useState<Location | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(editing);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!editing) return;
    api
      .record(id!)
      .then(async (r) => {
        setColl(r.record.collection || "");
        setForm({
          slug: r.record.slug,
          title: r.record.title,
          body: r.record.body,
          status: r.record.status || "draft",
        });
        setExtra(r.record.data || {});
        setWhen(whenFromRecord(r.record.when));
        try {
          const pins = await api.locations(id!);
          if (pins.locations.length > 0) {
            const p = pins.locations[0];
            setExistingPin(p);
            setWhere({ lat: String(p.lat), lng: String(p.lng), label: p.label });
          }
        } catch {
          /* no pin, or not allowed to read one */
        }
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load record."))
      .finally(() => setLoading(false));
  }, [id]);

  function set<K extends keyof RecordInput>(key: K, value: RecordInput[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }
  function setW<K extends keyof WhenForm>(key: K, value: WhenForm[K]) {
    setWhen((w) => ({ ...w, [key]: value }));
  }

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const input: RecordInput = { ...form, data: { ...stripWhenKeys(extra), ...whenToData(when) } };
      let recordId = id;
      if (editing) {
        await api.updateRecord(id!, input);
      } else {
        const created = await api.createRecord(coll, input);
        recordId = created.record.id;
      }
      await savePin(recordId!, where, existingPin);
      route(`/_/collections/${encodeURIComponent(coll)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed.");
      setBusy(false);
    }
  }

  if (loading) {
    return <div class="mx-auto max-w-[960px] px-4 py-8 text-sm text-dim">Loading…</div>;
  }

  const field =
    "mt-1 block w-full border border-ink px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none";
  const isEvent = when.starts !== "";

  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <div class="mb-6 flex items-center gap-3 text-sm text-dim">
        <a href={`/_/collections/${encodeURIComponent(coll)}`} class="hover:text-ink">
          {coll}
        </a>
        <span>/</span>
        <span class="text-ink">{editing ? "Edit" : "New"}</span>
      </div>
      <h1 class="mb-6 text-xl font-bold">
        {editing ? "Edit record" : `New ${coll} record`}
      </h1>
      {error && (
        <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>
      )}
      <div class="bg-white p-6 border border-ink">
        <form onSubmit={submit}>
          <label class="mb-4 block text-sm font-bold">
            Title
            <input
              type="text"
              required
              value={form.title}
              onInput={(e) => set("title", (e.target as HTMLInputElement).value)}
              class={field}
            />
          </label>
          <label class="mb-4 block text-sm font-bold">
            Slug
            <input
              type="text"
              required
              value={form.slug}
              onInput={(e) => set("slug", (e.target as HTMLInputElement).value)}
              class={field}
            />
          </label>
          <label class="mb-4 block text-sm font-bold">
            Body
            <textarea
              value={form.body}
              onInput={(e) => set("body", (e.target as HTMLTextAreaElement).value)}
              class={field + " min-h-40 resize-y"}
            />
          </label>
          <label class="mb-5 block text-sm font-bold">
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

          {/* --- When: makes the post an event --- */}
          <fieldset class="mb-5 border border-ink p-4" data-section="when">
            <legend class="px-1 text-sm font-bold">When</legend>
            <p class="mb-3 text-xs text-dim">
              Give the post a time and it becomes an event: listed by date and in the site's
              calendar feed. Leave it empty for an ordinary post.
            </p>
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="block text-sm font-bold">
                Starts
                <input
                  type={when.allDay ? "date" : "datetime-local"}
                  value={when.starts}
                  onInput={(e) => setW("starts", (e.target as HTMLInputElement).value)}
                  class={field}
                  name="when"
                />
              </label>
              <label class="block text-sm font-bold">
                Ends
                <input
                  type={when.allDay ? "date" : "datetime-local"}
                  value={when.ends}
                  onInput={(e) => setW("ends", (e.target as HTMLInputElement).value)}
                  class={field}
                  name="ends"
                />
              </label>
            </div>
            <label class="mt-3 flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={when.allDay}
                onChange={(e) => {
                  const allDay = (e.target as HTMLInputElement).checked;
                  setWhen((w) => ({
                    ...w,
                    allDay,
                    starts: allDay ? w.starts.slice(0, 10) : w.starts.length === 10 ? w.starts + "T09:00" : w.starts,
                    ends: allDay ? w.ends.slice(0, 10) : w.ends.length === 10 ? w.ends + "T10:00" : w.ends,
                  }));
                }}
              />
              All day
            </label>
            <div class="mt-3 grid gap-3 sm:grid-cols-2">
              <label class="block text-sm font-bold">
                Repeats
                <select
                  value={when.repeats}
                  onChange={(e) => setW("repeats", (e.target as HTMLSelectElement).value as WhenForm["repeats"])}
                  class={field}
                  name="repeats"
                  disabled={!isEvent}
                >
                  <option value="">Never</option>
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                  <option value="every 2 weeks">Every 2 weeks</option>
                  <option value="monthly">Monthly</option>
                  <option value="yearly">Yearly</option>
                  <option value="custom">Custom rule…</option>
                </select>
              </label>
              {when.repeats && when.repeats !== "custom" && (
                <label class="block text-sm font-bold">
                  Until
                  <input
                    type="date"
                    value={when.until}
                    onInput={(e) => setW("until", (e.target as HTMLInputElement).value)}
                    class={field}
                    name="until"
                  />
                </label>
              )}
              {when.repeats === "custom" && (
                <label class="block text-sm font-bold">
                  Rule (iCalendar RRULE)
                  <input
                    type="text"
                    value={when.rrule}
                    placeholder="FREQ=MONTHLY;BYDAY=1TU"
                    onInput={(e) => setW("rrule", (e.target as HTMLInputElement).value)}
                    class={field + " font-mono"}
                    name="rrule"
                  />
                </label>
              )}
            </div>
            {when.repeats && (
              <label class="mt-3 block text-sm font-bold">
                Skip these dates
                <input
                  type="text"
                  value={when.except}
                  placeholder="2026-11-25, 2026-12-23"
                  onInput={(e) => setW("except", (e.target as HTMLInputElement).value)}
                  class={field}
                  name="except"
                />
              </label>
            )}
            <label class="mt-3 block text-sm font-bold">
              Timezone
              <input
                type="text"
                value={when.timezone}
                placeholder="the site's (set in friendo.toml)"
                onInput={(e) => setW("timezone", (e.target as HTMLInputElement).value)}
                class={field}
                name="timezone"
              />
            </label>
          </fieldset>

          {/* --- Where: a map pin --- */}
          <fieldset class="mb-5 border border-ink p-4" data-section="where">
            <legend class="px-1 text-sm font-bold">Where</legend>
            <p class="mb-3 text-xs text-dim">
              A latitude and longitude pin the post on the map (and put the place in the calendar
              feed). Clear both to remove the pin.
            </p>
            <div class="grid gap-3 sm:grid-cols-3">
              <label class="block text-sm font-bold">
                Latitude
                <input
                  type="text"
                  inputMode="decimal"
                  value={where.lat}
                  onInput={(e) => setWhere((w) => ({ ...w, lat: (e.target as HTMLInputElement).value }))}
                  class={field}
                  name="lat"
                />
              </label>
              <label class="block text-sm font-bold">
                Longitude
                <input
                  type="text"
                  inputMode="decimal"
                  value={where.lng}
                  onInput={(e) => setWhere((w) => ({ ...w, lng: (e.target as HTMLInputElement).value }))}
                  class={field}
                  name="lng"
                />
              </label>
              <label class="block text-sm font-bold">
                Place
                <input
                  type="text"
                  value={where.label}
                  placeholder="defaults to the title"
                  onInput={(e) => setWhere((w) => ({ ...w, label: (e.target as HTMLInputElement).value }))}
                  class={field}
                  name="place"
                />
              </label>
            </div>
          </fieldset>

          <button
            type="submit"
            disabled={busy}
            class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50"
          >
            {busy ? "Saving…" : editing ? "Save" : "Create"}
          </button>
        </form>
      </div>
    </div>
  );
}

// --- When ---

type WhenForm = {
  starts: string; // datetime-local ("2026-10-04T19:00") or date when allDay
  ends: string;
  allDay: boolean;
  repeats: "" | "daily" | "weekly" | "every 2 weeks" | "monthly" | "yearly" | "custom";
  until: string;
  rrule: string;
  except: string; // comma-separated dates
  timezone: string;
};

const emptyWhen: WhenForm = { starts: "", ends: "", allDay: false, repeats: "", until: "", rrule: "", except: "", timezone: "" };

const WHEN_KEYS = ["when", "ends", "all_day", "timezone", "repeats", "except", "rrule"];

function stripWhenKeys(data: { [key: string]: unknown }): { [key: string]: unknown } {
  const out = { ...data };
  for (const k of WHEN_KEYS) delete out[k];
  return out;
}

// whenFromRecord fills the form from the API's `when`. A simple rule maps onto the
// Repeats menu; anything else shows as a custom RRULE so nothing is lost.
function whenFromRecord(w: When | null | undefined): WhenForm {
  if (!w || !w.starts) return emptyWhen;
  const f: WhenForm = {
    ...emptyWhen,
    allDay: w.all_day,
    starts: toInput(w.starts, w.all_day),
    ends: toInput(w.ends, w.all_day),
    except: (w.except || []).join(", "),
  };
  if (w.timezone && w.timezone !== "UTC") f.timezone = w.timezone;
  if (w.rule) {
    const parts = Object.fromEntries(w.rule.split(";").map((p) => p.split("=") as [string, string]));
    const simple = ["FREQ", "INTERVAL", "UNTIL"];
    const onlySimple = Object.keys(parts).every((k) => simple.includes(k));
    const freq = { DAILY: "daily", WEEKLY: "weekly", MONTHLY: "monthly", YEARLY: "yearly" }[parts.FREQ] as WhenForm["repeats"] | undefined;
    if (onlySimple && freq && (!parts.INTERVAL || (parts.INTERVAL === "2" && freq === "weekly"))) {
      f.repeats = parts.INTERVAL === "2" ? "every 2 weeks" : freq;
      if (parts.UNTIL) f.until = untilToDate(parts.UNTIL, w.starts);
    } else {
      f.repeats = "custom";
      f.rrule = w.rule;
    }
  }
  return f;
}

// untilToDate reads an UNTIL (a UTC stamp, or a date) back into the date the
// author meant, using the event's own offset.
function untilToDate(until: string, starts: string): string {
  if (/^\d{8}$/.test(until)) return `${until.slice(0, 4)}-${until.slice(4, 6)}-${until.slice(6, 8)}`;
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/.exec(until);
  if (!m) return "";
  const utc = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]);
  const off = /([+-])(\d{2}):(\d{2})$/.exec(starts);
  const minutes = off ? (off[1] === "-" ? -1 : 1) * (+off[2] * 60 + +off[3]) : 0;
  const local = new Date(utc + minutes * 60000);
  return local.toISOString().slice(0, 10);
}

// whenToData turns the form into the reserved keys the server lifts. An empty
// start means "not an event": `when: null` removes any existing event (the API
// leaves the event alone when `when` is simply absent).
function whenToData(w: WhenForm): { [key: string]: unknown } {
  if (!w.starts) return { when: null };
  const out: { [key: string]: unknown } = { when: w.starts.replace("T", " ") };
  if (w.ends) out.ends = w.ends.replace("T", " ");
  if (w.allDay) out.all_day = true;
  if (w.timezone.trim()) out.timezone = w.timezone.trim();
  if (w.repeats === "custom") {
    if (w.rrule.trim()) out.rrule = w.rrule.trim();
  } else if (w.repeats) {
    if (w.until) {
      const every = w.repeats === "every 2 weeks" ? "2 weeks" : { daily: "day", weekly: "week", monthly: "month", yearly: "year" }[w.repeats];
      out.repeats = { every, until: w.until };
    } else {
      out.repeats = w.repeats;
    }
  }
  if (w.repeats && w.except.trim()) {
    out.except = w.except.split(",").map((s) => s.trim()).filter(Boolean);
  }
  return out;
}

// --- Where ---

type WhereForm = { lat: string; lng: string; label: string };

// savePin reconciles the post's map pin with the form: add, replace, or remove.
async function savePin(recordId: string, where: WhereForm, existing: Location | null) {
  const lat = parseFloat(where.lat);
  const lng = parseFloat(where.lng);
  const has = where.lat.trim() !== "" && where.lng.trim() !== "";
  if (has && (isNaN(lat) || isNaN(lng))) throw new Error("Latitude and longitude must be numbers.");
  const unchanged =
    existing && has && existing.lat === lat && existing.lng === lng && (existing.label === where.label || where.label === "");
  if (unchanged) return;
  if (existing && (!has || existing.lat !== lat || existing.lng !== lng || existing.label !== where.label)) {
    await api.removeLocation(existing.id);
  }
  if (has) {
    await api.addLocation(recordId, lat, lng, where.label.trim());
  }
}
