package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// quotaFixture wires the self-service surface over fresh stores, the same way
// `friendo deploy` reaches it.
type quotaFixture struct {
	accounts *Accounts
	mux      *http.ServeMux
}

func newQuotaFixture(t *testing.T) *quotaFixture {
	t.Helper()
	accounts, err := OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	t.Cleanup(func() { accounts.Close() })
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	m := http.NewServeMux()
	NewAccountAuth(accounts, reg, "localhost").register(m)
	return &quotaFixture{accounts: accounts, mux: m}
}

// signIn creates an account and returns it with a session token.
func (f *quotaFixture) signIn(t *testing.T, email string) (*Account, string) {
	t.Helper()
	id, err := f.accounts.EnsureAccount(email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	tok, err := f.accounts.StartSession(id)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	acct, _ := f.accounts.Get(id)
	return acct, tok
}

func (f *quotaFixture) claim(token, sub string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/account/sites", strings.NewReader(`{"subdomain":"`+sub+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func (f *quotaFixture) listSites(token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/account/sites", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// TestSiteQuotaEnforced is the point of the whole tier: an account can't claim
// subdomains without end, and the refusal explains itself.
func TestSiteQuotaEnforced(t *testing.T) {
	f := newQuotaFixture(t)
	if err := f.accounts.SetDefaultSiteQuota(2); err != nil {
		t.Fatalf("SetDefaultSiteQuota: %v", err)
	}
	_, tok := f.signIn(t, "alice@example.com")

	for _, sub := range []string{"alice-one", "alice-two"} {
		if rec := f.claim(tok, sub); rec.Code != http.StatusCreated {
			t.Fatalf("claim %s = %d, want 201\n%s", sub, rec.Code, rec.Body.String())
		}
	}

	rec := f.claim(tok, "alice-three")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("claim past the cap = %d, want 403\n%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error   string `json:"error"`
		Used    int    `json:"used"`
		Allowed int    `json:"allowed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding 403 body: %v", err)
	}
	if body.Used != 2 || body.Allowed != 2 {
		t.Errorf("used/allowed = %d/%d, want 2/2", body.Used, body.Allowed)
	}
	// The message is the only thing most people will see — it has to name the
	// limit and a way forward, not just fail.
	for _, want := range []string{"2", "Delete", "operator"} {
		if !strings.Contains(body.Error, want) {
			t.Errorf("403 message %q does not mention %q", body.Error, want)
		}
	}
	// And nothing was created on the way out.
	if n, _ := f.accounts.CountSitesOwnedBy(mustAccountID(t, f, "alice@example.com")); n != 2 {
		t.Errorf("owns %d sites after a refused claim, want 2", n)
	}
}

// TestSiteQuotaAllowsReclaimingOwnSite guards the idempotent path: being at the
// cap must not lock you out of a site you already own (which `friendo deploy`
// re-claims on every push).
func TestSiteQuotaAllowsReclaimingOwnSite(t *testing.T) {
	f := newQuotaFixture(t)
	f.accounts.SetDefaultSiteQuota(1)
	_, tok := f.signIn(t, "alice@example.com")

	if rec := f.claim(tok, "alice-one"); rec.Code != http.StatusCreated {
		t.Fatalf("first claim = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := f.claim(tok, "alice-one"); rec.Code != http.StatusOK {
		t.Fatalf("re-claiming an owned site at the cap = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
}

// TestSiteQuotaOverrideAndOperatorExempt covers the two ways past the default:
// a per-account override, and holding operator.
func TestSiteQuotaOverrideAndOperatorExempt(t *testing.T) {
	f := newQuotaFixture(t)
	f.accounts.SetDefaultSiteQuota(1)

	alice, aliceTok := f.signIn(t, "alice@example.com")
	if rec := f.claim(aliceTok, "alice-one"); rec.Code != http.StatusCreated {
		t.Fatalf("claim = %d", rec.Code)
	}
	if rec := f.claim(aliceTok, "alice-two"); rec.Code != http.StatusForbidden {
		t.Fatalf("second claim = %d, want 403", rec.Code)
	}
	// Lifting one account's cap doesn't touch the default.
	if err := f.accounts.SetAccountSiteQuota(alice.ID, 3); err != nil {
		t.Fatalf("SetAccountSiteQuota: %v", err)
	}
	if rec := f.claim(aliceTok, "alice-two"); rec.Code != http.StatusCreated {
		t.Fatalf("claim after override = %d, want 201\n%s", rec.Code, rec.Body.String())
	}
	if f.accounts.DefaultSiteQuota() != 1 {
		t.Error("an override changed the network default")
	}
	// Clearing it puts them back on the default, which they're now past.
	if err := f.accounts.ClearAccountSiteQuota(alice.ID); err != nil {
		t.Fatalf("ClearAccountSiteQuota: %v", err)
	}
	if rec := f.claim(aliceTok, "alice-three"); rec.Code != http.StatusForbidden {
		t.Errorf("claim after clearing the override = %d, want 403", rec.Code)
	}

	// Operators run the network — they're never capped.
	if err := f.accounts.Grant("ops@example.com", "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	_, opsTok := f.signIn(t, "ops@example.com")
	for _, sub := range []string{"ops-one", "ops-two", "ops-three"} {
		if rec := f.claim(opsTok, sub); rec.Code != http.StatusCreated {
			t.Fatalf("operator claim %s = %d, want 201", sub, rec.Code)
		}
	}
}

// TestSiteQuotaUnlimited: a cap of 0 means no cap.
func TestSiteQuotaUnlimited(t *testing.T) {
	f := newQuotaFixture(t)
	if err := f.accounts.SetDefaultSiteQuota(QuotaUnlimited); err != nil {
		t.Fatalf("SetDefaultSiteQuota: %v", err)
	}
	_, tok := f.signIn(t, "alice@example.com")
	for _, sub := range []string{"one", "two", "three", "four"} {
		if rec := f.claim(tok, sub); rec.Code != http.StatusCreated {
			t.Fatalf("claim %s under an unlimited cap = %d", sub, rec.Code)
		}
	}
}

// TestListSitesReportsQuota — the console and CLI show used/allowed before
// anyone walks into the wall.
func TestListSitesReportsQuota(t *testing.T) {
	f := newQuotaFixture(t)
	f.accounts.SetDefaultSiteQuota(2)
	_, tok := f.signIn(t, "alice@example.com")
	f.claim(tok, "alice-one")

	rec := f.listSites(tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sites = %d", rec.Code)
	}
	var body struct {
		Sites []struct {
			Subdomain string `json:"subdomain"`
		} `json:"sites"`
		Quota struct {
			Used      int  `json:"used"`
			Allowed   int  `json:"allowed"`
			Unlimited bool `json:"unlimited"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Sites) != 1 {
		t.Errorf("sites = %v, want 1", body.Sites)
	}
	if body.Quota.Used != 1 || body.Quota.Allowed != 2 || body.Quota.Unlimited {
		t.Errorf("quota = %+v, want used 1 / allowed 2 / not unlimited", body.Quota)
	}
}

// TestDefaultSiteQuotaFallback: an unset (or corrupt) setting falls back to the
// built-in default rather than to "unlimited", which would be the unsafe read.
func TestDefaultSiteQuotaFallback(t *testing.T) {
	f := newQuotaFixture(t)
	if got := f.accounts.DefaultSiteQuota(); got != defaultSitesPerAccount {
		t.Errorf("unset default = %d, want %d", got, defaultSitesPerAccount)
	}
	f.accounts.conn.Exec(
		`INSERT INTO network_settings (key, value) VALUES (?, 'lots')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, quotaSitesKey)
	if got := f.accounts.DefaultSiteQuota(); got != defaultSitesPerAccount {
		t.Errorf("unreadable default = %d, want the built-in %d", got, defaultSitesPerAccount)
	}
}

func TestParseQuota(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"5", 5, false},
		{" 12 ", 12, false},
		{"0", QuotaUnlimited, false},
		{"unlimited", QuotaUnlimited, false},
		{"UNLIMITED", QuotaUnlimited, false},
		{"none", QuotaUnlimited, false},
		{"-1", 0, true},
		{"lots", 0, true},
		{"", 0, true},
	} {
		got, err := ParseQuota(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseQuota(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("ParseQuota(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if got := FormatQuota(QuotaUnlimited); got != "unlimited" {
		t.Errorf("FormatQuota(0) = %q, want \"unlimited\"", got)
	}
	if got := FormatQuota(7); got != "7" {
		t.Errorf("FormatQuota(7) = %q, want \"7\"", got)
	}
}

func mustAccountID(t *testing.T, f *quotaFixture, email string) string {
	t.Helper()
	acct, ok := f.accounts.GetByEmail(email)
	if !ok {
		t.Fatalf("no account for %q", email)
	}
	return acct.ID
}
