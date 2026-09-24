package network

import (
	"fmt"
	"time"
)

// Suspension is the abuse lever the network had no answer to before: a way to
// stop an account or a site without destroying anything. `friendo network destroy`
// is irreversible; a suspension is a pause an operator can undo.
//
// Two kinds, stored separately so neither needs a migration:
//
//	account_suspensions — the person can't sign in, claim sites, or use an
//	                      existing session (see ValidateSession)
//	site_suspensions    — the site stops serving; visitors get a hold page
//
// Neither deletes data. A suspended account's sites keep their files and rows.

// Suspension records why something is on hold and since when.
type Suspension struct {
	Reason string
	Since  time.Time
}

// ErrAccountSuspended is returned when a suspended account tries to sign in.
var ErrAccountSuspended = fmt.Errorf("this account is suspended on this network — contact the operator")

// SuspendAccount puts an account on hold. Operators are refused: suspending one
// could lock the last operator out of their own network, so they must be demoted
// first (friendo network operator revoke).
func (a *Accounts) SuspendAccount(accountID, reason string) error {
	acct, ok := a.Get(accountID)
	if !ok {
		return fmt.Errorf("no such account")
	}
	if acct.Has("operator") {
		return fmt.Errorf("%s is an operator — remove that first: friendo network operator revoke %s", acct.Email, acct.Email)
	}
	_, err := a.conn.Exec(
		`INSERT INTO account_suspensions (account_id, reason, created) VALUES (?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET reason = excluded.reason`,
		accountID, reason, nowRFC(),
	)
	return err
}

// ResumeAccount lifts an account suspension.
func (a *Accounts) ResumeAccount(accountID string) error {
	_, err := a.conn.Exec(`DELETE FROM account_suspensions WHERE account_id = ?`, accountID)
	return err
}

// AccountSuspension reports whether an account is on hold, and why.
func (a *Accounts) AccountSuspension(accountID string) (Suspension, bool) {
	return a.suspension(`SELECT reason, created FROM account_suspensions WHERE account_id = ?`, accountID)
}

// SuspendedAccounts returns every account suspension, keyed by account id — one
// query for the console's People table rather than one per row.
func (a *Accounts) SuspendedAccounts() (map[string]Suspension, error) {
	return a.suspensions(`SELECT account_id, reason, created FROM account_suspensions`)
}

// SuspendSite takes a site off the air without deleting it. The dispatcher serves
// a hold page in its place (see Dispatcher.SetSuspendedCheck).
func (a *Accounts) SuspendSite(subdomain, reason string) error {
	if !ValidSubdomain(subdomain) {
		return fmt.Errorf("invalid subdomain %q", subdomain)
	}
	_, err := a.conn.Exec(
		`INSERT INTO site_suspensions (subdomain, reason, created) VALUES (?, ?, ?)
		 ON CONFLICT(subdomain) DO UPDATE SET reason = excluded.reason`,
		subdomain, reason, nowRFC(),
	)
	return err
}

// ResumeSite puts a suspended site back on the air.
func (a *Accounts) ResumeSite(subdomain string) error {
	_, err := a.conn.Exec(`DELETE FROM site_suspensions WHERE subdomain = ?`, subdomain)
	return err
}

// SiteSuspension reports whether a site is on hold, and why. Its signature matches
// what Dispatcher.SetSuspendedCheck wants.
func (a *Accounts) SiteSuspension(subdomain string) (Suspension, bool) {
	return a.suspension(`SELECT reason, created FROM site_suspensions WHERE subdomain = ?`, subdomain)
}

// SuspendedSites returns every site suspension, keyed by subdomain.
func (a *Accounts) SuspendedSites() (map[string]Suspension, error) {
	return a.suspensions(`SELECT subdomain, reason, created FROM site_suspensions`)
}

// SuspensionForSession explains a token that stopped working: it belongs to a
// suspended account rather than having expired. Lets the API say which.
func (a *Accounts) SuspensionForSession(token string) (Suspension, bool) {
	acct, ok := a.sessionAccount(token)
	if !ok {
		return Suspension{}, false
	}
	return a.AccountSuspension(acct.ID)
}

// RevokeSessions drops every session an account holds and reports how many went.
// This is the answer to "I lost my laptop" that passwordless auth otherwise has
// none for — the next sign-in mints a fresh session.
func (a *Accounts) RevokeSessions(accountID string) (int, error) {
	res, err := a.conn.Exec(`DELETE FROM account_sessions WHERE account_id = ?`, accountID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (a *Accounts) suspension(query, arg string) (Suspension, bool) {
	var reason, created string
	if err := a.conn.QueryRow(query, arg).Scan(&reason, &created); err != nil {
		return Suspension{}, false
	}
	since, _ := time.Parse(rfc3339Z, created)
	return Suspension{Reason: reason, Since: since}, true
}

func (a *Accounts) suspensions(query string) (map[string]Suspension, error) {
	rows, err := a.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Suspension{}
	for rows.Next() {
		var key, reason, created string
		if rows.Scan(&key, &reason, &created) == nil {
			since, _ := time.Parse(rfc3339Z, created)
			out[key] = Suspension{Reason: reason, Since: since}
		}
	}
	return out, nil
}

// SuspensionMessage is what a suspended person reads. It says what happened and
// who can undo it, because nothing they do on their own will fix it.
func SuspensionMessage(s Suspension) string {
	if s.Reason != "" {
		return "This account is suspended on this network: " + s.Reason + ". Contact the operator to lift it."
	}
	return "This account is suspended on this network. Contact the operator to lift it."
}
