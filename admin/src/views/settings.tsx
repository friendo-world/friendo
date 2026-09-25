import { useEffect, useState } from "preact/hooks";
import { api, type AccessSettings, type Features, type Settings, type SettingsPatch } from "../api";
import { useAuth } from "../auth";
import { RoleBadge } from "../components/role-badge";
import { Toggle } from "../components/ui";
import { ContentToml } from "../components/content-toml";

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt class="text-xs font-bold text-dim">{label}</dt>
      <dd class="mt-1 text-sm font-bold">{value}</dd>
    </div>
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
  const { user, logout, reloadFeatures } = useAuth();
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
      if (p.features) reloadFeatures();
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
    defaultCollections: "content.default_collections",
  };
  const FEATURES: { key: keyof Features; label: string; hint: string }[] = [
    { key: "comments", label: "Comments", hint: "Members comment on posts: <friendo-comments>, record.comments, and the moderation queue." },
    { key: "reactions", label: "Reactions", hint: "Emoji reactions on posts and comments: <friendo-reactions>, record.reactions." },
    { key: "polls", label: "Polls", hint: "Polls in a post's front matter: <friendo-poll>, record.poll." },
    { key: "rsvp", label: "RSVPs", hint: "Going / maybe / can't go on events: <friendo-rsvp>, record.rsvps, and the Attendees list." },
    { key: "locations", label: "Map pins", hint: "Pins on posts: <friendo-map>, record.location, and the editor's Where section." },
    { key: "channels", label: "Channels", hint: "Realtime chat and feeds: <friendo-channel>." },
  ];
  const DEFAULTS = ["blog", "pages", "posts"];
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

      <h2 class="mb-3 text-sm font-bold text-dim">Content</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        {s && !s.content.types_declared && (
          <div class="mb-6 border-b border-ink pb-6" data-default-collections>
            <p class="text-sm font-bold">Built-in collections</p>
            <p class="mb-2 text-xs text-dim">
              {managedHint(
                "Shown in the content sidebar while friendo.toml lists no [content] types. A collection you start yourself appears regardless.",
                MK.defaultCollections
              )}
            </p>
            <div class="flex flex-wrap gap-4">
              {DEFAULTS.map((name) => (
                <label key={name} class="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    name={`default-${name}`}
                    checked={s.content.default_collections.includes(name)}
                    disabled={saving || isManaged(MK.defaultCollections)}
                    onChange={(e) => {
                      const on = (e.target as HTMLInputElement).checked;
                      const next = DEFAULTS.filter((n) => (n === name ? on : s.content.default_collections.includes(n)));
                      patch({ content: { default_collections: next } });
                    }}
                  />
                  {name}
                </label>
              ))}
            </div>
          </div>
        )}
        <ContentToml />
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Features</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        <p class="mb-4 text-xs text-dim">
          Turn a community feature off and the site refuses it: its API says no, its <code>&lt;friendo-*&gt;</code> tag
          shows nothing, and a page's <code>record.…</code> for it is empty. What people already wrote is kept for when
          it comes back.
        </p>
        <div class="space-y-4" data-features>
          {s &&
            FEATURES.map((f) => (
              <Toggle
                key={f.key}
                label={f.label}
                hint={managedHint(f.hint, `features.${f.key}`)}
                on={s.features[f.key]}
                disabled={saving || isManaged(`features.${f.key}`)}
                onToggle={() => patch({ features: { [f.key]: !s.features[f.key] } })}
              />
            ))}
        </div>
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
