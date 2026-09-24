package network

import (
	"fmt"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// ensureOwnerUser makes sure email is an owner user inside a site's own users
// table, so a session can be issued for it (the CLI's SSO exchange and the
// browser's "Open admin" both need one). The owner is a code-only account: it
// signs in to the site with an emailed code, or straight from its network
// account, never with a password it was never told.
//
// Shared by self-service claims and operator provisioning — a site with no
// owner user would leave its first-run setup open to whoever arrived first.
func ensureOwnerUser(reg *Registry, subdomain, email string) error {
	dir, ok := reg.Dir(subdomain)
	if !ok {
		return fmt.Errorf("no such site")
	}
	db, err := data.Open(dir)
	if err != nil {
		return err
	}
	defer db.Conn.Close()
	if _, err := db.GetUserByEmail(email); err == nil {
		return nil
	}
	_, err = db.CreateMember(email, "", "owner")
	return err
}

// linkOwner records account as the owner of a site and ensures the matching
// owner user exists inside it. The one call every provisioning path makes.
func linkOwner(accounts *Accounts, reg *Registry, subdomain string, acct *Account) error {
	if err := accounts.SetSiteOwner(subdomain, acct.ID); err != nil {
		return err
	}
	return ensureOwnerUser(reg, subdomain, acct.Email)
}

// LinkOwner is linkOwner for callers outside the package that only have an
// email — the on-box `friendo network provision --owner`. It creates the
// account if needed.
func LinkOwner(accounts *Accounts, reg *Registry, subdomain, email string) error {
	id, err := accounts.EnsureAccount(email)
	if err != nil {
		return err
	}
	acct, ok := accounts.Get(id)
	if !ok {
		return fmt.Errorf("account vanished")
	}
	return linkOwner(accounts, reg, subdomain, acct)
}
