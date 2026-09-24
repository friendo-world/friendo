package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{in: "Example.COM", want: "example.com"},
		{in: "  example.com  ", want: "example.com"},
		{in: "https://example.com/", want: "example.com"},
		{in: "example.com.", want: "example.com"},
		{in: "example.com:8080", want: "example.com"},
		{in: "www.example.co.uk", want: "www.example.co.uk"},
		{in: "", wantErr: true},
		{in: "localhost", wantErr: true},   // no dot — not a real domain
		{in: "192.168.1.1", wantErr: true}, // an IP, not a name
		{in: "-bad.com", wantErr: true},
		{in: "bad-.com", wantErr: true},
		{in: "exa mple.com", wantErr: true},
	} {
		got, err := NormalizeDomain(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("NormalizeDomain(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestUnverifiedDomainNeverRoutes is the security property of the whole tier: a
// claimed-but-unproven domain must not take traffic.
func TestUnverifiedDomainNeverRoutes(t *testing.T) {
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
	d.SetDomainLookup(accounts.SiteForDomain)

	get := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://"+host+"/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		d.ServeHTTP(rec, req)
		return rec
	}

	if _, err := accounts.AddDomain("zeta-example.com", "zeta"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	// Unverified: falls through to the apex listing, never to the tenant.
	body := get("zeta-example.com").Body.String()
	if strings.Contains(body, "Zeta") && !strings.Contains(body, "friendo network") {
		t.Errorf("an unverified domain served the tenant:\n%s", body)
	}

	// Verified: routes like a subdomain.
	if err := accounts.MarkDomainVerified("zeta-example.com"); err != nil {
		t.Fatalf("MarkDomainVerified: %v", err)
	}
	d.ForgetDomain("zeta-example.com")
	if rec := get("zeta-example.com"); rec.Code != http.StatusOK {
		t.Fatalf("verified domain = %d, want 200", rec.Code)
	}
	// And it's the same site the subdomain serves.
	if got, want := get("zeta-example.com").Body.String(), get("zeta.localhost").Body.String(); got != want {
		t.Error("the custom domain and the subdomain served different pages")
	}

	// Disconnecting stops it, once the cached answer is dropped.
	if err := accounts.RemoveDomain("zeta-example.com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	d.ForgetDomain("zeta-example.com")
	if body := get("zeta-example.com").Body.String(); !strings.Contains(body, "friendo network") {
		t.Errorf("a disconnected domain still served the tenant:\n%s", body)
	}
}

// TestSuspendedSiteHoldsOnCustomDomain — suspension can't be dodged by using the
// tenant's own address.
func TestSuspendedSiteHoldsOnCustomDomain(t *testing.T) {
	root := t.TempDir()
	reg, _ := NewRegistry(root)
	reg.Provision("zeta", "Zeta")
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	d.SetDomainLookup(accounts.SiteForDomain)
	d.SetSuspendedCheck(accounts.SiteSuspension)

	accounts.AddDomain("zeta-example.com", "zeta")
	accounts.MarkDomainVerified("zeta-example.com")
	accounts.SuspendSite("zeta", "spam")

	req := httptest.NewRequest("GET", "http://zeta-example.com/", nil)
	req.Host = "zeta-example.com"
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("suspended site on a custom domain = %d, want 403", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "on hold") {
		t.Errorf("expected the hold page:\n%s", body)
	}
	// The page names the address the visitor typed, not the internal subdomain.
	if !strings.Contains(body, "zeta-example.com") || strings.Contains(body, "zeta.localhost") {
		t.Errorf("hold page should name the custom domain:\n%s", body)
	}
}

// TestDomainCannotBeStolen: a domain already connected to one site can't be
// grabbed by another.
func TestDomainCannotBeStolen(t *testing.T) {
	f := newQuotaFixture(t)
	if _, err := f.accounts.AddDomain("shared.com", "alice-site"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	// Same site again is idempotent, and keeps the original token.
	first, _ := f.accounts.GetDomain("shared.com")
	again, err := f.accounts.AddDomain("shared.com", "alice-site")
	if err != nil {
		t.Fatalf("re-adding your own domain = %v, want nil", err)
	}
	if again.Token != first.Token {
		t.Error("re-adding a domain changed its token, invalidating instructions already sent")
	}
	// A different site is refused.
	if _, err := f.accounts.AddDomain("shared.com", "bob-site"); err == nil {
		t.Error("a second site was allowed to claim an already-connected domain")
	}
}

// TestDNSProviderVerification covers the self-hosted path end to end.
func TestDNSProviderVerification(t *testing.T) {
	f := newQuotaFixture(t)
	d, err := f.accounts.AddDomain("example.com", "alice-site")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	txt := map[string][]string{}
	p := &DNSProvider{BaseDomain: "localhost", Lookup: func(name string) ([]string, error) {
		v, ok := txt[name]
		if !ok {
			return nil, fmt.Errorf("no such host")
		}
		return v, nil
	}}

	// The instructions name both records, in the tenant's terms.
	in, err := p.Attach(d)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(in.Records) != 2 {
		t.Fatalf("got %d records, want a TXT and a CNAME", len(in.Records))
	}
	text := in.Text(d.Domain)
	for _, want := range []string{"_friendo-challenge.example.com", d.Token, "alice-site.localhost"} {
		if !strings.Contains(text, want) {
			t.Errorf("instructions missing %q:\n%s", want, text)
		}
	}

	// No record yet — a clear "not yet", not an error.
	if err := f.accounts.VerifyDomain("example.com", p); err == nil {
		t.Error("verification passed with no TXT record")
	} else if !strings.Contains(err.Error(), "isn't visible yet") {
		t.Errorf("unhelpful message: %v", err)
	}

	// Wrong value.
	txt[d.RecordName()] = []string{"not-the-token"}
	if err := f.accounts.VerifyDomain("example.com", p); err == nil {
		t.Error("verification passed with the wrong TXT value")
	}
	if got, _ := f.accounts.GetDomain("example.com"); got.Verified {
		t.Fatal("domain was marked verified on a failed check")
	}

	// Right value — and asking again once it's live is not an error.
	txt[d.RecordName()] = []string{"  " + d.Token + "  "}
	if err := f.accounts.VerifyDomain("example.com", p); err != nil {
		t.Fatalf("verification with the right TXT = %v", err)
	}
	if got, _ := f.accounts.GetDomain("example.com"); !got.Verified {
		t.Error("domain not marked verified")
	}
	if err := f.accounts.VerifyDomain("example.com", p); err != nil {
		t.Errorf("re-verifying a live domain = %v, want nil", err)
	}
}

// --- Cloudflare for SaaS, against a stub API ---

// cfStub stands in for Cloudflare's Custom Hostnames API.
type cfStub struct {
	hostnames map[string]*cfHostname
	server    *httptest.Server
	deleted   []string
	// fallbackOrigin is the zone-level setting EnsureFallbackOrigin manages.
	fallbackOrigin string
	originPuts     []string
}

func newCFStub(t *testing.T) *cfStub {
	t.Helper()
	s := &cfStub{hostnames: map[string]*cfHostname{}}
	mux := http.NewServeMux()

	ok := func(w http.ResponseWriter, result any) {
		raw, _ := json.Marshal(result)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": json.RawMessage(raw)})
	}

	mux.HandleFunc("/zones/zone1/custom_hostnames", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Hostname string `json:"hostname"`
				SSL      struct {
					Method string `json:"method"`
					Type   string `json:"type"`
				} `json:"ssl"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if _, exists := s.hostnames[body.Hostname]; exists {
				json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"errors":  []map[string]any{{"code": 1406, "message": "workers.api.error.duplicate_custom_hostname_found"}},
				})
				return
			}
			h := &cfHostname{
				ID: "cf-" + body.Hostname, Hostname: body.Hostname, Status: "pending",
				VerificationErrors: []string{"custom hostname does not CNAME to this zone."},
			}
			h.SSL.Status = "pending_validation"
			h.SSL.Method = body.SSL.Method
			// Cloudflare returns the certificate challenge separately from the
			// ownership record — the distinction this stub exists to model.
			if body.SSL.Method == "txt" {
				h.SSL.ValidationRecords = append(h.SSL.ValidationRecords, struct {
					TxtName  string `json:"txt_name"`
					TxtValue string `json:"txt_value"`
					HTTPUrl  string `json:"http_url"`
				}{TxtName: "_acme-challenge." + body.Hostname, TxtValue: "cf-dv-challenge"})
			}
			h.OwnershipVerification.Type = "txt"
			h.OwnershipVerification.Name = "_cf-custom-hostname." + body.Hostname
			h.OwnershipVerification.Value = "cf-ownership-token"
			s.hostnames[body.Hostname] = h
			ok(w, h)
		case http.MethodGet:
			want := r.URL.Query().Get("hostname")
			var out []cfHostname
			if h, exists := s.hostnames[want]; exists {
				out = append(out, *h)
			}
			ok(w, out)
		}
	})
	mux.HandleFunc("/zones/zone1/custom_hostnames/fallback_origin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				Origin string `json:"origin"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			s.fallbackOrigin = body.Origin
			s.originPuts = append(s.originPuts, body.Origin)
			ok(w, cfFallbackOrigin{Origin: body.Origin, Status: "pending_deployment"})
			return
		}
		if s.fallbackOrigin == "" {
			json.NewEncoder(w).Encode(map[string]any{
				"success": false, "errors": []map[string]any{{"message": "fallback origin not set"}},
			})
			return
		}
		ok(w, cfFallbackOrigin{Origin: s.fallbackOrigin, Status: "active"})
	})
	mux.HandleFunc("/zones/zone1/custom_hostnames/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/zones/zone1/custom_hostnames/")
		host := strings.TrimPrefix(id, "cf-")
		h, exists := s.hostnames[host]
		if !exists {
			json.NewEncoder(w).Encode(map[string]any{
				"success": false, "errors": []map[string]any{{"message": "not found"}},
			})
			return
		}
		if r.Method == http.MethodDelete {
			s.deleted = append(s.deleted, host)
			delete(s.hostnames, host)
			ok(w, map[string]string{"id": id})
			return
		}
		ok(w, h)
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

// activate flips a hostname to what Cloudflare reports once DNS is in place.
func (s *cfStub) activate(host string) {
	if h, ok := s.hostnames[host]; ok {
		h.Status = "active"
		h.SSL.Status = "active"
		h.VerificationErrors = nil
	}
}

func (s *cfStub) provider() *CloudflareProvider {
	return &CloudflareProvider{
		APIToken: "test-token", ZoneID: "zone1",
		CNAMETarget: "origin.friendo.world", FallbackOrigin: "origin.friendo.world",
		api: s.server.URL,
	}
}

// TestCloudflareProvider walks the friendo.world path: attach, wait, go live,
// detach — with Cloudflare, not friendo, doing the certificate work.
func TestCloudflareProvider(t *testing.T) {
	f := newQuotaFixture(t)
	stub := newCFStub(t)
	p := stub.provider()

	d, err := f.accounts.AddDomain("example.com", "alice-site")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	in, err := p.Attach(d)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if in.ProviderID != "cf-example.com" {
		t.Errorf("ProviderID = %q, want Cloudflare's hostname id", in.ProviderID)
	}
	// HTTP validation is the default, and it asks the tenant for exactly one
	// record. Anything more is noise for someone who has never edited DNS.
	if len(in.Records) != 1 || in.Records[0].Type != "CNAME" {
		t.Fatalf("http validation should need one CNAME, got %+v", in.Records)
	}
	text := in.Text("example.com")
	for _, want := range []string{"CNAME", "origin.friendo.world", "only record you need", "renewed automatically"} {
		if !strings.Contains(text, want) {
			t.Errorf("instructions missing %q:\n%s", want, text)
		}
	}
	// No TXT records at all — with the CNAME in place Cloudflare proves ownership
	// and issues the certificate itself.
	if strings.Contains(text, "TXT") {
		t.Errorf("http validation should not ask for a TXT record:\n%s", text)
	}
	if err := f.accounts.SetDomainProviderID(d.Domain, in.ProviderID); err != nil {
		t.Fatalf("SetDomainProviderID: %v", err)
	}

	// Still pending — verification relays Cloudflare's own reason, which is more
	// use to the tenant than a generic "not ready".
	if err := f.accounts.VerifyDomain("example.com", p); err == nil {
		t.Error("verification passed while Cloudflare was still pending")
	} else if !strings.Contains(err.Error(), "CNAME") {
		t.Errorf("pending message should relay Cloudflare's reason, got: %v", err)
	}

	// Cloudflare validates and issues.
	stub.activate("example.com")
	if err := f.accounts.VerifyDomain("example.com", p); err != nil {
		t.Fatalf("verification after activation = %v", err)
	}
	if got, _ := f.accounts.GetDomain("example.com"); !got.Verified {
		t.Error("domain not marked verified")
	}
	if sub, ok := f.accounts.SiteForDomain("example.com"); !ok || sub != "alice-site" {
		t.Errorf("SiteForDomain = %q, %v; want alice-site", sub, ok)
	}

	// Detach releases it at Cloudflare too, so the hostname isn't billed forever.
	stored, _ := f.accounts.GetDomain("example.com")
	if err := p.Detach(stored); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if len(stub.deleted) != 1 || stub.deleted[0] != "example.com" {
		t.Errorf("Cloudflare deletions = %v, want [example.com]", stub.deleted)
	}
}

// TestCloudflareAttachRecoversFromDuplicate: re-adding a domain Cloudflare
// already knows about must not dead-end the tenant.
func TestCloudflareAttachRecoversFromDuplicate(t *testing.T) {
	f := newQuotaFixture(t)
	stub := newCFStub(t)
	p := stub.provider()

	d, _ := f.accounts.AddDomain("example.com", "alice-site")
	if _, err := p.Attach(d); err != nil {
		t.Fatalf("first Attach: %v", err)
	}
	// Cloudflare now refuses the create as a duplicate; Attach should find the
	// existing hostname and carry on.
	in, err := p.Attach(d)
	if err != nil {
		t.Fatalf("second Attach = %v, want recovery", err)
	}
	if in.ProviderID != "cf-example.com" {
		t.Errorf("ProviderID = %q after recovery", in.ProviderID)
	}
}

// TestCloudflareFromEnv: configuration is opt-in, and absence is not an error —
// a self-hosted network just uses the DNS provider.
func TestCloudflareFromEnv(t *testing.T) {
	t.Setenv("FRIENDO_CF_API_TOKEN", "")
	t.Setenv("FRIENDO_CF_ZONE_ID", "")
	if _, ok := CloudflareFromEnv("friendo.world"); ok {
		t.Error("Cloudflare should be off when unconfigured")
	}
	t.Setenv("FRIENDO_CF_API_TOKEN", "tok")
	t.Setenv("FRIENDO_CF_ZONE_ID", "zone")
	cf, ok := CloudflareFromEnv("friendo.world")
	if !ok {
		t.Fatal("Cloudflare should be on when configured")
	}
	// With nothing else set, tenants point at the base domain.
	if cf.CNAMETarget != "friendo.world" {
		t.Errorf("CNAMETarget = %q, want the base domain by default", cf.CNAMETarget)
	}
	if cf.sslMethod() != "http" {
		t.Errorf("sslMethod = %q, want http by default", cf.sslMethod())
	}

	// A fallback origin doubles as the CNAME target unless one is named — they're
	// different settings, but pointing at the origin is the common arrangement.
	t.Setenv("FRIENDO_CF_FALLBACK_ORIGIN", "origin.friendo.world")
	cf, _ = CloudflareFromEnv("friendo.world")
	if cf.FallbackOrigin != "origin.friendo.world" {
		t.Errorf("FallbackOrigin = %q", cf.FallbackOrigin)
	}
	if cf.CNAMETarget != "origin.friendo.world" {
		t.Errorf("CNAMETarget = %q, want the fallback origin when unset", cf.CNAMETarget)
	}

	// And they can be split when the CNAME target is its own hostname.
	t.Setenv("FRIENDO_CF_CNAME_TARGET", "cname.friendo.world")
	t.Setenv("FRIENDO_CF_SSL_METHOD", "txt")
	cf, _ = CloudflareFromEnv("friendo.world")
	if cf.CNAMETarget != "cname.friendo.world" || cf.FallbackOrigin != "origin.friendo.world" {
		t.Errorf("target/origin = %q / %q, want them independent", cf.CNAMETarget, cf.FallbackOrigin)
	}
	if cf.sslMethod() != "txt" {
		t.Errorf("sslMethod = %q, want the txt override", cf.sslMethod())
	}
}

// TestCloudflareTXTValidation covers the pre-validation path — and specifically
// that the certificate challenge is surfaced. Showing only the ownership record
// leaves the tenant waiting on a cert they were never told how to get.
func TestCloudflareTXTValidation(t *testing.T) {
	f := newQuotaFixture(t)
	stub := newCFStub(t)
	p := stub.provider()
	p.SSLMethod = "txt"

	d, _ := f.accounts.AddDomain("example.com", "alice-site")
	in, err := p.Attach(d)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	text := in.Text("example.com")
	for _, want := range []string{
		"_cf-custom-hostname.example.com", "cf-ownership-token", // proves ownership
		"_acme-challenge.example.com", "cf-dv-challenge", // gets the cert issued
		"origin.friendo.world", // and where to point it
	} {
		if !strings.Contains(text, want) {
			t.Errorf("txt instructions missing %q:\n%s", want, text)
		}
	}
	if len(in.Records) != 3 {
		t.Errorf("got %d records, want ownership + certificate challenge + CNAME", len(in.Records))
	}
	// The CNAME comes last: the two proofs go in first, which is what lets a
	// tenant pre-validate before moving live traffic.
	if in.Records[len(in.Records)-1].Type != "CNAME" {
		t.Error("the CNAME should come after the records that prove the domain")
	}
}

// TestCloudflareEnsureFallbackOrigin: setup that used to be a manual dashboard
// step now happens at boot, and is left alone once it matches.
func TestCloudflareEnsureFallbackOrigin(t *testing.T) {
	stub := newCFStub(t)
	p := stub.provider()

	msg, err := p.EnsureFallbackOrigin()
	if err != nil {
		t.Fatalf("EnsureFallbackOrigin: %v", err)
	}
	if !strings.Contains(msg, "origin.friendo.world") {
		t.Errorf("message = %q, should name the origin", msg)
	}
	if stub.fallbackOrigin != "origin.friendo.world" {
		t.Errorf("Cloudflare's fallback origin = %q", stub.fallbackOrigin)
	}

	// Already correct — read it, don't write it again on every boot.
	if _, err := p.EnsureFallbackOrigin(); err != nil {
		t.Fatalf("second EnsureFallbackOrigin: %v", err)
	}
	if len(stub.originPuts) != 1 {
		t.Errorf("wrote the fallback origin %d times, want 1", len(stub.originPuts))
	}

	// Changed — write the new one.
	p.FallbackOrigin = "origin2.friendo.world"
	if _, err := p.EnsureFallbackOrigin(); err != nil {
		t.Fatalf("changing the fallback origin: %v", err)
	}
	if stub.fallbackOrigin != "origin2.friendo.world" || len(stub.originPuts) != 2 {
		t.Errorf("origin = %q after %d writes", stub.fallbackOrigin, len(stub.originPuts))
	}

	// Unconfigured is a clear error, not a silent no-op — this is the setting
	// whose absence makes every custom hostname 502.
	p.FallbackOrigin = ""
	if _, err := p.EnsureFallbackOrigin(); err == nil {
		t.Error("an unset fallback origin should report itself")
	}
}

// --- the account API surface (what `friendo domain` drives) ---

// domainFixture wires the self-service surface with a controllable provider.
func domainFixture(t *testing.T) (*quotaFixture, *DNSProvider, map[string][]string) {
	t.Helper()
	f := newQuotaFixture(t)
	txt := map[string][]string{}
	p := &DNSProvider{BaseDomain: "localhost", Lookup: func(name string) ([]string, error) {
		v, ok := txt[name]
		if !ok {
			return nil, fmt.Errorf("no such host")
		}
		return v, nil
	}}
	// Rebuild the mux with the provider attached.
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	aa := NewAccountAuth(f.accounts, reg, "localhost")
	aa.SetDomainProvider(p)
	m := http.NewServeMux()
	aa.register(m)
	f.mux = m
	return f, p, txt
}

func (f *quotaFixture) send(method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// TestDomainAPIOwnership: you can only connect a domain to a site you own.
func TestDomainAPIOwnership(t *testing.T) {
	f, _, _ := domainFixture(t)
	alice, aliceTok := f.signIn(t, "alice@example.com")
	_, bobTok := f.signIn(t, "bob@example.com")
	if err := f.accounts.SetSiteOwner("alice-site", alice.ID); err != nil {
		t.Fatalf("SetSiteOwner: %v", err)
	}

	// Alice can.
	rec := f.send("POST", "/api/account/domains", aliceTok, `{"domain":"alice.example","subdomain":"alice-site"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice add = %d\n%s", rec.Code, rec.Body.String())
	}
	// Bob can't — not his site.
	rec = f.send("POST", "/api/account/domains", bobTok, `{"domain":"bob.example","subdomain":"alice-site"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bob add to alice's site = %d, want 403", rec.Code)
	}
	// Bob can't verify or remove hers either.
	if rec := f.send("POST", "/api/account/domains/verify", bobTok, `{"domain":"alice.example"}`); rec.Code != http.StatusForbidden {
		t.Errorf("bob verify = %d, want 403", rec.Code)
	}
	if rec := f.send("DELETE", "/api/account/domains", bobTok, `{"domain":"alice.example"}`); rec.Code != http.StatusForbidden {
		t.Errorf("bob remove = %d, want 403", rec.Code)
	}
	// And it's still connected to alice.
	if d, ok := f.accounts.GetDomain("alice.example"); !ok || d.Subdomain != "alice-site" {
		t.Errorf("domain = %+v, %v after bob's attempts", d, ok)
	}
	// Signed out entirely: 401.
	if rec := f.send("POST", "/api/account/domains", "", `{"domain":"x.example","subdomain":"alice-site"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous add = %d, want 401", rec.Code)
	}
}

// TestDomainAPIRejectsNetworkOwnDomain — a subdomain of the network isn't a
// custom domain, and letting one be claimed would be a routing hijack.
func TestDomainAPIRejectsNetworkOwnDomain(t *testing.T) {
	f, _, _ := domainFixture(t)
	alice, tok := f.signIn(t, "alice@example.com")
	f.accounts.SetSiteOwner("alice-site", alice.ID)

	for _, bad := range []string{"localhost", "other.localhost", "deep.other.localhost"} {
		rec := f.send("POST", "/api/account/domains", tok,
			fmt.Sprintf(`{"domain":%q,"subdomain":"alice-site"}`, bad))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("claiming %q = %d, want 400\n%s", bad, rec.Code, rec.Body.String())
		}
	}
	// A nonsense domain is refused too, with an example rather than a regex.
	rec := f.send("POST", "/api/account/domains", tok, `{"domain":"not a domain","subdomain":"alice-site"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid domain = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "example.com") {
		t.Errorf("the error should show what good looks like:\n%s", rec.Body.String())
	}
}

// TestDomainAPILifecycle walks the tenant's whole path through the API.
func TestDomainAPILifecycle(t *testing.T) {
	f, _, txt := domainFixture(t)
	alice, tok := f.signIn(t, "alice@example.com")
	f.accounts.SetSiteOwner("alice-site", alice.ID)

	rec := f.send("POST", "/api/account/domains", tok, `{"domain":"alice.example","subdomain":"alice-site"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d\n%s", rec.Code, rec.Body.String())
	}
	var added struct {
		Domain       string `json:"domain"`
		Verified     bool   `json:"verified"`
		Instructions struct {
			Records []DNSRecord `json:"records"`
		} `json:"instructions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decoding add: %v", err)
	}
	if added.Verified {
		t.Error("a freshly added domain must not be verified")
	}
	if len(added.Instructions.Records) == 0 {
		t.Fatal("no DNS instructions returned — the tenant has nothing to act on")
	}

	// Verify before DNS: 409, meaning "not yet", with something to act on.
	if rec := f.send("POST", "/api/account/domains/verify", tok, `{"domain":"alice.example"}`); rec.Code != http.StatusConflict {
		t.Errorf("premature verify = %d, want 409", rec.Code)
	}

	// It shows up as pending in the list.
	rec = f.send("GET", "/api/account/domains", tok, "")
	if !strings.Contains(rec.Body.String(), "waiting for DNS") {
		t.Errorf("list should show it waiting:\n%s", rec.Body.String())
	}

	// Add the TXT record and verify for real.
	d, _ := f.accounts.GetDomain("alice.example")
	txt[d.RecordName()] = []string{d.Token}
	if rec := f.send("POST", "/api/account/domains/verify", tok, `{"domain":"alice.example"}`); rec.Code != http.StatusOK {
		t.Fatalf("verify = %d\n%s", rec.Code, rec.Body.String())
	}
	if sub, ok := f.accounts.SiteForDomain("alice.example"); !ok || sub != "alice-site" {
		t.Errorf("SiteForDomain = %q, %v", sub, ok)
	}
	rec = f.send("GET", "/api/account/domains", tok, "")
	if !strings.Contains(rec.Body.String(), "live") {
		t.Errorf("list should show it live:\n%s", rec.Body.String())
	}

	// Remove it.
	if rec := f.send("DELETE", "/api/account/domains", tok, `{"domain":"alice.example"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("remove = %d", rec.Code)
	}
	if _, ok := f.accounts.SiteForDomain("alice.example"); ok {
		t.Error("a removed domain still resolves")
	}
	if rec := f.send("POST", "/api/account/domains/verify", tok, `{"domain":"alice.example"}`); rec.Code != http.StatusNotFound {
		t.Errorf("verify after removal = %d, want 404", rec.Code)
	}
}
