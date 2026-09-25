import { useEffect, useMemo, useState } from "preact/hooks";
import { api, can, type Location, type Record as Rec } from "../api";
import { useAuth } from "../auth";
import { SidePanel } from "../components/side-panel";
import { Button, Confirm, ErrorBox, Field, Input, LinkButton, Notice, Segmented, Select, StatusChip } from "../components/ui";
import { BodyEditor } from "../components/body-editor";
import { AddField, FieldRow } from "../components/field-widgets";
import { emptyValue, fieldLabel, isEmptyValue, kindOf, slugify, widgetKind, type Data, type FieldDef, type Kind } from "../record/fields";
import {
  emptyWhen,
  emptyWhere,
  savePin,
  stripWhenKeys,
  whenFromRecord,
  whenToData,
  whereFromPin,
  type WhenForm,
  type WhereForm,
} from "../record/when-where";

// The record editor, as a panel over the collection's list. Handles both a new
// record (`id === "new"`) and an existing one. Besides title, slug and body it
// shows every field the record carries in `data` (and the ones its collection's
// other records have), then the When and Where sections any post may use.

const stamp = (s?: string) => (s ? s.replace("T", " ").replace("Z", "").slice(0, 16) : "");

// A value shaped {lat, lng} in data: the server pins it on create by itself.
function hasLatLng(data: Data): boolean {
  return Object.values(data).some(
    (v) => v && typeof v === "object" && !Array.isArray(v) && typeof (v as Data).lat === "number" && typeof (v as Data).lng === "number"
  );
}

// A record that came from a content/ file has an id derived from its path; such
// a record is the file's, and the next import writes the file over it again.
async function isFileManaged(collection: string, slug: string, id: string): Promise<boolean> {
  try {
    if (!window.crypto?.subtle) return false;
    const bytes = new TextEncoder().encode(`${collection}\n${slug}`);
    const digest = await window.crypto.subtle.digest("SHA-256", bytes);
    const hex = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
    return hex.slice(0, 24) === id;
  } catch {
    return false;
  }
}

export function RecordPanel({
  collection,
  id,
  initial,
  fields,
  onSaved,
  onDeleted,
  onClose,
}: {
  collection: string;
  id: string; // "new" or a record id
  initial: Rec | null;
  fields: FieldDef[];
  onSaved: (record: Rec, created: boolean) => void;
  onDeleted: (id: string) => void;
  onClose: () => void;
}) {
  const { user, features } = useAuth();
  const canPublish = can(user.role, "content.publish");
  const editing = id !== "new";

  const [loaded, setLoaded] = useState<Rec | null>(editing ? initial : null);
  const [loading, setLoading] = useState(editing && !initial);
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [body, setBody] = useState("");
  const [status, setStatus] = useState("draft");
  const [data, setData] = useState<Data>({});
  const [order, setOrder] = useState<string[]>([]);
  const [kinds, setKinds] = useState<{ [k: string]: Kind }>({});
  const [jsonErrors, setJsonErrors] = useState<{ [k: string]: string }>({});
  const [pendingImages, setPendingImages] = useState<{ [k: string]: File }>({});
  const [when, setWhen] = useState<WhenForm>(emptyWhen);
  const [where, setWhere] = useState<WhereForm>(emptyWhere);
  const [pin, setPin] = useState<Location | null>(null);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirming, setConfirming] = useState<"close" | "delete" | null>(null);
  const [fileManaged, setFileManaged] = useState(false);

  const byKey = useMemo(() => Object.fromEntries(fields.map((f) => [f.key, f])), [fields]);

  // Fill the form from a record (or start empty), with the collection's fields
  // first and anything else the record carries after them.
  function seed(r: Rec | null) {
    const d = stripWhenKeys((r?.data as Data) || {});
    const keys = [...fields.map((f) => f.key)];
    for (const k of Object.keys(d).sort()) if (!keys.includes(k)) keys.push(k);
    const ks: { [k: string]: Kind } = {};
    for (const k of keys) {
      const f = byKey[k];
      // A declared kind wins, unless the value is structured and the kind isn't:
      // then only the JSON widget can show it without loss.
      ks[k] = f?.declared ? (kindOf(d[k]) === "json" && f.kind !== "json" ? "json" : f.kind) : widgetKind(d[k], f?.kind || "text");
    }
    setTitle(r?.title || "");
    setSlug(r?.slug || "");
    setSlugTouched(!!r);
    setBody(r?.body || "");
    setStatus(r?.status || "draft");
    setData(d);
    setOrder(keys);
    setKinds(ks);
    setWhen(whenFromRecord(r?.when));
  }

  useEffect(() => {
    if (!editing) {
      seed(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        let r = initial;
        if (!r) r = (await api.record(id)).record;
        if (cancelled) return;
        setLoaded(r);
        seed(r);
        if (features.locations) try {
          const pins = await api.locations(id);
          if (!cancelled && pins.locations.length > 0) {
            setPin(pins.locations[0]);
            setWhere(whereFromPin(pins.locations[0]));
          }
        } catch {
          /* no pin, or not allowed to read one */
        }
        isFileManaged(collection, r.slug, r.id).then((v) => !cancelled && setFileManaged(v));
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load record.");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // The collection's fields can arrive after the panel opened (a deep link to
  // /new loads both at once): add any the rows don't have yet, declared first.
  useEffect(() => {
    if (loading) return;
    const declared = fields.filter((f) => f.declared).map((f) => f.key);
    const all = fields.map((f) => f.key);
    setOrder((o) => {
      const missing = all.filter((k) => !o.includes(k));
      if (missing.length === 0) return o;
      return [...declared, ...o.filter((k) => !declared.includes(k)), ...missing.filter((k) => !declared.includes(k))];
    });
    setKinds((k) => {
      const n = { ...k };
      for (const f of fields) if (!(f.key in n)) n[f.key] = f.kind;
      return n;
    });
  }, [fields, loading]);

  // Leaving the page with edits pending asks first, like closing the panel does.
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  function touch() {
    setDirty(true);
  }

  function setFieldValue(key: string, value: unknown) {
    setData((d) => {
      const n = { ...d };
      if (value === undefined) delete n[key];
      else n[key] = value;
      return n;
    });
    touch();
  }

  function removeField(key: string) {
    setData((d) => {
      const n = { ...d };
      delete n[key];
      return n;
    });
    setOrder((o) => o.filter((k) => k !== key));
    setJsonErrors((e) => {
      const n = { ...e };
      delete n[key];
      return n;
    });
    setPendingImages((p) => {
      const n = { ...p };
      delete n[key];
      return n;
    });
    touch();
  }

  function addField(key: string, kind: Kind) {
    setOrder((o) => [...o, key]);
    setKinds((k) => ({ ...k, [key]: kind }));
    setData((d) => ({ ...d, [key]: emptyValue(kind) }));
    touch();
  }

  function requestClose() {
    if (busy) return;
    if (dirty) setConfirming("close");
    else onClose();
  }

  async function save(e?: Event) {
    e?.preventDefault();
    if (busy) return;
    const broken = Object.values(jsonErrors).find(Boolean);
    if (broken) {
      setError("A structured field isn't valid JSON yet. " + broken);
      return;
    }
    if (!title.trim()) {
      setError("Give the record a title.");
      return;
    }
    for (const f of fields) {
      if (!f.declared) continue;
      const v = data[f.key];
      if (f.required && isEmptyValue(v)) {
        setError(`${fieldLabel(f.key)} is required.`);
        return;
      }
      if (f.choices && !isEmptyValue(v) && typeof v === "string" && !f.choices.includes(v)) {
        setError(`${fieldLabel(f.key)} must be one of: ${f.choices.join(", ")}.`);
        return;
      }
    }
    setBusy(true);
    setError("");
    try {
      const clean: Data = {};
      for (const k of Object.keys(data)) if (data[k] !== undefined) clean[k] = data[k];
      const finalSlug = slug.trim() || slugify(title);
      // `status` always goes along: the server reads a missing one as draft.
      const base = { title: title.trim(), slug: finalSlug, body, status };
      let record: Rec;
      if (editing) {
        record = (await api.updateRecord(id, { ...base, data: { ...clean, ...whenToData(when) } })).record;
      } else {
        record = (await api.createRecord(collection, { ...base, data: { ...clean, ...whenToData(when) } })).record;
      }
      // The pin. A new record whose data already holds a {lat,lng} was pinned by
      // the server on create, so don't add the Where pin on top of it.
      if (editing || !hasLatLng(clean)) {
        setPin(await savePin(record.id, where, pin));
      }
      // Images upload once the record exists, then their URLs are written back.
      // This second save carries no When keys, so the event stays as it is.
      const uploads = Object.entries(pendingImages);
      if (uploads.length > 0) {
        const withUrls = { ...clean };
        for (const [key, file] of uploads) {
          const up = await api.uploadFile(record.id, key, file);
          withUrls[key] = up.file.url;
        }
        record = (await api.updateRecord(record.id, { ...base, data: withUrls })).record;
        setPendingImages({});
      }
      setDirty(false);
      onSaved({ ...record, collection: record.collection || collection }, !editing);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed.");
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await api.deleteRecord(id);
      setDirty(false);
      onDeleted(id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed.");
      setBusy(false);
      setConfirming(null);
    }
  }

  const isEvent = when.starts !== "";
  const setW = <K extends keyof WhenForm>(key: K, value: WhenForm[K]) => {
    setWhen((w) => ({ ...w, [key]: value }));
    touch();
  };
  const setWh = <K extends keyof WhereForm>(key: K, value: WhereForm[K]) => {
    setWhere((w) => ({ ...w, [key]: value }));
    touch();
  };

  const heading = editing ? loaded?.title || "Edit record" : "New record";

  return (
    <SidePanel onRequestClose={requestClose} label={heading}>
      <form
        class="flex h-full flex-col"
        onSubmit={save}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
            e.preventDefault();
            save();
          }
        }}
      >
        <header class="flex items-center gap-3 border-b border-ink px-4 py-3">
          <span class="min-w-0 truncate text-sm">
            <span class="text-dim">{collection} / </span>
            <span class="font-bold">{heading}</span>
          </span>
          <div class="ml-auto flex shrink-0 gap-2">
            <Button type="submit" variant="primary" size="sm" disabled={busy || loading}>
              {busy ? "Saving…" : "Save"}
            </Button>
            <Button size="sm" disabled={busy} onClick={requestClose}>
              Close
            </Button>
          </div>
        </header>

        {confirming === "close" && (
          <div class="border-b border-ink">
            <Confirm
              message="Discard changes?"
              confirmLabel="Discard"
              cancelLabel="Keep editing"
              danger
              onConfirm={onClose}
              onCancel={() => setConfirming(null)}
            />
          </div>
        )}
        {error && (
          <div class="border-b border-ink">
            <ErrorBox>{error}</ErrorBox>
          </div>
        )}

        <div class="flex-1 overflow-y-auto px-4 py-4">
          {loading ? (
            <p class="text-sm text-dim">Loading…</p>
          ) : (
            <>
              {fileManaged && (
                <Notice class="mb-4 text-xs">
                  This record comes from a file in <code>content/</code>. Changes here are overwritten the next time
                  content/ is imported (for example on <code>friendo serve</code>); edit the file to keep them.
                </Notice>
              )}

              <Field label="Title" class="mb-4">
                <Input
                  type="text"
                  name="title"
                  required
                  value={title}
                  autofocus={!editing}
                  onInput={(e) => {
                    const t = (e.target as HTMLInputElement).value;
                    setTitle(t);
                    if (!slugTouched) setSlug(slugify(t));
                    touch();
                  }}
                />
              </Field>
              <Field
                label="Slug"
                hint={slugTouched ? "The record's address on the site." : "Follows the title until you change it."}
                class="mb-4"
              >
                <Input
                  type="text"
                  name="slug"
                  mono
                  value={slug}
                  class={slugTouched ? "" : "text-dim"}
                  onInput={(e) => {
                    setSlug((e.target as HTMLInputElement).value);
                    setSlugTouched(true);
                    touch();
                  }}
                />
              </Field>

              <div class="mb-5">
                <BodyEditor
                  value={body}
                  onChange={(md) => {
                    setBody(md);
                    touch();
                  }}
                />
              </div>

              <section data-section="fields" class="mb-5 border border-ink p-4">
                <h2 class="text-sm font-bold">Fields</h2>
                <p class="mb-2 text-xs text-dim">
                  The record's own fields, readable in templates as <code>record.data.&lt;name&gt;</code>. Any record can
                  have any fields.
                </p>
                {order.length === 0 && <p class="py-2 text-xs text-dim">No fields yet.</p>}
                {order.map((key) => (
                  <FieldRow
                    key={key}
                    fieldKey={key}
                    kind={kinds[key] || "text"}
                    declared={byKey[key]?.declared}
                    choices={byKey[key]?.choices}
                    required={byKey[key]?.required}
                    hint={byKey[key]?.hint}
                    value={data[key]}
                    pendingFile={pendingImages[key] || null}
                    onChange={(v) => setFieldValue(key, v)}
                    onPickFile={(f) => {
                      setPendingImages((p) => {
                        const n = { ...p };
                        if (f) n[key] = f;
                        else delete n[key];
                        return n;
                      });
                      touch();
                    }}
                    onJsonError={(msg) => setJsonErrors((e) => ({ ...e, [key]: msg }))}
                    onRemove={() => removeField(key)}
                  />
                ))}
                <div class="mt-3">
                  <AddField existing={order} onAdd={addField} />
                </div>
              </section>

              {/* --- When: makes the post an event --- */}
              <fieldset class="mb-5 border border-ink p-4" data-section="when">
                <legend class="px-1 text-sm font-bold">When</legend>
                <p class="mb-3 text-xs text-dim">
                  Give the post a time and it becomes an event: listed by date and in the site's calendar feed. Leave it
                  empty for an ordinary post.
                </p>
                <div class="grid gap-3 sm:grid-cols-2">
                  <Field label="Starts">
                    <Input type={when.allDay ? "date" : "datetime-local"} name="when" value={when.starts} onInput={(e) => setW("starts", (e.target as HTMLInputElement).value)} />
                  </Field>
                  <Field label="Ends">
                    <Input type={when.allDay ? "date" : "datetime-local"} name="ends" value={when.ends} onInput={(e) => setW("ends", (e.target as HTMLInputElement).value)} />
                  </Field>
                </div>
                <label class="mt-3 flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    name="all_day"
                    checked={when.allDay}
                    onChange={(e) => {
                      const allDay = (e.target as HTMLInputElement).checked;
                      setWhen((w) => ({
                        ...w,
                        allDay,
                        starts: allDay ? w.starts.slice(0, 10) : w.starts.length === 10 ? w.starts + "T09:00" : w.starts,
                        ends: allDay ? w.ends.slice(0, 10) : w.ends.length === 10 ? w.ends + "T10:00" : w.ends,
                      }));
                      touch();
                    }}
                  />
                  All day
                </label>
                <div class="mt-3 grid gap-3 sm:grid-cols-2">
                  <Field label="Repeats">
                    <Select name="repeats" value={when.repeats} disabled={!isEvent} onChange={(e) => setW("repeats", (e.target as HTMLSelectElement).value as WhenForm["repeats"])}>
                      <option value="">Never</option>
                      <option value="daily">Daily</option>
                      <option value="weekly">Weekly</option>
                      <option value="every 2 weeks">Every 2 weeks</option>
                      <option value="monthly">Monthly</option>
                      <option value="yearly">Yearly</option>
                      <option value="custom">Custom rule…</option>
                    </Select>
                  </Field>
                  {when.repeats && when.repeats !== "custom" && (
                    <Field label="Until">
                      <Input type="date" name="until" value={when.until} onInput={(e) => setW("until", (e.target as HTMLInputElement).value)} />
                    </Field>
                  )}
                  {when.repeats === "custom" && (
                    <Field label="Rule (iCalendar RRULE)">
                      <Input type="text" name="rrule" mono value={when.rrule} placeholder="FREQ=MONTHLY;BYDAY=1TU" onInput={(e) => setW("rrule", (e.target as HTMLInputElement).value)} />
                    </Field>
                  )}
                </div>
                {when.repeats && (
                  <Field label="Skip these dates" class="mt-3">
                    <Input type="text" name="except" value={when.except} placeholder="2026-11-25, 2026-12-23" onInput={(e) => setW("except", (e.target as HTMLInputElement).value)} />
                  </Field>
                )}
                <Field label="Timezone" class="mt-3">
                  <Input type="text" name="timezone" value={when.timezone} placeholder="the site's (set in friendo.toml)" onInput={(e) => setW("timezone", (e.target as HTMLInputElement).value)} />
                </Field>
              </fieldset>

              {/* --- Where: a map pin (unless pins are switched off) --- */}
              {features.locations && (
              <fieldset class="mb-5 border border-ink p-4" data-section="where">
                <legend class="px-1 text-sm font-bold">Where</legend>
                <p class="mb-3 text-xs text-dim">
                  A latitude and longitude pin the post on the map (and put the place in the calendar feed). Clear both
                  to remove the pin.
                </p>
                <div class="grid gap-3 sm:grid-cols-3">
                  <Field label="Latitude">
                    <Input type="text" inputMode="decimal" name="lat" value={where.lat} onInput={(e) => setWh("lat", (e.target as HTMLInputElement).value)} />
                  </Field>
                  <Field label="Longitude">
                    <Input type="text" inputMode="decimal" name="lng" value={where.lng} onInput={(e) => setWh("lng", (e.target as HTMLInputElement).value)} />
                  </Field>
                  <Field label="Place">
                    <Input type="text" name="place" value={where.label} placeholder="defaults to the title" onInput={(e) => setWh("label", (e.target as HTMLInputElement).value)} />
                  </Field>
                </div>
              </fieldset>
              )}
            </>
          )}
        </div>

        <footer class="border-t border-ink bg-white px-4 py-3">
          {confirming === "delete" ? (
            <Confirm
              message="Delete this record? This can't be undone."
              confirmLabel="Delete"
              danger
              busy={busy}
              onConfirm={remove}
              onCancel={() => setConfirming(null)}
            />
          ) : (
            <div class="flex flex-wrap items-center gap-3">
              <div data-section="status" class="flex items-center gap-2">
                {canPublish ? (
                  <Segmented
                    size="sm"
                    value={status}
                    options={[
                      { value: "draft", label: "Draft" },
                      { value: "pending", label: "Pending" },
                      { value: "published", label: "Published" },
                    ]}
                    onChange={(s) => {
                      setStatus(s);
                      touch();
                    }}
                  />
                ) : (
                  <>
                    <StatusChip status={status} />
                    <span class="text-xs text-dim">An editor publishes it.</span>
                  </>
                )}
              </div>
              {loaded && (
                <span class="text-xs text-dim">
                  {loaded.created && <>created {stamp(loaded.created)}</>}
                  {loaded.updated && <> · updated {stamp(loaded.updated)}</>}
                  {loaded.published_at && <> · published {stamp(loaded.published_at)}</>}
                  <> · </>
                  <code class="text-xs">{loaded.id}</code>
                </span>
              )}
              <div class="ml-auto flex gap-2">
                {loaded?.when && features.rsvp && (
                  <LinkButton size="sm" href={`/_/records/${encodeURIComponent(loaded.id)}/attendees`}>
                    Attendees
                  </LinkButton>
                )}
                {editing && (
                  <Button size="sm" variant="danger" disabled={busy} onClick={() => setConfirming("delete")}>
                    Delete
                  </Button>
                )}
              </div>
            </div>
          )}
        </footer>
      </form>
    </SidePanel>
  );
}
