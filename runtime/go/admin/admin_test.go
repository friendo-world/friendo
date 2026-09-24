package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// The localhost convenience must only ever fire for a request that is
// unmistakably from the same machine, with nothing in front and no provider to
// email a code — never for a site exposed through a proxy.
func TestLocalOpenRule(t *testing.T) {
	db, err := data.Open(t.TempDir())
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("FRIENDO_EMAIL_FROM", "")

	mk := func(host, remote string, hdr map[string]string) *http.Request {
		r := httptest.NewRequest("GET", "/_/api/me", nil)
		r.Host = host
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	cases := []struct {
		name string
		req  *http.Request
		env  map[string]string
		open bool
		want bool
	}{
		{"loopback + localhost", mk("localhost:3000", "127.0.0.1:5555", nil), nil, true, true},
		{"loopback + 127.0.0.1", mk("127.0.0.1:3000", "127.0.0.1:5555", nil), nil, true, true},
		{"ipv6 loopback", mk("[::1]:3000", "[::1]:5555", nil), nil, true, true},
		{"public host header", mk("example.com", "127.0.0.1:5555", nil), nil, true, false},
		{"remote peer", mk("localhost:3000", "203.0.113.9:5555", nil), nil, true, false},
		{"forwarded header", mk("localhost:3000", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "203.0.113.9"}), nil, true, false},
		{"forwarded proto", mk("localhost:3000", "127.0.0.1:5555", map[string]string{"X-Forwarded-Proto": "https"}), nil, true, false},
		{"email configured", mk("localhost:3000", "127.0.0.1:5555", nil), map[string]string{"RESEND_API_KEY": "k", "FRIENDO_EMAIL_FROM": "f"}, true, false},
		{"require-login", mk("localhost:3000", "127.0.0.1:5555", nil), nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			LocalOpen(c.open)
			defer LocalOpen(false)
			got := GetSessionUser(c.req, db) != nil
			if got != c.want {
				t.Fatalf("open = %v, want %v", got, c.want)
			}
		})
	}
}
