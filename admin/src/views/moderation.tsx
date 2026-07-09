import { useEffect, useState } from "preact/hooks";
import { api, type Comment, type CommentStatus } from "../api";

const TABS: { key: CommentStatus; label: string }[] = [
  { key: "pending", label: "Pending" },
  { key: "approved", label: "Approved" },
  { key: "rejected", label: "Rejected" },
];

export function Moderation() {
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
    <div class="mx-auto max-w-3xl px-4 py-8">
      <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">Comments</h1>
        <div class="flex gap-1 rounded-lg bg-gray-100 p-1">
          {TABS.map((t) => (
            <button
              key={t.key}
              onClick={() => setStatus(t.key)}
              class={
                "rounded px-3 py-1 text-sm font-medium " +
                (status === t.key ? "bg-white text-gray-900 shadow-sm" : "text-gray-500 hover:text-gray-900")
              }
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}

      {comments === null && !error ? (
        <p class="text-sm text-gray-400">Loading…</p>
      ) : comments && comments.length > 0 ? (
        <ul class="space-y-3">
          {comments.map((c) => (
            <li key={c.id} class="rounded-lg bg-white p-4 shadow-sm">
              <div class="mb-2 flex items-center gap-2 text-sm text-gray-500">
                <span class="font-medium text-gray-900">{c.author_name || "Anonymous"}</span>
                <span>·</span>
                <span>{c.created?.replace("T", " ").replace("Z", "")}</span>
              </div>
              <p class="mb-3 whitespace-pre-wrap text-sm text-gray-800">{c.body}</p>
              <div class="flex justify-end gap-2">
                {status !== "approved" && (
                  <button
                    onClick={() => moderate(c.id, "approved")}
                    class="rounded bg-green-600 px-2 py-1 text-xs font-medium text-white hover:bg-green-700"
                  >
                    Approve
                  </button>
                )}
                {status !== "rejected" && (
                  <button
                    onClick={() => moderate(c.id, "rejected")}
                    class="rounded bg-yellow-600 px-2 py-1 text-xs font-medium text-white hover:bg-yellow-700"
                  >
                    Reject
                  </button>
                )}
                <button
                  onClick={() => remove(c.id)}
                  class="rounded bg-red-600 px-2 py-1 text-xs font-medium text-white hover:bg-red-700"
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div class="rounded-lg bg-white p-6 text-center shadow-sm">
          <p class="text-sm text-gray-500">No {status} comments.</p>
        </div>
      )}
    </div>
  );
}
