import { useEffect, useRef, useState } from "preact/hooks";
import { Button } from "./ui";

// A rich-text editor for the body, built on TipTap the same way the SDK's
// <friendo-input type="richtext"> is: loaded from a CDN when first needed, and
// serialised back to markdown on every change so the body column stays markdown.
// If the library can't load (offline, blocked), `onError` fires and the caller
// falls back to the plain textarea with the value untouched.

const TIPTAP_VERSION = "2.27.2";
const TIPTAP_MARKDOWN_VERSION = "0.8.10";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Tiptap = { Editor: any; StarterKit: any; Link: any; Placeholder: any; Markdown: any };

let tiptapPromise: Promise<Tiptap> | null = null;

function loadTiptap(): Promise<Tiptap> {
  if (tiptapPromise) return tiptapPromise;
  const core = "https://esm.sh/@tiptap/core@" + TIPTAP_VERSION;
  const deps = "?deps=@tiptap/core@" + TIPTAP_VERSION;
  const load = (url: string) => import(/* @vite-ignore */ url);
  tiptapPromise = Promise.all([
    load(core),
    load("https://esm.sh/@tiptap/starter-kit@" + TIPTAP_VERSION + deps),
    load("https://esm.sh/@tiptap/extension-link@" + TIPTAP_VERSION + deps),
    load("https://esm.sh/@tiptap/extension-placeholder@" + TIPTAP_VERSION + deps),
    load("https://esm.sh/tiptap-markdown@" + TIPTAP_MARKDOWN_VERSION + deps),
  ]).then((m) => ({
    Editor: m[0].Editor,
    StarterKit: m[1].default,
    Link: m[2].default,
    Placeholder: m[3].default,
    Markdown: m[4].Markdown,
  }));
  tiptapPromise.catch(() => {
    tiptapPromise = null; // let a later attempt try again
  });
  return tiptapPromise;
}

type Tool = {
  key: string;
  label: string;
  title: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  run: ((chain: any) => any) | null;
  on: [string, object?];
};

const TOOLS: Tool[] = [
  { key: "bold", label: "B", title: "Bold", run: (c) => c.toggleBold(), on: ["bold"] },
  { key: "italic", label: "I", title: "Italic", run: (c) => c.toggleItalic(), on: ["italic"] },
  { key: "h2", label: "H2", title: "Heading", run: (c) => c.toggleHeading({ level: 2 }), on: ["heading", { level: 2 }] },
  { key: "h3", label: "H3", title: "Subheading", run: (c) => c.toggleHeading({ level: 3 }), on: ["heading", { level: 3 }] },
  { key: "bullet", label: "• List", title: "Bullet list", run: (c) => c.toggleBulletList(), on: ["bulletList"] },
  { key: "ordered", label: "1. List", title: "Numbered list", run: (c) => c.toggleOrderedList(), on: ["orderedList"] },
  { key: "quote", label: "❝", title: "Quote", run: (c) => c.toggleBlockquote(), on: ["blockquote"] },
  { key: "code", label: "</>", title: "Inline code", run: (c) => c.toggleCode(), on: ["code"] },
  { key: "link", label: "Link", title: "Link", run: null, on: ["link"] },
];

export function RichEditor({
  value,
  onChange,
  onError,
}: {
  value: string;
  onChange: (markdown: string) => void;
  onError: (message: string) => void;
}) {
  const mount = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const [editor, setEditor] = useState<any>(null);
  const [active, setActive] = useState<{ [key: string]: boolean }>({});
  const [loading, setLoading] = useState(true);
  const changeRef = useRef(onChange);
  changeRef.current = onChange;

  useEffect(() => {
    let cancelled = false;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let ed: any = null;
    loadTiptap()
      .then((T) => {
        if (cancelled || !mount.current) return;
        ed = new T.Editor({
          element: mount.current,
          extensions: [
            T.StarterKit,
            T.Link.configure({ openOnClick: false, autolink: true }),
            T.Placeholder.configure({ placeholder: "Write…" }),
            T.Markdown,
          ],
          content: value,
          editorProps: { attributes: { role: "textbox", "aria-multiline": "true" } },
        });
        ed.on("update", () => changeRef.current(ed.storage.markdown.getMarkdown() || ""));
        const sync = () => {
          const next: { [key: string]: boolean } = {};
          for (const t of TOOLS) next[t.key] = t.on.length > 1 ? ed.isActive(t.on[0], t.on[1]) : ed.isActive(t.on[0]);
          setActive(next);
        };
        ed.on("selectionUpdate", sync);
        ed.on("transaction", sync);
        setEditor(ed);
        setLoading(false);
      })
      .catch((e) => {
        if (!cancelled) onError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
      if (ed) ed.destroy();
    };
    // The editor owns its content after mount; `value` is only the starting text.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  function link(ed: any) {
    if (ed.isActive("link")) {
      ed.chain().focus().unsetLink().run();
      return;
    }
    const url = window.prompt("Link URL", "https://");
    if (!url) return;
    ed.chain().focus().setLink({ href: url }).run();
  }

  return (
    <div data-rich-editor>
      <div class="mb-1 flex flex-wrap gap-1">
        {TOOLS.map((t) => (
          <Button
            key={t.key}
            size="sm"
            title={t.title}
            disabled={!editor}
            aria-pressed={active[t.key] ? "true" : "false"}
            class={active[t.key] ? "bg-tint" : ""}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              if (!editor) return;
              if (t.run) t.run(editor.chain().focus()).run();
              else link(editor);
            }}
          >
            {t.label}
          </Button>
        ))}
      </div>
      <div class="border border-ink bg-white">
        {loading && <div class="px-3 py-2 text-sm text-dim">Loading editor…</div>}
        <div ref={mount} class="rich" />
      </div>
    </div>
  );
}
