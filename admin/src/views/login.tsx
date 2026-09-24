import { useState } from "preact/hooks";
import { api, type SetupStatus, type User } from "../api";

const field =
  "mt-1 block w-full border border-ink px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none";
const primary =
  "w-full bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50";

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
      <div class="bg-white p-6 border border-ink">
        <h1 class="mb-6 text-xl font-bold">Sign in</h1>
        {ssoFailed && !error && (
          <div class="mb-4 bg-manila px-3 py-2 text-sm text-ink">
            Couldn't sign you in from your network account — that link may have expired. Press
            “Open admin” again, or sign in below.
          </div>
        )}
        {error && (
          <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>
        )}

        {mode === "email" && (
          <form onSubmit={sendCode}>
            <label class="mb-5 block text-sm font-bold">
              Email
              <input type="email" required autofocus value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Sending…" : "Email me a code"}
            </button>
            {status.passwordLogin && (
              <button type="button" onClick={() => setMode("password")}
                class="mt-3 block w-full text-center text-xs text-dim hover:text-dim">
                Use a password instead
              </button>
            )}
          </form>
        )}

        {mode === "code" && (
          <form onSubmit={verify}>
            <p class="mb-4 text-sm text-dim">
              {devNote || <>We sent a 6-digit code to <span class="font-bold text-dim">{email}</span>.</>}
            </p>
            <label class="mb-5 block text-sm font-bold">
              Code
              <input type="text" inputMode="numeric" autocomplete="one-time-code" required autofocus
                value={code} maxLength={6}
                onInput={(e) => setCode((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <button type="button" onClick={() => { setMode("email"); setCode(""); setDevNote(""); }}
              class="mt-3 block w-full text-center text-xs text-dim hover:text-dim">
              Use a different email
            </button>
          </form>
        )}

        {mode === "password" && (
          <form onSubmit={loginWithPassword}>
            <label class="mb-4 block text-sm font-bold">
              Email
              <input type="email" required autofocus value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <label class="mb-5 block text-sm font-bold">
              Password
              <input type="password" required value={password}
                onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <button type="button" onClick={() => setMode("email")}
              class="mt-3 block w-full text-center text-xs text-dim hover:text-dim">
              Email me a code instead
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
