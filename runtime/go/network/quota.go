package network

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Quotas cap what one account can create on a network, so opening signups can't
// cost the operator unbounded money. The first cut caps the *number of sites* an
// account may own; storage accounting is a separate decision (see the v0.4 roadmap).
//
// Two levels, override wins:
//
//	network_settings["quota.sites_per_account"]  — the default for everyone
//	account_quotas(account_id, sites)            — a per-account override
//
// Accounts holding "operator" are never capped — they run the network.

// QuotaUnlimited is the cap value meaning "no limit". Stored as 0 so a plain
// integer column can express it without a second nullable column.
const QuotaUnlimited = 0

// defaultSitesPerAccount is the built-in cap used until an operator sets one.
// Deliberately small: a network that opens signups should start conservative,
// and raising it is a one-line change no one has to recompile for.
const defaultSitesPerAccount = 3

const quotaSitesKey = "quota.sites_per_account"

// DefaultSiteQuota reports how many sites an account may own by default.
func (a *Accounts) DefaultSiteQuota() int {
	var v string
	if err := a.conn.QueryRow(`SELECT value FROM network_settings WHERE key = ?`, quotaSitesKey).Scan(&v); err != nil {
		return defaultSitesPerAccount
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return defaultSitesPerAccount
	}
	return n
}

// SetDefaultSiteQuota sets the network-wide default cap. Pass QuotaUnlimited (0)
// to lift it entirely.
func (a *Accounts) SetDefaultSiteQuota(n int) error {
	if n < 0 {
		return fmt.Errorf("a site limit can't be negative (use 0 for unlimited)")
	}
	_, err := a.conn.Exec(
		`INSERT INTO network_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, quotaSitesKey, strconv.Itoa(n),
	)
	return err
}

// AccountSiteQuota returns an account's per-account override, if it has one.
func (a *Accounts) AccountSiteQuota(accountID string) (int, bool) {
	var n int
	if err := a.conn.QueryRow(`SELECT sites FROM account_quotas WHERE account_id = ?`, accountID).Scan(&n); err != nil {
		return 0, false
	}
	return n, true
}

// SetAccountSiteQuota lifts (or lowers) the cap for one account without touching
// the network-wide default.
func (a *Accounts) SetAccountSiteQuota(accountID string, n int) error {
	if n < 0 {
		return fmt.Errorf("a site limit can't be negative (use 0 for unlimited)")
	}
	_, err := a.conn.Exec(
		`INSERT INTO account_quotas (account_id, sites) VALUES (?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET sites = excluded.sites`, accountID, n,
	)
	return err
}

// ClearAccountSiteQuota drops an override so the account follows the default again.
func (a *Accounts) ClearAccountSiteQuota(accountID string) error {
	_, err := a.conn.Exec(`DELETE FROM account_quotas WHERE account_id = ?`, accountID)
	return err
}

// AccountQuotas returns every per-account override, keyed by account id.
func (a *Accounts) AccountQuotas() (map[string]int, error) {
	rows, err := a.conn.Query(`SELECT account_id, sites FROM account_quotas`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out, nil
}

// CountSitesOwnedBy reports how many sites an account owns.
func (a *Accounts) CountSitesOwnedBy(accountID string) (int, error) {
	var n int
	err := a.conn.QueryRow(`SELECT COUNT(*) FROM site_owners WHERE account_id = ?`, accountID).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// SiteUsage reports what an account has used and what it's allowed: the override
// if it has one, otherwise the network default — and QuotaUnlimited for operators.
func (a *Accounts) SiteUsage(acct *Account) (used, allowed int, err error) {
	used, err = a.CountSitesOwnedBy(acct.ID)
	if err != nil {
		return 0, 0, err
	}
	if acct.Has("operator") {
		return used, QuotaUnlimited, nil
	}
	if n, ok := a.AccountSiteQuota(acct.ID); ok {
		return used, n, nil
	}
	return used, a.DefaultSiteQuota(), nil
}

// AtSiteLimit reports whether an account has no room for another site.
func (a *Accounts) AtSiteLimit(acct *Account) (bool, int, int) {
	used, allowed, err := a.SiteUsage(acct)
	if err != nil {
		// Fail open rather than locking everyone out on a read error; a broken
		// count shouldn't be indistinguishable from a hit cap.
		return false, 0, QuotaUnlimited
	}
	return allowed != QuotaUnlimited && used >= allowed, used, allowed
}

// SiteLimitMessage is what someone reads when they hit the cap. It is the only
// explanation most people will get, so it names the limit and both ways out.
func SiteLimitMessage(used, allowed int) string {
	return fmt.Sprintf(
		"You've used all %d of the %s your account can have on this network (%d of %d). "+
			"Delete one you no longer need, or ask an operator to raise your limit.",
		allowed, plural(allowed, "site", "sites"), used, allowed,
	)
}

// FormatQuota renders a cap for humans — "unlimited" reads better than "0".
func FormatQuota(allowed int) string {
	if allowed == QuotaUnlimited {
		return "unlimited"
	}
	return strconv.Itoa(allowed)
}

// ParseQuota reads a cap written by a human: a number, or "unlimited"/"none".
func ParseQuota(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "unlimited" || s == "none" || s == "off" {
		return QuotaUnlimited, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q isn't a site limit — use a number, or 'unlimited'", s)
	}
	return n, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
