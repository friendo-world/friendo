import { useState } from "preact/hooks";
import { api, type SetupStatus, type User } from "../api";

const field =
  "mt-1 block w-full border border-ink px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none";
const primary =
  "w-full bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50";

// First-run setup: prove the owner's email with a code, then the account is
// created. If friendo.toml turned password sign-in on for this site, a password
// is collected instead (the code-only account would have nothing to type).
export function Setup({ status, onLogin }: { status: SetupStatus; onLogin: (u: User) => void }) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [step, setStep] = useState<"details" | "code">("details");
  const [devNote, setDevNote] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const usePassword = !!status.passwordLogin;

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Setup failed.");
    } finally {
      setBusy(false);
    }
  }

  const submitDetails = (e: Event) => {
    e.preventDefault();
    run(async () => {
      if (usePassword) {
        const { user } = await api.setup(email, name, password);
        onLogin(user);
        return;
      }
      const r = await api.setupRequestCode(email);
      if (r.code) {
        setCode(r.code);
        setDevNote("No email provider is set up, so here's your code: " + r.code);
      } else if (r.emailed === false) {
        setDevNote("No email provider is set up — the code is printed in the terminal running friendo serve.");
      } else {
        setDevNote("");
      }
      setStep("code");
    });
  };

  const submitCode = (e: Event) => {
    e.preventDefault();
    run(async () => {
      const { user } = await api.setupWithCode(email, name, code);
      onLogin(user);
    });
  };

  return (
    <div class="mx-auto mt-16 max-w-md px-4">
      <div class="bg-white p-6 border border-ink">
        <h1 class="mb-1 text-xl font-bold">Set up Friendo</h1>
        <p class="mb-6 text-sm text-dim">
          {step === "details"
            ? "Create the owner account for this site."
            : devNote || <>We sent a 6-digit code to <span class="font-bold text-dim">{email}</span>.</>}
        </p>
        {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

        {step === "details" ? (
          <form onSubmit={submitDetails}>
            <label class="mb-4 block text-sm font-bold">
              Email
              <input type="email" required autofocus value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <label class="mb-4 block text-sm font-bold">
              Name <span class="font-normal text-dim">(optional)</span>
              <input type="text" value={name} placeholder="Defaults to email prefix"
                onInput={(e) => setName((e.target as HTMLInputElement).value)} class={field} />
            </label>
            {usePassword ? (
              <>
                <label class="mb-1 block text-sm font-bold">
                  Password
                  <input type="password" required minLength={8} value={password}
                    onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
                </label>
                <p class="mb-5 text-xs text-dim">Minimum 8 characters</p>
              </>
            ) : (
              <p class="mb-5 text-xs text-dim">
                No password needed — we'll email you a code to confirm it's you.
              </p>
            )}
            <button type="submit" disabled={busy} class={primary}>
              {busy ? (usePassword ? "Creating…" : "Sending…") : usePassword ? "Create account" : "Email me a code"}
            </button>
          </form>
        ) : (
          <form onSubmit={submitCode}>
            <label class="mb-5 block text-sm font-bold">
              Code
              <input type="text" inputMode="numeric" autocomplete="one-time-code" required autofocus
                value={code} maxLength={6}
                onInput={(e) => setCode((e.target as HTMLInputElement).value)} class={field} />
            </label>
            <button type="submit" disabled={busy} class={primary}>
              {busy ? "Creating…" : "Create account"}
            </button>
            <button type="button" onClick={() => { setStep("details"); setCode(""); setDevNote(""); }}
              class="mt-3 block w-full text-center text-xs text-dim hover:text-dim">
              Use a different email
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
