import type { JSX } from "preact";
import { can, type User } from "../api";

type Tab = "dashboard" | "review" | "users" | "moderation" | "settings";

// The bar across the top of every admin page: the wordmark, then the sections
// this user can see, lowercase and separated by dots. The current one is blue.
export function Nav({
  user,
  active,
  onLogout,
}: {
  user: User;
  active: Tab | "";
  onLogout: () => void;
}) {
  // target="_top" on the "view site" link keeps the SPA's router from
  // treating "/" as one of its own pages: the browser leaves the admin instead.
  const link = (href: string, label: string, tab: Tab | "site") => (
    <a
      href={href}
      target={tab === "site" ? "_top" : undefined}
      class={
        "text-sm font-bold hover:underline " +
        (active === tab ? "text-link" : "text-ink")
      }
    >
      {label}
    </a>
  );
  const links: (JSX.Element | false)[] = [
    link("/_/", "dashboard", "dashboard"),
    can(user.role, "content.edit.any") && link("/_/review", "review", "review"),
    can(user.role, "user.manage") && link("/_/users", "users", "users"),
    can(user.role, "comment.moderate.own") && link("/_/moderation", "comments", "moderation"),
    can(user.role, "site.configure") && link("/_/settings", "settings", "settings"),
    link("/", "view site", "site"),
  ];

  return (
    <nav class="mx-auto flex max-w-[960px] flex-wrap items-baseline gap-x-2 gap-y-2 border-b border-ink px-4 pt-4 pb-2">
      <a href="/_/" class="mr-4 text-xl font-bold text-ink no-underline">
        <span class="text-base">&#10047;</span> friendo
      </a>
      {links
        .filter(Boolean)
        .flatMap((l, i) => (i === 0 ? [l] : [<span class="text-sm text-dim" key={"dot" + i}>&middot;</span>, l]))}
      <div class="ml-auto flex items-baseline gap-3">
        <span class="hidden text-xs text-dim sm:inline">{user.email}</span>
        <button
          onClick={onLogout}
          class="border border-ink px-2 py-1 text-xs font-bold text-ink hover:bg-ink hover:text-white"
        >
          Log out
        </button>
      </div>
    </nav>
  );
}
