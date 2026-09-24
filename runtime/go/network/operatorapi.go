package network

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// OperatorAPI is the network's management surface: every lever an operator
// has, as JSON under /api/network/* (plus the original /api/sites routes older
// CLIs use). It is what `friendo network …` calls from anywhere and what the
// <friendo-console> component drives in a browser — one API, two clients.
//
// Per the power boundary it manages site lifecycle + resources only; it never
// reaches into the content of a site.
type OperatorAPI struct {
	aa *AccountAuth

	// destroy removes a site. Defaults to the registry, but a running network
	// sets it to the dispatcher's DestroySite so deletes also evict the live
	// handler. See SetDestroyer.
	destroy func(string) error
}

// NewOperatorAPI builds the operator API over the same accounts + registry the
// identity handlers use.
func NewOperatorAPI(aa *AccountAuth) *OperatorAPI {
	return &OperatorAPI{aa: aa}
}

// SetDestroyer routes site deletion through fn — e.g. the dispatcher's
// DestroySite, which also evicts the live handler. Without it, deletes only
// touch disk and a cached site keeps serving.
func (op *OperatorAPI) SetDestroyer(fn func(string) error) { op.destroy = fn }

func (op *OperatorAPI) register(m *http.ServeMux) {
	gate := op.requireOperator
	// The original operator routes, byte-for-byte for older CLIs.
	m.HandleFunc("GET /api/whoami", gate(op.whoami))
	m.HandleFunc("GET /api/sites", gate(op.listSites))
	m.HandleFunc("POST /api/sites", gate(op.createSite))
	m.HandleFunc("DELETE /api/sites/{sub}", gate(op.destroySite))

	m.HandleFunc("GET /api/network", gate(op.summary))
	m.HandleFunc("PUT /api/network/settings", gate(op.updateSettings))
	m.HandleFunc("GET /api/network/sites", gate(op.listSites))
	m.HandleFunc("POST /api/network/sites/{sub}/suspend", gate(op.suspendSite))
	m.HandleFunc("POST /api/network/sites/{sub}/resume", gate(op.resumeSite))
	m.HandleFunc("PUT /api/network/sites/{sub}/owner", gate(op.setOwner))
	m.HandleFunc("GET /api/network/accounts", gate(op.listAccounts))
	m.HandleFunc("POST /api/network/accounts/{id}/suspend", gate(op.suspendAccount))
	m.HandleFunc("POST /api/network/accounts/{id}/resume", gate(op.resumeAccount))
	m.HandleFunc("POST /api/network/accounts/{id}/signout", gate(op.signOutAccount))
	m.HandleFunc("PUT /api/network/accounts/{id}/quota", gate(op.setAccountQuota))
	m.HandleFunc("GET /api/network/invites", gate(op.listInvites))
	m.HandleFunc("POST /api/network/invites", gate(op.createInvite))
	m.HandleFunc("POST /api/network/invites/prune", gate(op.pruneInvites))
	m.HandleFunc("DELETE /api/network/invites/{email}", gate(op.revokeInvite))
	m.HandleFunc("POST /api/network/operators", gate(op.grantOperator))
	m.HandleFunc("DELETE /api/network/operators/{email}", gate(op.revokeOperator))
	m.HandleFunc("GET /api/network/domains", gate(op.listDomains))
	m.HandleFunc("POST /api/network/domains", gate(op.addDomain))
	m.HandleFunc("POST /api/network/domains/verify", gate(op.verifyDomain))
	m.HandleFunc("DELETE /api/network/domains", gate(op.removeDomain))
}

// requireOperator gates a route on a signed-in account (cookie or Bearer) that
// holds the operator capability, telling "not signed in" (401) apart from
// "signed in but not an operator" (403) so the CLI and the console can guide.
func (op *OperatorAPI) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct, ok := op.aa.accountFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in — run: friendo login <network-url>"})
			return
		}
		if !acct.Has("operator") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "your account isn't an operator of this network"})
			return
		}
		next(w, r)
	}
}

func (op *OperatorAPI) accounts() *Accounts { return op.aa.accounts }
func (op *OperatorAPI) reg() *Registry      { return op.aa.reg }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// --- summary + settings ---

func (op *OperatorAPI) whoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"operator": true})
}

func (op *OperatorAPI) summaryJSON() map[string]any {
	sites, _ := op.reg().Sites()
	accounts, _ := op.accounts().List()
	invites, _ := op.accounts().Invites()
	return map[string]any{
		"base":          op.aa.base,
		"signups":       op.accounts().Signups(),
		"default_quota": FormatQuota(op.accounts().DefaultSiteQuota()),
		"home_site":     op.accounts().HomeSite(),
		"counts":        map[string]int{"sites": len(sites), "accounts": len(accounts), "invites": len(invites)},
	}
}

func (op *OperatorAPI) summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, op.summaryJSON())
}

// updateSettings changes any of the network-wide knobs in one call. Each field
// is optional; "home_site": "" goes back to the built-in landing page.
func (op *OperatorAPI) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Signups      *string `json:"signups"`
		DefaultQuota *string `json:"default_quota"`
		HomeSite     *string `json:"home_site"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Signups != nil {
		if err := op.accounts().SetSignups(strings.ToLower(strings.TrimSpace(*body.Signups))); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	}
	if body.DefaultQuota != nil {
		n, err := ParseQuota(*body.DefaultQuota)
		if err == nil {
			err = op.accounts().SetDefaultSiteQuota(n)
		}
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	}
	if body.HomeSite != nil {
		sub := strings.ToLower(strings.TrimSpace(*body.HomeSite))
		if sub != "" {
			if _, ok := op.reg().Dir(sub); !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "no site named " + sub + " — create it first"})
				return
			}
		}
		if err := op.accounts().SetHomeSite(sub); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, op.summaryJSON())
}

// --- sites ---

func (op *OperatorAPI) siteRows() []map[string]any {
	sites, _ := op.reg().Sites()
	held, _ := op.accounts().SuspendedSites()
	out := []map[string]any{}
	for _, s := range sites {
		row := map[string]any{
			"subdomain": s.Subdomain,
			"name":      s.Name,
			"address":   s.Subdomain + "." + op.aa.base,
			"owner":     "",
			"suspended": false,
			"domains":   op.aa.domainRows([]string{s.Subdomain}),
		}
		if id, ok := op.accounts().SiteOwner(s.Subdomain); ok {
			if acct, ok := op.accounts().Get(id); ok {
				row["owner"] = acct.Email
			}
		}
		if h, ok := held[s.Subdomain]; ok {
			row["suspended"], row["reason"] = true, h.Reason
		}
		out = append(out, row)
	}
	return out
}

func (op *OperatorAPI) listSites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sites": op.siteRows()})
}

// createSite provisions a site and links an owner — the acting operator unless
// an owner email is given. Operators may take reserved names; that's how the
// network's own sites (www, docs) come to be.
func (op *OperatorAPI) createSite(w http.ResponseWriter, r *http.Request) {
	var body struct{ Subdomain, Name, Owner string }
	if !decode(w, r, &body) {
		return
	}
	actor, _ := op.aa.accountFromRequest(r)
	sub := strings.ToLower(strings.TrimSpace(body.Subdomain))
	site, err := op.reg().Provision(sub, body.Name)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	owner := actor
	if addr := strings.ToLower(strings.TrimSpace(body.Owner)); addr != "" && addr != actor.Email {
		id, err := op.accounts().EnsureAccount(addr)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		owner, _ = op.accounts().Get(id)
	}
	if err := linkOwner(op.accounts(), op.reg(), sub, owner); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": map[string]string{
		"subdomain": site.Subdomain, "name": site.Name, "owner": owner.Email,
	}})
}

func (op *OperatorAPI) destroySite(w http.ResponseWriter, r *http.Request) {
	destroy := op.reg().Destroy
	if op.destroy != nil {
		destroy = op.destroy
	}
	sub := r.PathValue("sub")
	if err := destroy(sub); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	op.accounts().RemoveSiteOwner(sub)
	if op.accounts().HomeSite() == sub {
		op.accounts().SetHomeSite("")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (op *OperatorAPI) suspendSite(w http.ResponseWriter, r *http.Request) {
	var body struct{ Reason string }
	if r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	if err := op.accounts().SuspendSite(r.PathValue("sub"), strings.TrimSpace(body.Reason)); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (op *OperatorAPI) resumeSite(w http.ResponseWriter, r *http.Request) {
	if err := op.accounts().ResumeSite(r.PathValue("sub")); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setOwner hands a site to another account (creating it if needed) and makes
// sure they have an owner user inside the site.
func (op *OperatorAPI) setOwner(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string }
	if !decode(w, r, &body) {
		return
	}
	sub := r.PathValue("sub")
	if _, ok := op.reg().Dir(sub); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such site"})
		return
	}
	id, err := op.accounts().EnsureAccount(body.Email)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	acct, _ := op.accounts().Get(id)
	if err := linkOwner(op.accounts(), op.reg(), sub, acct); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"site": sub, "owner": acct.Email})
}

// --- accounts ---

func (op *OperatorAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, _ := op.accounts().List()
	overrides, _ := op.accounts().AccountQuotas()
	held, _ := op.accounts().SuspendedAccounts()
	out := []map[string]any{}
	for _, acct := range accounts {
		used, allowed, _ := op.accounts().SiteUsage(acct)
		sites, _ := op.accounts().SitesOwnedBy(acct.ID)
		if sites == nil {
			sites = []string{}
		}
		_, override := overrides[acct.ID]
		row := map[string]any{
			"id": acct.ID, "email": acct.Email, "operator": acct.Has("operator"),
			"used": used, "allowed": allowed, "unlimited": allowed == QuotaUnlimited,
			"override": override, "sites": sites, "suspended": false,
		}
		if h, ok := held[acct.ID]; ok {
			row["suspended"], row["reason"] = true, h.Reason
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (op *OperatorAPI) suspendAccount(w http.ResponseWriter, r *http.Request) {
	var body struct{ Reason string }
	if r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if err := op.accounts().SuspendAccount(id, strings.TrimSpace(body.Reason)); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	// Suspension is immediate — ValidateSession refuses a suspended account's
	// existing sessions — and their token is left in place so what they see is
	// "suspended, contact the operator" rather than a bare "sign in again".
	writeJSON(w, http.StatusOK, map[string]any{"suspended": true})
}

func (op *OperatorAPI) resumeAccount(w http.ResponseWriter, r *http.Request) {
	if err := op.accounts().ResumeAccount(r.PathValue("id")); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// signOutAccount drops every session an account holds — the lost-laptop
// answer. They can sign straight back in; anyone holding the old token can't.
func (op *OperatorAPI) signOutAccount(w http.ResponseWriter, r *http.Request) {
	n, err := op.accounts().RevokeSessions(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ended": n})
}

// setAccountQuota sets one account's own limit ("5", "unlimited") or puts it
// back on the network default ("default" or "").
func (op *OperatorAPI) setAccountQuota(w http.ResponseWriter, r *http.Request) {
	var body struct{ Sites string }
	if !decode(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if _, ok := op.accounts().Get(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such account"})
		return
	}
	raw := strings.TrimSpace(body.Sites)
	if raw == "" || strings.EqualFold(raw, "default") {
		if err := op.accounts().ClearAccountSiteQuota(id); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account": id, "allowed": FormatQuota(op.accounts().DefaultSiteQuota()), "override": false})
		return
	}
	n, err := ParseQuota(raw)
	if err == nil {
		err = op.accounts().SetAccountSiteQuota(id, n)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": id, "allowed": FormatQuota(n), "override": true})
}

// --- invites ---

func inviteRows(list []Invite) []map[string]any {
	out := []map[string]any{}
	for _, inv := range list {
		out = append(out, map[string]any{
			"email": inv.Email, "status": inv.Status(), "expires": inv.Expires(), "invited_by": inv.InvitedBy,
		})
	}
	return out
}

func (op *OperatorAPI) listInvites(w http.ResponseWriter, r *http.Request) {
	list, _ := op.accounts().Invites()
	writeJSON(w, http.StatusOK, map[string]any{"invites": inviteRows(list)})
}

// createInvite mints an invite so someone can sign in even under invite-only.
// "operator": true grants the capability outright, which creates the account —
// an operator is a decision, not a pending invitation.
func (op *OperatorAPI) createInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Days     int    `json:"days"`
		Operator bool   `json:"operator"`
	}
	if !decode(w, r, &body) {
		return
	}
	addr := strings.ToLower(strings.TrimSpace(body.Email))
	if addr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required to invite someone"})
		return
	}
	if body.Operator {
		if err := op.accounts().Grant(addr, "operator"); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"email": addr, "operator": true})
		return
	}
	by := ""
	if actor, ok := op.aa.accountFromRequest(r); ok {
		by = actor.Email
	}
	inv, err := op.accounts().CreateInvite(addr, by, time.Duration(body.Days)*24*time.Hour)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"email": inv.Email, "status": inv.Status(), "expires": inv.Expires(), "invited_by": inv.InvitedBy,
	})
}

func (op *OperatorAPI) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := op.accounts().RevokeInvite(r.PathValue("email")); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (op *OperatorAPI) pruneInvites(w http.ResponseWriter, r *http.Request) {
	n, err := op.accounts().PruneInvites()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}

// --- operators ---

func (op *OperatorAPI) grantOperator(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string }
	if !decode(w, r, &body) {
		return
	}
	if err := op.accounts().Grant(body.Email, "operator"); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": strings.ToLower(strings.TrimSpace(body.Email)), "operator": true})
}

// revokeOperator demotes an account. Refusing to demote yourself keeps a
// network from ending up with no operator by accident.
func (op *OperatorAPI) revokeOperator(w http.ResponseWriter, r *http.Request) {
	addr := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
	if actor, ok := op.aa.accountFromRequest(r); ok && actor.Email == addr {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "you can't remove your own operator access — ask another operator"})
		return
	}
	if err := op.accounts().Revoke(addr, "operator"); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- custom domains, for any site ---

func (op *OperatorAPI) listDomains(w http.ResponseWriter, r *http.Request) {
	list, _ := op.accounts().Domains()
	out := []map[string]any{}
	for _, d := range list {
		out = append(out, map[string]any{"domain": d.Domain, "site": d.Subdomain, "verified": d.Verified, "status": d.Status()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

func (op *OperatorAPI) addDomain(w http.ResponseWriter, r *http.Request) {
	var body struct{ Domain, Site string }
	if !decode(w, r, &body) {
		return
	}
	if _, ok := op.reg().Dir(strings.ToLower(strings.TrimSpace(body.Site))); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such site"})
		return
	}
	out, status, err := op.aa.attachDomain(body.Domain, body.Site)
	if err != nil {
		fail(w, status, err)
		return
	}
	writeJSON(w, status, out)
}

func (op *OperatorAPI) verifyDomain(w http.ResponseWriter, r *http.Request) {
	var body struct{ Domain string }
	if !decode(w, r, &body) {
		return
	}
	d, found := op.accounts().GetDomain(body.Domain)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that domain isn't connected to this network"})
		return
	}
	if status, err := op.aa.checkDomain(d); err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error(), "domain": d.Domain})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": d.Domain, "site": d.Subdomain, "verified": true, "status": "live"})
}

// removeDomain disconnects a custom domain — the operator's copy of the
// tenant's own command, for when a tenant can't do it themselves.
func (op *OperatorAPI) removeDomain(w http.ResponseWriter, r *http.Request) {
	var body struct{ Domain string }
	if !decode(w, r, &body) {
		return
	}
	d, found := op.accounts().GetDomain(body.Domain)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that domain isn't connected to this network"})
		return
	}
	if err := op.aa.detachDomain(d); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
