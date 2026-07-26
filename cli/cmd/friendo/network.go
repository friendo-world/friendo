package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
//     `friendo network login <url>` first.
func newNetworkCommand() *cobra.Command {
	var root string
	var netURL string

	cmd := &cobra.Command{
		Use:   "network",
		Short: "Run or manage a friendo network — one operator hosting many sites",
		Long: `Network mode: one process serves many sites by subdomain, each its own
folder + database. It's the same binary friendo.world runs.

Manage a network you run on this box with --root, or a remote network with
--network <url> (after 'friendo network login <url>').`,
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
			// The apex serves the operator console + the identity endpoints
			// (device-auth for the CLI). Route deletes through the dispatcher so a
			// destroyed site stops serving at once.
			console := network.NewConsole(reg, accounts, baseDomain)
			console.SetDestroyer(d.DestroySite)
			d.HandleApex(network.ApexRouter(console, network.NewAccountAuth(accounts, reg, baseDomain)))

			// Designate the first operator from env (turnkey containers). Otherwise
			// the first person to sign in at the apex console claims operator.
			if email := os.Getenv("FRIENDO_OPERATOR_EMAIL"); email != "" {
				if err := accounts.Grant(email, "operator"); err != nil {
					fmt.Fprintf(os.Stderr, "Could not grant operator to %q: %v\n", email, err)
				} else {
					fmt.Printf("Operator: %s (from FRIENDO_OPERATOR_EMAIL)\n", email)
				}
			} else if any, _ := accounts.AnyOperator(); !any {
				fmt.Println("No operators yet — the first sign-in at the apex console will claim operator " +
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
	var deployName string
	deployCmd := &cobra.Command{
		Use:   "deploy <subdomain>",
		Short: "Provision a site on the network and push this folder to it (one command)",
		Long: `Deploy the site in the current directory to a network: it provisions the
subdomain (as an operator) and then pushes your templates, assets, and content
(as the site's admin). Requires --network <url> and a prior 'friendo network login'.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if netURL == "" {
				exitOnErr(fmt.Errorf("network deploy requires --network <url> (run: friendo network login <url>)"))
			}
			client, _ := remoteClient() // exits if not signed in
			sub := args[0]

			// 1. Provision the site (idempotent — an existing site just gets pushed to).
			if _, err := client.Provision(sub, deployName); err != nil {
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

	// friendo network provision <subdomain>
	var name string
	provision := &cobra.Command{
		Use:   "provision <subdomain>",
		Short: "Create a new site on the network",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if client, ok := remoteClient(); ok {
				site, err := client.Provision(args[0], name)
				exitOnErr(err)
				fmt.Printf("Provisioned %q on %s\n", site.Subdomain, strings.TrimRight(netURL, "/"))
				return
			}
			site, err := openReg().Provision(args[0], name)
			exitOnErr(err)
			fmt.Printf("Provisioned %q → %s\n", site.Subdomain, site.Dir)
		},
	}
	provision.Flags().StringVar(&name, "name", "", "Display name (defaults to the subdomain)")

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
					fmt.Printf("  %-24s %s\n", s.Subdomain, s.Name)
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
		Short: "Manage operators (on-box; who runs the network)",
	}
	opGrant := &cobra.Command{
		Use:   "grant <email>",
		Short: "Grant an account the operator capability",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			accounts, err := network.OpenAccounts(root)
			exitOnErr(err)
			defer accounts.Close()
			exitOnErr(accounts.Grant(args[0], "operator"))
			fmt.Printf("Granted operator to %q — they sign in with 'friendo login'.\n", args[0])
		},
	}
	operator.AddCommand(opGrant)

	// friendo network signups <open|invite> — set who may create an account.
	signups := &cobra.Command{
		Use:   "signups <open|invite>",
		Short: "Set the signup policy (open = anyone; invite = operator-invited only)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			accounts, err := network.OpenAccounts(root)
			exitOnErr(err)
			defer accounts.Close()
			exitOnErr(accounts.SetSignups(args[0]))
			fmt.Printf("Signups set to %q.\n", args[0])
		},
	}

	// friendo network invite <email> — pre-create an account so they can sign in
	// even when signups are invite-only.
	invite := &cobra.Command{
		Use:   "invite <email>",
		Short: "Invite someone by pre-creating their account",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			accounts, err := network.OpenAccounts(root)
			exitOnErr(err)
			defer accounts.Close()
			if _, err := accounts.EnsureAccount(args[0]); err != nil {
				exitOnErr(err)
			}
			fmt.Printf("Invited %q — they can now sign in with 'friendo login'.\n", args[0])
		},
	}

	cmd.AddCommand(serve, deployCmd, provision, sites, destroy, operator, signups, invite)
	return cmd
}

// exitOnErr prints err and exits non-zero. Shared by the network subcommands.
func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
