import { useEffect, useRef, useState } from "preact/hooks";
import { Button, Chip, Input, Select, Textarea, cls } from "./ui";
import { KINDS, fieldLabel, isImageUrl, validateFieldName, type Kind } from "../record/fields";

// One editable row per field in the record panel. Each widget edits only the
// value it owns: `onChange(undefined)` drops the key, anything else replaces it.
// Whatever a widget can't represent falls to the JSON widget, which commits only
// when its text parses, so nothing a record carries is ever lost by opening it.

type RowProps = {
  fieldKey: string;
  kind: Kind;
  value: unknown;
  // From friendo.toml: a declared field stays on every record (no ×), may offer
  // a menu of choices, be required, and carry a hint.
  declared?: boolean;
  choices?: string[];
  required?: boolean;
  hint?: string;
  pendingFile?: File | null;
  onChange: (value: unknown) => void;
  onPickFile?: (file: File | null) => void;
  onJsonError: (message: string) => void;
  onRemove: () => void;
};

export function FieldRow(props: RowProps) {
  const { fieldKey, kind, declared, required, hint, onRemove } = props;
  const kindLabel = KINDS.find((k) => k.value === kind)?.label || kind;
  return (
    <div data-field={fieldKey} class="border-b border-ink py-3 last:border-b-0">
      <div class="flex items-baseline gap-2">
        <label for={`field-${fieldKey}`} class="text-sm font-bold">
          {fieldLabel(fieldKey)}
        </label>
        <code class="text-xs text-dim">{fieldKey}</code>
        <span class="text-xs text-dim">
          · {kindLabel}
          {required && " · required"}
        </span>
        {!declared && (
          <button
            type="button"
            aria-label={`Remove field ${fieldKey}`}
            title="Remove this field from the record"
            onClick={onRemove}
            class="ml-auto px-1 text-sm text-dim hover:text-crimson"
          >
            ×
          </button>
        )}
      </div>
      <Widget {...props} />
      {hint && <p class="mt-1 text-xs text-dim">{hint}</p>}
    </div>
  );
}

function Widget(p: RowProps) {
  const id = `field-${p.fieldKey}`;
  switch (p.kind) {
    case "checkbox":
      return (
        <label class="mt-1 flex items-center gap-2 text-sm">
          <input
            id={id}
            type="checkbox"
            name={p.fieldKey}
            checked={!!p.value}
            onChange={(e) => p.onChange((e.target as HTMLInputElement).checked)}
          />
          {p.value ? "Yes" : "No"}
        </label>
      );
    case "number":
      return (
        <Input
          id={id}
          type="number"
          step="any"
          name={p.fieldKey}
          value={p.value === null || p.value === undefined ? "" : String(p.value)}
          onInput={(e) => {
            const v = (e.target as HTMLInputElement).value;
            p.onChange(v === "" ? undefined : Number(v));
          }}
        />
      );
    case "longtext":
      return (
        <Textarea
          id={id}
          name={p.fieldKey}
          class="min-h-24"
          value={p.value === null || p.value === undefined ? "" : String(p.value)}
          onInput={(e) => p.onChange((e.target as HTMLTextAreaElement).value)}
        />
      );
    case "tags":
      return <TagsWidget id={id} name={p.fieldKey} value={p.value} onChange={p.onChange} />;
    case "image":
      return (
        <ImageWidget
          id={id}
          name={p.fieldKey}
          value={p.value}
          pendingFile={p.pendingFile || null}
          onChange={p.onChange}
          onPickFile={p.onPickFile || (() => {})}
        />
      );
    case "json":
      return <JsonWidget id={id} name={p.fieldKey} value={p.value} onChange={p.onChange} onError={p.onJsonError} />;
    default:
      if (p.choices && p.choices.length) {
        const current = p.value === null || p.value === undefined ? "" : String(p.value);
        // A value outside the menu (an old one, or from a file) stays selectable so
        // opening the record doesn't lose it.
        const options = current && !p.choices.includes(current) ? [current, ...p.choices] : p.choices;
        return (
          <Select id={id} name={p.fieldKey} value={current} onChange={(e) => p.onChange((e.target as HTMLSelectElement).value)}>
            <option value="">—</option>
            {options.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
        );
      }
      return (
        <Input
          id={id}
          type="text"
          name={p.fieldKey}
          value={p.value === null || p.value === undefined ? "" : String(p.value)}
          onInput={(e) => p.onChange((e.target as HTMLInputElement).value)}
        />
      );
  }
}

// Chips plus a box to type the next one; Enter or a comma adds, Backspace on an
// empty box takes the last one back.
function TagsWidget({ id, name, value, onChange }: { id: string; name: string; value: unknown; onChange: (v: unknown) => void }) {
  const tags = Array.isArray(value) ? value.map(String) : [];
  const [draft, setDraft] = useState("");

  function commit() {
    const t = draft.trim().replace(/,+$/, "").trim();
    if (t && !tags.includes(t)) onChange([...tags, t]);
    setDraft("");
  }

  return (
    <div class="mt-1 border border-ink bg-white px-2 py-1" data-tags={name}>
      <div class="flex flex-wrap items-center gap-1">
        {tags.map((t) => (
          <Chip key={t} class="bg-tint">
            {t}
            <button
              type="button"
              aria-label={`Remove ${t}`}
              onClick={() => onChange(tags.filter((x) => x !== t))}
              class="ml-1 text-dim hover:text-crimson"
            >
              ×
            </button>
          </Chip>
        ))}
        <input
          id={id}
          type="text"
          name={name}
          value={draft}
          placeholder={tags.length ? "" : "Type a tag and press Enter"}
          onInput={(e) => setDraft((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              commit();
            } else if (e.key === "Backspace" && draft === "" && tags.length) {
              onChange(tags.slice(0, -1));
            }
          }}
          onBlur={commit}
          class="min-w-32 flex-1 py-0.5 text-sm focus:outline-none"
        />
      </div>
    </div>
  );
}

// An image field holds a URL. Picking a file shows it right away but uploads it
// when the record saves (the upload needs the record's id).
function ImageWidget({
  id,
  name,
  value,
  pendingFile,
  onChange,
  onPickFile,
}: {
  id: string;
  name: string;
  value: unknown;
  pendingFile: File | null;
  onChange: (v: unknown) => void;
  onPickFile: (f: File | null) => void;
}) {
  const url = typeof value === "string" ? value : "";
  const fileInput = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState("");
  useEffect(() => {
    if (!pendingFile) {
      setPreview("");
      return;
    }
    const u = URL.createObjectURL(pendingFile);
    setPreview(u);
    return () => URL.revokeObjectURL(u);
  }, [pendingFile]);

  const shown = preview || (url && isImageUrl(url) ? url : "");
  return (
    <div class="mt-1 flex flex-wrap items-start gap-3" data-image={name}>
      {shown ? (
        <img src={shown} alt="" class="h-24 max-w-48 border border-ink object-cover" />
      ) : (
        <div class="flex h-24 w-32 items-center justify-center border border-ink bg-tint text-xs text-dim">No image</div>
      )}
      <div class="flex min-w-0 flex-1 flex-col gap-2">
        {pendingFile ? (
          <span class="text-xs text-dim">
            {pendingFile.name} · uploads when you save
          </span>
        ) : (
          <Input
            id={id}
            type="text"
            name={name}
            mono
            placeholder="/assets/uploads/… or https://…"
            value={url}
            onInput={(e) => onChange((e.target as HTMLInputElement).value)}
            class="mt-0 text-xs"
          />
        )}
        <div class="flex gap-2">
          <Button size="sm" onClick={() => fileInput.current?.click()}>
            {url || pendingFile ? "Replace" : "Choose image"}
          </Button>
          {(url || pendingFile) && (
            <Button
              size="sm"
              variant="quiet"
              onClick={() => {
                onPickFile(null);
                onChange("");
              }}
            >
              Remove
            </Button>
          )}
        </div>
        <input
          ref={fileInput}
          type="file"
          accept="image/*"
          class="hidden"
          onChange={(e) => {
            const f = (e.target as HTMLInputElement).files?.[0] || null;
            onPickFile(f);
            (e.target as HTMLInputElement).value = "";
          }}
        />
      </div>
    </div>
  );
}

// Anything structured, edited as JSON text. The text is the widget's own until
// it parses; only then does the value change, so a half-typed edit can't wipe it.
function JsonWidget({
  id,
  name,
  value,
  onChange,
  onError,
}: {
  id: string;
  name: string;
  value: unknown;
  onChange: (v: unknown) => void;
  onError: (message: string) => void;
}) {
  const [text, setText] = useState(() => JSON.stringify(value === undefined ? null : value, null, 2));
  const [problem, setProblem] = useState("");
  return (
    <div class="mt-1">
      <Textarea
        id={id}
        name={name}
        mono
        class="min-h-24 text-xs"
        value={text}
        aria-invalid={problem ? "true" : undefined}
        onInput={(e) => {
          const t = (e.target as HTMLTextAreaElement).value;
          setText(t);
          try {
            const parsed = JSON.parse(t);
            setProblem("");
            onError("");
            onChange(parsed);
          } catch (err) {
            const msg = "Not valid JSON: " + (err instanceof Error ? err.message : String(err));
            setProblem(msg);
            onError(msg);
          }
        }}
      />
      {problem && <p class="mt-1 text-xs text-crimson">{problem}</p>}
    </div>
  );
}

// The strip at the bottom of the Fields section that adds a field to this record.
export function AddField({ existing, onAdd }: { existing: string[]; onAdd: (key: string, kind: Kind) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Kind>("text");
  const [problem, setProblem] = useState("");

  if (!open) {
    return (
      <Button size="sm" onClick={() => setOpen(true)} data-add-field>
        Add a field
      </Button>
    );
  }

  function add() {
    const msg = validateFieldName(name, existing);
    if (msg) {
      setProblem(msg);
      return;
    }
    onAdd(name.trim(), kind);
    setName("");
    setKind("text");
    setProblem("");
    setOpen(false);
  }

  return (
    <div class="border border-ink bg-tint p-3" data-add-field-form>
      <div class="grid gap-3 sm:grid-cols-[1fr_auto_auto_auto] sm:items-end">
        <label class={cls.label}>
          Field name
          <Input
            name="new-field-name"
            placeholder="e.g. subtitle"
            value={name}
            autofocus
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
        </label>
        <label class={cls.label}>
          Kind
          <Select name="new-field-kind" value={kind} onChange={(e) => setKind((e.target as HTMLSelectElement).value as Kind)}>
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </Select>
        </label>
        <Button variant="primary" onClick={add}>
          Add
        </Button>
        <Button
          onClick={() => {
            setOpen(false);
            setProblem("");
          }}
        >
          Cancel
        </Button>
      </div>
      {problem && <p class="mt-2 text-xs text-crimson">{problem}</p>}
    </div>
  );
}
