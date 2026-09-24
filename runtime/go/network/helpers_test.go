package network

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustAccounts opens an accounts store for a test, cleaned up on exit.
func mustAccounts(t *testing.T, root string) *Accounts {
	t.Helper()
	a, err := OpenAccounts(root)
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// sessionFor mints an account session token for email (no capabilities).
func sessionFor(t *testing.T, accounts *Accounts, email string) string {
	t.Helper()
	id, err := accounts.EnsureAccount(email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	tok, err := accounts.StartSession(id)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return tok
}

// operatorToken mints a session token for email after granting it operator.
func operatorToken(t *testing.T, accounts *Accounts, email string) string {
	t.Helper()
	if err := accounts.Grant(email, "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	acct, _ := accounts.GetByEmail(email)
	tok, err := accounts.StartSession(acct.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return tok
}

// testNetwork wires a dispatcher + apex router the way `friendo network serve`
// does, so tests drive the real path: Host-based dispatch, /api/* on the apex,
// the home site, /_/sso on sites.
type testNetwork struct {
	t        *testing.T
	reg      *Registry
	accounts *Accounts
	d        *Dispatcher
	aa       *AccountAuth
	op       *OperatorAPI
}

func newTestNetwork(t *testing.T) *testNetwork {
	t.Helper()
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	t.Cleanup(d.Close)
	d.SetSuspendedCheck(accounts.SiteSuspension)
	d.SetHomeSite(accounts.HomeSite)
	aa := NewAccountAuth(accounts, reg, "localhost")
	op := NewOperatorAPI(aa)
	op.SetDestroyer(d.DestroySite)
	d.SetSSOExchange(aa.ExchangeSSOCode)
	d.HandleApex(ApexRouter(aa, op))
	return &testNetwork{t: t, reg: reg, accounts: accounts, d: d, aa: aa, op: op}
}

// req drives one request through the dispatcher. token may be a Bearer token
// ("tok"), a cookie ("cookie:tok"), or empty.
func (n *testNetwork) req(method, host, path, token, body string) *httptest.ResponseRecorder {
	n.t.Helper()
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if strings.HasPrefix(token, "cookie:") {
		r.AddCookie(&http.Cookie{Name: accountCookie, Value: strings.TrimPrefix(token, "cookie:")})
	} else if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	n.d.ServeHTTP(rec, r)
	return rec
}

// api is req against the apex.
func (n *testNetwork) api(method, path, token, body string) *httptest.ResponseRecorder {
	return n.req(method, "localhost", path, token, body)
}
