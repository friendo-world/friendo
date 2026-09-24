import { useEffect, useState } from "preact/hooks";
import { api, type AccessSettings, type Settings, type SettingsPatch } from "../api";
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

function Toggle({
  label,
  hint,
  on,
  disabled,
  onToggle,
}: {
  label: string;
  hint: string;
  on: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  return (
    <label class="flex items-start justify-between gap-4">
      <span>
        <span class="text-sm font-medium">{label}</span>
        <span class="mt-1 block text-xs text-gray-400">{hint}</span>
      </span>
      <button
        type="button"
        role="switch"
        aria-checked={on ? "true" : "false"}
        disabled={disabled}
        onClick={onToggle}
        class={
          "relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors " +
          (on ? "bg-blue-600" : "bg-gray-300") +
          (disabled ? " opacity-50" : "")
        }
      >
        <span
          class={
            "inline-block h-5 w-5 translate-y-0.5 rounded-full bg-white transition-transform " +
            (on ? "translate-x-5" : "translate-x-0.5")
          }
        />
      </button>
    </label>
  );
}

// Presets set the access policy in one click. They're just bundles of the same
// three settings a site owner can also flip individually below.
type AccessPreset = Pick<AccessSettings, "default_role" | "signups_enabled" | "require_approval">;
const PRESETS: { key: string; label: string; hint: string; access: AccessPreset }[] = [
  {
    key: "personal",
    label: "Personal",
    hint: "Just you (and invited editors). No public sign-ups.",
    access: { default_role: "member", signups_enabled: false, require_approval: false },
  },
  {
    key: "community",
    label: "Community",
    hint: "Anyone can join and post their own; admins moderate.",
    access: { default_role: "contributor", signups_enabled: true, require_approval: false },
  },
  {
    key: "blog",
    label: "Blog",
    hint: "Readers can comment; contributors write; editors publish.",
    access: { default_role: "member", signups_enabled: true, require_approval: true },
  },
];

function matchesPreset(a: AccessSettings, p: AccessPreset) {
  return (
    a.default_role === p.default_role &&
    a.signups_enabled === p.signups_enabled &&
    a.require_approval === p.require_approval
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

  async function patch(p: SettingsPatch) {
    setSaving(true);
    try {
      setS(await api.updateSettings(p));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save.");
    } finally {
      setSaving(false);
    }
  }

  const activePreset = s ? PRESETS.find((p) => matchesPreset(s.access, p.access))?.key : undefined;

  // DB keys that friendo.toml's [settings] block has frozen — their controls render
  // read-only. (These are the internal setting keys the API reports in `managed`.)
  const MK = {
    autoApprove: "moderation.auto_approve",
    defaultRole: "access.default_role",
    signups: "access.signups_enabled",
    approval: "content.require_approval",
    submissions: "content.accept_submissions",
    passwordLogin: "access.password_login",
  };
  const isManaged = (k: string) => s?.managed?.includes(k) ?? false;
  const accessManaged = isManaged(MK.defaultRole) || isManaged(MK.signups) || isManaged(MK.approval);
  const managedHint = (base: string, k: string) => (isManaged(k) ? base + " · Set in friendo.toml." : base);

  return (
    <div class="mx-auto max-w-3xl px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">Settings</h1>
      {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}
      {s && s.managed.length > 0 && (
        <div class="mb-4 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-700">
          Some settings are defined in <code class="rounded bg-amber-100 px-1.5 py-0.5">friendo.toml</code> and
          are read-only here. Edit the file to change them.
        </div>
      )}

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

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Access &amp; roles</h2>
      <div class="mb-8 rounded-lg bg-white p-6 shadow-sm">
        <p class="mb-3 text-xs text-gray-400">Pick a preset, or fine-tune the settings below.</p>
        <div class="mb-6 grid grid-cols-1 gap-2 sm:grid-cols-3">
          {PRESETS.map((p) => (
            <button
              key={p.key}
              type="button"
              disabled={!s || saving || accessManaged}
              onClick={() => patch({ access: p.access })}
              class={
                "rounded-lg border p-3 text-left " +
                (activePreset === p.key ? "border-blue-500 bg-blue-50" : "border-gray-200 hover:bg-gray-50")
              }
            >
              <span class="block text-sm font-medium">{p.label}</span>
              <span class="mt-1 block text-xs text-gray-400">{p.hint}</span>
            </button>
          ))}
        </div>

        <div class="space-y-4 border-t border-gray-100 pt-4">
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="text-sm font-medium">New members can post</span>
              <span class="mt-1 block text-xs text-gray-400">
                {managedHint("What a visitor becomes when they sign up.", MK.defaultRole)}
              </span>
            </span>
            <select
              disabled={!s || saving || isManaged(MK.defaultRole)}
              value={s?.access.default_role}
              onChange={(e) => patch({ access: { default_role: (e.target as HTMLSelectElement).value as AccessSettings["default_role"] } })}
              class="rounded-md border border-gray-300 px-2 py-1 text-sm disabled:opacity-50"
            >
              <option value="member">Member — comment &amp; react only</option>
              <option value="contributor">Contributor — can post their own</option>
            </select>
          </label>

          {s && (
            <Toggle
              label="Allow sign-ups"
              hint={managedHint("When off, only invited accounts can join.", MK.signups)}
              on={s.access.signups_enabled}
              disabled={saving || isManaged(MK.signups)}
              onToggle={() => patch({ access: { signups_enabled: !s.access.signups_enabled } })}
            />
          )}
          {s && (
            <Toggle
              label="Posts need approval"
              hint={managedHint("When on, posts by contributors wait as drafts until an editor publishes them.", MK.approval)}
              on={s.access.require_approval}
              disabled={saving || isManaged(MK.approval)}
              onToggle={() => patch({ access: { require_approval: !s.access.require_approval } })}
            />
          )}
          {s && (
            <Toggle
              label="Members can submit posts"
              hint={managedHint("When on, signed-in members can submit posts from a page's <friendo-form>. Submissions always wait in the review queue until an editor publishes them.", MK.submissions)}
              on={s.content.accept_submissions}
              disabled={saving || isManaged(MK.submissions)}
              onToggle={() => patch({ content: { accept_submissions: !s.content.accept_submissions } })}
            />
          )}
        </div>
      </div>

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Signing in</h2>
      <div class="mb-8 rounded-lg bg-white p-6 shadow-sm">
        {s && (
          <Toggle
            label="Allow signing in with a password"
            hint={managedHint("Everyone can always sign in with a code emailed to them. Turn this on to also allow passwords — you'll be able to set one when adding or editing a user.", MK.passwordLogin)}
            on={s.access.password_login}
            disabled={saving || isManaged(MK.passwordLogin)}
            onToggle={() => patch({ access: { password_login: !s.access.password_login } })}
          />
        )}
      </div>

      <h2 class="mb-3 text-sm font-semibold tracking-wide text-gray-500 uppercase">Comments</h2>
      <div class="mb-8 rounded-lg bg-white p-6 shadow-sm">
        {s && (
          <Toggle
            label="Auto-approve comments"
            hint={managedHint("When on, new comments publish immediately. When off, they wait in the moderation queue.", MK.autoApprove)}
            on={s.moderation.auto_approve}
            disabled={saving || isManaged(MK.autoApprove)}
            onToggle={() => patch({ moderation: { auto_approve: !s.moderation.auto_approve } })}
          />
        )}
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
