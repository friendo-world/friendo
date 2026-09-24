package network

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestOperatorAPIAuth: the operator API is gated on a signed-in account with the
// operator capability, reachable by Bearer token (CLI) or cookie (browser), and
// the original /api/sites routes still work for older CLIs.
func TestOperatorAPIAuth(t *testing.T) {
	n := newTestNetwork(t)

	if code := n.api("GET", "/api/sites", "", "").Code; code != http.StatusUnauthorized {
		t.Fatalf("unauth /api/sites = %d, want 401", code)
	}
	member := sessionFor(t, n.accounts, "member@x.com")
	if code := n.api("GET", "/api/sites", member, "").Code; code != http.StatusForbidden {
		t.Fatalf("non-operator /api/sites = %d, want 403", code)
	}
	if code := n.api("GET", "/api/network", "cookie:"+member, "").Code; code != http.StatusForbidden {
		t.Fatalf("non-operator cookie /api/network = %d, want 403", code)
	}

	tok := operatorToken(t, n.accounts, "op@x.com")
	if code := n.api("GET", "/api/whoami", tok, "").Code; code != http.StatusOK {
		t.Errorf("whoami with operator token = %d, want 200", code)
	}
	if code := n.api("GET", "/api/network", "cookie:"+tok, "").Code; code != http.StatusOK {
		t.Errorf("operator cookie /api/network = %d, want 200", code)
	}

	// Create a site via the original route: provisioned AND owned by the operator,
	// with an owner user inside so its first-run setup is closed.
	rec := n.api("POST", "/api/sites", tok, `{"subdomain":"alpha","name":"Alpha"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d\n%s", rec.Code, rec.Body.String())
	}
	if _, ok := n.reg.Dir("alpha"); !ok {
		t.Fatal("alpha not provisioned via API")
	}
	op, _ := n.accounts.GetByEmail("op@x.com")
	if owner, ok := n.accounts.SiteOwner("alpha"); !ok || owner != op.ID {
		t.Fatalf("alpha owner = %q, want the operator", owner)
	}
	if body := n.req("GET", "alpha.localhost", "/_/api/setup", "", "").Body.String(); !strings.Contains(body, `"needsSetup":false`) {
		t.Fatalf("alpha's setup should be closed by the owner user: %s", body)
	}
	if body := n.api("GET", "/api/sites", tok, "").Body.String(); !strings.Contains(body, "alpha") {
		t.Errorf("list missing alpha: %s", body)
	}

	// A different owner can be named at creation.
	rec = n.api("POST", "/api/sites", tok, `{"subdomain":"beta","owner":"Bea@X.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create beta = %d\n%s", rec.Code, rec.Body.String())
	}
	bea, ok := n.accounts.GetByEmail("bea@x.com")
	if !ok {
		t.Fatal("naming an owner should create their account")
	}
	if owner, _ := n.accounts.SiteOwner("beta"); owner != bea.ID {
		t.Errorf("beta owner = %q, want bea", owner)
	}

	// Cache the site, then destroy via API → evicted, gone, and un-owned.
	if code := n.req("GET", "alpha.localhost", "/", "", "").Code; code != http.StatusOK {
		t.Fatalf("serve alpha = %d, want 200", code)
	}
	if code := n.api("DELETE", "/api/sites/alpha", tok, "").Code; code != http.StatusNoContent {
		t.Fatalf("destroy = %d, want 204", code)
	}
	if _, ok := n.reg.Dir("alpha"); ok {
		t.Error("alpha not removed via API")
	}
	if _, ok := n.accounts.SiteOwner("alpha"); ok {
		t.Error("alpha's ownership row survived destroy")
	}
	if code := n.req("GET", "alpha.localhost", "/", "", "").Code; code != http.StatusNotFound {
		t.Errorf("alpha after API destroy = %d, want 404", code)
	}
}

// TestOperatorAPISettingsAndHomeSite covers the network knobs: signups, the
// default site cap, and the home site (which must name a real site).
func TestOperatorAPISettingsAndHomeSite(t *testing.T) {
	n := newTestNetwork(t)
	tok := operatorToken(t, n.accounts, "op@x.com")

	var sum map[string]any
	json.Unmarshal(n.api("GET", "/api/network", tok, "").Body.Bytes(), &sum)
	if sum["signups"] != "invite" || sum["home_site"] != "" {
		t.Fatalf("defaults = %v", sum)
	}
	if rec := n.api("PUT", "/api/network/settings", tok, `{"signups":"open","default_quota":"5"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings = %d\n%s", rec.Code, rec.Body.String())
	}
	if n.accounts.Signups() != "open" || n.accounts.DefaultSiteQuota() != 5 {
		t.Fatalf("settings not applied: %s %d", n.accounts.Signups(), n.accounts.DefaultSiteQuota())
	}
	if rec := n.api("PUT", "/api/network/settings", tok, `{"signups":"whatever"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad signups = %d, want 400", rec.Code)
	}
	if rec := n.api("PUT", "/api/network/settings", tok, `{"home_site":"nope"}`); rec.Code != http.StatusNotFound {
		t.Errorf("home_site for a missing site = %d, want 404", rec.Code)
	}
	n.api("POST", "/api/sites", tok, `{"subdomain":"www"}`)
	if rec := n.api("PUT", "/api/network/settings", tok, `{"home_site":"www"}`); rec.Code != http.StatusOK {
		t.Fatalf("set home_site = %d\n%s", rec.Code, rec.Body.String())
	}
	if n.accounts.HomeSite() != "www" {
		t.Fatalf("home site = %q", n.accounts.HomeSite())
	}
	if rec := n.api("PUT", "/api/network/settings", tok, `{"home_site":""}`); rec.Code != http.StatusOK || n.accounts.HomeSite() != "" {
		t.Errorf("clearing home_site failed: %d %q", rec.Code, n.accounts.HomeSite())
	}
	// Destroying the home site clears the setting rather than leaving a dangling name.
	n.api("PUT", "/api/network/settings", tok, `{"home_site":"www"}`)
	n.api("DELETE", "/api/sites/www", tok, "")
	if n.accounts.HomeSite() != "" {
		t.Error("destroying the home site should clear home_site")
	}
}

// TestOperatorAPIPeopleAndInvites: suspend/resume a site and a person, sign
// someone out everywhere, per-account limits, invites with expiry/revoke/prune,
// and operator grant/revoke (never yourself).
func TestOperatorAPIPeopleAndInvites(t *testing.T) {
	n := newTestNetwork(t)
	tok := operatorToken(t, n.accounts, "op@x.com")
	n.api("POST", "/api/sites", tok, `{"subdomain":"zeta","owner":"alice@x.com"}`)
	alice, _ := n.accounts.GetByEmail("alice@x.com")
	aliceTok, _ := n.accounts.StartSession(alice.ID)

	// Site on hold, with a reason, then back.
	if rec := n.api("POST", "/api/network/sites/zeta/suspend", tok, `{"reason":"spam"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("suspend site = %d\n%s", rec.Code, rec.Body.String())
	}
	if s, ok := n.accounts.SiteSuspension("zeta"); !ok || s.Reason != "spam" {
		t.Fatalf("SiteSuspension = %+v, %v", s, ok)
	}
	if !strings.Contains(n.api("GET", "/api/network/sites", tok, "").Body.String(), `"suspended":true`) {
		t.Error("sites list should show zeta suspended")
	}
	if rec := n.api("POST", "/api/network/sites/zeta/resume", tok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("resume site = %d", rec.Code)
	}

	// Suspend a person: their existing session stops working at once.
	if rec := n.api("POST", "/api/network/accounts/"+alice.ID+"/suspend", tok, `{"reason":"abuse"}`); rec.Code != http.StatusOK {
		t.Fatalf("suspend account = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := n.api("GET", "/api/account", aliceTok, ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("suspended alice whoami = %d, want 403 saying so: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(n.api("GET", "/api/network/accounts", tok, "").Body.String(), `"reason":"abuse"`) {
		t.Error("accounts list should show alice's suspension")
	}
	n.api("POST", "/api/network/accounts/"+alice.ID+"/resume", tok, "")
	aliceTok, _ = n.accounts.StartSession(alice.ID)
	if rec := n.api("POST", "/api/network/accounts/"+alice.ID+"/signout", tok, ""); rec.Code != http.StatusOK {
		t.Fatalf("signout = %d", rec.Code)
	}
	if code := n.api("GET", "/api/account", aliceTok, "").Code; code != http.StatusUnauthorized {
		t.Errorf("alice after signout = %d, want 401", code)
	}

	// Per-account limit, then back to the default.
	if rec := n.api("PUT", "/api/network/accounts/"+alice.ID+"/quota", tok, `{"sites":"20"}`); rec.Code != http.StatusOK {
		t.Fatalf("quota = %d\n%s", rec.Code, rec.Body.String())
	}
	if q, ok := n.accounts.AccountSiteQuota(alice.ID); !ok || q != 20 {
		t.Errorf("alice quota = %d, %v", q, ok)
	}
	n.api("PUT", "/api/network/accounts/"+alice.ID+"/quota", tok, `{"sites":"default"}`)
	if _, ok := n.accounts.AccountSiteQuota(alice.ID); ok {
		t.Error("quota override survived reset")
	}
	if rec := n.api("PUT", "/api/network/accounts/"+alice.ID+"/quota", tok, `{"sites":"heaps"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad quota = %d, want 400", rec.Code)
	}

	// Invites: mint with a custom expiry, list, revoke, prune.
	if rec := n.api("POST", "/api/network/invites", tok, `{"email":"Ada@X.com","days":3}`); rec.Code != http.StatusCreated {
		t.Fatalf("invite = %d\n%s", rec.Code, rec.Body.String())
	}
	inv, ok := n.accounts.GetInvite("ada@x.com")
	if !ok || inv.InvitedBy != "op@x.com" {
		t.Fatalf("invite = %+v, %v", inv, ok)
	}
	if _, exists := n.accounts.GetByEmail("ada@x.com"); exists {
		t.Error("inviting should not create the account until they sign in")
	}
	if !strings.Contains(n.api("GET", "/api/network/invites", tok, "").Body.String(), "ada@x.com") {
		t.Error("invites list missing ada")
	}
	if rec := n.api("DELETE", "/api/network/invites/ada@x.com", tok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke invite = %d", rec.Code)
	}
	if _, ok := n.accounts.GetInvite("ada@x.com"); ok {
		t.Error("invite survived revocation")
	}
	if rec := n.api("POST", "/api/network/invites/prune", tok, ""); rec.Code != http.StatusOK {
		t.Errorf("prune = %d", rec.Code)
	}
	// An "operator" invite grants outright.
	n.api("POST", "/api/network/invites", tok, `{"email":"boss@x.com","operator":true}`)
	if acct, ok := n.accounts.GetByEmail("boss@x.com"); !ok || !acct.Has("operator") {
		t.Error("operator invite should create an operator account")
	}

	// Operators: grant, revoke, but never yourself.
	n.api("POST", "/api/network/operators", tok, `{"email":"alice@x.com"}`)
	if a, _ := n.accounts.GetByEmail("alice@x.com"); !a.Has("operator") {
		t.Error("grant operator failed")
	}
	if rec := n.api("DELETE", "/api/network/operators/alice@x.com", tok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke operator = %d", rec.Code)
	}
	if a, _ := n.accounts.GetByEmail("alice@x.com"); a.Has("operator") {
		t.Error("revoke operator failed")
	}
	if rec := n.api("DELETE", "/api/network/operators/op@x.com", tok, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("self-revoke = %d, want 400", rec.Code)
	}
}

// TestOperatorAPIDomains: an operator can connect, verify and disconnect a
// domain on any site, sharing the tenant path's provider seam.
func TestOperatorAPIDomains(t *testing.T) {
	n := newTestNetwork(t)
	tok := operatorToken(t, n.accounts, "op@x.com")
	n.api("POST", "/api/sites", tok, `{"subdomain":"zeta"}`)
	n.aa.SetDomainProvider(&stubProvider{ready: false})

	rec := n.api("POST", "/api/network/domains", tok, `{"domain":"Example.com","site":"zeta"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"instructions"`) {
		t.Fatalf("add domain = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := n.api("POST", "/api/network/domains/verify", tok, `{"domain":"example.com"}`); rec.Code != http.StatusConflict {
		t.Fatalf("verify before DNS = %d, want 409", rec.Code)
	}
	n.aa.domains.(*stubProvider).ready = true
	if rec := n.api("POST", "/api/network/domains/verify", tok, `{"domain":"example.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("verify = %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(n.api("GET", "/api/network/domains", tok, "").Body.String(), `"verified":true`) {
		t.Error("domains list should show example.com verified")
	}
	if rec := n.api("DELETE", "/api/network/domains", tok, `{"domain":"example.com"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("remove = %d", rec.Code)
	}
	if _, ok := n.accounts.GetDomain("example.com"); ok {
		t.Error("domain survived removal")
	}
	if rec := n.api("POST", "/api/network/domains", tok, `{"domain":"x.localhost","site":"zeta"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a name under the base domain = %d, want 400", rec.Code)
	}
}

// stubProvider is a DomainProvider whose readiness the test flips.
type stubProvider struct{ ready bool }

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Attach(d Domain) (Instructions, error) {
	return Instructions{Records: []DNSRecord{{Type: "CNAME", Name: d.Domain, Value: "stub.example"}}}, nil
}
func (s *stubProvider) Check(Domain) (bool, string, error) {
	if s.ready {
		return true, "", nil
	}
	return false, "DNS not there yet", nil
}
func (s *stubProvider) Detach(Domain) error { return nil }
