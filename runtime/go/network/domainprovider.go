package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// A DomainProvider is how a custom domain proves it belongs to the tenant and
// gets its certificate. It's a seam for the same reason the email sender is one:
// friendo.world and a self-hosted network are not in the same situation.
//
//   - CloudflareProvider — Cloudflare for SaaS. Cloudflare validates ownership
//     and issues/renews the certificate; the network process never touches TLS.
//     This is friendo.world's path, and it's the one that shares plumbing with
//     selling domains later.
//   - DNSProvider — the default. A TXT record we check ourselves, for a network
//     where TLS is already handled by whatever sits in front.
//
// Whichever is in play, the rule above it doesn't move: a domain routes only
// once it's verified.
type DomainProvider interface {
	// Name identifies the provider in logs and the console.
	Name() string
	// Attach registers a newly added domain and returns what the tenant has to do.
	Attach(d Domain) (Instructions, error)
	// Check reports whether the domain is ready to serve. When it isn't, detail
	// says what's still missing, in words a tenant can act on.
	Check(d Domain) (ready bool, detail string, err error)
	// Detach releases a domain that's been disconnected. Best-effort: a failure
	// here must not stop the domain being removed from the network.
	Detach(d Domain) error
}

// DNSRecord is one row of "add this at your registrar".
type DNSRecord struct {
	Type  string `json:"type"` // TXT | CNAME
	Name  string `json:"name"`
	Value string `json:"value"`
	Why   string `json:"why"` // plain-language reason — most people have never done this
}

// Instructions is everything a tenant needs after adding a domain.
type Instructions struct {
	Records    []DNSRecord `json:"records"`
	Note       string      `json:"note,omitempty"`
	ProviderID string      `json:"-"` // stored against the domain, never shown
}

// Text renders instructions for a terminal.
func (in Instructions) Text(domain string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "To finish connecting %s, add these records with whoever you bought the domain from:\n", domain)
	for i, r := range in.Records {
		fmt.Fprintf(&b, "\n  %d. %s record — %s\n       Name:  %s\n       Value: %s\n", i+1, r.Type, r.Why, r.Name, r.Value)
	}
	if in.Note != "" {
		fmt.Fprintf(&b, "\n%s\n", in.Note)
	}
	fmt.Fprintf(&b, "\nDNS can take a few minutes (occasionally a few hours) to travel. Then run:\n  friendo domain verify %s\n", domain)
	return b.String()
}

// --- the self-hosted default: a TXT record we check ourselves ---

// DNSProvider verifies a domain with a TXT record and leaves TLS to whatever is
// already terminating it. The right choice for a self-hosted network.
type DNSProvider struct {
	BaseDomain string
	// Lookup resolves TXT records. nil means real DNS; tests pass their own.
	Lookup func(name string) ([]string, error)
}

func (p *DNSProvider) Name() string { return "dns" }

func (p *DNSProvider) Attach(d Domain) (Instructions, error) {
	return Instructions{Records: []DNSRecord{
		{Type: "TXT", Name: d.RecordName(), Value: d.Token, Why: "proves the domain is yours"},
		{Type: "CNAME", Name: d.Domain, Value: d.Subdomain + "." + p.BaseDomain, Why: "points the domain at your site"},
	}}, nil
}

func (p *DNSProvider) Check(d Domain) (bool, string, error) {
	lookup := p.Lookup
	if lookup == nil {
		lookup = net.LookupTXT
	}
	records, err := lookup(d.RecordName())
	if err != nil {
		return false, fmt.Sprintf("the TXT record at %s isn't visible yet — DNS may still be travelling", d.RecordName()), nil
	}
	for _, r := range records {
		if strings.TrimSpace(r) == d.Token {
			return true, "", nil
		}
	}
	return false, fmt.Sprintf("the TXT record at %s doesn't have the right value yet — it should be %q",
		d.RecordName(), d.Token), nil
}

func (p *DNSProvider) Detach(Domain) error { return nil }

// --- Cloudflare for SaaS ---

// CloudflareProvider connects domains through Cloudflare's Custom Hostnames API.
// Cloudflare validates ownership and issues and renews the certificate, so the
// network process stays out of TLS entirely — the same shape it already has for
// the wildcard.
type CloudflareProvider struct {
	APIToken string // needs Zone → SSL and Certificates → Edit
	ZoneID   string

	// CNAMETarget is what tenants point their domain at — a hostname inside the
	// zone, so Cloudflare recognises the traffic as a custom hostname.
	CNAMETarget string

	// FallbackOrigin is where Cloudflare forwards that traffic: a proxied record
	// in the zone pointing at this network. It is zone-level configuration, set
	// once (see EnsureFallbackOrigin) — a different thing from CNAMETarget, even
	// when the two are the same hostname.
	FallbackOrigin string

	// SSLMethod is how Cloudflare validates the certificate. "http" is the default
	// and needs nothing from the tenant beyond the CNAME; "txt" is for pre-validating
	// a domain before its live traffic moves, at the cost of extra DNS records.
	SSLMethod string

	// api is the Cloudflare API root; overridden in tests.
	api string
	hc  *http.Client
}

// CloudflareFromEnv builds the provider from FRIENDO_CF_* if it's configured,
// reporting false when it isn't so the caller can fall back to DNS.
func CloudflareFromEnv(baseDomain string) (*CloudflareProvider, bool) {
	token, zone := os.Getenv("FRIENDO_CF_API_TOKEN"), os.Getenv("FRIENDO_CF_ZONE_ID")
	if token == "" || zone == "" {
		return nil, false
	}
	fallback := strings.TrimSpace(os.Getenv("FRIENDO_CF_FALLBACK_ORIGIN"))
	target := strings.TrimSpace(os.Getenv("FRIENDO_CF_CNAME_TARGET"))
	// Tenants can point at the fallback origin directly, so it's the better
	// default than the bare base domain when one is configured.
	if target == "" {
		target = fallback
	}
	if target == "" {
		target = baseDomain
	}
	method := strings.ToLower(strings.TrimSpace(os.Getenv("FRIENDO_CF_SSL_METHOD")))
	return &CloudflareProvider{
		APIToken: token, ZoneID: zone,
		CNAMETarget: target, FallbackOrigin: fallback, SSLMethod: method,
		// An escape hatch for pointing a staging network at a mock instead of the
		// real API. Unset in every normal deployment.
		api: strings.TrimRight(strings.TrimSpace(os.Getenv("FRIENDO_CF_API_BASE")), "/"),
	}, true
}

// sslMethod is the validation method, defaulting to the one that asks least of
// the tenant.
func (p *CloudflareProvider) sslMethod() string {
	if p.SSLMethod == "txt" {
		return "txt"
	}
	return "http"
}

func (p *CloudflareProvider) Name() string { return "cloudflare" }

func (p *CloudflareProvider) client() *http.Client {
	if p.hc == nil {
		p.hc = &http.Client{Timeout: 20 * time.Second}
	}
	return p.hc
}

func (p *CloudflareProvider) root() string {
	if p.api != "" {
		return p.api
	}
	return "https://api.cloudflare.com/client/v4"
}

// cfHostname is the slice of Cloudflare's custom-hostname object we care about.
type cfHostname struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"` // pending | active | ...
	// VerificationErrors says why the *hostname* isn't active — usually that the
	// CNAME isn't in place yet.
	VerificationErrors []string `json:"verification_errors"`
	SSL                struct {
		Status string `json:"status"`
		Method string `json:"method"`
		// ValidationRecords carries the certificate challenge. For the txt method
		// this is the record that actually gets the cert issued — distinct from
		// ownership_verification, and missing it strands the tenant.
		ValidationRecords []struct {
			TxtName  string `json:"txt_name"`
			TxtValue string `json:"txt_value"`
			HTTPUrl  string `json:"http_url"`
		} `json:"validation_records"`
		ValidationErrs []struct {
			Message string `json:"message"`
		} `json:"validation_errors"`
	} `json:"ssl"`
	// OwnershipVerification is the pre-validation TXT record — only needed when
	// proving ownership *before* pointing the domain here.
	OwnershipVerification struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"ownership_verification"`
}

type cfResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (p *CloudflareProvider) do(method, path string, body any, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, p.root()+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("reaching Cloudflare: %w", err)
	}
	defer res.Body.Close()

	var envelope cfResponse
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("Cloudflare returned something unreadable (HTTP %d)", res.StatusCode)
	}
	if !envelope.Success {
		if len(envelope.Errors) > 0 {
			return fmt.Errorf("Cloudflare: %s", envelope.Errors[0].Message)
		}
		return fmt.Errorf("Cloudflare rejected the request (HTTP %d)", res.StatusCode)
	}
	if out != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

func (p *CloudflareProvider) Attach(d Domain) (Instructions, error) {
	var got cfHostname
	err := p.do("POST", "/zones/"+p.ZoneID+"/custom_hostnames", map[string]any{
		"hostname": d.Domain,
		"ssl": map[string]any{
			"method": p.sslMethod(),
			"type":   "dv",
		},
	}, &got)
	if err != nil {
		// An already-registered hostname is recoverable: find it and carry on,
		// so re-adding a domain isn't a dead end.
		if existing, findErr := p.find(d.Domain); findErr == nil {
			got = existing
		} else {
			return Instructions{}, err
		}
	}

	cname := DNSRecord{
		Type: "CNAME", Name: d.Domain, Value: p.CNAMETarget,
		Why: "points the domain at your site",
	}

	// With HTTP validation the CNAME is the whole job: once it resolves here,
	// Cloudflare proves ownership and issues the certificate on its own. One
	// record beats three for someone who has never touched DNS.
	if p.sslMethod() == "http" {
		return Instructions{
			Records:    []DNSRecord{cname},
			Note:       "That's the only record you need. Once it resolves, the certificate is issued and renewed automatically — there's nothing to install.",
			ProviderID: got.ID,
		}, nil
	}

	// TXT validation is for proving a domain *before* moving its live traffic, so
	// it needs both the ownership record and the certificate challenge. Leaving
	// the latter out strands the tenant waiting on a cert they can't get.
	records := []DNSRecord{}
	if ov := got.OwnershipVerification; ov.Name != "" && ov.Value != "" {
		records = append(records, DNSRecord{
			Type:  strings.ToUpper(orDefault(ov.Type, "TXT")),
			Name:  ov.Name,
			Value: ov.Value,
			Why:   "proves the domain is yours",
		})
	}
	for _, vr := range got.SSL.ValidationRecords {
		if vr.TxtName == "" || vr.TxtValue == "" {
			continue
		}
		records = append(records, DNSRecord{
			Type: "TXT", Name: vr.TxtName, Value: vr.TxtValue,
			Why: "gets the certificate issued",
		})
	}
	records = append(records, cname)
	return Instructions{
		Records:    records,
		Note:       "The certificate is issued and renewed automatically once these are in place — there's nothing to install.",
		ProviderID: got.ID,
	}, nil
}

// --- fallback origin (zone-level, set once) ---

// cfFallbackOrigin is the zone's fallback origin as Cloudflare reports it.
type cfFallbackOrigin struct {
	Origin string `json:"origin"`
	Status string `json:"status"` // active | pending_deployment | ...
}

// FallbackOriginStatus reads the zone's current fallback origin.
func (p *CloudflareProvider) FallbackOriginStatus() (string, string, error) {
	var got cfFallbackOrigin
	if err := p.do("GET", "/zones/"+p.ZoneID+"/custom_hostnames/fallback_origin", nil, &got); err != nil {
		return "", "", err
	}
	return got.Origin, got.Status, nil
}

// EnsureFallbackOrigin points the zone's fallback origin at p.FallbackOrigin,
// leaving it alone when it already matches. Without this every custom hostname
// validates and then has nowhere to go — it used to be a manual dashboard step,
// which is exactly the kind of setup that gets skipped once and debugged for an
// hour. Returns a line worth logging at boot.
func (p *CloudflareProvider) EnsureFallbackOrigin() (string, error) {
	if p.FallbackOrigin == "" {
		return "", fmt.Errorf("no fallback origin configured (set FRIENDO_CF_FALLBACK_ORIGIN)")
	}
	if current, status, err := p.FallbackOriginStatus(); err == nil && strings.EqualFold(current, p.FallbackOrigin) {
		return fmt.Sprintf("fallback origin %s (%s)", current, orDefault(status, "unknown")), nil
	}
	var got cfFallbackOrigin
	if err := p.do("PUT", "/zones/"+p.ZoneID+"/custom_hostnames/fallback_origin",
		map[string]any{"origin": p.FallbackOrigin}, &got); err != nil {
		return "", err
	}
	return fmt.Sprintf("fallback origin set to %s (%s)",
		orDefault(got.Origin, p.FallbackOrigin), orDefault(got.Status, "pending_deployment")), nil
}

func (p *CloudflareProvider) Check(d Domain) (bool, string, error) {
	got, err := p.lookup(d)
	if err != nil {
		return false, "", err
	}
	if got.Status == "active" && got.SSL.Status == "active" {
		return true, "", nil
	}
	// Cloudflare's own validation errors are the most useful thing to relay.
	if len(got.SSL.ValidationErrs) > 0 {
		return false, got.SSL.ValidationErrs[0].Message, nil
	}
	if len(got.VerificationErrors) > 0 {
		return false, got.VerificationErrors[0], nil
	}
	if got.Status != "active" {
		return false, fmt.Sprintf("Cloudflare is still checking the domain is yours (status: %s)", orDefault(got.Status, "pending")), nil
	}
	return false, fmt.Sprintf("the certificate is still being issued (status: %s)", orDefault(got.SSL.Status, "pending")), nil
}

func (p *CloudflareProvider) Detach(d Domain) error {
	id := d.ProviderID
	if id == "" {
		got, err := p.find(d.Domain)
		if err != nil {
			return nil // nothing there to release
		}
		id = got.ID
	}
	return p.do("DELETE", "/zones/"+p.ZoneID+"/custom_hostnames/"+id, nil, nil)
}

// lookup fetches a domain's custom-hostname record, by stored id when we have
// one and by hostname search otherwise.
func (p *CloudflareProvider) lookup(d Domain) (cfHostname, error) {
	if d.ProviderID != "" {
		var got cfHostname
		if err := p.do("GET", "/zones/"+p.ZoneID+"/custom_hostnames/"+d.ProviderID, nil, &got); err == nil {
			return got, nil
		}
		// Fall through to a search — the id may be stale.
	}
	return p.find(d.Domain)
}

func (p *CloudflareProvider) find(hostname string) (cfHostname, error) {
	var list []cfHostname
	if err := p.do("GET", "/zones/"+p.ZoneID+"/custom_hostnames?hostname="+hostname, nil, &list); err != nil {
		return cfHostname{}, err
	}
	for _, h := range list {
		if strings.EqualFold(h.Hostname, hostname) {
			return h, nil
		}
	}
	return cfHostname{}, fmt.Errorf("%s isn't registered with Cloudflare yet", hostname)
}

// orDefault returns v, or fallback when v is empty.
func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
