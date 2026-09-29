import { api, type Location, type When } from "../api";
import { toInput } from "../when";

// The two front-matter sections any post may use: When (the calendar — start,
// end, all day, repeats, skipped dates) and Location (a place on the map). When
// is sent as one `when` key inside `fields`, which the API lifts into the post's
// event; Location talks to the locations API.

// --- When ---

export type WhenForm = {
  start: string; // datetime-local ("2026-10-04T19:00") or date when allDay
  end: string;
  allDay: boolean;
  repeats: "" | "daily" | "weekly" | "every 2 weeks" | "monthly" | "yearly" | "custom";
  until: string;
  rrule: string;
  except: string; // comma-separated dates
  timezone: string;
};

export const emptyWhen: WhenForm = { start: "", end: "", allDay: false, repeats: "", until: "", rrule: "", except: "", timezone: "" };

// The one name the server reads out of `fields` and turns into the post's event.
export const WHEN_KEYS = ["when"];

export function stripWhenKeys(data: { [key: string]: unknown }): { [key: string]: unknown } {
  const out = { ...data };
  for (const k of WHEN_KEYS) delete out[k];
  return out;
}

// whenFromRecord fills the form from the API's `when`. A simple rule maps onto the
// Repeats menu; anything else shows as a custom RRULE so nothing is lost.
export function whenFromRecord(w: When | null | undefined): WhenForm {
  if (!w || !w.start) return emptyWhen;
  const f: WhenForm = {
    ...emptyWhen,
    allDay: w.all_day,
    start: toInput(w.start, w.all_day),
    end: toInput(w.end, w.all_day),
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
      if (parts.UNTIL) f.until = untilToDate(parts.UNTIL, w.start);
    } else {
      f.repeats = "custom";
      f.rrule = w.rule;
    }
  }
  return f;
}

// untilToDate reads an UNTIL (a UTC stamp, or a date) back into the date the
// author meant, using the event's own offset.
export function untilToDate(until: string, starts: string): string {
  if (/^\d{8}$/.test(until)) return `${until.slice(0, 4)}-${until.slice(4, 6)}-${until.slice(6, 8)}`;
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/.exec(until);
  if (!m) return "";
  const utc = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]);
  const off = /([+-])(\d{2}):(\d{2})$/.exec(starts);
  const minutes = off ? (off[1] === "-" ? -1 : 1) * (+off[2] * 60 + +off[3]) : 0;
  const local = new Date(utc + minutes * 60000);
  return local.toISOString().slice(0, 10);
}

// whenToData turns the form into the `when` map the server lifts. An empty
// start means "not an event": `when: null` removes any existing event (the API
// leaves the event alone when `when` is simply absent).
export function whenToData(w: WhenForm): { [key: string]: unknown } {
  if (!w.start) return { when: null };
  const when: { [key: string]: unknown } = { start: w.start.replace("T", " ") };
  if (w.end) when.end = w.end.replace("T", " ");
  if (w.allDay) when.all_day = true;
  if (w.timezone.trim()) when.timezone = w.timezone.trim();
  if (w.repeats === "custom") {
    if (w.rrule.trim()) when.rrule = w.rrule.trim();
  } else if (w.repeats) {
    if (w.until) {
      const every = w.repeats === "every 2 weeks" ? "2 weeks" : { daily: "day", weekly: "week", monthly: "month", yearly: "year" }[w.repeats];
      when.repeats = { every, until: w.until };
    } else {
      when.repeats = w.repeats;
    }
  }
  if (w.repeats && w.except.trim()) {
    when.except = w.except.split(",").map((s) => s.trim()).filter(Boolean);
  }
  return { when };
}

// --- Location ---

export type WhereForm = { lat: string; lng: string; label: string };

export const emptyWhere: WhereForm = { lat: "", lng: "", label: "" };

export function whereFromPin(p: Location | null): WhereForm {
  return p ? { lat: String(p.lat), lng: String(p.lng), label: p.label } : emptyWhere;
}

// savePin reconciles the post's location with the form: add, replace, or remove.
// Returns the location the post has afterwards, so a later save starts from the truth.
export async function savePin(recordId: string, where: WhereForm, existing: Location | null): Promise<Location | null> {
  const lat = parseFloat(where.lat);
  const lng = parseFloat(where.lng);
  const has = where.lat.trim() !== "" && where.lng.trim() !== "";
  if (has && (isNaN(lat) || isNaN(lng))) throw new Error("Latitude and longitude must be numbers.");
  const unchanged =
    existing && has && existing.lat === lat && existing.lng === lng && (existing.label === where.label || where.label === "");
  if (unchanged) return existing;
  if (existing && (!has || existing.lat !== lat || existing.lng !== lng || existing.label !== where.label)) {
    await api.removeLocation(existing.id);
  }
  if (has) {
    const r = await api.addLocation(recordId, lat, lng, where.label.trim());
    return r.location;
  }
  return null;
}
