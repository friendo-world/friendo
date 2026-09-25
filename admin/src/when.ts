import type { When } from "./api";

// formatWhen prints a post's time for a list: "Sat Oct 4, 10:00 am – 4:00 pm",
// "Sat Oct 4" (all day), with "· weekly" when it repeats. Times are shown as the
// event's own wall clock (the RFC 3339 string carries its offset), not the
// browser's, so an editor in another zone sees what the site says.
export function formatWhen(w: When | null | undefined, opts: { next?: boolean } = {}): string {
  if (!w || !w.starts) return "";
  const src = opts.next && w.next ? w.next : w;
  const s = wall(src.starts);
  const e = src.ends ? wall(src.ends) : null;
  const day = (d: Date) => d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric", timeZone: "UTC" });
  const clock = (d: Date) => d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit", timeZone: "UTC" });
  let out: string;
  if (src.all_day) {
    out = e && day(e) !== day(s) ? `${day(s)} – ${day(e)}` : day(s);
  } else if (!e) {
    out = `${day(s)}, ${clock(s)}`;
  } else if (day(e) === day(s)) {
    out = `${day(s)}, ${clock(s)} – ${clock(e)}`;
  } else {
    out = `${day(s)}, ${clock(s)} – ${day(e)}, ${clock(e)}`;
  }
  if (w.repeats) out += ` · ${w.repeats}`;
  return out;
}

// wall reads an RFC 3339 stamp as a UTC Date holding the *wall-clock* fields, so
// the formatters above (timeZone: "UTC") print the event's local time.
function wall(rfc: string): Date {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(rfc);
  if (!m) return new Date(rfc);
  return new Date(Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]));
}

// toInput turns an RFC 3339 stamp into what a datetime-local / date input takes.
export function toInput(rfc: string, allDay: boolean): string {
  if (!rfc) return "";
  return allDay ? rfc.slice(0, 10) : rfc.slice(0, 16);
}
