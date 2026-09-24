package network

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAccountSuspensionBlocksEverything: suspension is the abuse lever, so it has
// to hold every door — existing sessions, new sign-ins, and new claims.
func TestAccountSuspensionBlocksEverything(t *testing.T) {
	f := newQuotaFixture(t)
	alice, tok := f.signIn(t, "alice@example.com")

	// Working before.
	if rec := f.claim(tok, "alice-one"); rec.Code != http.StatusCreated {
		t.Fatalf("claim before suspension = %d", rec.Code)
	}

	if err := f.accounts.SuspendAccount(alice.ID, "spam"); err != nil {
		t.Fatalf("SuspendAccount: %v", err)
	}

	// An already-issued session stops working — suspension isn't only about
	// future logins, or an abuser just keeps going for 30 days.
	if _, ok := f.accounts.ValidateSession(tok); ok {
		t.Error("a suspended account's existing session still validates")
	}
	rec := f.claim(tok, "alice-two")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("claim while suspended = %d, want 403\n%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "suspended") || !strings.Contains(body, "spam") {
		t.Errorf("403 should explain the suspension and its reason, got:\n%s", body)
	}

	// And they can't sign back in, with the reason distinguished from a bad code.
	code, err := f.accounts.RequestOTP("alice@example.com")
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	if _, err := f.accounts.VerifyOTP("alice@example.com", code); err != ErrAccountSuspended {
		t.Errorf("VerifyOTP while suspended = %v, want ErrAccountSuspended", err)
	}

	// Resuming puts everything back (bar the sessions, which are separate).
	if err := f.accounts.ResumeAccount(alice.ID); err != nil {
		t.Fatalf("ResumeAccount: %v", err)
	}
	if _, ok := f.accounts.ValidateSession(tok); !ok {
		t.Error("session should work again after resuming")
	}
	if rec := f.claim(tok, "alice-two"); rec.Code != http.StatusCreated {
		t.Errorf("claim after resuming = %d, want 201", rec.Code)
	}
}

// TestOperatorCannotBeSuspended guards against an operator locking themselves —
// or the last operator — out of their own network.
func TestOperatorCannotBeSuspended(t *testing.T) {
	f := newQuotaFixture(t)
	if err := f.accounts.Grant("ops@example.com", "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	ops, _ := f.accounts.GetByEmail("ops@example.com")
	err := f.accounts.SuspendAccount(ops.ID, "oops")
	if err == nil {
		t.Fatal("suspending an operator should be refused")
	}
	if !strings.Contains(err.Error(), "operator revoke") {
		t.Errorf("the refusal should say how to proceed, got: %v", err)
	}
	// Demoting them first makes it possible.
	if err := f.accounts.Revoke("ops@example.com", "operator"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := f.accounts.SuspendAccount(ops.ID, "oops"); err != nil {
		t.Errorf("suspending a demoted operator = %v, want nil", err)
	}
}

// TestRevokeSessions is the lost-laptop answer: drop every session, but leave the
// account able to sign straight back in.
func TestRevokeSessions(t *testing.T) {
	f := newQuotaFixture(t)
	alice, laptop := f.signIn(t, "alice@example.com")
	phone, err := f.accounts.StartSession(alice.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	n, err := f.accounts.RevokeSessions(alice.ID)
	if err != nil {
		t.Fatalf("RevokeSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("revoked %d sessions, want 2", n)
	}
	for name, tok := range map[string]string{"laptop": laptop, "phone": phone} {
		if _, ok := f.accounts.ValidateSession(tok); ok {
			t.Errorf("%s session survived revocation", name)
		}
	}
	// Not suspended — a fresh sign-in works.
	fresh, err := f.accounts.StartSession(alice.ID)
	if err != nil {
		t.Fatalf("StartSession after revoke: %v", err)
	}
	if _, ok := f.accounts.ValidateSession(fresh); !ok {
		t.Error("a new session should work after revoking the old ones")
	}
}

// TestSuspendedSiteServesHoldPage — visitors get an explanation, not a 404, and
// the site's data is never touched.
func TestSuspendedSiteServesHoldPage(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Provision("zeta", "Zeta"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	d.SetSuspendedCheck(accounts.SiteSuspension)

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://zeta.localhost/", nil)
		rec := httptest.NewRecorder()
		d.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(); rec.Code != http.StatusOK {
		t.Fatalf("site before suspension = %d, want 200", rec.Code)
	}

	if err := accounts.SuspendSite("zeta", "unpaid invoice"); err != nil {
		t.Fatalf("SuspendSite: %v", err)
	}
	rec := get()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("suspended site = %d, want 403", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"on hold", "zeta.localhost", "unpaid invoice", "Nothing has been deleted"} {
		if !strings.Contains(body, want) {
			t.Errorf("hold page missing %q:\n%s", want, body)
		}
	}

	// Resuming takes effect on the next request, cached handler and all.
	if err := accounts.ResumeSite("zeta"); err != nil {
		t.Fatalf("ResumeSite: %v", err)
	}
	if rec := get(); rec.Code != http.StatusOK {
		t.Errorf("resumed site = %d, want 200", rec.Code)
	}
	// The site's files were never touched.
	if _, ok := reg.Dir("zeta"); !ok {
		t.Error("suspension removed the site directory")
	}
}

// TestHoldPageEscapesReason — the reason is operator-supplied text rendered into
// HTML; it must not be able to inject markup.
func TestHoldPageEscapesReason(t *testing.T) {
	root := t.TempDir()
	reg, _ := NewRegistry(root)
	reg.Provision("zeta", "Zeta")
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	d.SetSuspendedCheck(accounts.SiteSuspension)

	accounts.SuspendSite("zeta", `<script>alert(1)</script>`)
	req := httptest.NewRequest("GET", "http://zeta.localhost/", nil)
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("hold page did not escape the reason:\n%s", rec.Body.String())
	}
}
