import { useEffect, useState } from "preact/hooks";
import { api, type AccessSettings, type Settings, type SettingsPatch } from "../api";
import { useAuth } from "../auth";
import { RoleBadge } from "../components/role-badge";

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt class="text-xs font-bold text-dim">{label}</dt>
      <dd class="mt-1 text-sm font-bold">{value}</dd>
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
        <span class="text-sm font-bold">{label}</span>
        <span class="mt-1 block text-xs text-dim">{hint}</span>
      </span>
      <button
        type="button"
        role="switch"
        aria-checked={on ? "true" : "false"}
        disabled={disabled}
        onClick={onToggle}
        class={
          "relative inline-flex h-6 w-11 shrink-0 border border-ink transition-colors " +
          (on ? "bg-link" : "bg-white") +
          (disabled ? " opacity-50" : "")
        }
      >
        <span
          class={
            "inline-block h-4 w-4 translate-y-[3px] transition-transform " +
            (on ? "translate-x-[25px] bg-white" : "translate-x-[3px] bg-ink")
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
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">Settings</h1>
      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}
      {s && s.managed.length > 0 && (
        <div class="mb-4 bg-manila px-3 py-2 text-sm text-ink">
          Some settings are defined in <code class="bg-white px-1.5 py-0.5">friendo.toml</code> and
          are read-only here. Edit the file to change them.
        </div>
      )}

      <h2 class="mb-3 text-sm font-bold text-dim">Site</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        <dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Name" value={s ? s.site.name : "…"} />
          <Stat label="Collections" value={s ? s.collections : "…"} />
          <Stat label="Users" value={s ? s.users : "…"} />
        </dl>
        <p class="mt-4 text-xs text-dim">
          Site name and content types are configured in <code class="bg-tint px-1.5 py-0.5">friendo.toml</code>.
        </p>
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Access &amp; roles</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        <p class="mb-3 text-xs text-dim">Pick a preset, or fine-tune the settings below.</p>
        <div class="mb-6 grid grid-cols-1 gap-2 sm:grid-cols-3">
          {PRESETS.map((p) => (
            <button
              key={p.key}
              type="button"
              disabled={!s || saving || accessManaged}
              onClick={() => patch({ access: p.access })}
              class={
                " border p-3 text-left " +
                (activePreset === p.key ? "border-link bg-tint" : "border-ink hover:bg-tint")
              }
            >
              <span class="block text-sm font-bold">{p.label}</span>
              <span class="mt-1 block text-xs text-dim">{p.hint}</span>
            </button>
          ))}
        </div>

        <div class="space-y-4 border-t border-ink pt-4">
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="text-sm font-bold">New members can post</span>
              <span class="mt-1 block text-xs text-dim">
                {managedHint("What a visitor becomes when they sign up.", MK.defaultRole)}
              </span>
            </span>
            <select
              disabled={!s || saving || isManaged(MK.defaultRole)}
              value={s?.access.default_role}
              onChange={(e) => patch({ access: { default_role: (e.target as HTMLSelectElement).value as AccessSettings["default_role"] } })}
              class="border border-ink px-2 py-1 text-sm disabled:opacity-50"
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

      <h2 class="mb-3 text-sm font-bold text-dim">Signing in</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
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

      <h2 class="mb-3 text-sm font-bold text-dim">Comments</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
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

      <h2 class="mb-3 text-sm font-bold text-dim">Your account</h2>
      <div class="bg-white p-6 border border-ink">
        <dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Name" value={user.name} />
          <Stat label="Email" value={user.email} />
          <div>
            <dt class="text-xs font-bold text-dim">Role</dt>
            <dd class="mt-1"><RoleBadge role={user.role} /></dd>
          </div>
        </dl>
        <div class="mt-6 border-t border-ink pt-4">
          <button onClick={logout}
            class="border border-ink px-4 py-2 text-sm font-bold text-dim hover:bg-tint">
            Log out
          </button>
        </div>
      </div>
    </div>
  );
}
