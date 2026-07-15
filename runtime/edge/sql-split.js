// splitStatements drops full-line comments and splits a multi-statement SQL string
// (schema / migration) into individual statements. Kept in its own dependency-free
// module (no schema.sql import) so it can be unit-tested in plain Node without
// booting a Worker. Mirrors runtime/go/data/data.go SplitStatements — the two must
// split identically (enforced by tests/splitter-cases.json).
//
// It filters lines whose trimmed form starts with "--" *before* splitting on ";",
// so a comment-led file (schema.sql opens with a comment) doesn't fold its comment
// into — and thereby drop — the first real statement.
export function splitStatements(sql) {
  return sql
    .split("\n")
    .filter((line) => !line.trim().startsWith("--"))
    .join("\n")
    .split(";")
    .map((s) => s.trim())
    .filter(Boolean);
}
