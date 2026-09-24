package network

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Custom domains let a tenant serve their site at their own address instead of
// <subdomain>.<baseDomain>. Three things have to be true before that can happen,
// and they are deliberately separate:
//
//  1. the domain is recorded against a site the account owns  (AddDomain)
//  2. the tenant has proved they control it                   (VerifyDomain)
//  3. only then does it route, and only then may it get a cert
//
// Step 2 is the one that matters. Without it, anyone could claim a domain they
// don't own and quietly wait for its DNS to point here — so an unverified domain
// never routes and never gets a certificate.
//
// *How* a domain proves itself and gets its certificate is a DomainProvider
// (see domainprovider.go): Cloudflare for SaaS on friendo.world, a plain DNS TXT
// check for a self-hosted network. This file only owns the records.

// domainChallengePrefix is the label a tenant adds a TXT record under. Its own
// subdomain rather than the apex, so it can't collide with SPF or anything else
// already living on their apex TXT record.
const domainChallengePrefix = "_friendo-challenge"

// Domain is a custom hostname pointed at one site.
type Domain struct {
	Domain    string
	Subdomain string
	Token     string // our own challenge value (the DNS provider uses it)
	// ProviderID is the provider's opaque handle for this domain — a Cloudflare
	// custom-hostname id, say. Empty for providers that don't need one.
	ProviderID string
	Verified   bool
	Created    time.Time
}

// RecordName is the DNS name the tenant creates a TXT record at.
func (d Domain) RecordName() string { return domainChallengePrefix + "." + d.Domain }

// Status describes a domain for a human.
func (d Domain) Status() string {
	if d.Verified {
		return "live"
	}
	return "waiting for DNS"
}

// domainRe is a conservative hostname check: dotted labels, letters/digits/dashes,
// no leading or trailing dash. Anything stranger is refused rather than guessed at.
var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// NormalizeDomain lowercases and trims a hostname and checks it's usable as one.
func NormalizeDomain(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".") // a trailing dot is legal DNS, awkward here
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "", fmt.Errorf("a domain is required")
	}
	if net.ParseIP(s) != nil {
		return "", fmt.Errorf("%q is an IP address — use a domain name", s)
	}
	if !domainRe.MatchString(s) {
		return "", fmt.Errorf("%q doesn't look like a domain (try something like example.com)", s)
	}
	return s, nil
}

// AddDomain records a custom domain against a site, unverified. Re-adding a
// domain you already hold returns the existing record with its token intact, so
// someone who lost the instructions can just ask again.
func (a *Accounts) AddDomain(domain, subdomain string) (Domain, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return Domain{}, err
	}
	if !ValidSubdomain(subdomain) {
		return Domain{}, fmt.Errorf("invalid site %q", subdomain)
	}
	if existing, ok := a.GetDomain(domain); ok {
		if existing.Subdomain != subdomain {
			return Domain{}, fmt.Errorf("%s is already connected to another site on this network", domain)
		}
		return existing, nil
	}
	d := Domain{
		Domain:    domain,
		Subdomain: subdomain,
		Token:     newToken(16),
		Created:   time.Now().UTC(),
	}
	if _, err := a.conn.Exec(
		`INSERT INTO site_domains (domain, subdomain, token, verified, created) VALUES (?, ?, ?, 0, ?)`,
		d.Domain, d.Subdomain, d.Token, d.Created.Format(rfc3339Z),
	); err != nil {
		return Domain{}, err
	}
	return d, nil
}

// GetDomain returns a custom domain record.
func (a *Accounts) GetDomain(domain string) (Domain, bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var sub, token, providerID, created string
	var verified int
	if err := a.conn.QueryRow(
		`SELECT subdomain, token, provider_id, verified, created FROM site_domains WHERE domain = ?`, domain,
	).Scan(&sub, &token, &providerID, &verified, &created); err != nil {
		return Domain{}, false
	}
	t, _ := time.Parse(rfc3339Z, created)
	return Domain{
		Domain: domain, Subdomain: sub, Token: token, ProviderID: providerID,
		Verified: verified != 0, Created: t,
	}, true
}

// DomainsForSite lists the custom domains pointed at one site.
func (a *Accounts) DomainsForSite(subdomain string) ([]Domain, error) {
	return a.domains(`SELECT domain, subdomain, token, provider_id, verified, created FROM site_domains
	                  WHERE subdomain = ? ORDER BY domain`, subdomain)
}

// Domains lists every custom domain on the network — the operator's view.
func (a *Accounts) Domains() ([]Domain, error) {
	return a.domains(`SELECT domain, subdomain, token, provider_id, verified, created FROM site_domains ORDER BY domain`)
}

// RemoveDomain disconnects a custom domain. The site is untouched; it keeps
// serving at its <subdomain>.<baseDomain> address.
func (a *Accounts) RemoveDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	res, err := a.conn.Exec(`DELETE FROM site_domains WHERE domain = ?`, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s isn't connected to this network", domain)
	}
	return nil
}

// SiteForDomain resolves a request host to a site — but only for a *verified*
// domain. This is the dispatcher's fallback when a host isn't under the base
// domain, and the reason an unverified claim can never steal traffic.
func (a *Accounts) SiteForDomain(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	var sub string
	if err := a.conn.QueryRow(
		`SELECT subdomain FROM site_domains WHERE domain = ? AND verified = 1`, host,
	).Scan(&sub); err != nil {
		return "", false
	}
	return sub, true
}

// VerifyDomain asks the provider whether a domain is ready and, if it is, marks
// it verified — which is what makes it route. Asking again once it's live is not
// an error; people re-run this when they're unsure it worked.
func (a *Accounts) VerifyDomain(domain string, p DomainProvider) error {
	d, ok := a.GetDomain(domain)
	if !ok {
		return fmt.Errorf("%s hasn't been added yet — run: friendo domain add %s", domain, domain)
	}
	if d.Verified {
		return nil
	}
	ready, detail, err := p.Check(d)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%s isn't ready yet: %s", d.Domain, detail)
	}
	return a.MarkDomainVerified(d.Domain)
}

// SetDomainProviderID records the provider's handle for a domain.
func (a *Accounts) SetDomainProviderID(domain, id string) error {
	_, err := a.conn.Exec(`UPDATE site_domains SET provider_id = ? WHERE domain = ?`,
		id, strings.ToLower(strings.TrimSpace(domain)))
	return err
}

// MarkDomainVerified flips a domain live.
func (a *Accounts) MarkDomainVerified(domain string) error {
	_, err := a.conn.Exec(`UPDATE site_domains SET verified = 1 WHERE domain = ?`,
		strings.ToLower(strings.TrimSpace(domain)))
	return err
}

func (a *Accounts) domains(query string, args ...any) ([]Domain, error) {
	rows, err := a.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		var domain, sub, token, providerID, created string
		var verified int
		if rows.Scan(&domain, &sub, &token, &providerID, &verified, &created) == nil {
			t, _ := time.Parse(rfc3339Z, created)
			out = append(out, Domain{
				Domain: domain, Subdomain: sub, Token: token, ProviderID: providerID,
				Verified: verified != 0, Created: t,
			})
		}
	}
	return out, nil
}
