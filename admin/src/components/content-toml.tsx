import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api";
import { Button, ErrorBox } from "./ui";
import { useToast } from "./toast";

// The [content] block that matches the site as it is: every collection, and the
// fields their records carry, ready to paste into friendo.toml so the folder
// catches up with what was started ad hoc here. The runtime never edits the file
// itself; `friendo pull` writes this same block into a local copy.
export function ContentToml() {
  const toast = useToast();
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const area = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    api
      .contentToml()
      .then((r) => setText(r.toml))
      .catch((e) => setError(e instanceof Error ? e.message : "Couldn't build the block."));
  }, []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      toast("Copied");
    } catch {
      area.current?.select();
      toast("Select the text and copy it");
    }
  }

  return (
    <div id="content">
      <p class="mb-3 text-xs text-dim">
        Collections can be started here, from a form, or from a <code>content/</code> folder, so the site can know
        about more than your <code>friendo.toml</code> says. This is the <code>[content]</code> block that matches the
        site right now: every collection, and the fields their records carry. Paste it over the{" "}
        <code>[content]</code> section of your file (<code>friendo pull</code> does this for you), then edit the kinds
        or add <code>choices</code>, <code>required</code> or a <code>hint</code> as you like.
      </p>
      {error && <ErrorBox class="mb-3">{error}</ErrorBox>}
      <textarea
        ref={area}
        readOnly
        name="content-toml"
        value={text || (error ? "" : "Loading…")}
        class="block min-h-64 w-full border border-ink bg-white px-3 py-2 font-mono text-xs focus:outline-none"
        onFocus={(e) => (e.target as HTMLTextAreaElement).select()}
      />
      <div class="mt-3">
        <Button variant="primary" size="sm" disabled={!text} onClick={copy}>
          Copy
        </Button>
      </div>
    </div>
  );
}
