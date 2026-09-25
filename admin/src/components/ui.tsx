import type { ComponentChildren, JSX } from "preact";

// The admin's house-style building blocks: paper and ink, hairline borders, no
// radius, no shadows, nothing uppercase. Every component renders a native element
// and passes `name`, `data-*`, `disabled` and the rest straight through, so forms,
// autofill and the browser tests all see ordinary HTML.

export const cls = {
  input:
    "mt-1 block w-full border border-ink bg-white px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none disabled:opacity-50",
  label: "block text-sm font-bold",
  hint: "mt-1 block text-xs font-normal text-dim",
};

type Variant = "primary" | "secondary" | "danger" | "quiet";
type Size = "md" | "sm";

const VARIANT: Record<Variant, string> = {
  primary: "bg-ink text-white hover:bg-link",
  secondary: "border border-ink bg-white text-ink hover:bg-tint",
  danger: "bg-crimson text-white hover:bg-ink",
  quiet: "bg-tint text-dim hover:bg-manila hover:text-ink",
};
const SIZE: Record<Size, string> = { md: "px-4 py-2 text-sm", sm: "px-2 py-1 text-xs" };

function buttonClass(variant: Variant, size: Size, extra?: string) {
  return `font-bold disabled:opacity-50 ${VARIANT[variant]} ${SIZE[size]} ${extra || ""}`;
}

type ButtonProps = Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, "class" | "size"> & {
  variant?: Variant;
  size?: Size;
  class?: string;
};

// Buttons default to type="button": only the one that saves a form should submit it.
export function Button({ variant = "secondary", size = "md", class: extra, type, ...rest }: ButtonProps) {
  return <button type={type || "button"} {...rest} class={buttonClass(variant, size, extra)} />;
}

type LinkButtonProps = Omit<JSX.AnchorHTMLAttributes<HTMLAnchorElement>, "class"> & {
  variant?: Variant;
  size?: Size;
  class?: string;
};

export function LinkButton({ variant = "secondary", size = "md", class: extra, ...rest }: LinkButtonProps) {
  return <a {...rest} class={buttonClass(variant, size, "inline-block no-underline " + (extra || ""))} />;
}

type InputProps = Omit<JSX.InputHTMLAttributes<HTMLInputElement>, "class"> & { class?: string; mono?: boolean };

export function Input({ class: extra, mono, ...rest }: InputProps) {
  return <input {...rest} class={`${cls.input} ${mono ? "font-mono" : ""} ${extra || ""}`} />;
}

type TextareaProps = Omit<JSX.TextareaHTMLAttributes<HTMLTextAreaElement>, "class"> & { class?: string; mono?: boolean };

export function Textarea({ class: extra, mono, ...rest }: TextareaProps) {
  return <textarea {...rest} class={`${cls.input} resize-y ${mono ? "font-mono" : ""} ${extra || ""}`} />;
}

type SelectProps = Omit<JSX.SelectHTMLAttributes<HTMLSelectElement>, "class"> & { class?: string };

export function Select({ class: extra, ...rest }: SelectProps) {
  return <select {...rest} class={`${cls.input} ${extra || ""}`} />;
}

// A labelled control: the label text, the control, then an optional hint below.
export function Field({
  label,
  hint,
  children,
  class: extra,
}: {
  label: ComponentChildren;
  hint?: ComponentChildren;
  children: ComponentChildren;
  class?: string;
}) {
  return (
    <label class={`${cls.label} ${extra || ""}`}>
      {label}
      {children}
      {hint && <span class={cls.hint}>{hint}</span>}
    </label>
  );
}

export function Chip({ children, class: extra }: { children: ComponentChildren; class?: string }) {
  return <span class={`inline-block border border-ink px-1.5 py-0.5 text-xs ${extra || ""}`}>{children}</span>;
}

// A record's status as a plain word: Draft, Pending, Published.
export const STATUS_LABEL: { [status: string]: string } = {
  draft: "Draft",
  pending: "Pending",
  published: "Published",
};
const STATUS_CLASS: { [status: string]: string } = {
  draft: "bg-white text-dim",
  pending: "bg-manila text-ink",
  published: "bg-ink text-white",
};

export function StatusChip({ status }: { status: string }) {
  return <Chip class={STATUS_CLASS[status] || "bg-white text-dim"}>{STATUS_LABEL[status] || status}</Chip>;
}

export function Card({ children, class: extra }: { children: ComponentChildren; class?: string }) {
  return <div class={`border border-ink bg-white p-6 ${extra || ""}`}>{children}</div>;
}

export function ErrorBox({ children, class: extra }: { children: ComponentChildren; class?: string }) {
  return (
    <div role="alert" class={`border border-crimson bg-tint px-3 py-2 text-sm text-crimson ${extra || ""}`}>
      {children}
    </div>
  );
}

export function Notice({ children, class: extra }: { children: ComponentChildren; class?: string }) {
  return <div class={`bg-manila px-3 py-2 text-sm text-ink ${extra || ""}`}>{children}</div>;
}

// A row of joined buttons where exactly one is pressed. Real buttons with a
// `value`, so a test can pick `button[value="published"]`.
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  disabled,
  size = "md",
  class: extra,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  disabled?: boolean;
  size?: Size;
  class?: string;
}) {
  const pad = size === "sm" ? "px-2 py-1 text-xs" : "px-3 py-1 text-sm";
  return (
    <div role="group" class={`inline-flex border border-ink bg-white ${extra || ""}`}>
      {options.map((o, i) => (
        <button
          key={o.value}
          type="button"
          value={o.value}
          disabled={disabled}
          aria-pressed={value === o.value ? "true" : "false"}
          onClick={() => onChange(o.value)}
          class={
            `${pad} font-bold disabled:opacity-50 ` +
            (i > 0 ? "border-l border-ink " : "") +
            (value === o.value ? "bg-ink text-white" : "text-dim hover:bg-tint hover:text-ink")
          }
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// An on/off switch with its label and a hint, as Settings uses.
export function Toggle({
  label,
  hint,
  on,
  disabled,
  onToggle,
}: {
  label: string;
  hint?: string;
  on: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) {
  return (
    <label class="flex items-start justify-between gap-4">
      <span>
        <span class="text-sm font-bold">{label}</span>
        {hint && <span class="mt-1 block text-xs text-dim">{hint}</span>}
      </span>
      <button
        type="button"
        role="switch"
        aria-checked={on ? "true" : "false"}
        disabled={disabled}
        onClick={onToggle}
        class={
          "relative inline-flex h-6 w-11 shrink-0 border border-ink transition-colors " +
          (on ? "bg-link" : "bg-white") +
          (disabled ? " opacity-50" : "")
        }
      >
        <span
          class={
            "inline-block h-4 w-4 translate-y-[3px] transition-transform " +
            (on ? "translate-x-[25px] bg-white" : "translate-x-[3px] bg-ink")
          }
        />
      </button>
    </label>
  );
}

// An inline yes/no strip that stands in for the browser's confirm(): the question,
// the action, and a way out, right where the action was asked for.
export function Confirm({
  message,
  confirmLabel,
  cancelLabel = "Cancel",
  danger,
  busy,
  onConfirm,
  onCancel,
}: {
  message: ComponentChildren;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div role="alertdialog" class="flex flex-wrap items-center gap-3 border border-ink bg-manila px-3 py-2 text-sm">
      <span class="flex-1">{message}</span>
      <Button variant={danger ? "danger" : "primary"} size="sm" disabled={busy} onClick={onConfirm}>
        {confirmLabel}
      </Button>
      <Button size="sm" disabled={busy} onClick={onCancel}>
        {cancelLabel}
      </Button>
    </div>
  );
}
