import { useState } from "preact/hooks";
import { api } from "../api";

const field =
  "mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none";

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
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-1 text-xl font-bold">Upgrade your admin</h1>
        {done ? (
          <>
            <p class="mb-6 text-sm text-gray-500">
              Your account has been upgraded. Please log in with your existing password.
            </p>
            <button onClick={onDone}
              class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">
              Continue to log in
            </button>
          </>
        ) : (
          <>
            <p class="mb-6 text-sm text-gray-500">
              Friendo now uses email-based accounts. Enter your email and your existing admin
              password to upgrade it to a full owner account.
            </p>
            {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}
            <form onSubmit={submit}>
              <label class="mb-4 block text-sm font-medium">
                Email
                <input type="email" required autofocus value={email}
                  onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
              </label>
              <label class="mb-5 block text-sm font-medium">
                Existing admin password
                <input type="password" required value={password}
                  onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
              </label>
              <button type="submit" disabled={busy}
                class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
                {busy ? "Upgrading…" : "Upgrade account"}
              </button>
            </form>
          </>
        )}
      </div>
    </div>
  );
}
