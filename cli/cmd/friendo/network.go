package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendo-world/friendo/cli/internal/deploy"
	"github.com/friendo-world/friendo/runtime/go/network"
	"github.com/friendo-world/friendo/runtime/go/storage"
)

// newNetworkCommand builds the `friendo network` command group — operator mode.
// A network is one process hosting many sites by subdomain; friendo.world is
// just the reference network.
//
// Commands run in one of two modes:
//   - local  (--root, default): operate directly on the network's folder, for
//     use ON the box that runs `friendo network serve`.
//   - remote (--network <url>): operate over the network's operator API as a
//     signed-in operator, for managing a network from elsewhere. Run
//     `friendo login <url>` first.
func newNetworkCommand() *cobra.Command {
	var root string
	var netURL string

	cmd := &cobra.Command{
		Use:   "network",
		Short: "Run or manage a friendo network — one operator hosting many sites",
		Long: `Network mode: one process serves many sites by subdomain, each its own
folder + database. It's the same binary friendo.world runs.

Manage a network you run on this box with --root, or a remote network with
--network <url> (after 'friendo login <url>').`,
	}
	cmd.PersistentFlags().StringVar(&root, "root", "./network", "Local network directory (on-box operation)")
	cmd.PersistentFlags().StringVar(&netURL, "network", "", "Remote network URL (operate over the operator API)")

	// So every on-box subcommand (signups, invite, operator, serve) points at the
	// same directory Coolify runs the network from. An explicit --root still wins.
	cmd.PersistentPreRun = func(c *cobra.Command, args []string) {
		if !c.Flags().Changed("root") {
			if v := os.Getenv("FRIENDO_NETWORK_ROOT"); v != "" {
				root = v
			}
		}
	}

	openReg := func() *network.Registry {
		reg, err := network.NewRegistry(root)
		exitOnErr(err)
		return reg
	}
	openAccounts := func() *network.Accounts {
		accounts, err := network.OpenAccounts(root)
		exitOnErr(err)
		return accounts
	}

	// remoteClient returns an authenticated operator client when --network is set,
	// or (nil, false) for local mode. Exits with guidance if not signed in.
	remoteClient := func() (*deploy.OperatorClient, bool) {
		if netURL == "" {
			return nil, false
		}
		url := strings.TrimRight(netURL, "/")
		cfg, err := deploy.LoadConfig()
		exitOnErr(err)
		auth, ok := cfg.NetworkAuth(url)
		if !ok || auth.Token == "" {
			exitOnErr(fmt.Errorf("not signed in to %s — run: friendo login %s", url, url))
		}
		return deploy.NewOperatorClient(url, auth.Token), true
	}

	// friendo network serve
	var port int
	var baseDomain string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve every site on the network by subdomain",
		Run: func(cmd *cobra.Command, args []string) {
			// Env fallbacks (so a container / Coolify can drive config without flags).
			// An explicit flag always wins over the env var. (root is resolved in the
			// network command's PersistentPreRun, shared by every subcommand.)
			if !cmd.Flags().Changed("base-domain") {
				if v := os.Getenv("FRIENDO_BASE_DOMAIN"); v != "" {
					baseDomain = v
				}
			}
			if !cmd.Flags().Changed("port") {
				if v := os.Getenv("FRIENDO_PORT"); v != "" {
					if p, err := strconv.Atoi(v); err == nil {
						port = p
					}
				}
			}

			reg := openReg()
			accounts, err := network.OpenAccounts(root)
			exitOnErr(err)
			d := network.NewDispatcher(reg, baseDomain, 0)
			// A suspended site serves a hold page instead of the tenant; a verified
			// custom domain routes to its site like a subdomain would; a domain
			// that's added but not yet verified gets a page saying how to finish.
			d.SetSuspendedCheck(accounts.SiteSuspension)
			d.SetDomainLookup(accounts.SiteForDomain)
			d.SetDomainStatus(accounts.GetDomain)
			// The apex serves the operator's home site with the network's own
			// pages + API over it. Route deletes through the dispatcher so a
			// destroyed site stops serving at once.
			d.SetHomeSite(accounts.HomeSite)

			aa := network.NewAccountAuth(accounts, reg, baseDomain)
			op := network.NewOperatorAPI(aa)
			op.SetDestroyer(d.DestroySite)
			d.SetSSOExchange(aa.ExchangeSSOCode)
			// Cloudflare for SaaS when it's configured — it validates ownership and
			// issues/renews the certificate, so this process never handles TLS. A
			// self-hosted network falls back to a TXT check it can do on its own.
			if cf, ok := network.CloudflareFromEnv(baseDomain); ok {
				aa.SetDomainProvider(cf)
				fmt.Printf("Custom domains: Cloudflare for SaaS — tenants CNAME to %s\n", cf.CNAMETarget)
				// Point the zone's fallback origin at this network. Without it every
				// custom hostname validates and then has nowhere to go, and it's a
				// dashboard step that only ever gets skipped once.
				if cf.FallbackOrigin == "" {
					fmt.Fprintln(os.Stderr, "  ! FRIENDO_CF_FALLBACK_ORIGIN is not set — custom hostnames will have no origin "+
						"to reach. Set it to a proxied record in the zone that points at this network.")
				} else if msg, err := cf.EnsureFallbackOrigin(); err != nil {
					fmt.Fprintf(os.Stderr, "  ! could not set the Cloudflare fallback origin: %v\n", err)
				} else {
					fmt.Printf("  %s\n", msg)
				}
			} else {
				fmt.Println("Custom domains: DNS verification (set FRIENDO_CF_API_TOKEN + FRIENDO_CF_ZONE_ID for Cloudflare)")
			}
			aa.SetDomainChangedHook(d.ForgetDomain)
			d.HandleApex(network.ApexRouter(aa, op))

			// Designate the first operator from env (turnkey containers). Otherwise
			// the first person to sign in at the apex console claims operator.
			if email := os.Getenv("FRIENDO_OPERATOR_EMAIL"); email != "" {
				if err := accounts.Grant(email, "operator"); err != nil {
					fmt.Fprintf(os.Stderr, "Could not grant operator to %q: %v\n", email, err)
				} else {
					fmt.Printf("Operator: %s (from FRIENDO_OPERATOR_EMAIL)\n", email)
				}
			} else if any, _ := accounts.AnyOperator(); !any {
				fmt.Println("No operators yet — the first sign-in at /account will claim operator " +
					"(or set FRIENDO_OPERATOR_EMAIL).")
			}
			fmt.Println(storage.EnvStatus())
			fmt.Printf("friendo network listening on :%d — sites at <subdomain>.%s\n", port, baseDomain)
			exitOnErr(d.ListenAndServe(port))
		},
	}
	serve.Flags().IntVarP(&port, "port", "p", 3000, "Port to listen on")
	serve.Flags().StringVar(&baseDomain, "base-domain", "localhost", "Base domain; sites are served at <subdomain>.<base-domain>")

	// friendo network deploy <subdomain> — provision + push this folder, one shot.
	var deployName, deployOwner string
	deployCmd := &cobra.Command{
		Use:   "deploy <subdomain>",
		Short: "Provision a site on the network and push this folder to it (one command)",
		Long: `Deploy the site in the current directory to a network: it provisions the
subdomain (as an operator) and then pushes your templates, assets, and content
(as the site's admin). Requires --network <url> and a prior 'friendo login'.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if netURL == "" {
				exitOnErr(fmt.Errorf("network deploy requires --network <url> (run: friendo login <url>)"))
			}
			client, _ := remoteClient() // exits if not signed in
			sub := args[0]

			// 1. Provision the site (idempotent — an existing site just gets pushed to).
			if _, err := client.Provision(sub, deployName, deployOwner); err != nil {
				if !strings.Contains(err.Error(), "already exists") {
					exitOnErr(err)
				}
				fmt.Printf("Site %q already exists — pushing to it.\n", sub)
			} else {
				fmt.Printf("Provisioned %q.\n", sub)
			}

			// 2. Push this folder's content to the new site (owner auth via push).
			siteURL := deploy.SiteURLForSubdomain(strings.TrimRight(netURL, "/"), sub)
			exitOnErr(deploy.RunPush(deploy.PushOptions{Target: siteURL, Data: true}))
			fmt.Printf("\nDeployed → %s\n", siteURL)
		},
	}
	deployCmd.Flags().StringVar(&deployName, "name", "", "Display name for the site (defaults to the subdomain)")
	deployCmd.Flags().StringVar(&deployOwner, "owner", "", "Email of the account that should own the site (defaults to you)")

	// friendo network provision <subdomain>
	var name, owner string
	provision := &cobra.Command{
		Use:   "provision <subdomain>",
		Short: "Create a new site on the network",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				site, err := client.Provision(args[0], name, owner)
				exitOnErr(err)
				fmt.Printf("Provisioned %q on %s (owner: %s)\n", site.Subdomain, strings.TrimRight(netURL, "/"), site.Owner)
				return
			}
			site, err := openReg().Provision(args[0], name)
			exitOnErr(err)
			// Link an owner so the site's first-run setup isn't left open to
			// whoever reaches it first. On the box there's no signed-in account,
			// so it's --owner, else the bootstrap operator's email.
			if owner == "" {
				owner = os.Getenv("FRIENDO_OPERATOR_EMAIL")
			}
			if owner == "" {
				fmt.Printf("Provisioned %q → %s\n", site.Subdomain, site.Dir)
				fmt.Println("  ! no owner linked — the first person to open its admin will claim it. " +
					"Pass --owner <email> (or set FRIENDO_OPERATOR_EMAIL).")
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			exitOnErr(network.LinkOwner(accounts, openReg(), site.Subdomain, owner))
			fmt.Printf("Provisioned %q → %s (owner: %s)\n", site.Subdomain, site.Dir, owner)
		},
	}
	provision.Flags().StringVar(&name, "name", "", "Display name (defaults to the subdomain)")
	provision.Flags().StringVar(&owner, "owner", "", "Email of the account that should own the site")

	// friendo network sites
	sites := &cobra.Command{
		Use:   "sites",
		Short: "List the sites on the network",
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				list, err := client.Sites()
				exitOnErr(err)
				if len(list) == 0 {
					fmt.Println("No sites yet.")
					return
				}
				for _, s := range list {
					status := ""
					if s.Suspended {
						status = "  ON HOLD"
						if s.Reason != "" {
							status += " (" + s.Reason + ")"
						}
					}
					fmt.Printf("  %-24s %-24s %s%s\n", s.Subdomain, s.Name, s.Owner, status)
				}
				return
			}
			list, err := openReg().Sites()
			exitOnErr(err)
			if len(list) == 0 {
				fmt.Println("No sites yet. Create one with: friendo network provision <subdomain>")
				return
			}
			for _, s := range list {
				fmt.Printf("  %-24s %s\n", s.Subdomain, s.Name)
			}
		},
	}

	// friendo network sites suspend/resume — take a site off the air without
	// deleting it. The reversible counterpart to destroy.
	var siteReason string
	sitesSuspend := &cobra.Command{
		Use:   "suspend <subdomain>",
		Short: "Show visitors a hold notice instead of the site (reversible)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				exitOnErr(client.SuspendSite(args[0], siteReason))
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.SuspendSite(args[0], siteReason))
			}
			fmt.Printf("%q is on hold. Nothing was deleted — put it back with: "+
				"friendo network sites resume %s\n", args[0], args[0])
		},
	}
	sitesSuspend.Flags().StringVar(&siteReason, "reason", "", "Why — shown on the hold page")

	sitesResume := &cobra.Command{
		Use:   "resume <subdomain>",
		Short: "Put a site on hold back on the air",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				exitOnErr(client.ResumeSite(args[0]))
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.ResumeSite(args[0]))
			}
			fmt.Printf("%q is serving again.\n", args[0])
		},
	}
	sites.AddCommand(sitesSuspend, sitesResume)

	// friendo network destroy <subdomain>
	var yes bool
	destroy := &cobra.Command{
		Use:   "destroy <subdomain>",
		Short: "Delete a site and all its data (irreversible)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !yes {
				fmt.Printf("This permanently deletes %q and all its data. Re-run with --yes to confirm.\n", args[0])
				return
			}
			if client, ok := remoteClient(); ok {
				exitOnErr(client.Destroy(args[0]))
				fmt.Printf("Destroyed %q on %s.\n", args[0], strings.TrimRight(netURL, "/"))
				return
			}
			exitOnErr(openReg().Destroy(args[0]))
			fmt.Printf("Destroyed %q.\n", args[0])
		},
	}
	destroy.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")

	// friendo network operator grant <email> (local, on-box) — make an account an
	// operator (creating it if needed). They sign in passwordless with 'friendo login'.
	operator := &cobra.Command{
		Use:   "operator",
		Short: "Manage operators (who runs the network)",
	}
	opGrant := &cobra.Command{
		Use:   "grant <email>",
		Short: "Grant an account the operator capability",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				exitOnErr(client.GrantOperator(args[0]))
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.Grant(args[0], "operator"))
			}
			fmt.Printf("Granted operator to %q — they sign in with 'friendo login'.\n", args[0])
		},
	}
	opRevoke := &cobra.Command{
		Use:   "revoke <email>",
		Short: "Remove the operator capability from an account",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				exitOnErr(client.RevokeOperator(args[0]))
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.Revoke(args[0], "operator"))
			}
			fmt.Printf("%q is no longer an operator — their account and sites are untouched.\n", args[0])
		},
	}
	operator.AddCommand(opGrant, opRevoke)

	// friendo network signups <open|invite> — set who may create an account.
	signups := &cobra.Command{
		Use:   "signups <open|invite>",
		Short: "Set the signup policy (open = anyone; invite = operator-invited only)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				_, err := client.UpdateSettings(&args[0], nil, nil)
				exitOnErr(err)
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.SetSignups(args[0]))
			}
			fmt.Printf("Signups set to %q.\n", args[0])
		},
	}

	// friendo network invite <email> — mint an invite so they can sign in even
	// when signups are invite-only. Invites expire, and can be revoked.
	var inviteDays int
	invite := &cobra.Command{
		Use:   "invite <email>",
		Short: "Invite someone to the network",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				inv, err := client.Invite(args[0], inviteDays)
				exitOnErr(err)
				fmt.Printf("Invited %s — they can sign in with 'friendo login' (expires %s).\n", inv.Email, inv.Expires)
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			ttl := time.Duration(inviteDays) * 24 * time.Hour
			inv, err := accounts.CreateInvite(args[0], "", ttl)
			exitOnErr(err)
			fmt.Printf("Invited %s — they can sign in with 'friendo login' (expires %s).\n", inv.Email, inv.Expires())
		},
	}
	invite.Flags().IntVar(&inviteDays, "days", 14, "How many days the invite stays good")

	// friendo network invites — see what's outstanding, revoke, tidy up.
	invites := &cobra.Command{
		Use:   "invites",
		Short: "List outstanding invites",
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				list, err := client.Invites()
				exitOnErr(err)
				if len(list) == 0 {
					fmt.Println("No invites outstanding. Invite someone with: friendo network invite <email>")
					return
				}
				for _, inv := range list {
					fmt.Printf("  %-32s %-8s %s\n", inv.Email, inv.Status, inv.Expires)
				}
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			list, err := accounts.Invites()
			exitOnErr(err)
			if len(list) == 0 {
				fmt.Println("No invites outstanding. Invite someone with: friendo network invite <email>")
				return
			}
			for _, inv := range list {
				fmt.Printf("  %-32s %-8s %s\n", inv.Email, inv.Status(), inv.Expires())
			}
		},
	}
	invitesRevoke := &cobra.Command{
		Use:   "revoke <email>",
		Short: "Withdraw an invite",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				exitOnErr(client.RevokeInvite(args[0]))
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				exitOnErr(accounts.RevokeInvite(args[0]))
			}
			fmt.Printf("Revoked the invite for %s.\n", args[0])
		},
	}
	invitesPrune := &cobra.Command{
		Use:   "prune",
		Short: "Forget invites that have already expired",
		Run: func(cmd *cobra.Command, args []string) {
			var n int
			var err error
			if client, ok := remoteClient(); ok {
				n, err = client.PruneInvites()
			} else {
				accounts := openAccounts()
				defer accounts.Close()
				n, err = accounts.PruneInvites()
			}
			exitOnErr(err)
			fmt.Printf("Removed %d expired invite(s).\n", n)
		},
	}
	invites.AddCommand(invitesRevoke, invitesPrune)

	// friendo network accounts — who's on the network, and the levers for when
	// something goes wrong: suspend, resume, and sign someone out everywhere.
	accountsCmd := &cobra.Command{
		Use:   "accounts",
		Short: "List the accounts on the network",
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				list, err := client.Accounts()
				exitOnErr(err)
				if len(list) == 0 {
					fmt.Println("No accounts yet.")
					return
				}
				for _, a := range list {
					role := "member"
					if a.Operator {
						role = "operator"
					}
					status := "active"
					if a.Suspended {
						status = "SUSPENDED"
						if a.Reason != "" {
							status += " (" + a.Reason + ")"
						}
					}
					fmt.Printf("  %-32s %-9s %d of %-10s %s\n", a.Email, role, a.Used, a.AllowedLabel(), status)
				}
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			list, err := accounts.List()
			exitOnErr(err)
			if len(list) == 0 {
				fmt.Println("No accounts yet.")
				return
			}
			held, _ := accounts.SuspendedAccounts()
			for _, acct := range list {
				role := "member"
				if acct.Has("operator") {
					role = "operator"
				}
				used, allowed, err := accounts.SiteUsage(acct)
				if err != nil {
					continue
				}
				status := "active"
				if s, ok := held[acct.ID]; ok {
					status = "SUSPENDED"
					if s.Reason != "" {
						status += " (" + s.Reason + ")"
					}
				}
				fmt.Printf("  %-32s %-9s %d of %-10s %s\n",
					acct.Email, role, used, network.FormatQuota(allowed), status)
			}
		},
	}

	// byEmail resolves an account for the account subcommands, with guidance
	// rather than a bare "not found".
	byEmail := func(accounts *network.Accounts, email string) *network.Account {
		acct, ok := accounts.GetByEmail(email)
		if !ok {
			exitOnErr(fmt.Errorf("no account for %q — see who exists with: friendo network accounts", email))
		}
		return acct
	}

	var suspendReason string
	accountsSuspend := &cobra.Command{
		Use:   "suspend <email>",
		Short: "Block an account from signing in or creating sites (reversible)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				a, err := client.Account(args[0])
				exitOnErr(err)
				exitOnErr(client.SuspendAccount(a.ID, suspendReason))
				fmt.Printf("Suspended %s — they can't sign in or create sites. Their sites are untouched — "+
					"put them on hold separately if you need to.\n", a.Email)
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			acct := byEmail(accounts, args[0])
			exitOnErr(accounts.SuspendAccount(acct.ID, suspendReason))
			n, _ := accounts.RevokeSessions(acct.ID)
			fmt.Printf("Suspended %s (%d session(s) ended). Their sites are untouched — "+
				"put them on hold separately if you need to.\n", acct.Email, n)
		},
	}
	accountsSuspend.Flags().StringVar(&suspendReason, "reason", "", "Why — shown to you, and to them when they try to sign in")

	accountsResume := &cobra.Command{
		Use:   "resume <email>",
		Short: "Let a suspended account back in",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				a, err := client.Account(args[0])
				exitOnErr(err)
				exitOnErr(client.ResumeAccount(a.ID))
				fmt.Printf("%s can sign in again.\n", a.Email)
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			acct := byEmail(accounts, args[0])
			exitOnErr(accounts.ResumeAccount(acct.ID))
			fmt.Printf("%s can sign in again.\n", acct.Email)
		},
	}

	accountsSignout := &cobra.Command{
		Use:   "signout <email>",
		Short: "Sign an account out of every device (for a lost laptop)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				a, err := client.Account(args[0])
				exitOnErr(err)
				n, err := client.SignOutAccount(a.ID)
				exitOnErr(err)
				fmt.Printf("Ended %d session(s) for %s — they can sign in again with 'friendo login'.\n", n, a.Email)
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			acct := byEmail(accounts, args[0])
			n, err := accounts.RevokeSessions(acct.ID)
			exitOnErr(err)
			fmt.Printf("Ended %d session(s) for %s — they can sign in again with 'friendo login'.\n", n, acct.Email)
		},
	}
	accountsCmd.AddCommand(accountsSuspend, accountsResume, accountsSignout)

	// friendo network quota — how many sites one account may create. Without a
	// cap, opening signups lets a single account claim subdomains without end.
	var quotaDefault string
	quota := &cobra.Command{
		Use:   "quota [email] [limit]",
		Short: "Show or set how many sites an account can create",
		Long: `Every account can create a limited number of sites. Run with no arguments to
see the network default and what everyone is using.

  friendo network quota                      show the limits and who's near them
  friendo network quota --default 5          everyone gets 5 sites
  friendo network quota --default unlimited  no limit for anyone
  friendo network quota ada@example.com 20   give one person their own limit
  friendo network quota ada@example.com default   put them back on the default

Operators are never limited.`,
		Args: cobra.MaximumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				switch {
				case quotaDefault != "":
					sum, err := client.UpdateSettings(nil, &quotaDefault, nil)
					exitOnErr(err)
					fmt.Printf("Everyone can now create %s site(s).\n", sum.DefaultQuota)
				case len(args) == 2:
					a, err := client.Account(args[0])
					exitOnErr(err)
					exitOnErr(client.SetAccountQuota(a.ID, args[1]))
					fmt.Printf("Updated the limit for %s.\n", a.Email)
				case len(args) == 1:
					a, err := client.Account(args[0])
					exitOnErr(err)
					fmt.Printf("%s — %d of %s site(s) used\n", a.Email, a.Used, a.AllowedLabel())
				default:
					sum, err := client.Summary()
					exitOnErr(err)
					fmt.Printf("Default: %s site(s) per account\n\n", sum.DefaultQuota)
					list, err := client.Accounts()
					exitOnErr(err)
					for _, a := range list {
						note := ""
						if a.Operator {
							note = "  (operator — never limited)"
						} else if a.Override {
							note = "  (own limit)"
						}
						fmt.Printf("  %-32s %d of %s%s\n", a.Email, a.Used, a.AllowedLabel(), note)
					}
				}
				return
			}
			accounts := openAccounts()
			defer accounts.Close()

			// friendo network quota --default <n|unlimited>
			if quotaDefault != "" {
				n, err := network.ParseQuota(quotaDefault)
				exitOnErr(err)
				exitOnErr(accounts.SetDefaultSiteQuota(n))
				fmt.Printf("Everyone can now create %s site(s).\n", network.FormatQuota(n))
				return
			}

			// friendo network quota <email> <limit>
			if len(args) > 0 {
				acct, ok := accounts.GetByEmail(args[0])
				if !ok {
					exitOnErr(fmt.Errorf("no account for %q — invite them first: friendo network invite %s", args[0], args[0]))
				}
				if len(args) == 1 {
					used, allowed, err := accounts.SiteUsage(acct)
					exitOnErr(err)
					fmt.Printf("%s — %d of %s site(s) used\n", acct.Email, used, network.FormatQuota(allowed))
					return
				}
				if strings.EqualFold(strings.TrimSpace(args[1]), "default") {
					exitOnErr(accounts.ClearAccountSiteQuota(acct.ID))
					fmt.Printf("%s now uses the network default (%s site(s)).\n",
						acct.Email, network.FormatQuota(accounts.DefaultSiteQuota()))
					return
				}
				n, err := network.ParseQuota(args[1])
				exitOnErr(err)
				exitOnErr(accounts.SetAccountSiteQuota(acct.ID, n))
				fmt.Printf("%s can now create %s site(s).\n", acct.Email, network.FormatQuota(n))
				return
			}

			// No arguments — show the default and everyone's usage against it.
			fmt.Printf("Default: %s site(s) per account\n", network.FormatQuota(accounts.DefaultSiteQuota()))
			list, err := accounts.List()
			exitOnErr(err)
			if len(list) == 0 {
				fmt.Println("No accounts yet.")
				return
			}
			overrides, _ := accounts.AccountQuotas()
			fmt.Println()
			for _, acct := range list {
				used, allowed, err := accounts.SiteUsage(acct)
				if err != nil {
					continue
				}
				note := ""
				if acct.Has("operator") {
					note = "  (operator — never limited)"
				} else if _, ok := overrides[acct.ID]; ok {
					note = "  (own limit)"
				}
				fmt.Printf("  %-32s %d of %s%s\n", acct.Email, used, network.FormatQuota(allowed), note)
			}
		},
	}
	quota.Flags().StringVar(&quotaDefault, "default", "", "Set the network-wide limit (a number, or 'unlimited')")

	// friendo network home [subdomain] — which site the bare domain shows.
	var clearHome bool
	home := &cobra.Command{
		Use:   "home [subdomain]",
		Short: "Show or set the site served at the network's bare domain",
		Long: `The bare domain (the address with no subdomain) shows one of the network's
sites — its home site. Until you pick one, it shows a plain page that says this
is a friendo network and where to sign in.

  friendo network home            show which site is the home site
  friendo network home www        serve the "www" site at the bare domain
  friendo network home --clear    go back to the built-in page

The network's own pages (/login, /account, /network, /activate) still work on
the bare domain. A home site can take one over on purpose by defining a page
at that path — e.g. pages/account.html with <friendo-account> in it.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			want := ""
			set := clearHome || len(args) == 1
			if len(args) == 1 {
				want = args[0]
			}
			if client, ok := remoteClient(); ok {
				var sum deploy.NetworkSummary
				var err error
				if set {
					sum, err = client.UpdateSettings(nil, nil, &want)
				} else {
					sum, err = client.Summary()
				}
				exitOnErr(err)
				printHome(sum.HomeSite, sum.Base)
				return
			}
			accounts := openAccounts()
			defer accounts.Close()
			if set {
				if want != "" {
					if _, ok := openReg().Dir(want); !ok {
						exitOnErr(fmt.Errorf("no site named %q — create it first: friendo network provision %s", want, want))
					}
				}
				exitOnErr(accounts.SetHomeSite(want))
			}
			printHome(accounts.HomeSite(), os.Getenv("FRIENDO_BASE_DOMAIN"))
		},
	}
	home.Flags().BoolVar(&clearHome, "clear", false, "Go back to the built-in page at the bare domain")

	cmd.AddCommand(serve, deployCmd, provision, sites, destroy, operator, signups,
		invite, invites, accountsCmd, quota, home)
	return cmd
}

// printHome reports the home-site setting in plain words.
func printHome(sub, base string) {
	if base == "" {
		base = "the bare domain"
	}
	if sub == "" {
		fmt.Printf("No home site — %s shows the built-in page. Pick one with: friendo network home <subdomain>\n", base)
		return
	}
	fmt.Printf("Home site: %q serves at %s\n", sub, base)
}

// exitOnErr prints err and exits non-zero. Shared by the network subcommands.
func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
