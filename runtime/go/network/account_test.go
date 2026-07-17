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

	m := http.NewServeMux()
	NewAccountAuth(accounts, "localhost").register(m)

	do := func(method, path, token, jsonBody string, form url.Values) *httptest.ResponseRecorder {
		var req *http.Request
		if form != nil {
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(jsonBody))
			if jsonBody != "" {
				req.Header.Set("Content-Type", "application/json")
			}
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

	// 2. Browser: request an OTP for the email, then approve the device with it.
	email := "you@example.com"
	otp, err := accounts.RequestOTP(email)
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	approve := do("POST", "/activate", "", "", url.Values{"code": {ds.UserCode}, "email": {email}, "otp": {otp}})
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), "Device linked") {
		t.Fatalf("approve failed: %d\n%s", approve.Code, approve.Body.String())
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
	if strings.Contains(who.Body.String(), "operator") {
		t.Error("account should not be operator before grant")
	}

	// 5. Granting a capability shows up in whoami.
	if err := accounts.Grant(email, "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if body := do("GET", "/api/account", tok.Token, "", nil).Body.String(); !strings.Contains(body, "operator") {
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
