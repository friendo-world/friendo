import { useState } from "preact/hooks";
import { api } from "../api";

const field =
  "mt-1 block w-full border border-ink px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none";

// Shown only on the Go runtime when a legacy (Phase 1) admin password exists.
export function Migrate({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.migrate(email, password);
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Migration failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="mx-auto mt-16 max-w-md px-4">
      <div class="bg-white p-6 border border-ink">
        <h1 class="mb-1 text-xl font-bold">Upgrade your admin</h1>
        {done ? (
          <>
            <p class="mb-6 text-sm text-dim">
              Your account has been upgraded. Please log in with your existing password.
            </p>
            <button onClick={onDone}
              class="w-full bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link">
              Continue to log in
            </button>
          </>
        ) : (
          <>
            <p class="mb-6 text-sm text-dim">
              Friendo now uses email-based accounts. Enter your email and your existing admin
              password to upgrade it to a full owner account.
            </p>
            {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}
            <form onSubmit={submit}>
              <label class="mb-4 block text-sm font-bold">
                Email
                <input type="email" required autofocus value={email}
                  onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
              </label>
              <label class="mb-5 block text-sm font-bold">
                Existing admin password
                <input type="password" required value={password}
                  onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
              </label>
              <button type="submit" disabled={busy}
                class="w-full bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50">
                {busy ? "Upgrading…" : "Upgrade account"}
              </button>
            </form>
          </>
        )}
      </div>
    </div>
  );
}
