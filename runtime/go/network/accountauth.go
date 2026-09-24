package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/email"
)

// accountCookie is the network account session cookie, set on the apex host.
// It is the browser's counterpart to the Bearer token the CLI caches: the same
// session store, the same account, reached either way.
const accountCookie = "friendo_account"

// AccountAuth serves the network's identity + self-service surface: passwordless
// sign-in (email code) for browsers, device-auth for the CLI, account whoami,
// account-owned site creation, custom domains, and the two ways into a site's
// admin — the in-process SSO exchange `friendo deploy` uses and the one-time
// "Open admin" link the account page uses. See design/network-accounts.md.
type AccountAuth struct {
	accounts *Accounts
	reg      *Registry
	base     string

	// domains is how a custom domain proves itself and gets a certificate.
	// Defaults to the self-hosted DNS check; friendo.world sets Cloudflare.
	domains DomainProvider

	// domainChanged, when set, is called after a domain starts or stops routing
	// so the dispatcher can drop its cached answer instead of waiting out the TTL.
	domainChanged func(host string)

	// sso holds one-time "Open admin" codes (code → grant) until a site's
	// /_/sso landing redeems them. In-process, because every site shares this
	// binary; a code lives for a minute and is consumed on first use.
	sso sync.Map
}

// ssoGrant is a pending "Open admin" hand-off: a minted site session waiting
// for the browser to arrive at that site.
type ssoGrant struct {
	sub     string
	session string
	expires time.Time
}

// ssoCodeTTL is how long an "Open admin" link stays redeemable.
const ssoCodeTTL = time.Minute

// NewAccountAuth builds the identity + self-service handlers.
func NewAccountAuth(accounts *Accounts, reg *Registry, baseDomain string) *AccountAuth {
	return &AccountAuth{
		accounts: accounts,
		reg:      reg,
		base:     strings.ToLower(strings.TrimSpace(baseDomain)),
	}
}

// SetDomainProvider chooses how custom domains are verified and certificated.
func (aa *AccountAuth) SetDomainProvider(p DomainProvider) { aa.domains = p }

// SetDomainChangedHook registers a callback for when a domain's routing changes.
func (aa *AccountAuth) SetDomainChangedHook(fn func(host string)) { aa.domainChanged = fn }

// domainProvider returns the configured provider, defaulting to the DNS check so
// a self-hosted network works with no configuration at all.
func (aa *AccountAuth) domainProvider() DomainProvider {
	if aa.domains == nil {
		aa.domains = &DNSProvider{BaseDomain: aa.base}
	}
	return aa.domains
}

// register mounts the identity + self-service routes on a mux. Every path here
// is kept stable: older CLIs depend on them.
func (aa *AccountAuth) register(m *http.ServeMux) {
	// Browser sign-in (email code) — the same account the CLI links by device.
	m.HandleFunc("POST /api/auth/request-code", aa.requestCode)
	m.HandleFunc("POST /api/auth/verify-code", aa.verifyCode)
	m.HandleFunc("POST /api/auth/logout", aa.logout)
	// Device-auth for the CLI; the browser approves via /api/auth/device/approve
	// once signed in (the /activate page is a shell around <friendo-activate>).
	m.HandleFunc("POST /api/auth/device/start", aa.deviceStart)
	m.HandleFunc("POST /api/auth/device/poll", aa.devicePoll)
	m.HandleFunc("POST /api/auth/device/approve", aa.deviceApprove)
	m.HandleFunc("GET /api/account", aa.whoami)
	// Self-service (account auth): create/own sites and get into them.
	m.HandleFunc("POST /api/account/sites", aa.createSite)
	m.HandleFunc("GET /api/account/sites", aa.listSites)
	m.HandleFunc("POST /api/account/sites/{sub}/admin-link", aa.adminLink)
	m.HandleFunc("POST /api/sso/exchange", aa.ssoExchange)
	// Custom domains, for a site the account owns.
	m.HandleFunc("POST /api/account/domains", aa.addDomain)
	m.HandleFunc("GET /api/account/domains", aa.listDomains)
	m.HandleFunc("POST /api/account/domains/verify", aa.verifyDomain)
	m.HandleFunc("DELETE /api/account/domains", aa.removeDomain)
}

// --- who's asking ---

// accountFromRequest resolves the signed-in account from a Bearer token (the
// CLI) or the account cookie (a browser), whichever is present.
func (aa *AccountAuth) accountFromRequest(r *http.Request) (*Account, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return aa.accounts.ValidateSession(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	}
	if ck, err := r.Cookie(accountCookie); err == nil {
		return aa.accounts.ValidateSession(ck.Value)
	}
	return nil, false
}

// deny explains a rejected request. A suspended account gets a 403 saying so —
// otherwise the same bare 401 would send someone off to re-run `friendo login`
// over and over against a network that will never let them back in.
func (aa *AccountAuth) deny(w http.ResponseWriter, r *http.Request) {
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	} else if ck, err := r.Cookie(accountCookie); err == nil {
		token = ck.Value
	}
	if token != "" {
		if s, suspended := aa.accounts.SuspensionForSession(token); suspended {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": SuspensionMessage(s)})
			return
		}
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setAccountCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: accountCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureRequest(r),
		MaxAge: 30 * 24 * 3600, // matches the session's own life
	})
}

func clearAccountCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: accountCookie, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureRequest(r), MaxAge: -1,
	})
}

// accountJSON is what a signed-in browser or CLI learns about itself.
func accountJSON(acct *Account) map[string]any {
	caps := acct.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return map[string]any{"email": acct.Email, "capabilities": caps, "operator": acct.Has("operator")}
}

// --- browser sign-in (email code) ---

// requestCode emails a one-time code. No account is created yet; that happens
// when the code verifies (and only if the signup policy allows it).
func (aa *AccountAuth) requestCode(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	addr := strings.ToLower(strings.TrimSpace(body.Email))
	code, err := aa.accounts.RequestOTP(addr)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrCodeAlreadySent) {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	email.SendLoginCode(addr, code)
	resp := map[string]any{"sent": true, "emailed": email.Configured()}
	if otpEcho() {
		resp["code"] = code
	}
	writeJSON(w, http.StatusOK, resp)
}

// verifyCode checks the code, creates the account if the signup policy allows,
// and starts a browser session. First-operator bootstrap lives here: when no
// operator exists yet, the first verified email claims it (mirroring a site's
// first-run setup), so a network is reachable even without FRIENDO_OPERATOR_EMAIL.
func (aa *AccountAuth) verifyCode(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Code string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	addr := strings.ToLower(strings.TrimSpace(body.Email))
	id, firstOperator, err := aa.verifyAndBootstrap(addr, strings.TrimSpace(body.Code))
	if err != nil {
		writeJSON(w, verifyStatus(err), map[string]string{"error": verifyMessage(err)})
		return
	}
	token, err := aa.accounts.StartSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not start session"})
		return
	}
	setAccountCookie(w, r, token)
	acct, _ := aa.accounts.Get(id)
	out := accountJSON(acct)
	out["first_operator"] = firstOperator
	writeJSON(w, http.StatusOK, out)
}

// verifyAndBootstrap is the shared verify step for the sign-in page and the
// device approval: VerifyOTP plus the first-operator claim.
func (aa *AccountAuth) verifyAndBootstrap(addr, code string) (id string, firstOperator bool, err error) {
	// With no operator yet, pre-creating the account lets it past an invite-only
	// signup gate — the person setting the network up is exactly who's expected.
	if any, _ := aa.accounts.AnyOperator(); !any {
		firstOperator = true
		aa.accounts.EnsureAccount(addr)
	}
	id, err = aa.accounts.VerifyOTP(addr, code)
	if err != nil {
		return "", false, err
	}
	if firstOperator {
		if err := aa.accounts.Grant(addr, "operator"); err != nil {
			return "", false, err
		}
	}
	return id, firstOperator, nil
}

func verifyStatus(err error) int {
	switch {
	case errors.Is(err, ErrSignupsInviteOnly), errors.Is(err, ErrAccountSuspended):
		return http.StatusForbidden
	case errors.Is(err, ErrTooManyWrongCodes):
		return http.StatusTooManyRequests
	}
	return http.StatusUnauthorized
}

func verifyMessage(err error) string {
	if errors.Is(err, ErrSignupsInviteOnly) {
		return "Your code was correct, but sign-ups are invite-only on this network — ask an operator to invite you."
	}
	return err.Error()
}

func (aa *AccountAuth) logout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(accountCookie); err == nil {
		aa.accounts.EndSession(ck.Value)
	}
	clearAccountCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// --- device-auth (CLI side) ---

func (aa *AccountAuth) deviceStart(w http.ResponseWriter, r *http.Request) {
	deviceCode, userCode, err := aa.accounts.StartDevice()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not start device auth"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": deviceCode,
		"user_code":   userCode,
		// A path the CLI resolves against its network URL (the server can't know its
		// public scheme/host behind a proxy).
		"verification_uri": "/activate?code=" + userCode,
		"interval":         2,
		"expires_in":       900,
	})
}

func (aa *AccountAuth) devicePoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	token, pending, err := aa.accounts.PollDevice(body.DeviceCode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if pending {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "authorization_pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// deviceApprove links a device to the signed-in account — what the browser does
// on /activate after signing in with a code.
func (aa *AccountAuth) deviceApprove(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct {
		UserCode string `json:"user_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := aa.accounts.ApproveDevice(body.UserCode, acct.ID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "that device code is invalid or expired — run the command again"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approved": true, "email": acct.Email})
}

func (aa *AccountAuth) whoami(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	writeJSON(w, http.StatusOK, accountJSON(acct))
}

// --- self-service: account-owned sites ---

// createSite creates (or verifies ownership of) a site the account owns. It also
// ensures the site's owner user exists so a session can be issued for it.
func (aa *AccountAuth) createSite(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Subdomain, Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	sub := strings.ToLower(strings.TrimSpace(body.Subdomain))
	if !ValidSubdomain(sub) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subdomain — use lowercase letters, digits and hyphens"})
		return
	}

	if _, exists := aa.reg.Dir(sub); exists {
		// Idempotent for a site you own; a conflict otherwise.
		if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "that subdomain is taken"})
			return
		}
		if err := ensureOwnerUser(aa.reg, sub, acct.Email); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"site": map[string]string{"subdomain": sub, "name": body.Name}})
		return
	}

	// Names the network keeps for itself. An operator provisions those on
	// purpose (the docs site, the home site); everyone else picks another.
	if IsReserved(sub) && !acct.Has("operator") {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("%q is reserved on this network — pick another name", sub),
		})
		return
	}

	// A brand-new site — check the account's quota before creating anything.
	// Operators are exempt; everyone else gets a message that names the limit.
	if over, used, allowed := aa.accounts.AtSiteLimit(acct); over {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error":   SiteLimitMessage(used, allowed),
			"used":    used,
			"allowed": allowed,
		})
		return
	}

	site, err := aa.reg.Provision(sub, body.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := linkOwner(aa.accounts, aa.reg, sub, acct); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": map[string]string{"subdomain": site.Subdomain, "name": site.Name}})
}

// listSites reports the account's sites with everything the account page shows
// in one call: name, address, hold status, domains — and the quota, so the
// limit is visible before someone walks into it rather than only when they hit it.
func (aa *AccountAuth) listSites(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	subs, err := aa.accounts.SitesOwnedBy(acct.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list sites"})
		return
	}
	held, _ := aa.accounts.SuspendedSites()
	sites := []map[string]any{}
	for _, sub := range subs {
		dir, exists := aa.reg.Dir(sub)
		row := map[string]any{
			"subdomain": sub,
			"name":      sub,
			"address":   sub + "." + aa.base,
			"exists":    exists,
			"suspended": false,
			"domains":   aa.domainRows([]string{sub}),
		}
		if exists {
			row["name"] = siteName(dir, sub)
		}
		if s, ok := held[sub]; ok {
			row["suspended"], row["reason"] = true, s.Reason
		}
		sites = append(sites, row)
	}
	used, allowed, _ := aa.accounts.SiteUsage(acct)
	writeJSON(w, http.StatusOK, map[string]any{
		"sites": sites,
		"quota": map[string]any{"used": used, "allowed": allowed, "unlimited": allowed == QuotaUnlimited, "exempt": acct.Has("operator")},
	})
}

// --- getting into a site's admin ---

// mintSiteSession opens a site the account owns and issues a site admin session
// for its owner user. In network mode this is in-process: the same binary
// holds every site's database.
func (aa *AccountAuth) mintSiteSession(acct *Account, sub string) (string, int, error) {
	if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
		return "", http.StatusForbidden, fmt.Errorf("you don't own that site")
	}
	dir, ok := aa.reg.Dir(sub)
	if !ok {
		return "", http.StatusNotFound, fmt.Errorf("no such site")
	}
	db, err := data.Open(dir)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("could not open site")
	}
	defer db.Conn.Close()
	user, err := db.GetUserByEmail(acct.Email)
	if err != nil {
		user, err = db.CreateMember(acct.Email, "", "owner")
		if err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("could not create owner")
		}
	}
	token, err := db.CreateSession(user.ID, "", "network-account")
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("could not issue session")
	}
	return token, http.StatusOK, nil
}

// ssoExchange hands the CLI a site admin session (friendo_session) for a
// subdomain the account owns — what `friendo deploy` pushes with.
func (aa *AccountAuth) ssoExchange(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Subdomain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	token, status, err := aa.mintSiteSession(acct, strings.ToLower(strings.TrimSpace(body.Subdomain)))
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session": token})
}

// adminLink is the browser's way into a site: it mints a session the same way
// the CLI's exchange does, parks it behind a one-time code, and returns the
// site's /_/sso URL that redeems it. Cookies are per host, so the hand-off has
// to happen at the site's own address; the code is what crosses over.
// Operators are not exempt from the ownership check — the power boundary is
// site lifecycle, not site content.
func (aa *AccountAuth) adminLink(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(r.PathValue("sub")))
	token, status, err := aa.mintSiteSession(acct, sub)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	code := newToken(24)
	aa.sso.Store(code, ssoGrant{sub: sub, session: token, expires: time.Now().Add(ssoCodeTTL)})
	writeJSON(w, http.StatusOK, map[string]any{
		"url":     aa.siteURL(r, sub) + "/_/sso?code=" + code,
		"site":    sub,
		"expires": int(ssoCodeTTL.Seconds()),
	})
}

// ExchangeSSOCode redeems an "Open admin" code for the site session it holds.
// Single-use, and only for the site it was minted for. The dispatcher calls
// this from a site's /_/sso landing.
func (aa *AccountAuth) ExchangeSSOCode(code, sub string) (string, bool) {
	v, ok := aa.sso.Load(code)
	if !ok {
		return "", false
	}
	g := v.(ssoGrant)
	if time.Now().After(g.expires) {
		aa.sso.Delete(code)
		return "", false
	}
	if g.sub != sub {
		return "", false // not for this site; leave it for the right one
	}
	aa.sso.Delete(code)
	return g.session, true
}

// siteURL builds a site's public address as seen from this request: the scheme
// a proxy reports, and the port the apex was reached on (so a local network on
// :3000 links to demo.localhost:3000).
func (aa *AccountAuth) siteURL(r *http.Request, sub string) string {
	scheme := "http"
	if secureRequest(r) {
		scheme = "https"
	}
	host := sub + "." + aa.base
	if _, port, err := net.SplitHostPort(r.Host); err == nil && port != "" && port != "80" && port != "443" {
		host += ":" + port
	}
	return scheme + "://" + host
}

// --- custom domains ---

// ownedSite checks the account owns a subdomain, writing the error itself when
// it doesn't.
func (aa *AccountAuth) ownedSite(w http.ResponseWriter, acct *Account, sub string) bool {
	sub = strings.ToLower(strings.TrimSpace(sub))
	if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "you don't own that site"})
		return false
	}
	return true
}

// attachDomain records a custom domain for a site (unverified) and asks the
// provider what the tenant has to do next. Nothing routes until they've done
// it. Shared by the tenant and operator endpoints; the caller decides who may.
func (aa *AccountAuth) attachDomain(raw, sub string) (map[string]any, int, error) {
	domain, err := NormalizeDomain(raw)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	// The network's own domain is not a custom domain — that's what subdomains are.
	if aa.base != "" && (domain == aa.base || strings.HasSuffix(domain, "."+aa.base)) {
		return nil, http.StatusBadRequest, fmt.Errorf("%s is already part of this network — custom domains are for a domain you own elsewhere", domain)
	}
	sub = strings.ToLower(strings.TrimSpace(sub))
	d, err := aa.accounts.AddDomain(domain, sub)
	if err != nil {
		return nil, http.StatusConflict, err
	}
	instructions, err := aa.domainProvider().Attach(d)
	if err != nil {
		// The record stays so the tenant can retry without losing their place.
		return nil, http.StatusBadGateway, err
	}
	if instructions.ProviderID != "" {
		aa.accounts.SetDomainProviderID(d.Domain, instructions.ProviderID)
	}
	// Drop any cached "nobody has connected this" answer, so the domain's page
	// says "finish setting it up" straight away rather than after the TTL.
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	return map[string]any{
		"domain":       d.Domain,
		"site":         d.Subdomain,
		"verified":     false,
		"status":       d.Status(),
		"instructions": instructions,
		"text":         instructions.Text(d.Domain),
	}, http.StatusCreated, nil
}

// checkDomain asks the provider whether a domain's DNS is in place yet and, if
// so, marks it verified so it starts routing.
func (aa *AccountAuth) checkDomain(d Domain) (int, error) {
	if err := aa.accounts.VerifyDomain(d.Domain, aa.domainProvider()); err != nil {
		// Not an error in the tenant's world — DNS just isn't there yet. 409 says
		// "try again later" rather than "you did something wrong".
		return http.StatusConflict, err
	}
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	return http.StatusOK, nil
}

// detachDomain disconnects a custom domain. Released at the provider first, but
// that failure never strands anyone — the record goes either way.
func (aa *AccountAuth) detachDomain(d Domain) error {
	if err := aa.domainProvider().Detach(d); err != nil {
		log.Printf("[network] releasing %s at the provider failed: %v", d.Domain, err)
	}
	if err := aa.accounts.RemoveDomain(d.Domain); err != nil {
		return err
	}
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	return nil
}

// domainRows lists the domains on some sites in API shape.
func (aa *AccountAuth) domainRows(subs []string) []map[string]any {
	out := []map[string]any{}
	for _, sub := range subs {
		list, err := aa.accounts.DomainsForSite(sub)
		if err != nil {
			continue
		}
		for _, d := range list {
			out = append(out, map[string]any{
				"domain": d.Domain, "site": d.Subdomain, "verified": d.Verified, "status": d.Status(),
			})
		}
	}
	return out
}

// addDomain connects a custom domain to one of the account's sites.
func (aa *AccountAuth) addDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Domain, Subdomain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if !aa.ownedSite(w, acct, body.Subdomain) {
		return
	}
	out, status, err := aa.attachDomain(body.Domain, body.Subdomain)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, status, out)
}

// listDomains reports the custom domains on the account's sites.
func (aa *AccountAuth) listDomains(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	subs, err := aa.accounts.SitesOwnedBy(acct.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list sites"})
		return
	}
	if only := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("site"))); only != "" {
		if !aa.ownedSite(w, acct, only) {
			return
		}
		subs = []string{only}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": aa.domainRows(subs)})
}

// verifyDomain asks the provider whether the tenant's DNS is in place yet.
func (aa *AccountAuth) verifyDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Domain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	d, found := aa.accounts.GetDomain(body.Domain)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that domain isn't connected to this network"})
		return
	}
	if !aa.ownedSite(w, acct, d.Subdomain) {
		return
	}
	if status, err := aa.checkDomain(d); err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error(), "domain": d.Domain})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": d.Domain, "site": d.Subdomain, "verified": true, "status": "live"})
}

// removeDomain disconnects a custom domain. The site keeps serving at its
// <subdomain>.<baseDomain> address.
func (aa *AccountAuth) removeDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromRequest(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Domain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	d, found := aa.accounts.GetDomain(body.Domain)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that domain isn't connected to this network"})
		return
	}
	if !aa.ownedSite(w, acct, d.Subdomain) {
		return
	}
	if err := aa.detachDomain(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// otpEcho reports whether login codes should be shown inline — the shared
// dev-only rule (explicit FRIENDO_OTP_ECHO and no email provider configured).
func otpEcho() bool { return email.EchoEnabled() }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
