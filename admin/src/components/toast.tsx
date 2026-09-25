import { createContext, type ComponentChildren } from "preact";
import { useCallback, useContext, useRef, useState } from "preact/hooks";

// One short line of feedback at the bottom of the screen ("Saved", "3 records
// published") that fades after a few seconds. The live region is always in the
// DOM so screen readers announce it; it's only visible while it has something to say.
type Show = (message: string) => void;

const ToastContext = createContext<Show>(() => {});

export function ToastProvider({ children }: { children: ComponentChildren }) {
  const [message, setMessage] = useState("");
  const timer = useRef<number | undefined>(undefined);
  const show = useCallback<Show>((m) => {
    setMessage(m);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setMessage(""), 3000);
  }, []);
  return (
    <ToastContext.Provider value={show}>
      {children}
      <div
        role="status"
        aria-live="polite"
        class={
          message
            ? "fixed bottom-4 left-1/2 z-50 -translate-x-1/2 border border-ink bg-manila px-3 py-2 text-sm text-ink"
            : "sr-only"
        }
      >
        {message}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): Show {
  return useContext(ToastContext);
}
