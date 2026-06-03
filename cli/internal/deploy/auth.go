package deploy

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// authenticateSite returns a SiteClient with a valid admin session for target.
//
// It reuses a cached session when possible. Otherwise, if the site has no admin
// yet (a freshly provisioned/deployed site), it walks the user through creating
// the first admin via /_/api/setup; if the site already has an admin, it prompts
// for email + password. The resulting session is cached in ~/.friendo/config.
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

	client := NewSiteClient(target, "")

	needsSetup, err := client.NeedsSetup()
	if err != nil {
		return nil, fmt.Errorf("contacting %s: %w", target, err)
	}

	var email string
	if needsSetup {
		email, err = setupFirstAdmin(target, client)
	} else {
		email, err = loginExistingAdmin(target, client)
	}
	if err != nil {
		return nil, err
	}

	cfg.SetSiteAuth(target, email, client.Cookie())
	if err := cfg.Save(); err != nil {
		fmt.Printf("Warning: could not cache session: %v\n", err)
	}
	return client, nil
}

// setupFirstAdmin creates the first admin account on an empty site.
func setupFirstAdmin(target string, client *SiteClient) (string, error) {
	fmt.Printf("%s has no admin account yet — let's create one.\n", target)

	fd := int(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	name, err := readLine(reader, "Name (optional): ")
	if err != nil {
		return "", err
	}
	password, err := readPassword(reader, fd, "Password (min 8 chars): ")
	if err != nil {
		return "", err
	}

	if err := client.Setup(email, name, password); err != nil {
		return "", err
	}
	fmt.Println("Admin account created.")
	return email, nil
}

// loginExistingAdmin authenticates against a site that already has an admin.
func loginExistingAdmin(target string, client *SiteClient) (string, error) {
	fmt.Printf("Log in to %s\n", target)

	fd := int(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	password, err := readPassword(reader, fd, "Password: ")
	if err != nil {
		return "", err
	}

	if err := client.Login(email, password); err != nil {
		return "", err
	}
	return email, nil
}

// readLine reads and trims a single echoed line from the reader.
func readLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// readPassword reads a password without echo on an interactive terminal,
// falling back to the buffered reader for piped (non-interactive) stdin.
func readPassword(reader *bufio.Reader, fd int, prompt string) (string, error) {
	fmt.Print(prompt)
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return strings.TrimSpace(line), nil
}
