package network

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

// operatorCookie is the operator session cookie, scoped to the apex host only.
const operatorCookie = "friendo_operator"

// Console is the operator surface served on the network's apex (the bare base
// domain): first-run setup, sign-in, and a dashboard to create / list / destroy
// sites. Per the power boundary it manages site lifecycle + resources only — it
// never reaches into the content of a site.
type Console struct {
	reg  *Registry
	ops  *Operators
	base string
	mux  *http.ServeMux
	tpl  *template.Template

	// destroy removes a site. Defaults to the registry, but a running network
	// sets it to the dispatcher's DestroySite so deletes also evict the live
	// handler. See SetDestroyer.
	destroy func(string) error
}

// NewConsole builds the operator console over a registry + operator store.
func NewConsole(reg *Registry, ops *Operators, baseDomain string) *Console {
	c := &Console{
		reg:  reg,
		ops:  ops,
		base: strings.ToLower(strings.TrimSpace(baseDomain)),
		tpl:  template.Must(template.New("console").Parse(consoleTemplates)),
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /login", c.getLogin)
	m.HandleFunc("POST /login", c.postLogin)
	m.HandleFunc("POST /setup", c.postSetup)
	m.HandleFunc("POST /logout", c.postLogout)
	m.HandleFunc("POST /sites", c.requireAuth(c.postCreateSite))
	m.HandleFunc("POST /sites/{sub}/destroy", c.requireAuth(c.postDestroySite))
	m.HandleFunc("GET /", c.requireAuth(c.getDashboard))

	// JSON operator API (for the CLI). Bearer-token or cookie auth.
	m.HandleFunc("POST /api/login", c.apiLogin)
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

func (c *Console) currentOperator(r *http.Request) (string, bool) {
	ck, err := r.Cookie(operatorCookie)
	if err != nil {
		return "", false
	}
	return c.ops.ValidateSession(ck.Value)
}

// requireAuth gates a page/action behind a valid operator session. Unauthenticated
// requests are redirected to /login (or the first-run setup it renders).
func (c *Console) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.currentOperator(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (c *Console) needsSetup() bool {
	n, err := c.ops.Count()
	return err == nil && n == 0
}

// --- handlers ---

func (c *Console) getLogin(w http.ResponseWriter, r *http.Request) {
	if c.needsSetup() {
		c.render(w, "setup", pageData{})
		return
	}
	c.render(w, "login", pageData{})
}

func (c *Console) postSetup(w http.ResponseWriter, r *http.Request) {
	// First-run only: refuse once an operator exists.
	if !c.needsSetup() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email, password := r.FormValue("email"), r.FormValue("password")
	if err := c.ops.Create(email, password); err != nil {
		c.render(w, "setup", pageData{Error: err.Error()})
		return
	}
	c.signIn(w, r, email, password)
}

func (c *Console) postLogin(w http.ResponseWriter, r *http.Request) {
	c.signIn(w, r, r.FormValue("email"), r.FormValue("password"))
}

// signIn authenticates and, on success, sets the session cookie and redirects to
// the dashboard; on failure it re-renders the login form with an error.
func (c *Console) signIn(w http.ResponseWriter, r *http.Request, email, password string) {
	id, err := c.ops.Authenticate(email, password)
	if err != nil {
		c.render(w, "login", pageData{Error: err.Error()})
		return
	}
	token, err := c.ops.StartSession(id)
	if err != nil {
		c.render(w, "login", pageData{Error: "could not start session"})
		return
	}
	setSessionCookie(w, r, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (c *Console) postLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(operatorCookie); err == nil {
		c.ops.EndSession(ck.Value)
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

func (c *Console) renderDashboard(w http.ResponseWriter, errMsg string) {
	sites, err := c.reg.Sites()
	if err != nil {
		http.Error(w, "network error", http.StatusInternalServerError)
		return
	}
	c.render(w, "dashboard", pageData{Sites: sites, Base: c.base, Error: errMsg})
}

func (c *Console) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

type pageData struct {
	Sites []Site
	Base  string
	Error string
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

// apiOperator resolves the operator from a Bearer token (CLI) or session cookie
// (browser).
func (c *Console) apiOperator(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return c.ops.ValidateSession(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	}
	return c.currentOperator(r)
}

func (c *Console) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.apiOperator(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// apiLogin exchanges operator credentials for a session token the CLI caches.
func (c *Console) apiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	id, err := c.ops.Authenticate(body.Email, body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}
	token, err := c.ops.StartSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not start session"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
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
<input name="email" type="email" placeholder="Email" required>
<input name="password" type="password" placeholder="Password" required>
<button type="submit">Sign in</button></form>{{end}}

{{define "setup"}}<!doctype html><meta charset="utf-8"><title>Set up · friendo network</title>{{template "styles"}}
<h1>friendo network</h1><p class="muted">Create the first operator</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/setup" class="row">
<input name="email" type="email" placeholder="Email" required>
<input name="password" type="password" placeholder="Password (8+ chars)" required>
<button type="submit">Create operator</button></form>{{end}}

{{define "dashboard"}}<!doctype html><meta charset="utf-8"><title>friendo network</title>{{template "styles"}}
<form method="post" action="/logout" style="float:right;margin:0"><button>Sign out</button></form>
<h1>friendo network</h1><p class="muted">{{len .Sites}} site(s)</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/sites" class="row">
<input name="subdomain" placeholder="subdomain" required>
<input name="name" placeholder="Display name (optional)">
<button type="submit">Create site</button></form>
<table><tr><th>Subdomain</th><th>Name</th><th></th></tr>
{{range .Sites}}<tr>
<td><a href="//{{.Subdomain}}.{{$.Base}}/">{{.Subdomain}}.{{$.Base}}</a></td>
<td>{{.Name}}</td>
<td><form method="post" action="/sites/{{.Subdomain}}/destroy" style="margin:0" onsubmit="return confirm('Delete {{.Subdomain}} and all its data?')"><button class="danger">Delete</button></form></td>
</tr>{{end}}
</table>{{end}}
`
