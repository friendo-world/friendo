package network

import (
	"net/http"
	"strings"
	"testing"
)

// TestBrowserSignIn: the JSON sign-in the components use. The first verified
// email claims operator (bootstrap, even under invite-only); after that a
// non-operator still gets a session (the account page is for everyone) but the
// operator API says no; and wrong guesses are capped.
func TestBrowserSignIn(t *testing.T) {
	n := newTestNetwork(t)

	code, err := n.accounts.RequestOTP("boss@x.com")
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	rec := n.api("POST", "/api/auth/verify-code", "", `{"email":"Boss@X.com","code":"`+code+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"first_operator":true`) {
		t.Fatalf("bootstrap sign-in = %d\n%s", rec.Code, rec.Body.String())
	}
	var cookie string
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == accountCookie {
			cookie = ck.Value
		}
	}
	if cookie == "" {
		t.Fatal("no account cookie set")
	}
	if a, _ := n.accounts.GetByEmail("boss@x.com"); !a.Has("operator") {
		t.Error("first sign-in did not claim operator")
	}
	if code := n.api("GET", "/api/network", "cookie:"+cookie, "").Code; code != http.StatusOK {
		t.Errorf("operator cookie on /api/network = %d, want 200", code)
	}

	// Invite-only now blocks a stranger, even with the right code.
	code2, _ := n.accounts.RequestOTP("stranger@x.com")
	rec = n.api("POST", "/api/auth/verify-code", "", `{"email":"stranger@x.com","code":"`+code2+`"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "invite-only") {
		t.Fatalf("stranger under invite-only = %d\n%s", rec.Code, rec.Body.String())
	}

	// An invited non-operator signs in and can use the account API, not the operator API.
	n.accounts.CreateInvite("guest@x.com", "boss@x.com", 0)
	code3, _ := n.accounts.RequestOTP("guest@x.com")
	rec = n.api("POST", "/api/auth/verify-code", "", `{"email":"guest@x.com","code":"`+code3+`"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"operator":true`) {
		t.Fatalf("guest sign-in = %d\n%s", rec.Code, rec.Body.String())
	}
	var guest string
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == accountCookie {
			guest = ck.Value
		}
	}
	if code := n.api("GET", "/api/account/sites", "cookie:"+guest, "").Code; code != http.StatusOK {
		t.Errorf("guest /api/account/sites = %d, want 200", code)
	}
	if code := n.api("GET", "/api/network", "cookie:"+guest, "").Code; code != http.StatusForbidden {
		t.Errorf("guest /api/network = %d, want 403", code)
	}

	// Logout ends the session.
	n.api("POST", "/api/auth/logout", "cookie:"+guest, "")
	if code := n.api("GET", "/api/account", "cookie:"+guest, "").Code; code != http.StatusUnauthorized {
		t.Errorf("after logout = %d, want 401", code)
	}

	// Request-code throttle and the wrong-guess cap.
	if rec := n.api("POST", "/api/auth/request-code", "", `{"email":"guest@x.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("request-code = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := n.api("POST", "/api/auth/request-code", "", `{"email":"guest@x.com"}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("immediate resend = %d, want 429", rec.Code)
	}
	for i := 0; i < otpMaxAttempts-1; i++ {
		if rec := n.api("POST", "/api/auth/verify-code", "", `{"email":"guest@x.com","code":"000000"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong guess %d = %d, want 401", i, rec.Code)
		}
	}
	if rec := n.api("POST", "/api/auth/verify-code", "", `{"email":"guest@x.com","code":"000000"}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("guess past the cap = %d, want 429", rec.Code)
	}
	if rec := n.api("POST", "/api/auth/request-code", "", `{"email":"not-an-email"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad email = %d, want 400", rec.Code)
	}
}

// TestReservedSubdomains: self-service can't take the network's own names;
// operators can; an owned reserved site still re-claims idempotently.
func TestReservedSubdomains(t *testing.T) {
	n := newTestNetwork(t)
	n.accounts.SetSignups("open")
	member := sessionFor(t, n.accounts, "member@x.com")
	rec := n.api("POST", "/api/account/sites", member, `{"subdomain":"www"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reserved") {
		t.Fatalf("member claiming www = %d\n%s", rec.Code, rec.Body.String())
	}
	if _, ok := n.reg.Dir("www"); ok {
		t.Fatal("www was provisioned for a member")
	}
	op := operatorToken(t, n.accounts, "op@x.com")
	if rec := n.api("POST", "/api/account/sites", op, `{"subdomain":"www"}`); rec.Code != http.StatusCreated {
		t.Fatalf("operator claiming www = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := n.api("POST", "/api/account/sites", op, `{"subdomain":"www"}`); rec.Code != http.StatusOK {
		t.Errorf("operator re-claiming own www = %d, want 200", rec.Code)
	}
	if rec := n.api("POST", "/api/account/sites", member, `{"subdomain":"docs"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("member claiming docs = %d, want 400", rec.Code)
	}
	if rec := n.api("POST", "/api/account/sites", member, `{"subdomain":"my-blog"}`); rec.Code != http.StatusCreated {
		t.Errorf("member claiming an ordinary name = %d, want 201", rec.Code)
	}
	// The list carries the address, existence and quota.
	body := n.api("GET", "/api/account/sites", member, "").Body.String()
	if !strings.Contains(body, `"address":"my-blog.localhost"`) || !strings.Contains(body, `"used":1`) {
		t.Errorf("account sites list = %s", body)
	}
}
