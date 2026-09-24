package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/email"
)

// AccountAuth serves the network's identity + self-service surface: passwordless
// device-auth for the CLI (a browser /activate page + OTP), account whoami, and
// account-owned site creation + the in-process SSO exchange that `friendo deploy`
// uses. See design/network-accounts.md.
type AccountAuth struct {
	accounts *Accounts
	reg      *Registry
	base     string
	tpl      *template.Template

	// domains is how a custom domain proves itself and gets a certificate.
	// Defaults to the self-hosted DNS check; friendo.world sets Cloudflare.
	domains DomainProvider

	// domainChanged, when set, is called after a domain starts or stops routing
	// so the dispatcher can drop its cached answer instead of waiting out the TTL.
	domainChanged func(host string)
}

// NewAccountAuth builds the identity + self-service handlers.
func NewAccountAuth(accounts *Accounts, reg *Registry, baseDomain string) *AccountAuth {
	return &AccountAuth{
		accounts: accounts,
		reg:      reg,
		base:     strings.ToLower(strings.TrimSpace(baseDomain)),
		tpl:      template.Must(template.New("activate").Parse(activateTemplates)),
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

// register mounts the identity + self-service routes on a mux.
func (aa *AccountAuth) register(m *http.ServeMux) {
	m.HandleFunc("POST /api/auth/device/start", aa.deviceStart)
	m.HandleFunc("POST /api/auth/device/poll", aa.devicePoll)
	m.HandleFunc("GET /activate", aa.activateGet)
	m.HandleFunc("POST /activate", aa.activatePost)
	m.HandleFunc("GET /api/account", aa.whoami)
	// Self-service (account Bearer auth): create/own sites and get a site session.
	m.HandleFunc("POST /api/account/sites", aa.createSite)
	m.HandleFunc("GET /api/account/sites", aa.listSites)
	m.HandleFunc("POST /api/sso/exchange", aa.ssoExchange)
	// Custom domains, for a site the account owns.
	m.HandleFunc("POST /api/account/domains", aa.addDomain)
	m.HandleFunc("GET /api/account/domains", aa.listDomains)
	m.HandleFunc("POST /api/account/domains/verify", aa.verifyDomain)
	m.HandleFunc("DELETE /api/account/domains", aa.removeDomain)
}

// ApexRouter combines the identity endpoints with the operator console into one
// apex handler: identity routes are matched first, everything else falls to the
// console.
func ApexRouter(console *Console, aa *AccountAuth) http.Handler {
	m := http.NewServeMux()
	aa.register(m)
	m.Handle("/", console)
	return m
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

func (aa *AccountAuth) whoami(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": acct.Email, "capabilities": acct.Capabilities})
}

// deny explains a rejected token. A suspended account gets a 403 saying so —
// otherwise the same bare 401 would send someone off to re-run `friendo login`
// over and over against a network that will never let them back in.
func (aa *AccountAuth) deny(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if s, suspended := aa.accounts.SuspensionForSession(token); suspended {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": SuspensionMessage(s)})
			return
		}
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func (aa *AccountAuth) accountFromBearer(r *http.Request) (*Account, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, false
	}
	return aa.accounts.ValidateSession(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
}

// --- self-service: account-owned sites + SSO ---

// createSite creates (or verifies ownership of) a site the account owns. It also
// ensures the site's owner user exists so an SSO session can be issued.
func (aa *AccountAuth) createSite(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subdomain"})
		return
	}

	if _, exists := aa.reg.Dir(sub); exists {
		// Idempotent for a site you own; a conflict otherwise.
		if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "that subdomain is taken"})
			return
		}
		if err := aa.ensureOwnerUser(sub, acct.Email); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"site": map[string]string{"subdomain": sub, "name": body.Name}})
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
	if err := aa.accounts.SetSiteOwner(sub, acct.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := aa.ensureOwnerUser(sub, acct.Email); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": map[string]string{"subdomain": site.Subdomain, "name": site.Name}})
}

func (aa *AccountAuth) listSites(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	subs, err := aa.accounts.SitesOwnedBy(acct.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list sites"})
		return
	}
	// The quota rides along so the CLI and console can show used / allowed
	// *before* someone walks into the cap rather than only when they hit it.
	used, allowed, _ := aa.accounts.SiteUsage(acct)
	writeJSON(w, http.StatusOK, map[string]any{
		"sites": subs,
		"quota": map[string]any{"used": used, "allowed": allowed, "unlimited": allowed == QuotaUnlimited},
	})
}

// --- custom domains ---

// ownedSite resolves the subdomain in a request body and checks the account owns
// it, writing the error itself when it doesn't.
func (aa *AccountAuth) ownedSite(w http.ResponseWriter, acct *Account, sub string) bool {
	sub = strings.ToLower(strings.TrimSpace(sub))
	if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "you don't own that site"})
		return false
	}
	return true
}

// addDomain connects a custom domain to one of the account's sites. It records
// the domain unverified and hands back what the tenant has to do next — nothing
// routes until they've done it.
func (aa *AccountAuth) addDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Domain, Subdomain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	domain, err := NormalizeDomain(body.Domain)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// The network's own domain is not a custom domain — that's what subdomains are.
	if aa.base != "" && (domain == aa.base || strings.HasSuffix(domain, "."+aa.base)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("%s is already part of this network — custom domains are for a domain you own elsewhere", domain),
		})
		return
	}
	if !aa.ownedSite(w, acct, body.Subdomain) {
		return
	}

	d, err := aa.accounts.AddDomain(domain, strings.ToLower(strings.TrimSpace(body.Subdomain)))
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	instructions, err := aa.domainProvider().Attach(d)
	if err != nil {
		// The record stays so the tenant can retry without losing their place.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if instructions.ProviderID != "" {
		aa.accounts.SetDomainProviderID(d.Domain, instructions.ProviderID)
	}
	// Drop any cached "nobody has connected this" answer, so the domain's page
	// says "finish setting it up" straight away rather than after the TTL.
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"domain":       d.Domain,
		"site":         d.Subdomain,
		"verified":     false,
		"instructions": instructions,
	})
}

// listDomains reports the custom domains on the account's sites.
func (aa *AccountAuth) listDomains(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
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
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

// verifyDomain asks the provider whether the tenant's DNS is in place yet.
func (aa *AccountAuth) verifyDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
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
	if err := aa.accounts.VerifyDomain(d.Domain, aa.domainProvider()); err != nil {
		// Not an error in the tenant's world — DNS just isn't there yet. 409 says
		// "try again later" rather than "you did something wrong".
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "domain": d.Domain})
		return
	}
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": d.Domain, "site": d.Subdomain, "verified": true})
}

// removeDomain disconnects a custom domain. The site keeps serving at its
// <subdomain>.<baseDomain> address.
func (aa *AccountAuth) removeDomain(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
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
	// Release it at the provider first, but never let that failure strand the
	// tenant — the record goes either way.
	if err := aa.domainProvider().Detach(d); err != nil {
		log.Printf("[network] releasing %s at the provider failed: %v", d.Domain, err)
	}
	if err := aa.accounts.RemoveDomain(d.Domain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if aa.domainChanged != nil {
		aa.domainChanged(d.Domain)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ssoExchange issues a site admin session (friendo_session) for a subdomain the
// account owns. In network mode this is in-process: the network opens the site's
// DB and mints a session for the site's owner user.
func (aa *AccountAuth) ssoExchange(w http.ResponseWriter, r *http.Request) {
	acct, ok := aa.accountFromBearer(r)
	if !ok {
		aa.deny(w, r)
		return
	}
	var body struct{ Subdomain string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	sub := strings.ToLower(strings.TrimSpace(body.Subdomain))
	if owner, ok := aa.accounts.SiteOwner(sub); !ok || owner != acct.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "you don't own that site"})
		return
	}
	dir, ok := aa.reg.Dir(sub)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such site"})
		return
	}
	db, err := data.Open(dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not open site"})
		return
	}
	defer db.Conn.Close()
	user, err := db.GetUserByEmail(acct.Email)
	if err != nil {
		user, err = db.CreateUser(acct.Email, "", newToken(24), "owner")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create owner"})
			return
		}
	}
	token, err := db.CreateSession(user.ID, "", "cli-sso")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not issue session"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session": token})
}

// ensureOwnerUser makes sure the account's email is the site's owner user, so an
// SSO session can be issued for it.
func (aa *AccountAuth) ensureOwnerUser(subdomain, email string) error {
	dir, ok := aa.reg.Dir(subdomain)
	if !ok {
		return fmt.Errorf("no such site")
	}
	db, err := data.Open(dir)
	if err != nil {
		return err
	}
	defer db.Conn.Close()
	if _, err := db.GetUserByEmail(email); err == nil {
		return nil
	}
	_, err = db.CreateUser(email, "", newToken(24), "owner")
	return err
}

// --- /activate (browser side: authenticate + approve the device) ---

func (aa *AccountAuth) activateGet(w http.ResponseWriter, r *http.Request) {
	aa.render(w, activateData{Code: strings.ToUpper(r.URL.Query().Get("code"))})
}

func (aa *AccountAuth) activatePost(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	addr := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	otp := strings.TrimSpace(r.FormValue("otp"))

	if otp == "" {
		// Step 1 — send a one-time code to the email.
		devCode, err := aa.accounts.RequestOTP(addr)
		if err != nil {
			aa.render(w, activateData{Code: code, Email: addr, Error: err.Error()})
			return
		}
		// Deliver by email in production; the dev echo shows it inline locally.
		email.SendLoginCode(addr, devCode)
		data := activateData{Code: code, Email: addr, Sent: true}
		if otpEcho() {
			data.DevCode = devCode
		}
		aa.render(w, data)
		return
	}

	// Step 2 — verify the code and approve the device.
	accountID, err := aa.accounts.VerifyOTP(addr, otp)
	if err != nil {
		// The code may be fine but the account isn't allowed (invite-only) — say so
		// rather than implying the code was wrong.
		msg := "Invalid or expired code — try again."
		if errors.Is(err, ErrSignupsInviteOnly) {
			msg = "Your code was correct, but sign-ups are invite-only on this network — ask an operator to invite you."
		}
		aa.render(w, activateData{Code: code, Email: addr, Sent: true, Error: msg})
		return
	}
	if err := aa.accounts.ApproveDevice(code, accountID); err != nil {
		aa.render(w, activateData{Code: code, Email: addr, Sent: true, Error: "That device code is invalid or expired."})
		return
	}
	aa.render(w, activateData{Done: true, Email: addr})
}

type activateData struct {
	Code    string // the device user-code
	Email   string
	Sent    bool   // an OTP has been sent (show the code field)
	Done    bool   // approved
	DevCode string // dev-only echo of the OTP
	Error   string
}

func (aa *AccountAuth) render(w http.ResponseWriter, data activateData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := aa.tpl.ExecuteTemplate(w, "activate", data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// otpEcho reports whether login codes should be shown inline (dev only).
func otpEcho() bool { return os.Getenv("FRIENDO_OTP_ECHO") != "" }

const activateTemplates = `
{{define "activate"}}<!doctype html><meta charset="utf-8"><title>Link a device · friendo</title>
<style>
body{font-family:system-ui,sans-serif;max-width:26rem;margin:3rem auto;padding:0 1rem;line-height:1.5;color:#111}
h1{font-size:1.3rem}input{padding:.5rem;font:inherit;border:1px solid #d1d5db;border-radius:6px;width:100%;box-sizing:border-box;margin:.25rem 0}
button{padding:.5rem .9rem;font:inherit;border:0;border-radius:6px;background:#111;color:#fff;cursor:pointer;margin-top:.5rem}
.muted{color:#6b7280}.err{color:#b91c1c}.ok{color:#15803d}code{background:#f3f4f6;padding:.1rem .3rem;border-radius:4px}
</style>
<h1>Link a device</h1>
{{if .Done}}
  <p class="ok">✓ Device linked as <strong>{{.Email}}</strong>.</p>
  <p class="muted">You can close this tab and return to your terminal.</p>
{{else}}
  <p class="muted">Sign in to approve the code shown in your terminal.</p>
  {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
  <form method="post" action="/activate">
    <label>Device code<input name="code" value="{{.Code}}" required></label>
    <label>Email<input name="email" type="email" value="{{.Email}}" required></label>
    {{if .Sent}}
      <p class="muted">We sent a one-time code to {{.Email}}.{{if .DevCode}} <span class="muted">(dev: <code>{{.DevCode}}</code>)</span>{{end}}</p>
      <label>One-time code<input name="otp" inputmode="numeric" autocomplete="one-time-code" required autofocus></label>
      <button type="submit">Verify &amp; approve</button>
    {{else}}
      <button type="submit">Send me a code</button>
    {{end}}
  </form>
{{end}}{{end}}
`
