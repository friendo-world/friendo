package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Profiles.
//
// An author row is a profile: the public face of an account (one account may have
// several). v0.6 gives every profile an address — /profiles/<slug> — plus a bio and
// a `data` blob for whatever fields a site declares under [profiles] fields in
// friendo.toml. Templates see a profile as record.author on every post and as
// `record` on pages/profiles/[slug].html; the API serves it under /_/api/profiles.
// A profile never carries the account's email.

// SettingProfileVisibility is the site setting that says who may see profile
// pages and the profiles API: "members" (anyone signed in; the default) or
// "public". friendo.toml spells it `[settings] profile_visibility`.
const SettingProfileVisibility = "profile_visibility"

// ErrSlugTaken is returned when a profile asks for a slug another profile holds.
var ErrSlugTaken = errors.New("that address is already taken")

// ErrLastProfile is returned when an account tries to delete its only profile.
var ErrLastProfile = errors.New("an account needs at least one profile")

// ProfileVisibility returns "public" or "members" (anything else reads as members).
func (db *DB) ProfileVisibility() string {
	if db.GetSetting(SettingProfileVisibility, "members") == "public" {
		return "public"
	}
	return "members"
}

// ProfilesVisibleTo reports whether a viewer may see profiles: everyone when the
// site made them public, otherwise only someone signed in.
func (db *DB) ProfilesVisibleTo(signedIn bool) bool {
	return signedIn || db.ProfileVisibility() == "public"
}

// Slugify turns a display name into an address: lowercase letters and digits,
// runs of anything else collapsed to one dash, trimmed, at most 64 characters.
// "Pat O'Brien" → "pat-o-brien". Empty in → empty out (callers pick a fallback).
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	return out
}

// uniqueSlug returns base, or base-2, base-3, … — the first address no other
// profile on the site holds. excludeID is the profile being (re)named, so keeping
// its own slug is never a collision.
func (db *DB) uniqueSlug(base, excludeID string) string {
	if base == "" {
		base = "member"
	}
	candidate := base
	for i := 2; ; i++ {
		var id string
		err := db.Conn.QueryRow(
			`SELECT id FROM authors WHERE site_id = ? AND slug = ? AND id != ?`,
			db.SiteID, candidate, excludeID,
		).Scan(&id)
		if err == sql.ErrNoRows {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// EnsureAuthorSlugs gives every profile without an address one, derived from its
// name — except a visitor's, which has no profile page. Run on every Open (cheap: a no-op once the backfill has happened) so a
// database upgraded from before 0016 has profile pages the moment the new
// binary starts.
func (db *DB) EnsureAuthorSlugs() error {
	rows, err := db.Conn.Query(
		`SELECT id, name FROM authors WHERE site_id = ? AND slug = '' AND role != ? ORDER BY created`, db.SiteID, RoleVisitor,
	)
	if err != nil {
		return err
	}
	type pending struct{ id, name string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		slug := db.uniqueSlug(Slugify(p.name), p.id)
		if _, err := db.Conn.Exec(`UPDATE authors SET slug = ? WHERE id = ? AND site_id = ?`, slug, p.id, db.SiteID); err != nil {
			return err
		}
	}
	return nil
}

// profileCols is the column order profileMap and scanProfiles agree on.
const profileCols = `id, name, avatar, role, slug, bio, data, created`

// profileMap is a profile as templates and the API see it. Never the email.
func profileMap(id, name, avatar, role, slug, bio, dataJSON, created string) map[string]any {
	return map[string]any{
		"id":      id,
		"name":    name,
		"avatar":  avatar,
		"role":    role,
		"slug":    slug,
		"bio":     bio,
		"fields":  decodeData(dataJSON),
		"created": created,
		"url":     "/profiles/" + slug,
	}
}

func scanProfiles(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, avatar, role, slug, bio, dataJSON, created string
		if err := rows.Scan(&id, &name, &avatar, &role, &slug, &bio, &dataJSON, &created); err != nil {
			return nil, err
		}
		out = append(out, profileMap(id, name, avatar, role, slug, bio, dataJSON, created))
	}
	return out, rows.Err()
}

// ProfileBySlug returns one profile by its address, or sql.ErrNoRows.
func (db *DB) ProfileBySlug(slug string) (map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+profileCols+` FROM authors WHERE site_id = ? AND slug = ? AND role != ? LIMIT 1`, db.SiteID, slug, RoleVisitor)
	if err != nil {
		return nil, err
	}
	list, err := scanProfiles(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// ProfileByID returns one profile by id, or sql.ErrNoRows.
func (db *DB) ProfileByID(id string) (map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+profileCols+` FROM authors WHERE site_id = ? AND id = ? AND role != ? LIMIT 1`, db.SiteID, id, RoleVisitor)
	if err != nil {
		return nil, err
	}
	list, err := scanProfiles(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// ListProfiles returns every member profile on the site (visitors have none), by name — collections.profiles
// in templates, GET /_/api/profiles, and the static export's profile pages.
func (db *DB) ListProfiles() ([]map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+profileCols+` FROM authors WHERE site_id = ? AND role != ? ORDER BY name COLLATE NOCASE, created`, db.SiteID, RoleVisitor)
	if err != nil {
		return nil, err
	}
	return scanProfiles(rows)
}

// AttachAuthors sets record["author"] on every record — the post's profile as a
// profile map, or nil for an orphaned author_id — with one query, the way
// AttachWhen does, so {{ record.author.name }} costs nothing per record.
func (db *DB) AttachAuthors(records []map[string]any) {
	if len(records) == 0 {
		return
	}
	rows, err := db.Conn.Query(`SELECT `+profileCols+` FROM authors WHERE site_id = ?`, db.SiteID)
	if err != nil {
		return
	}
	list, err := scanProfiles(rows)
	if err != nil {
		return
	}
	byID := make(map[string]map[string]any, len(list))
	for _, p := range list {
		id, _ := p["id"].(string)
		byID[id] = p
	}
	for _, r := range records {
		authorID, _ := r["author_id"].(string)
		if p := byID[authorID]; p != nil {
			r["author"] = p
		} else {
			r["author"] = nil
		}
	}
}

// ProfileUpdate is what a member may change about one of their profiles. nil
// means "leave it"; Data replaces the declared fields when present.
type ProfileUpdate struct {
	Name   *string
	Avatar *string
	Bio    *string
	Slug   *string
	Data   map[string]any
}

// UpdateProfile edits one of an account's profiles. It returns sql.ErrNoRows when
// the profile isn't the account's, ErrSlugTaken when the requested address belongs
// to someone else, and the updated profile otherwise. An empty requested slug
// re-derives one from the (new) name.
func (db *DB) UpdateProfile(userID, authorID string, in ProfileUpdate) (map[string]any, error) {
	if !db.authorBelongsTo(authorID, userID) {
		return nil, sql.ErrNoRows
	}
	current, err := db.ProfileByID(authorID)
	if err != nil {
		return nil, err
	}
	name, _ := current["name"].(string)
	avatar, _ := current["avatar"].(string)
	bio, _ := current["bio"].(string)
	slug, _ := current["slug"].(string)
	dataJSON := "{}"
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Avatar != nil {
		avatar = strings.TrimSpace(*in.Avatar)
	}
	if in.Bio != nil {
		bio = strings.TrimSpace(*in.Bio)
	}
	if in.Slug != nil {
		want := Slugify(*in.Slug)
		if want == "" {
			want = Slugify(name)
		}
		if want != slug {
			if db.uniqueSlug(want, authorID) != want {
				return nil, ErrSlugTaken
			}
			slug = want
		}
	}
	if slug == "" {
		slug = db.uniqueSlug(Slugify(name), authorID)
	}
	if in.Data != nil {
		b, err := json.Marshal(in.Data)
		if err != nil {
			return nil, err
		}
		dataJSON = string(b)
	} else if cur, ok := current["fields"].(map[string]any); ok {
		b, _ := json.Marshal(cur)
		dataJSON = string(b)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.Conn.Exec(
		`UPDATE authors SET name = ?, avatar = ?, bio = ?, slug = ?, data = ?, updated = ? WHERE id = ? AND site_id = ?`,
		name, avatar, bio, slug, dataJSON, now, authorID, db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	return db.ProfileByID(authorID)
}

// DeleteProfile removes one of an account's profiles. It refuses the last one
// (ErrLastProfile) and answers sql.ErrNoRows for a profile that isn't the
// account's. Content the profile wrote keeps its author_id; the joins that show
// an author's name already tolerate a missing row.
func (db *DB) DeleteProfile(userID, authorID string) error {
	if !db.authorBelongsTo(authorID, userID) {
		return sql.ErrNoRows
	}
	var n int
	db.Conn.QueryRow(`SELECT COUNT(*) FROM authors WHERE site_id = ? AND user_id = ?`, db.SiteID, userID).Scan(&n)
	if n <= 1 {
		return ErrLastProfile
	}
	if _, err := db.Conn.Exec(`DELETE FROM authors WHERE id = ? AND site_id = ?`, authorID, db.SiteID); err != nil {
		return err
	}
	db.DeleteFollowsForAuthor(authorID)
	db.DeleteNotificationsForAuthor(authorID)
	db.DeleteMembershipsForAuthor(authorID)
	// If it was the chosen default, fall back to the earliest remaining profile.
	_, err := db.Conn.Exec(
		`UPDATE users SET default_author_id = '' WHERE id = ? AND site_id = ? AND default_author_id = ?`,
		userID, db.SiteID, authorID,
	)
	return err
}

// PostsByAuthor returns a profile's published posts across every collection,
// newest first, each carrying its `collection` — {{ record.posts }} on a profile page.
func (db *DB) PostsByAuthor(authorID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated, data
		 FROM posts WHERE site_id = ? AND author_id = ? AND status = 'published' ORDER BY created DESC`,
		db.SiteID, authorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, collection, slug, title, body, aid, status, publishedAt, created, updated, dataJSON string
		if err := rows.Scan(&id, &collection, &slug, &title, &body, &aid, &status, &publishedAt, &created, &updated, &dataJSON); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "collection": collection, "slug": slug, "title": title, "body": body,
			"author_id": aid, "status": status, "published_at": publishedAt,
			"created": created, "updated": updated, "fields": decodeData(dataJSON),
		})
	}
	return out, rows.Err()
}

// CommentsByAuthor returns a profile's approved comments on published posts,
// newest first, each with the post's `post_title`, `post_slug` and
// `post_collection` — {{ record.comments }} on a profile page.
func (db *DB) CommentsByAuthor(authorID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT c.id, c.post_id, c.body, c.created, p.title, p.slug, p.collection
		 FROM comments c JOIN posts p ON p.id = c.post_id
		 WHERE c.site_id = ? AND c.author_id = ? AND c.status = 'approved' AND p.status = 'published'
		 ORDER BY c.created DESC`,
		db.SiteID, authorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, postID, body, created, title, slug, collection string
		if err := rows.Scan(&id, &postID, &body, &created, &title, &slug, &collection); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "post_id": postID, "body": body, "created": created,
			"post_title": title, "post_slug": slug, "post_collection": collection,
		})
	}
	return out, rows.Err()
}
