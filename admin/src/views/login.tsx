import { useState } from "preact/hooks";
import { api, type SetupStatus, type User } from "../api";

const field =
  "mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none";
const primary =
  "w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50";

// Sign in with a code sent to your email — the default everywhere. A site that
// allows passwords (Settings → Signing in) also gets a "use a password" link.
export function Login({ status, onLogin }: { status: SetupStatus; onLogin: (u: User) => void }) {
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [mode, setMode] = useState<"email" | "code" | "password">("email");
  const [devNote, setDevNote] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // The "Open admin" handoff from a network account lands here with ?error=sso
  // when its one-time code was rejected or expired.
  const ssoFailed =
    typeof window !== "undefined" &&
    new URLSearchParams(window.location.search).get("error") === "sso";

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed.");
    } finally {
      setBusy(false);
    }
  }

  const sendCode = (e: Event) => {
    e.preventDefault();
    run(async () => {
      const r = await api.requestCode(email);
      // Local dev: no email provider, so the runtime hands the code back.
      if (r.code) {
        setCode(r.code);
        setDevNote("No email provider is set up, so here's your code: " + r.code);
      } else if (r.emailed === false) {
        setDevNote("No email provider is set up — the code is printed in the terminal running friendo serve.");
      } else {
        setDevNote("");
      }
      setMode("code");
    });
  };

  const verify = (e: Event) => {
    e.preventDefault();
    run(async () => {
      const { user } = await api.verifyCode(email, code);
      onLogin(user);
    });
  };

  const loginWithPassword = (e: Event) => {
    e.preventDefault();
    run(async () => {
      const { user } = await api.login(email, password);
      onLogin(user);
    });
  };

  return (
    <div class="mx-auto mt-16 max-w-md px-4">
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-6 text-xl font-bold">Sign in</h1>
        {ssoFailed && !error && (
          <div class="mb-4 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-700">
            Couldn't sign you in from your network account — that link may have expired. Press
            “Open admin” again, or sign in below.
          </div>
        )}
        {error && (
          <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>
        )}

        {mode === "email" && (
          <form onSubmit={sendCode}>
            <label class="mb-5 block text-sm font-medium">
              Email
              <input type="email" required autofocus value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Sending…" : "Email me a code"}
            </button>
            {status.passwordLogin && (
              <button type="button" onClick={() => setMode("password")}
                class="mt-3 block w-full text-center text-xs text-gray-500 hover:text-gray-700">
                Use a password instead
              </button>
            )}
          </form>
        )}

        {mode === "code" && (
          <form onSubmit={verify}>
            <p class="mb-4 text-sm text-gray-500">
              {devNote || <>We sent a 6-digit code to <span class="font-medium text-gray-700">{email}</span>.</>}
            </p>
            <label class="mb-5 block text-sm font-medium">
              Code
              <input type="text" inputMode="numeric" autocomplete="one-time-code" required autofocus
                value={code} maxLength={6}
                onInput={(e) => setCode((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <button type="button" onClick={() => { setMode("email"); setCode(""); setDevNote(""); }}
              class="mt-3 block w-full text-center text-xs text-gray-500 hover:text-gray-700">
              Use a different email
            </button>
          </form>
        )}

        {mode === "password" && (
          <form onSubmit={loginWithPassword}>
            <label class="mb-4 block text-sm font-medium">
              Email
              <input type="email" required autofocus value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <label class="mb-5 block text-sm font-medium">
              Password
              <input type="password" required value={password}
                onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <button type="button" onClick={() => setMode("email")}
              class="mt-3 block w-full text-center text-xs text-gray-500 hover:text-gray-700">
              Email me a code instead
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
