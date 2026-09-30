package data

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

// UploadKeyTTL is how long after sending a drop-box post its images may follow.
const UploadKeyTTL = 30 * time.Minute

func hashUploadKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// CreateUploadKey makes the one-time key that lets whoever sent a drop-box post
// add images to it for a little while, and returns it. Only its hash is stored.
func (db *DB) CreateUploadKey(postID string) (string, error) {
	key := generateToken()
	expires := time.Now().UTC().Add(UploadKeyTTL).Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO upload_keys (post_id, site_id, key_hash, expires_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(post_id) DO UPDATE SET key_hash = excluded.key_hash, expires_at = excluded.expires_at`,
		postID, db.SiteID, hashUploadKey(key), expires,
	)
	if err != nil {
		return "", err
	}
	return key, nil
}

// UploadKeyValid reports whether key is the live upload key for a post.
func (db *DB) UploadKeyValid(postID, key string) bool {
	if key == "" {
		return false
	}
	var hash, expiresAt string
	err := db.Conn.QueryRow(
		`SELECT key_hash, expires_at FROM upload_keys WHERE site_id = ? AND post_id = ?`,
		db.SiteID, postID,
	).Scan(&hash, &expiresAt)
	if err != nil {
		return false
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", expiresAt); err != nil || time.Now().UTC().After(t) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(hashUploadKey(key))) == 1
}

// ClearExpiredUploadKeys deletes the keys whose time is up. Run on every Open.
func (db *DB) ClearExpiredUploadKeys() error {
	_, err := db.Conn.Exec(
		`DELETE FROM upload_keys WHERE site_id = ? AND expires_at < ?`,
		db.SiteID, time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	)
	return err
}

// FileByKey returns the post a stored upload belongs to ("", "" if the key isn't
// a tracked upload) — how /assets/uploads/* checks whether it may be shown yet.
func (db *DB) FileByKey(storageKey string) (recordType, recordID string) {
	db.Conn.QueryRow(
		`SELECT record_type, record_id FROM files WHERE site_id = ? AND r2_key = ? LIMIT 1`,
		db.SiteID, storageKey,
	).Scan(&recordType, &recordID)
	return recordType, recordID
}

// UnpublishedUploadKeys lists the stored keys (assets/uploads/…) of uploads on
// posts that aren't published — what a static export leaves out.
func (db *DB) UnpublishedUploadKeys() []string {
	rows, err := db.Conn.Query(
		`SELECT f.r2_key FROM files f JOIN posts p ON p.id = f.record_id
		 WHERE f.site_id = ? AND f.record_type = 'post' AND p.status != 'published'
		   AND f.r2_key LIKE 'assets/uploads/%'`,
		db.SiteID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out
}
