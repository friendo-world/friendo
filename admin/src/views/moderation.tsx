import { useEffect, useState } from "preact/hooks";
import { api, type Comment, type CommentStatus } from "../api";

const TABS: { key: CommentStatus; label: string }[] = [
  { key: "pending", label: "Pending" },
  { key: "approved", label: "Approved" },
  { key: "rejected", label: "Rejected" },
];

// Comments by status, as a section of the Moderation page.
export function CommentsQueue() {
  const [status, setStatus] = useState<CommentStatus>("pending");
  const [comments, setComments] = useState<Comment[] | null>(null);
  const [error, setError] = useState("");

  function load(s: CommentStatus) {
    setComments(null);
    setError("");
    api
      .comments(s)
      .then((r) => setComments(r.comments))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load comments."));
  }

  useEffect(() => load(status), [status]);

  async function moderate(id: string, next: CommentStatus) {
    try {
      await api.setCommentStatus(id, next);
      load(status);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Action failed.");
    }
  }

  async function remove(id: string) {
    if (!confirm("Delete this comment permanently?")) return;
    try {
      await api.deleteComment(id);
      load(status);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Delete failed.");
    }
  }

  return (
    <section data-section="comments">
      <div class="mb-3 flex items-center justify-between">
        <h2 class="text-sm font-bold text-dim">Comments</h2>
        <div class="flex border border-ink">
          {TABS.map((t, i) => (
            <button
              key={t.key}
              onClick={() => setStatus(t.key)}
              class={
                "px-3 py-1 text-sm font-bold " +
                (i > 0 ? "border-l border-ink " : "") +
                (status === t.key ? "bg-ink text-white" : "text-dim hover:bg-tint hover:text-ink")
              }
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

      {comments === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : comments && comments.length > 0 ? (
        <ul class="space-y-3">
          {comments.map((c) => (
            <li key={c.id} class="bg-white p-4 border border-ink">
              <div class="mb-2 flex items-center gap-2 text-sm text-dim">
                <span class="font-bold text-ink">{c.author_name || "Anonymous"}</span>
                <span>·</span>
                <span>{c.created?.replace("T", " ").replace("Z", "")}</span>
              </div>
              <p class="mb-3 whitespace-pre-wrap text-sm text-dim">{c.body}</p>
              <div class="flex justify-end gap-2">
                {status !== "approved" && (
                  <button
                    onClick={() => moderate(c.id, "approved")}
                    class="bg-ink px-2 py-1 text-xs font-bold text-white hover:bg-link"
                  >
                    Approve
                  </button>
                )}
                {status !== "rejected" && (
                  <button
                    onClick={() => moderate(c.id, "rejected")}
                    class="bg-dim px-2 py-1 text-xs font-bold text-white hover:bg-ink"
                  >
                    Reject
                  </button>
                )}
                <button
                  onClick={() => remove(c.id)}
                  class="bg-crimson px-2 py-1 text-xs font-bold text-white hover:bg-ink"
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div class="bg-white p-6 text-center border border-ink">
          <p class="text-sm text-dim">No {status} comments.</p>
        </div>
      )}
    </section>
  );
}
