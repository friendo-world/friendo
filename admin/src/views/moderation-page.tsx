import { can } from "../api";
import { useAuth } from "../auth";
import { ReviewQueue } from "./review";
import { CommentsQueue } from "./moderation";

// Everything waiting on a moderator, on one page: posts to review (editors and
// up), then comments (anyone who may moderate — authors see their own posts').
export function ModerationPage() {
  const { user, features } = useAuth();
  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">Moderation</h1>
      <div class="space-y-10">
        {can(user.role, "content.edit.any") && <ReviewQueue />}
        {can(user.role, "comment.moderate.own") &&
          (features.comments ? (
            <CommentsQueue />
          ) : (
            <section data-section="comments-off">
              <h2 class="mb-1 text-sm font-bold text-dim">Comments</h2>
              <p class="text-xs text-dim">
                Comments are turned off for this site{can(user.role, "site.configure") ? <> (Settings → Features)</> : null}.
              </p>
            </section>
          ))}
      </div>
    </div>
  );
}
