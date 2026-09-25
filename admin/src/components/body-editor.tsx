import { useMemo, useState } from "preact/hooks";
import { marked } from "marked";
import { Segmented, Textarea, Toggle } from "./ui";
import { RichEditor } from "./rich-editor";
import { richTextOn, setRichTextOn } from "../record/prefs";

// The body is markdown. By default it's written as such, in Courier, with a
// preview beside the switch; a per-browser "Rich text" toggle swaps in the
// formatting-button editor for people who'd rather not see the asterisks.

// The site renders bodies with goldmark, which drops raw HTML; escaping it here
// keeps the preview honest about what a page will show.
const escapeHtml = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
marked.use({
  gfm: true,
  renderer: {
    html({ text }) {
      return escapeHtml(text);
    },
  },
});

export function BodyEditor({ value, onChange }: { value: string; onChange: (md: string) => void }) {
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [rich, setRich] = useState(richTextOn);
  const [notice, setNotice] = useState("");

  const html = useMemo(() => (mode === "preview" ? String(marked.parse(value || "", { async: false })) : ""), [mode, value]);

  function toggleRich() {
    const next = !rich;
    setRich(next);
    setRichTextOn(next);
    setNotice("");
    if (next) setMode("write");
  }

  return (
    <div data-section="body">
      <div class="mb-2 flex flex-wrap items-center gap-3">
        <span class="text-sm font-bold">Body</span>
        {!rich && (
          <Segmented
            size="sm"
            value={mode}
            options={[
              { value: "write", label: "Write" },
              { value: "preview", label: "Preview" },
            ]}
            onChange={setMode}
          />
        )}
        <div class="ml-auto">
          <Toggle label="Rich text" on={rich} onToggle={toggleRich} />
        </div>
      </div>
      {notice && <p class="mb-2 bg-manila px-3 py-2 text-xs">{notice}</p>}
      {rich ? (
        <RichEditor
          value={value}
          onChange={onChange}
          onError={() => {
            setRich(false);
            setRichTextOn(false);
            setNotice("The rich editor couldn't load (it needs the internet the first time). Write Markdown here instead.");
          }}
        />
      ) : mode === "preview" ? (
        <div class="preview min-h-64 border border-ink bg-white px-4 py-3 text-sm" dangerouslySetInnerHTML={{ __html: html }} />
      ) : (
        <Textarea
          name="body"
          mono
          class="mt-0 min-h-64"
          value={value}
          placeholder="Write in Markdown…"
          onInput={(e) => onChange((e.target as HTMLTextAreaElement).value)}
        />
      )}
      <p class="mt-1 text-xs text-dim">
        {rich
          ? "Formatting buttons edit the same Markdown underneath; switching back may tidy it a little."
          : "Markdown: **bold**, *italic*, # headings, - lists, [links](https://…)."}
      </p>
    </div>
  );
}
