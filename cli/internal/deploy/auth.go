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
// It reuses a cached session when possible (e.g. the one `friendo deploy` mints
// via the network's SSO exchange). For self-hosted / custom-domain sites it walks
// the user through creating the first owner via /_/api/setup, or signs in to an
// existing account — with a code sent to their email by default, or a password
// where the site allows one. The resulting session is cached in ~/.friendo/config.
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
		fmt.Println("Your saved session has expired — signing in again.")
		cfg.ClearSiteAuth(target)
	}

	client := NewSiteClient(target, "")

	status, err := client.Status()
	if err != nil {
		return nil, fmt.Errorf("contacting %s: %w", target, err)
	}

	var email string
	switch {
	case status.NeedsSetup && status.AllowsPassword():
		email, err = setupFirstAdmin(target, client)
	case status.NeedsSetup:
		email, err = setupFirstOwnerWithCode(target, client, status)
	case status.AllowsPassword():
		email, err = loginExistingAdmin(target, client)
	default:
		email, err = loginWithCode(target, client, status)
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

// setupFirstOwnerWithCode creates the first owner on an empty site by proving the
// email with a one-time code (the default: no password involved).
func setupFirstOwnerWithCode(target string, client *SiteClient, status SetupStatus) (string, error) {
	fmt.Printf("%s has no owner account yet — let's create one.\n", target)
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	name, err := readLine(reader, "Name (optional): ")
	if err != nil {
		return "", err
	}
	echoed, err := client.SetupRequestCode(email)
	if err != nil {
		return "", err
	}
	code, err := readCode(reader, email, echoed, status)
	if err != nil {
		return "", err
	}
	if err := client.SetupWithCode(email, name, code); err != nil {
		return "", err
	}
	fmt.Println("Owner account created.")
	return email, nil
}

// loginWithCode signs in to an existing account with an emailed code.
func loginWithCode(target string, client *SiteClient, status SetupStatus) (string, error) {
	fmt.Printf("Sign in to %s\n", target)
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	echoed, err := client.RequestCode(email)
	if err != nil {
		return "", err
	}
	code, err := readCode(reader, email, echoed, status)
	if err != nil {
		return "", err
	}
	if err := client.VerifyCode(email, code); err != nil {
		return "", err
	}
	return email, nil
}

// readCode explains where the code went and reads it back. In local dev the
// server echoes the code, so the prompt just confirms it rather than making the
// person go looking.
func readCode(reader *bufio.Reader, email, echoed string, status SetupStatus) (string, error) {
	switch {
	case echoed != "":
		fmt.Printf("No email provider is set up on that site, so it handed the code straight back: %s\n", echoed)
	case status.EmailsCodes():
		fmt.Printf("We emailed a 6-digit code to %s.\n", email)
	default:
		fmt.Println("No email provider is set up on that site — the code is printed in the terminal running it.")
	}
	code, err := readLine(reader, "Code: ")
	if err != nil {
		return "", err
	}
	if echoed != "" && code == "" {
		code = echoed
	}
	return code, nil
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
