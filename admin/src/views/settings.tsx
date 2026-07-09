import { useEffect, useState } from "preact/hooks";
import { api, type Settings } from "../api";
import { useAuth } from "../auth";
import { RoleBadge } from "../components/role-badge";

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt class="text-xs font-medium text-gray-400 uppercase">{label}</dt>
      <dd class="mt-1 text-sm font-medium">{value}</dd>
    </div>
  );
}

export function SettingsView() {
  const { user, logout } = useAuth();
  const [s, setS] = useState<Settings | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .settings()
      .then(setS)
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load settings."));
  }, []);

  async function toggleAutoApprove() {
    if (!s) return;
    const next = !s.moderation.auto_approve;
    setSaving(true);
    try {
      const r = await api.updateSettings({ moderation: { auto_approve: next } });
      setS({ ...s, moderation: r.moderation });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div class="mx-auto max-w-3xl px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">Settings</h1>
      {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Site</h2>
      <div class="mb-8 rounded-lg bg-white p-6 shadow-sm">
        <dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Name" value={s ? s.site.name : "…"} />
          <Stat label="Collections" value={s ? s.collections : "…"} />
          <Stat label="Users" value={s ? s.users : "…"} />
        </dl>
        <p class="mt-4 text-xs text-gray-400">
          Site name and content types are configured in <code class="rounded bg-gray-100 px-1.5 py-0.5">friendo.toml</code>.
        </p>
      </div>

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Comments</h2>
      <div class="mb-8 rounded-lg bg-white p-6 shadow-sm">
        <label class="flex items-start justify-between gap-4">
          <span>
            <span class="text-sm font-medium">Auto-approve comments</span>
            <span class="mt-1 block text-xs text-gray-400">
              When on, new comments publish immediately. When off, they wait in the
              moderation queue for review.
            </span>
          </span>
          <button
            type="button"
            role="switch"
            aria-checked={s?.moderation.auto_approve ? "true" : "false"}
            disabled={!s || saving}
            onClick={toggleAutoApprove}
            class={
              "relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors " +
              (s?.moderation.auto_approve ? "bg-blue-600" : "bg-gray-300") +
              (!s || saving ? " opacity-50" : "")
            }
          >
            <span
              class={
                "inline-block h-5 w-5 translate-y-0.5 rounded-full bg-white transition-transform " +
                (s?.moderation.auto_approve ? "translate-x-5" : "translate-x-0.5")
              }
            />
          </button>
        </label>
      </div>

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Your account</h2>
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Name" value={user.name} />
          <Stat label="Email" value={user.email} />
          <div>
            <dt class="text-xs font-medium text-gray-400 uppercase">Role</dt>
            <dd class="mt-1"><RoleBadge role={user.role} /></dd>
          </div>
        </dl>
        <div class="mt-6 border-t border-gray-100 pt-4">
          <button onClick={logout}
            class="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50">
            Log out
          </button>
        </div>
      </div>
    </div>
  );
}
