// Package email delivers transactional email (today: passwordless login codes)
// via Resend. It's shared by a site's own member login and the network's
// device-auth, so the delivery path lives in exactly one place.
//
// Configure with RESEND_API_KEY + FRIENDO_EMAIL_FROM. When unset, Configured
// reports false and callers fall back to the dev OTP echo.
package email

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// Configured reports whether an email provider is wired up.
func Configured() bool {
	return os.Getenv("RESEND_API_KEY") != "" && os.Getenv("FRIENDO_EMAIL_FROM") != ""
}

// SendLoginCode delivers a one-time login code. Best-effort: a delivery error is
// logged, not returned, so a provider hiccup never leaks whether an email exists.
func SendLoginCode(to, code string) {
	if !Configured() {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"from":    os.Getenv("FRIENDO_EMAIL_FROM"),
		"to":      []string{to},
		"subject": "Your sign-in code",
		"text":    fmt.Sprintf("Your code is %s. It expires in 10 minutes.", code),
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", strings.NewReader(string(payload)))
	if err != nil {
		log.Printf("login code email: build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("RESEND_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("login code email: send: %v", err)
		return
	}
	resp.Body.Close()
}
