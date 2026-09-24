package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDeviceAuthFlow drives the whole passwordless device-auth flow end-to-end:
// the CLI starts a device authorization, the browser approves it with an OTP, and
// the CLI polls for an account session token, then whoami reflects the account +
// any granted capability.
func TestDeviceAuthFlow(t *testing.T) {
	accounts, err := OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	defer accounts.Close()

	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// This test exercises device-auth, not the signup policy — let new emails in,
	// and seat an operator so the first-sign-in bootstrap doesn't fire here.
	if err := accounts.SetSignups("open"); err != nil {
		t.Fatalf("SetSignups: %v", err)
	}
	if err := accounts.Grant("root@example.com", "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	m := http.NewServeMux()
	NewAccountAuth(accounts, reg, "localhost").register(m)

	do := func(method, path, token, jsonBody string, _ url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(jsonBody))
		if jsonBody != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return rec
	}

	// 1. CLI starts device auth.
	start := do("POST", "/api/auth/device/start", "", "", nil)
	if start.Code != http.StatusOK {
		t.Fatalf("device/start = %d", start.Code)
	}
	var ds struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &ds); err != nil || ds.DeviceCode == "" || ds.UserCode == "" {
		t.Fatalf("bad device/start response: %s", start.Body.String())
	}
	if !strings.Contains(ds.VerificationURI, ds.UserCode) {
		t.Errorf("verification_uri should carry the user code: %q", ds.VerificationURI)
	}

	// Before approval, poll is pending (202).
	if code := do("POST", "/api/auth/device/poll", "", `{"device_code":"`+ds.DeviceCode+`"}`, nil).Code; code != http.StatusAccepted {
		t.Fatalf("poll before approve = %d, want 202", code)
	}

	// 2. Browser: sign in with an emailed code (cookie session), then approve the
	//    device from that session — what <friendo-activate> does on /activate.
	email := "you@example.com"
	otp, err := accounts.RequestOTP(email)
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	signin := do("POST", "/api/auth/verify-code", "", `{"email":"`+email+`","code":"`+otp+`"}`, nil)
	if signin.Code != http.StatusOK {
		t.Fatalf("verify-code = %d\n%s", signin.Code, signin.Body.String())
	}
	var cookie string
	for _, ck := range signin.Result().Cookies() {
		if ck.Name == accountCookie {
			cookie = ck.Value
		}
	}
	if cookie == "" {
		t.Fatal("verify-code set no account cookie")
	}
	approveReq := httptest.NewRequest("POST", "/api/auth/device/approve", strings.NewReader(`{"user_code":"`+ds.UserCode+`"}`))
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.AddCookie(&http.Cookie{Name: accountCookie, Value: cookie})
	approve := httptest.NewRecorder()
	m.ServeHTTP(approve, approveReq)
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), `"approved":true`) {
		t.Fatalf("approve failed: %d\n%s", approve.Code, approve.Body.String())
	}
	// A wrong device code is refused.
	bad := httptest.NewRequest("POST", "/api/auth/device/approve", strings.NewReader(`{"user_code":"NOPE-NOPE"}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.AddCookie(&http.Cookie{Name: accountCookie, Value: cookie})
	badRec := httptest.NewRecorder()
	m.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Errorf("bad user code = %d, want 400", badRec.Code)
	}

	// 3. CLI poll now returns an account session token.
	poll := do("POST", "/api/auth/device/poll", "", `{"device_code":"`+ds.DeviceCode+`"}`, nil)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll after approve = %d\n%s", poll.Code, poll.Body.String())
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(poll.Body.Bytes(), &tok); err != nil || tok.Token == "" {
		t.Fatalf("no token from poll: %s", poll.Body.String())
	}

	// 4. whoami reflects the account.
	who := do("GET", "/api/account", tok.Token, "", nil)
	if who.Code != http.StatusOK || !strings.Contains(who.Body.String(), email) {
		t.Fatalf("whoami = %d\n%s", who.Code, who.Body.String())
	}
	if strings.Contains(who.Body.String(), `"operator":true`) {
		t.Error("account should not be operator before grant")
	}

	// 5. Granting a capability shows up in whoami.
	if err := accounts.Grant(email, "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if body := do("GET", "/api/account", tok.Token, "", nil).Body.String(); !strings.Contains(body, `"operator":true`) {
		t.Errorf("expected operator capability after grant: %s", body)
	}

	// Device codes are single-use: a second poll no longer yields a token.
	if code := do("POST", "/api/auth/device/poll", "", `{"device_code":"`+ds.DeviceCode+`"}`, nil).Code; code == http.StatusOK {
		t.Error("device code should be consumed after issuing a token")
	}

	// Unauthenticated whoami is rejected.
	if code := do("GET", "/api/account", "", "", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("unauth whoami = %d, want 401", code)
	}
}
