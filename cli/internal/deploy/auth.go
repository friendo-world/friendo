package deploy

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// authenticateSite returns a SiteClient with a valid admin session for target.
// It reuses a cached session when possible and prompts for the site admin's
// email + password otherwise, caching the resulting session in ~/.friendo/config.
func authenticateSite(target string) (*SiteClient, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// Reuse a cached session if it's still valid.
	if auth, ok := cfg.SiteAuth(target); ok && auth.Cookie != "" {
		client := NewSiteClient(target, auth.Cookie)
		if client.SessionValid() {
			return client, nil
		}
		fmt.Println("Your saved session has expired — please log in again.")
		cfg.ClearSiteAuth(target)
	}

	email, password, err := promptCredentials(target)
	if err != nil {
		return nil, err
	}

	client := NewSiteClient(target, "")
	if err := client.Login(email, password); err != nil {
		return nil, err
	}

	cfg.SetSiteAuth(target, email, client.Cookie())
	if err := cfg.Save(); err != nil {
		fmt.Printf("Warning: could not cache session: %v\n", err)
	}
	return client, nil
}

// promptCredentials reads the site admin's email and password from the terminal.
// The password is read without echo on an interactive terminal.
func promptCredentials(target string) (string, string, error) {
	fmt.Printf("Log in to %s\n", target)

	fd := int(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("reading email: %w", err)
	}
	email = strings.TrimSpace(email)

	fmt.Print("Password: ")
	var password string
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", "", fmt.Errorf("reading password: %w", err)
		}
		password = string(b)
	} else {
		// Non-interactive stdin (piped): read from the same buffered reader.
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("reading password: %w", err)
		}
		password = line
	}

	return email, strings.TrimSpace(password), nil
}
