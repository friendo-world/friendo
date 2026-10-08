import { useEffect, useState } from "preact/hooks";
import { api, type Features, type ProfileVisibility, type Settings, type SettingValues, type SettingsPatch } from "../api";
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

// Presets set the membership policy in one click. They're just bundles of the
// same three settings a site owner can also flip individually below — plus who
// sees profiles (a personal site keeps them to members; a community or blog shows
// its people to everyone).
type Preset = Pick<SettingValues, "signups_are_contributors" | "open_signups" | "posts_need_review">;
const PRESETS: { key: string; label: string; hint: string; values: Preset; profiles: ProfileVisibility }[] = [
  {
    key: "personal",
    label: "Personal",
    hint: "Just you (and people you add). No public sign-ups.",
    values: { signups_are_contributors: false, open_signups: false, posts_need_review: false },
    profiles: "members",
  },
  {
    key: "community",
    label: "Community",
    hint: "Anyone can join and post their own; moderators keep it tidy.",
    values: { signups_are_contributors: true, open_signups: true, posts_need_review: false },
    profiles: "public",
  },
  {
    key: "blog",
    label: "Blog",
    hint: "Readers can comment; contributors write; editors publish.",
    values: { signups_are_contributors: false, open_signups: true, posts_need_review: true },
    profiles: "public",
  },
];

function matchesPreset(s: SettingValues, p: Preset) {
  return (
    s.signups_are_contributors === p.signups_are_contributors &&
    s.open_signups === p.open_signups &&
    s.posts_need_review === p.posts_need_review
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

  const activePreset = s ? PRESETS.find((p) => matchesPreset(s, p.values))?.key : undefined;

  const FEATURES: { key: keyof Features; label: string; hint: string }[] = [
    { key: "comments", label: "Comments", hint: "Members comment on posts: <friendo-comments>, post.comments, and the review queue." },
    { key: "reactions", label: "Reactions", hint: "Emoji reactions on posts and comments: <friendo-reactions>, post.reactions." },
    { key: "polls", label: "Polls", hint: "Polls in a post's front matter: <friendo-poll>, post.poll." },
    { key: "rsvp", label: "RSVPs", hint: "Going / maybe / can't go on events: <friendo-rsvp>, event.rsvps, and the RSVPs list." },
    { key: "locations", label: "Locations", hint: "A place on a post: <friendo-map>, post.location, and the editor's Location section." },
    { key: "chats", label: "Chats", hint: "Realtime chat and feeds: <friendo-chat>." },
    { key: "follows", label: "Follows", hint: "Members follow each other: <friendo-follow>, user.following, profile.followers, and the by_following filter." },
    { key: "groups", label: "Groups", hint: "Groups with members, as posts in the groups collection: <friendo-group>, <friendo-groups>, user.groups, group.members, and the in_group filter." },
  ];
  const DEFAULTS = ["blog", "pages"];
  const isManaged = (k: string) => s?.managed?.includes(k) ?? false;
  const presetsManaged = isManaged("signups_are_contributors") || isManaged("open_signups") || isManaged("posts_need_review");
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
          <Stat label="Members" value={s ? s.users : "…"} />
        </dl>
        <p class="mt-4 text-xs text-dim">
          Site name and collections are configured in <code class="bg-tint px-1.5 py-0.5">friendo.toml</code>.
        </p>
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Content</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        {s && !s.collections_declared && (
          <div class="mb-6 border-b border-ink pb-6" data-default-collections>
            <p class="text-sm font-bold">Built-in collections</p>
            <p class="mb-2 text-xs text-dim">
              {managedHint(
                "Shown in the content sidebar while friendo.toml lists no [content] collections. A collection you start yourself appears regardless.",
                "default_collections"
              )}
            </p>
            <div class="flex flex-wrap gap-4">
              {DEFAULTS.map((name) => (
                <label key={name} class="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    name={`default-${name}`}
                    checked={s.default_collections.includes(name)}
                    disabled={saving || isManaged("default_collections")}
                    onChange={(e) => {
                      const on = (e.target as HTMLInputElement).checked;
                      const next = DEFAULTS.filter((n) => (n === name ? on : s.default_collections.includes(n)));
                      patch({ default_collections: next });
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
          shows nothing, and a page's <code>post.…</code> for it is empty. What people already wrote is kept for when
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

      <h2 class="mb-3 text-sm font-bold text-dim">Members &amp; roles</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        <p class="mb-3 text-xs text-dim">Everyone with an account is a member; roles add powers. Pick a preset, or fine-tune the settings below.</p>
        <div class="mb-6 grid grid-cols-1 gap-2 sm:grid-cols-3">
          {PRESETS.map((p) => (
            <button
              key={p.key}
              type="button"
              disabled={!s || saving || presetsManaged}
              onClick={() => patch({ ...p.values, ...(isManaged("profile_visibility") ? {} : { profile_visibility: p.profiles }) })}
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
          {s && (
            <Toggle
              label="Anyone can sign up"
              hint={managedHint("When off, only people an admin adds have accounts.", "open_signups")}
              on={s.open_signups}
              disabled={saving || isManaged("open_signups")}
              onToggle={() => patch({ open_signups: !s.open_signups })}
            />
          )}
          {s && (
            <Toggle
              label="New members start as contributors"
              hint={managedHint("A contributor writes their own posts. Off, a new member can comment, react, vote and RSVP.", "signups_are_contributors")}
              on={s.signups_are_contributors}
              disabled={saving || isManaged("signups_are_contributors")}
              onToggle={() => patch({ signups_are_contributors: !s.signups_are_contributors })}
            />
          )}
          {s && (
            <Toggle
              label="Contributors' posts wait for review"
              hint={managedHint("When on, a contributor's post waits in the review queue until a moderator approves it.", "posts_need_review")}
              on={s.posts_need_review}
              disabled={saving || isManaged("posts_need_review")}
              onToggle={() => patch({ posts_need_review: !s.posts_need_review })}
            />
          )}
          {s && (
            <Toggle
              label="Members can post"
              hint={managedHint("When on, any member can post from a page's <friendo-form>. Their posts always wait in the review queue.", "members_can_post")}
              on={s.members_can_post}
              disabled={saving || isManaged("members_can_post")}
              onToggle={() => patch({ members_can_post: !s.members_can_post })}
            />
          )}
          {s && (
            <Toggle
              label="Members can add images"
              hint={managedHint("When on, a member posting from a <friendo-form> can include images. They wait for review with the post. Contributors and up can always add images.", "members_can_upload")}
              on={s.members_can_upload}
              disabled={saving || isManaged("members_can_upload")}
              onToggle={() => patch({ members_can_upload: !s.members_can_upload })}
            />
          )}
          {s && (
            <Toggle
              label="Members can be anonymous"
              hint={managedHint("When on, a member can post or comment as \"Anonymous\". Moderators and editors still see who wrote it.", "members_can_be_anonymous")}
              on={s.members_can_be_anonymous}
              disabled={saving || isManaged("members_can_be_anonymous")}
              onToggle={() => patch({ members_can_be_anonymous: !s.members_can_be_anonymous })}
            />
          )}
          {s && s.features.groups && (
            <Toggle
              label="Members can start groups"
              hint={managedHint("When on, any member can start a group and is its admin. Off, only contributors and up can — from the admin or a page's <friendo-groups>.", "members_can_start_groups")}
              on={s.members_can_start_groups}
              disabled={saving || isManaged("members_can_start_groups")}
              onToggle={() => patch({ members_can_start_groups: !s.members_can_start_groups })}
            />
          )}
          {s && s.features.reactions && (
            <Toggle
              label="Visitors can react"
              hint={managedHint("When on, someone who hasn't signed in can react. Their browser remembers them, and if they sign in later their reactions come with them.", "visitors_can_react")}
              on={s.visitors_can_react}
              disabled={saving || isManaged("visitors_can_react")}
              onToggle={() => patch({ visitors_can_react: !s.visitors_can_react })}
            />
          )}
          {s && s.features.polls && (
            <Toggle
              label="Visitors can vote"
              hint={managedHint("When on, someone who hasn't signed in can vote in polls — one vote per browser, so a determined person can vote twice.", "visitors_can_vote")}
              on={s.visitors_can_vote}
              disabled={saving || isManaged("visitors_can_vote")}
              onToggle={() => patch({ visitors_can_vote: !s.visitors_can_vote })}
            />
          )}
          {s && s.features.rsvp && (
            <Toggle
              label="Visitors can RSVP"
              hint={managedHint("When on, someone who hasn't signed in can answer an event, no name needed. They can add a name and an email for a reminder the day before. Organizers see them as \"Visitor\" (or \"Robin (visitor)\") with that email; nobody else sees it.", "visitors_can_rsvp")}
              on={s.visitors_can_rsvp}
              disabled={saving || isManaged("visitors_can_rsvp")}
              onToggle={() => patch({ visitors_can_rsvp: !s.visitors_can_rsvp })}
            />
          )}
          {s && s.features.comments && (
            <Toggle
              label="Visitors can comment"
              hint={managedHint("When on, someone who hasn't signed in can comment, with a name if they like. Their comments always wait for review.", "visitors_can_comment")}
              on={s.visitors_can_comment}
              disabled={saving || isManaged("visitors_can_comment")}
              onToggle={() => patch({ visitors_can_comment: !s.visitors_can_comment })}
            />
          )}
          {s && (
            <Toggle
              label="Visitors can post"
              hint={managedHint("When on, someone who hasn't signed in can post from a page's <friendo-form>. Their posts always wait in the review queue.", "visitors_can_post")}
              on={s.visitors_can_post}
              disabled={saving || isManaged("visitors_can_post")}
              onToggle={() => patch({ visitors_can_post: !s.visitors_can_post })}
            />
          )}
          {s && s.visitors_can_post && (
            <Toggle
              label="Visitors can add images"
              hint={managedHint("When on, a visitor posting from a <friendo-form> can include images. They wait for review with the post, and go if the visitor takes the post back.", "visitors_can_upload")}
              on={s.visitors_can_upload}
              disabled={saving || isManaged("visitors_can_upload")}
              onToggle={() => patch({ visitors_can_upload: !s.visitors_can_upload })}
            />
          )}
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="text-sm font-bold">Profiles are visible to</span>
              <span class="mt-1 block text-xs text-dim">
                {managedHint("Every profile has a page at /profiles/<slug> (when your site has pages/profiles/[slug].html) and shows in the profiles API.", "profile_visibility")}
              </span>
            </span>
            <select
              name="profile_visibility"
              disabled={!s || saving || isManaged("profile_visibility")}
              value={s?.profile_visibility ?? "members"}
              onChange={(e) => patch({ profile_visibility: (e.target as HTMLSelectElement).value as ProfileVisibility })}
              class="border border-ink px-2 py-1 text-sm disabled:opacity-50"
            >
              <option value="members">Members</option>
              <option value="public">Everyone</option>
            </select>
          </label>
        </div>
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Signing in</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        {s && (
          <Toggle
            label="Allow signing in with a password"
            hint={managedHint("Everyone can always sign in with a code emailed to them. Turn this on to also allow passwords — you'll be able to set one when adding or editing a member.", "password_login")}
            on={s.password_login}
            disabled={saving || isManaged("password_login")}
            onToggle={() => patch({ password_login: !s.password_login })}
          />
        )}
      </div>

      <h2 class="mb-3 text-sm font-bold text-dim">Comments</h2>
      <div class="mb-8 bg-white p-6 border border-ink">
        {s && (
          <Toggle
            label="Comments wait for review"
            hint={managedHint("When on, a new comment waits in the review queue until a moderator approves it. Off, comments show at once.", "comments_need_review")}
            on={s.comments_need_review}
            disabled={saving || isManaged("comments_need_review")}
            onToggle={() => patch({ comments_need_review: !s.comments_need_review })}
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
            Sign out
          </button>
        </div>
      </div>
    </div>
  );
}
