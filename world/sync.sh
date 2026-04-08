#!/bin/bash
#
# Sync a local Friendo site to the edge (D1 + R2).
#
# Usage:
#   bash sync.sh [options] [site-dir]
#
# Options:
#   --remote          Sync to production Cloudflare (default: local miniflare)
#   --site-id NAME    Override the site_id (default: directory basename)
#   --schema-only     Only apply the schema, don't sync data
#
# Examples:
#   bash sync.sh ../../testsite                    # local dev
#   bash sync.sh --remote --site-id my-blog .      # production deploy
#
# Requires: wrangler CLI, sqlite3

set -euo pipefail

# --- Parse arguments ---

REMOTE=""
SITE_ID=""
SCHEMA_ONLY=""
SITE_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote)     REMOTE="--env production"; shift ;;
    --site-id)    SITE_ID="$2"; shift 2 ;;
    --schema-only) SCHEMA_ONLY=1; shift ;;
    -*)           echo "Unknown option: $1" >&2; exit 1 ;;
    *)            SITE_DIR="$1"; shift ;;
  esac
done

SITE_DIR="${SITE_DIR:-../testsite}"
SITE_DIR="$(cd "$SITE_DIR" && pwd)"

if [ ! -d "$SITE_DIR/pages" ]; then
  echo "Error: $SITE_DIR/pages not found."
  echo "Pass a Friendo site directory as an argument."
  exit 1
fi

# Default site_id to directory basename.
if [ -z "$SITE_ID" ]; then
  SITE_ID="$(basename "$SITE_DIR")"
fi

# Local flag for wrangler commands (empty string for remote).
LOCAL_FLAG=""
if [ -z "$REMOTE" ]; then
  LOCAL_FLAG="--local"
fi

SITE_NAME="$SITE_ID"
DB_NAME="friendo-world"

echo "Syncing site: $SITE_ID"
echo "Source:       $SITE_DIR"
echo "Target:       $([ -n "$LOCAL_FLAG" ] && echo 'local (miniflare)' || echo 'production')"
echo ""

# --- 1. Apply schema ---

echo "=== Applying schema ==="
wrangler d1 execute "$DB_NAME" $REMOTE $LOCAL_FLAG --file=schema.sql
echo ""

if [ -n "$SCHEMA_ONLY" ]; then
  echo "Schema applied. Exiting (--schema-only)."
  exit 0
fi

# --- 2. Register site ---

echo "=== Registering site ==="
wrangler d1 execute "$DB_NAME" $REMOTE $LOCAL_FLAG --command \
  "INSERT INTO sites (id, name, subdomain) VALUES ('$SITE_ID', '$SITE_NAME', '$SITE_ID') ON CONFLICT(id) DO UPDATE SET name = '$SITE_NAME', subdomain = '$SITE_ID'"
echo ""

# --- 3. Sync posts from local SQLite ---

echo "=== Syncing records ==="

DB_FILE="$SITE_DIR/data/friendo.db"

if [ -f "$DB_FILE" ]; then
  POST_COUNT=$(sqlite3 "$DB_FILE" "SELECT count(*) FROM posts" 2>/dev/null || echo "0")

  if [ "$POST_COUNT" -gt 0 ]; then
    echo "Found $POST_COUNT posts in local database."

    # Build a single SQL file with all upserts for efficiency.
    SYNC_SQL=$(mktemp)
    trap "rm -f $SYNC_SQL" EXIT

    sqlite3 "$DB_FILE" -separator '␟' \
      "SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated FROM posts" | \
    while IFS='␟' read -r id collection slug title body author_id status published_at created updated; do
      # Escape single quotes for SQL.
      title="${title//\'/\'\'}"
      body="${body//\'/\'\'}"
      slug="${slug//\'/\'\'}"
      cat >> "$SYNC_SQL" <<EOSQL
INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, created, updated)
  VALUES ('$id', '$SITE_ID', '$collection', '$slug', '$title', '$body', '$author_id', '$status', '$published_at', '$created', '$updated')
  ON CONFLICT(id) DO UPDATE SET
    collection=excluded.collection, slug=excluded.slug, title=excluded.title,
    body=excluded.body, author_id=excluded.author_id, status=excluded.status,
    published_at=excluded.published_at, updated=excluded.updated;
EOSQL
    done

    if [ -s "$SYNC_SQL" ]; then
      wrangler d1 execute "$DB_NAME" $REMOTE $LOCAL_FLAG --file="$SYNC_SQL"
      echo "Posts synced."
    fi
  else
    echo "No posts in local database."
  fi
else
  echo "No local database found at $DB_FILE."
fi

# Insert a sample post if the database was empty or missing.
if [ ! -f "$DB_FILE" ] || [ "$(sqlite3 "$DB_FILE" "SELECT count(*) FROM posts" 2>/dev/null || echo 0)" -eq 0 ]; then
  echo "Inserting sample post..."
  wrangler d1 execute "$DB_NAME" $REMOTE $LOCAL_FLAG --command \
    "INSERT INTO posts (id, site_id, collection, slug, title, body, status, created, updated) VALUES ('sample-1', '$SITE_ID', 'blog', 'hello-world', 'Hello World', 'This is a sample post deployed to friendo.world.', 'published', datetime('now'), datetime('now')) ON CONFLICT(id) DO NOTHING"
fi
echo ""

# --- 4. Upload templates and assets to R2 ---

echo "=== Uploading to R2 ==="

BUCKET="friendo-assets"
PREFIX="sites/$SITE_ID"
FILE_COUNT=0

upload_file() {
  local src="$1"
  local rel="$2"
  local key="$BUCKET/$PREFIX/$rel"
  wrangler r2 object put "$key" $REMOTE $LOCAL_FLAG --file "$src" --content-type "$(mime_type "$rel")" 2>/dev/null
  echo "  $rel"
  FILE_COUNT=$((FILE_COUNT + 1))
}

mime_type() {
  case "$1" in
    *.html) echo "text/html" ;;
    *.css)  echo "text/css" ;;
    *.js)   echo "application/javascript" ;;
    *.json) echo "application/json" ;;
    *.png)  echo "image/png" ;;
    *.jpg|*.jpeg) echo "image/jpeg" ;;
    *.svg)  echo "image/svg+xml" ;;
    *.gif)  echo "image/gif" ;;
    *.woff2) echo "font/woff2" ;;
    *.woff) echo "font/woff" ;;
    *.ico)  echo "image/x-icon" ;;
    *)      echo "application/octet-stream" ;;
  esac
}

# Pages
find "$SITE_DIR/pages" -type f | while read -r f; do
  rel="${f#$SITE_DIR/}"
  upload_file "$f" "$rel"
done

# Templates
if [ -d "$SITE_DIR/templates" ]; then
  find "$SITE_DIR/templates" -type f | while read -r f; do
    rel="${f#$SITE_DIR/}"
    upload_file "$f" "$rel"
  done
fi

# Public assets
if [ -d "$SITE_DIR/public" ]; then
  find "$SITE_DIR/public" -type f | while read -r f; do
    rel="${f#$SITE_DIR/}"
    upload_file "$f" "$rel"
  done
fi

echo ""
echo "=== Done ==="
echo ""

if [ -n "$LOCAL_FLAG" ]; then
  echo "Start the worker:  cd world && wrangler dev"
  echo "View the site:     https://$SITE_ID.local.friendo.world (with tunnel)"
  echo "                   http://localhost:8787/?site=$SITE_ID (without tunnel)"
else
  echo "Site is live at:   https://$SITE_ID.friendo.world"
fi
