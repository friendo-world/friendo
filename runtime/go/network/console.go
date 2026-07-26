package network

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/email"
)

// operatorCookie is the operator session cookie, scoped to the apex host only.
const operatorCookie = "friendo_operator"

// Console is the operator surface served on the network's apex (the bare base
// domain): first-run setup, sign-in, and a dashboard to create / list / destroy
// sites. Per the power boundary it manages site lifecycle + resources only — it
// never reaches into the content of a site.
type Console struct {
	reg      *Registry
	accounts *Accounts
	base     string
	mux      *http.ServeMux
	tpl      *template.Template

	// destroy removes a site. Defaults to the registry, but a running network
	// sets it to the dispatcher's DestroySite so deletes also evict the live
	// handler. See SetDestroyer.
	destroy func(string) error
}

// NewConsole builds the operator console over a registry + the accounts store.
// Access requires an account with the operator capability; sign-in is passwordless
// (email OTP). The first sign-in claims operator when none exists yet.
func NewConsole(reg *Registry, accounts *Accounts, baseDomain string) *Console {
	c := &Console{
		reg:      reg,
		accounts: accounts,
		base:     strings.ToLower(strings.TrimSpace(baseDomain)),
		tpl:      template.Must(template.New("console").Parse(consoleTemplates)),
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /login", c.getLogin)
	m.HandleFunc("POST /login", c.postLogin)
	m.HandleFunc("POST /logout", c.postLogout)
	m.HandleFunc("POST /sites", c.requireAuth(c.postCreateSite))
	m.HandleFunc("POST /sites/{sub}/destroy", c.requireAuth(c.postDestroySite))
	m.HandleFunc("POST /signups", c.requireAuth(c.postSignups))
	m.HandleFunc("POST /invite", c.requireAuth(c.postInvite))
	m.HandleFunc("GET /", c.requireAuth(c.getDashboard))

	// JSON operator API (for the CLI). Account Bearer token + operator capability.
	m.HandleFunc("GET /api/whoami", c.requireAPIAuth(c.apiWhoami))
	m.HandleFunc("GET /api/sites", c.requireAPIAuth(c.apiListSites))
	m.HandleFunc("POST /api/sites", c.requireAPIAuth(c.apiCreateSite))
	m.HandleFunc("DELETE /api/sites/{sub}", c.requireAPIAuth(c.apiDestroySite))

	c.mux = m
	return c
}

func (c *Console) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.mux.ServeHTTP(w, r) }

// SetDestroyer routes site deletion through fn — e.g. the dispatcher's
// DestroySite, which also evicts the live handler — instead of the registry
// directly. Without it, deletes only touch disk and a cached site keeps serving.
func (c *Console) SetDestroyer(fn func(string) error) { c.destroy = fn }

// --- auth ---

// currentOperator resolves the signed-in operator from the session cookie: a
// valid account session that also holds the operator capability.
func (c *Console) currentOperator(r *http.Request) (*Account, bool) {
	ck, err := r.Cookie(operatorCookie)
	if err != nil {
		return nil, false
	}
	acct, ok := c.accounts.ValidateSession(ck.Value)
	if !ok || !acct.Has("operator") {
		return nil, false
	}
	return acct, true
}

// requireAuth gates a page/action behind a valid operator session; otherwise it
// redirects to the passwordless sign-in.
func (c *Console) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.currentOperator(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// --- handlers ---

func (c *Console) getLogin(w http.ResponseWriter, r *http.Request) {
	c.render(w, "login", pageData{})
}

// postLogin runs the two-step passwordless sign-in: step 1 emails a code, step 2
// verifies it and (if the account is an operator, or is claiming the first
// operator slot) starts the session.
func (c *Console) postLogin(w http.ResponseWriter, r *http.Request) {
	addr := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	code := strings.TrimSpace(r.FormValue("code"))

	if code == "" {
		// Step 1 — send a one-time code.
		devCode, err := c.accounts.RequestOTP(addr)
		if err != nil {
			c.render(w, "login", pageData{Error: err.Error()})
			return
		}
		email.SendLoginCode(addr, devCode)
		data := pageData{Email: addr, Sent: true}
		if otpEcho() {
			data.DevCode = devCode
		}
		c.render(w, "login", data)
		return
	}

	// Step 2 — verify. First-operator bootstrap: if no operator exists yet, let the
	// first verified email claim it. Pre-ensuring the account also lets it past the
	// invite-only signup gate, which otherwise blocks a brand-new email.
	firstRun := false
	if any, _ := c.accounts.AnyOperator(); !any {
		firstRun = true
		c.accounts.EnsureAccount(addr)
	}
	id, err := c.accounts.VerifyOTP(addr, code)
	if err != nil {
		c.render(w, "login", pageData{Email: addr, Sent: true, Error: "Invalid or expired code — try again."})
		return
	}
	acct, _ := c.accounts.Get(id)
	if acct == nil || !acct.Has("operator") {
		if firstRun {
			if err := c.accounts.Grant(addr, "operator"); err != nil {
				c.render(w, "login", pageData{Email: addr, Sent: true, Error: err.Error()})
				return
			}
		} else {
			c.render(w, "login", pageData{Error: "That account isn't an operator of this network."})
			return
		}
	}
	token, err := c.accounts.StartSession(id)
	if err != nil {
		c.render(w, "login", pageData{Error: "could not start session"})
		return
	}
	setSessionCookie(w, r, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (c *Console) postLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(operatorCookie); err == nil {
		c.accounts.EndSession(ck.Value)
	}
	clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (c *Console) getDashboard(w http.ResponseWriter, r *http.Request) {
	c.renderDashboard(w, "")
}

func (c *Console) postCreateSite(w http.ResponseWriter, r *http.Request) {
	sub := strings.ToLower(strings.TrimSpace(r.FormValue("subdomain")))
	if _, err := c.reg.Provision(sub, r.FormValue("name")); err != nil {
		c.renderDashboard(w, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (c *Console) postDestroySite(w http.ResponseWriter, r *http.Request) {
	destroy := c.reg.Destroy
	if c.destroy != nil {
		destroy = c.destroy
	}
	if err := destroy(r.PathValue("sub")); err != nil {
		c.renderDashboard(w, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// postSignups switches the network's signup policy (open ↔ invite-only).
func (c *Console) postSignups(w http.ResponseWriter, r *http.Request) {
	if err := c.accounts.SetSignups(strings.ToLower(strings.TrimSpace(r.FormValue("policy")))); err != nil {
		c.renderDashboard(w, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// postInvite pre-creates an account so it can sign in even under invite-only,
// optionally granting it the operator capability.
func (c *Console) postInvite(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if email == "" {
		c.renderDashboard(w, "email is required to invite someone")
		return
	}
	if _, err := c.accounts.EnsureAccount(email); err != nil {
		c.renderDashboard(w, err.Error())
		return
	}
	if r.FormValue("operator") != "" {
		if err := c.accounts.Grant(email, "operator"); err != nil {
			c.renderDashboard(w, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (c *Console) renderDashboard(w http.ResponseWriter, errMsg string) {
	sites, err := c.reg.Sites()
	if err != nil {
		http.Error(w, "network error", http.StatusInternalServerError)
		return
	}
	// Map each site to its owner's email for the sites table.
	owners := make(map[string]string, len(sites))
	for _, s := range sites {
		if id, ok := c.accounts.SiteOwner(s.Subdomain); ok {
			if acct, ok := c.accounts.Get(id); ok {
				owners[s.Subdomain] = acct.Email
			}
		}
	}
	accounts, _ := c.accounts.List()
	c.render(w, "dashboard", pageData{
		Sites:    sites,
		Base:     c.base,
		Error:    errMsg,
		Signups:  c.accounts.Signups(),
		Accounts: accounts,
		Owners:   owners,
	})
}

func (c *Console) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

type pageData struct {
	Sites    []Site
	Base     string
	Error    string
	Signups  string            // "open" | "invite"
	Accounts []*Account        // network accounts (for the operator view)
	Owners   map[string]string // subdomain → owner email
	// Sign-in (passwordless OTP).
	Email   string // the email a code was sent to
	Sent    bool   // a code has been sent (show the code field)
	DevCode string // dev-only echo of the code
}

// --- cookies ---

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: operatorCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureRequest(r),
		MaxAge: 7 * 24 * 3600,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: operatorCookie, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureRequest(r), MaxAge: -1,
	})
}

// --- JSON operator API (for the CLI) ---

// apiAccount resolves a valid account from a Bearer token (CLI) or session cookie
// (browser), without checking capabilities.
func (c *Console) apiAccount(r *http.Request) (*Account, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return c.accounts.ValidateSession(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	}
	if ck, err := r.Cookie(operatorCookie); err == nil {
		return c.accounts.ValidateSession(ck.Value)
	}
	return nil, false
}

// requireAPIAuth gates the operator API on a signed-in account that holds the
// operator capability, distinguishing "not signed in" (401) from "signed in but
// not an operator" (403) so the CLI can guide the user.
func (c *Console) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct, ok := c.apiAccount(r)
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

// apiWhoami confirms a token is valid (used by the CLI to check its session).
func (c *Console) apiWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"operator": true})
}

func (c *Console) apiListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := c.reg.Sites()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list sites"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

func (c *Console) apiCreateSite(w http.ResponseWriter, r *http.Request) {
	var body struct{ Subdomain, Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	site, err := c.reg.Provision(strings.ToLower(strings.TrimSpace(body.Subdomain)), body.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": site})
}

func (c *Console) apiDestroySite(w http.ResponseWriter, r *http.Request) {
	destroy := c.reg.Destroy
	if c.destroy != nil {
		destroy = c.destroy
	}
	if err := destroy(r.PathValue("sub")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// consoleTemplates holds the (deliberately minimal) operator console markup. A
// richer SPA can replace it later; this proves the surface end-to-end.
const consoleTemplates = `
{{define "styles"}}<style>
body{font-family:system-ui,sans-serif;max-width:44rem;margin:3rem auto;padding:0 1rem;line-height:1.5;color:#111}
h1{margin-bottom:.1rem}.muted{color:#6b7280}form{margin:1rem 0}
input{padding:.4rem .5rem;font:inherit;border:1px solid #d1d5db;border-radius:6px}
button{padding:.4rem .8rem;font:inherit;border:0;border-radius:6px;background:#111;color:#fff;cursor:pointer}
button.danger{background:#b91c1c}.err{color:#b91c1c}a{color:#2563eb}
table{border-collapse:collapse;width:100%;margin-top:1rem}td,th{text-align:left;padding:.4rem .5rem;border-bottom:1px solid #eee}
.row{display:flex;gap:.5rem;flex-wrap:wrap;align-items:center}
</style>{{end}}

{{define "login"}}<!doctype html><meta charset="utf-8"><title>Sign in · friendo network</title>{{template "styles"}}
<h1>friendo network</h1><p class="muted">Operator sign in</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/login" class="row">
<input name="email" type="email" placeholder="Email" value="{{.Email}}" required {{if not .Sent}}autofocus{{end}}>
{{if .Sent}}
<p class="muted" style="width:100%;margin:.25rem 0">We sent a one-time code to {{.Email}}.{{if .DevCode}} <span class="muted">(dev: <code>{{.DevCode}}</code>)</span>{{end}}</p>
<input name="code" inputmode="numeric" autocomplete="one-time-code" placeholder="One-time code" required autofocus>
<button type="submit">Verify &amp; sign in</button>
{{else}}
<button type="submit">Send code</button>
{{end}}
</form>{{end}}

{{define "dashboard"}}<!doctype html><meta charset="utf-8"><title>friendo network</title>{{template "styles"}}
<form method="post" action="/logout" style="float:right;margin:0"><button>Sign out</button></form>
<h1>friendo network</h1><p class="muted">{{len .Sites}} site(s)</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}

<h2>Sites</h2>
<form method="post" action="/sites" class="row">
<input name="subdomain" placeholder="subdomain" required>
<input name="name" placeholder="Display name (optional)">
<button type="submit">Create site</button></form>
<table><tr><th>Subdomain</th><th>Name</th><th>Owner</th><th></th></tr>
{{range .Sites}}<tr>
<td><a href="//{{.Subdomain}}.{{$.Base}}/">{{.Subdomain}}.{{$.Base}}</a></td>
<td>{{.Name}}</td>
<td class="muted">{{with index $.Owners .Subdomain}}{{.}}{{else}}—{{end}}</td>
<td><form method="post" action="/sites/{{.Subdomain}}/destroy" style="margin:0" onsubmit="return confirm('Delete {{.Subdomain}} and all its data?')"><button class="danger">Delete</button></form></td>
</tr>{{end}}
</table>

<h2>Who can join</h2>
<p class="muted">
{{if eq .Signups "open"}}<strong>Open</strong> — anyone can sign in and create a site.{{else}}<strong>Invite-only</strong> — only people you invite can sign in.{{end}}
</p>
<form method="post" action="/signups" style="margin:.25rem 0">
{{if eq .Signups "open"}}<input type="hidden" name="policy" value="invite"><button>Switch to invite-only</button>
{{else}}<input type="hidden" name="policy" value="open"><button>Switch to open</button>{{end}}
</form>

<h2>People</h2>
<form method="post" action="/invite" class="row">
<input name="email" type="email" placeholder="email to invite" required>
<label class="muted"><input type="checkbox" name="operator" value="1"> operator</label>
<button type="submit">Invite</button></form>
<table><tr><th>Email</th><th>Role</th></tr>
{{range .Accounts}}<tr>
<td>{{.Email}}</td>
<td class="muted">{{if .Has "operator"}}operator{{else}}member{{end}}</td>
</tr>{{else}}<tr><td colspan="2" class="muted">No accounts yet.</td></tr>{{end}}
</table>{{end}}
`
