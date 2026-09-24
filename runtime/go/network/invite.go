package network

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Invites are how someone gets onto an invite-only network. Minting one used to
// be the whole story — the invite *was* a pre-created account, so it never
// expired and there was no way to take it back. An invite is now its own record
// with an expiry, which makes the rest of the lifecycle possible: list what is
// outstanding, revoke one that shouldn't be, and let stale ones lapse on their own.
//
// The signup gate (see VerifyOTP) accepts an email that either already has an
// account or holds a valid invite. Accounts pre-created the old way still work.

// DefaultInviteTTL is how long an invite stays good. Long enough to sit in an
// inbox over a holiday, short enough that a forgotten one doesn't linger forever.
const DefaultInviteTTL = 14 * 24 * time.Hour

// Invite is an outstanding invitation to join the network.
type Invite struct {
	Email     string
	ExpiresAt time.Time
	Created   time.Time
	InvitedBy string // operator email, when known
}

// Expired reports whether the invite has lapsed.
func (i Invite) Expired() bool { return time.Now().UTC().After(i.ExpiresAt) }

// Status describes an invite for a human: "pending" or "expired".
func (i Invite) Status() string {
	if i.Expired() {
		return "expired"
	}
	return "pending"
}

// Expires renders how long an invite has left, for people rather than machines.
func (i Invite) Expires() string {
	if i.Expired() {
		return "expired"
	}
	// Rounded, not truncated — a fresh 14-day invite should read "in 14 day(s)",
	// not "in 13".
	d := time.Until(i.ExpiresAt)
	if d >= 24*time.Hour {
		return fmt.Sprintf("in %d day(s)", int(math.Round(d.Hours()/24)))
	}
	return fmt.Sprintf("in %d hour(s)", int(math.Ceil(d.Hours())))
}

// CreateInvite mints (or refreshes) an invite for an email. Re-inviting someone
// extends their invite rather than erroring — the operator's intent is the same
// either way. ttl <= 0 uses DefaultInviteTTL.
func (a *Accounts) CreateInvite(email, invitedBy string, ttl time.Duration) (Invite, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return Invite{}, fmt.Errorf("email is required")
	}
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	if _, err := a.conn.Exec(
		`INSERT INTO invites (email, expires_at, created, invited_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET expires_at = excluded.expires_at, invited_by = excluded.invited_by`,
		email, expires.Format(rfc3339Z), now.Format(rfc3339Z), strings.ToLower(strings.TrimSpace(invitedBy)),
	); err != nil {
		return Invite{}, err
	}
	return Invite{Email: email, ExpiresAt: expires, Created: now, InvitedBy: invitedBy}, nil
}

// ValidInvite returns an unexpired invite for an email, if there is one.
func (a *Accounts) ValidInvite(email string) (Invite, bool) {
	inv, ok := a.GetInvite(email)
	if !ok || inv.Expired() {
		return Invite{}, false
	}
	return inv, true
}

// GetInvite returns an invite whether or not it has expired.
func (a *Accounts) GetInvite(email string) (Invite, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	var expires, created, by string
	if err := a.conn.QueryRow(
		`SELECT expires_at, created, invited_by FROM invites WHERE email = ?`, email,
	).Scan(&expires, &created, &by); err != nil {
		return Invite{}, false
	}
	return invite(email, expires, created, by), true
}

// Invites lists every invite, soonest to expire first.
func (a *Accounts) Invites() ([]Invite, error) {
	rows, err := a.conn.Query(`SELECT email, expires_at, created, invited_by FROM invites ORDER BY expires_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		var email, expires, created, by string
		if rows.Scan(&email, &expires, &created, &by) == nil {
			out = append(out, invite(email, expires, created, by))
		}
	}
	return out, nil
}

// RevokeInvite withdraws an invite. It does not touch an account that has already
// been created from it — use suspension for that.
func (a *Accounts) RevokeInvite(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := a.conn.Exec(`DELETE FROM invites WHERE email = ?`, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no outstanding invite for %q", email)
	}
	return nil
}

// PruneInvites drops invites that have lapsed, returning how many went. Expired
// invites are already refused at sign-in; this just tidies the list.
func (a *Accounts) PruneInvites() (int, error) {
	res, err := a.conn.Exec(`DELETE FROM invites WHERE expires_at <= ?`, nowRFC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func invite(email, expires, created, by string) Invite {
	exp, _ := time.Parse(rfc3339Z, expires)
	cre, _ := time.Parse(rfc3339Z, created)
	return Invite{Email: email, ExpiresAt: exp, Created: cre, InvitedBy: by}
}
