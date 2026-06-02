import { useState } from "preact/hooks";
import { api, type User } from "../api";

const field =
  "mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none";

export function Setup({ onLogin }: { onLogin: (u: User) => void }) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const { user } = await api.setup(email, name, password);
      onLogin(user);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Setup failed.");
      setBusy(false);
    }
  }

  return (
    <div class="mx-auto mt-16 max-w-md px-4">
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-1 text-xl font-bold">Set up Friendo</h1>
        <p class="mb-6 text-sm text-gray-500">Create your admin account to get started.</p>
        {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}
        <form onSubmit={submit}>
          <label class="mb-4 block text-sm font-medium">
            Email
            <input type="email" required autofocus value={email}
              onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="mb-4 block text-sm font-medium">
            Name <span class="font-normal text-gray-400">(optional)</span>
            <input type="text" value={name} placeholder="Defaults to email prefix"
              onInput={(e) => setName((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="mb-1 block text-sm font-medium">
            Password
            <input type="password" required minLength={8} value={password}
              onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <p class="mb-5 text-xs text-gray-400">Minimum 8 characters</p>
          <button type="submit" disabled={busy}
            class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
            {busy ? "Creating…" : "Create account"}
          </button>
        </form>
      </div>
    </div>
  );
}
