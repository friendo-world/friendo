// D1 schema migrations for the edge runtime.
//
// Migrations run once per database, on the first request that an isolate handles
// (see ensureMigrated in index.js). Each migration is recorded in the
// schema_migrations table, so applied ones are skipped. To add a migration,
// create runtime/edge/migrations/NNNN_name.sql, import it, and append an entry
// with the next id. Keep statements idempotent where practical.

import baselineSchema from "./schema.sql";
import authorProfiles from "./migrations/0002_author_profiles.sql";
import postData from "./migrations/0003_post_data.sql";
import commentStatus from "./migrations/0004_comment_status.sql";
import communityConstraints from "./migrations/0005_community_constraints.sql";
import siteSettings from "./migrations/0006_site_settings.sql";
import pollSlug from "./migrations/0007_poll_slug.sql";
import roleOwner from "./migrations/0008_role_owner.sql";

export const MIGRATIONS = [
  { id: 1, name: "baseline", sql: baselineSchema },
  { id: 2, name: "author_profiles", sql: authorProfiles },
  { id: 3, name: "post_data", sql: postData },
  { id: 4, name: "comment_status", sql: commentStatus },
  { id: 5, name: "community_constraints", sql: communityConstraints },
  { id: 6, name: "site_settings", sql: siteSettings },
  { id: 7, name: "poll_slug", sql: pollSlug },
  { id: 8, name: "role_owner", sql: roleOwner },
];

// splitStatements drops full-line comments and splits SQL into statements.
function splitStatements(sql) {
  return sql
    .split("\n")
    .filter((line) => !line.trim().startsWith("--"))
    .join("\n")
    .split(";")
    .map((s) => s.trim())
    .filter(Boolean);
}

function nowISO() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

// runMigrations applies any migrations not yet recorded in schema_migrations.
// Each migration's statements + its bookkeeping insert run as one D1 batch
// (a transaction), so a migration is all-or-nothing.
export async function runMigrations(db) {
  await db
    .prepare(
      "CREATE TABLE IF NOT EXISTS schema_migrations (id INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)"
    )
    .run();

  const { results } = await db.prepare("SELECT id FROM schema_migrations").all();
  const applied = new Set((results || []).map((r) => r.id));

  for (const migration of MIGRATIONS) {
    if (applied.has(migration.id)) continue;

    const stmts = splitStatements(migration.sql).map((s) => db.prepare(s));
    stmts.push(
      db
        .prepare("INSERT OR IGNORE INTO schema_migrations (id, name, applied_at) VALUES (?, ?, ?)")
        .bind(migration.id, migration.name, nowISO())
    );
    await db.batch(stmts);
  }
}
