package network

import (
	"strings"
	"testing"
	"time"
)

// signupWithCode runs a full passwordless sign-in for an email and returns the
// error the gate produced (nil on success).
func signupWithCode(t *testing.T, a *Accounts, email string) error {
	t.Helper()
	code, err := a.RequestOTP(email)
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	_, err = a.VerifyOTP(email, code)
	return err
}

// TestInviteGatesSignup: on an invite-only network a valid invite is the way in,
// and nothing else is.
func TestInviteGatesSignup(t *testing.T) {
	f := newQuotaFixture(t)
	a := f.accounts // defaults to invite-only

	// Uninvited — a correct code still isn't enough.
	if err := signupWithCode(t, a, "stranger@example.com"); err != ErrSignupsInviteOnly {
		t.Errorf("uninvited sign-in = %v, want ErrSignupsInviteOnly", err)
	}
	if _, exists := a.GetByEmail("stranger@example.com"); exists {
		t.Error("a refused sign-in created an account anyway")
	}

	// Invited — in, and the invite is consumed on the way.
	if _, err := a.CreateInvite("ada@example.com", "op@example.com", 0); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if err := signupWithCode(t, a, "ada@example.com"); err != nil {
		t.Fatalf("invited sign-in = %v, want success", err)
	}
	if _, still := a.GetInvite("ada@example.com"); still {
		t.Error("the invite should be consumed once it's been used")
	}
	// And they can sign in again afterwards — the account carries them now.
	if err := signupWithCode(t, a, "ada@example.com"); err != nil {
		t.Errorf("second sign-in = %v, want success", err)
	}
}

// TestInviteExpires: an invite that has lapsed is no better than none.
func TestInviteExpires(t *testing.T) {
	f := newQuotaFixture(t)
	a := f.accounts

	if _, err := a.CreateInvite("late@example.com", "", -time.Hour); err != nil {
		// A negative TTL falls back to the default, so write the expiry directly
		// to model an invite that has already lapsed.
		t.Fatalf("CreateInvite: %v", err)
	}
	a.conn.Exec(`UPDATE invites SET expires_at = ? WHERE email = ?`,
		time.Now().UTC().Add(-time.Hour).Format(rfc3339Z), "late@example.com")

	inv, ok := a.GetInvite("late@example.com")
	if !ok || !inv.Expired() || inv.Status() != "expired" {
		t.Fatalf("invite = %+v, want an expired one", inv)
	}
	if _, ok := a.ValidInvite("late@example.com"); ok {
		t.Error("an expired invite should not be valid")
	}
	if err := signupWithCode(t, a, "late@example.com"); err != ErrSignupsInviteOnly {
		t.Errorf("sign-in on an expired invite = %v, want ErrSignupsInviteOnly", err)
	}

	// Pruning tidies it away; a fresh invite works.
	n, err := a.PruneInvites()
	if err != nil || n != 1 {
		t.Errorf("PruneInvites = %d, %v; want 1, nil", n, err)
	}
	if _, err := a.CreateInvite("late@example.com", "", 0); err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	if err := signupWithCode(t, a, "late@example.com"); err != nil {
		t.Errorf("sign-in on a fresh invite = %v, want success", err)
	}
}

// TestInviteRevoke: an invite can be taken back before it's used.
func TestInviteRevoke(t *testing.T) {
	f := newQuotaFixture(t)
	a := f.accounts

	a.CreateInvite("nope@example.com", "", 0)
	if err := a.RevokeInvite("nope@example.com"); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if err := signupWithCode(t, a, "nope@example.com"); err != ErrSignupsInviteOnly {
		t.Errorf("sign-in on a revoked invite = %v, want ErrSignupsInviteOnly", err)
	}
	// Revoking one that isn't there says so rather than silently succeeding.
	if err := a.RevokeInvite("nobody@example.com"); err == nil {
		t.Error("revoking a nonexistent invite should report that")
	}
}

// TestInvitesListAndRefresh: the list is what an operator manages, and
// re-inviting extends rather than erroring.
func TestInvitesListAndRefresh(t *testing.T) {
	f := newQuotaFixture(t)
	a := f.accounts

	a.CreateInvite("one@example.com", "op@example.com", 24*time.Hour)
	a.CreateInvite("two@example.com", "op@example.com", 0)

	list, err := a.Invites()
	if err != nil {
		t.Fatalf("Invites: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d invites, want 2", len(list))
	}
	// Soonest to expire first.
	if list[0].Email != "one@example.com" {
		t.Errorf("invites out of order: %s first", list[0].Email)
	}
	if list[0].InvitedBy != "op@example.com" {
		t.Errorf("InvitedBy = %q", list[0].InvitedBy)
	}
	if !strings.Contains(list[1].Expires(), "day") {
		t.Errorf("Expires() = %q, want something in days", list[1].Expires())
	}

	// Re-inviting extends the same invite instead of creating a second one.
	before, _ := a.GetInvite("one@example.com")
	if _, err := a.CreateInvite("one@example.com", "op@example.com", 0); err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	after, _ := a.GetInvite("one@example.com")
	if !after.ExpiresAt.After(before.ExpiresAt) {
		t.Error("re-inviting should push the expiry out")
	}
	if list, _ := a.Invites(); len(list) != 2 {
		t.Errorf("re-inviting made a duplicate: %d invites", len(list))
	}
}

// TestOpenSignupsSkipInvites: with signups open, an invite isn't needed.
func TestOpenSignupsSkipInvites(t *testing.T) {
	f := newQuotaFixture(t)
	if err := f.accounts.SetSignups("open"); err != nil {
		t.Fatalf("SetSignups: %v", err)
	}
	if err := signupWithCode(t, f.accounts, "anyone@example.com"); err != nil {
		t.Errorf("open sign-up = %v, want success", err)
	}
}

// TestPreCreatedAccountStillSignsIn: accounts invited the old way (pre-created,
// no invite row) must keep working after the invite table landed.
func TestPreCreatedAccountStillSignsIn(t *testing.T) {
	f := newQuotaFixture(t)
	if _, err := f.accounts.EnsureAccount("legacy@example.com"); err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := signupWithCode(t, f.accounts, "legacy@example.com"); err != nil {
		t.Errorf("pre-created account sign-in = %v, want success", err)
	}
}
