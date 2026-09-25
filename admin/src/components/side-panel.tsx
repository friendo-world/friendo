import type { ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";

// A panel that slides over the right side of the page, leaving the list showing
// behind a translucent wash. Escape or a click on the wash asks to close; the
// owner decides (it may want to ask about unsaved changes first). Focus moves
// into the panel when it opens and back to where it was when it closes.
export function SidePanel({
  onRequestClose,
  label,
  children,
}: {
  onRequestClose: () => void;
  label: string;
  children: ComponentChildren;
}) {
  const panel = useRef<HTMLElement>(null);
  const closeRef = useRef(onRequestClose);
  closeRef.current = onRequestClose;

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>("input, textarea, select, button");
    first?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeRef.current();
      }
    };
    document.addEventListener("keydown", onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
      previous?.focus?.();
    };
  }, []);

  return (
    <div class="fixed inset-0 z-40">
      <div class="absolute inset-0 bg-paper/60" onClick={onRequestClose} />
      <section
        ref={panel}
        data-panel
        role="dialog"
        aria-modal="true"
        aria-label={label}
        class="absolute inset-y-0 right-0 flex w-full max-w-[720px] flex-col border-l border-ink bg-paper"
      >
        {children}
      </section>
    </div>
  );
}
