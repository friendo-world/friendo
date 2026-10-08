// Package email delivers transactional email (passwordless login codes, and
// the event reminders people ask for) via Resend. It's shared by a site's own
// member login and the network's device-auth, so the delivery path lives in
// exactly one place.
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

// EchoEnabled reports whether a login code may be returned in the API response
// instead of (only) emailed — a DEV-ONLY affordance so sign-in is testable with
// no provider. It needs an explicit FRIENDO_OTP_ECHO=1 (or true/yes/on) AND no
// email provider: once real delivery is configured the echo is off no matter
// what, so a production site never leaks codes. One rule, shared by a site's
// own sign-in and the network's.
func EchoEnabled() bool {
	if Configured() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FRIENDO_OTP_ECHO"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Send delivers one plain-text email through the provider. With none
// configured it reports that, and sends nothing.
func Send(to, subject, text string) error {
	if !Configured() {
		return fmt.Errorf("no email provider is configured")
	}
	payload, _ := json.Marshal(map[string]any{
		"from":    os.Getenv("FRIENDO_EMAIL_FROM"),
		"to":      []string{to},
		"subject": subject,
		"text":    text,
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("RESEND_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("provider answered %s", resp.Status)
	}
	return nil
}

// SendLoginCode delivers a one-time login code. Best-effort: a delivery error is
// logged, not returned, so a provider hiccup never leaks whether an email exists.
func SendLoginCode(to, code string) {
	if !Configured() {
		return
	}
	if err := Send(to, "Your sign-in code", fmt.Sprintf("Your code is %s. It expires in 10 minutes.", code)); err != nil {
		log.Printf("login code email: %v", err)
	}
}

// SendEventReminder tells someone an event they said they're coming to is
// tomorrow: its title, when, and the page to look at (link may be "").
func SendEventReminder(to, title, when, link string) error {
	text := fmt.Sprintf("Reminder: %s is coming up — %s.", title, when)
	if link != "" {
		text += "\n\n" + link
	}
	text += "\n\nYou asked for this reminder when you RSVP'd. There's nothing else coming from this address."
	return Send(to, "Reminder: "+title, text)
}
