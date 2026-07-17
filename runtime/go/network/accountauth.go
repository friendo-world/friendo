package network

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"
)

// AccountAuth serves the network's identity surface: passwordless device-auth for
// the CLI (a browser /activate page + OTP) and the account whoami endpoint.
// See design/network-accounts.md.
type AccountAuth struct {
	accounts *Accounts
	base     string
	tpl      *template.Template
}

// NewAccountAuth builds the identity handlers over an accounts store.
func NewAccountAuth(accounts *Accounts, baseDomain string) *AccountAuth {
	return &AccountAuth{
		accounts: accounts,
		base:     strings.ToLower(strings.TrimSpace(baseDomain)),
		tpl:      template.Must(template.New("activate").Parse(activateTemplates)),
	}
}

// register mounts the identity routes on a mux.
func (aa *AccountAuth) register(m *http.ServeMux) {
	m.HandleFunc("POST /api/auth/device/start", aa.deviceStart)
	m.HandleFunc("POST /api/auth/device/poll", aa.devicePoll)
	m.HandleFunc("GET /activate", aa.activateGet)
	m.HandleFunc("POST /activate", aa.activatePost)
	m.HandleFunc("GET /api/account", aa.whoami)
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": acct.Email, "capabilities": acct.Capabilities})
}

func (aa *AccountAuth) accountFromBearer(r *http.Request) (*Account, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, false
	}
	return aa.accounts.ValidateSession(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
}

// --- /activate (browser side: authenticate + approve the device) ---

func (aa *AccountAuth) activateGet(w http.ResponseWriter, r *http.Request) {
	aa.render(w, activateData{Code: strings.ToUpper(r.URL.Query().Get("code"))})
}

func (aa *AccountAuth) activatePost(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	otp := strings.TrimSpace(r.FormValue("otp"))

	if otp == "" {
		// Step 1 — send a one-time code to the email.
		devCode, err := aa.accounts.RequestOTP(email)
		if err != nil {
			aa.render(w, activateData{Code: code, Email: email, Error: err.Error()})
			return
		}
		// TODO(email): deliver devCode via Resend in production. For now it's shown
		// inline only when the dev OTP echo is on.
		data := activateData{Code: code, Email: email, Sent: true}
		if otpEcho() {
			data.DevCode = devCode
		}
		aa.render(w, data)
		return
	}

	// Step 2 — verify the code and approve the device.
	accountID, err := aa.accounts.VerifyOTP(email, otp)
	if err != nil {
		aa.render(w, activateData{Code: code, Email: email, Sent: true, Error: "Invalid or expired code — try again."})
		return
	}
	if err := aa.accounts.ApproveDevice(code, accountID); err != nil {
		aa.render(w, activateData{Code: code, Email: email, Sent: true, Error: "That device code is invalid or expired."})
		return
	}
	aa.render(w, activateData{Done: true, Email: email})
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
