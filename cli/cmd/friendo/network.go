package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/friendo-world/friendo/runtime/go/network"
)

// newNetworkCommand builds the `friendo network` command group — operator mode.
// A network is one process hosting many sites by subdomain; friendo.world is
// just the reference network. Site-owner commands (deploy/push/pull) are
// separate; these are the operator-facing fleet commands.
func newNetworkCommand() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "network",
		Short: "Run a friendo network — one operator hosting many sites",
		Long: `Network mode: one process serves many sites by subdomain, each its own
folder + database. It's the same binary friendo.world runs — point a wildcard
domain at it and you're an operator too.`,
	}
	cmd.PersistentFlags().StringVar(&root, "root", "./network", "Directory holding the network's site folders")

	openReg := func() *network.Registry {
		reg, err := network.NewRegistry(root)
		exitOnErr(err)
		return reg
	}

	// friendo network serve
	var port int
	var baseDomain string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve every site on the network by subdomain",
		Run: func(cmd *cobra.Command, args []string) {
			reg := openReg()
			ops, err := network.OpenOperators(root)
			exitOnErr(err)
			d := network.NewDispatcher(reg, baseDomain, 0)
			// The apex (bare base domain) is the operator console. Route deletes
			// through the dispatcher so a destroyed site stops serving at once.
			console := network.NewConsole(reg, ops, baseDomain)
			console.SetDestroyer(d.DestroySite)
			d.HandleApex(console)
			if n, _ := ops.Count(); n == 0 {
				fmt.Printf("No operators yet — open the apex to create the first one, "+
					"or run: friendo network --root %s operator add <email>\n", root)
			}
			exitOnErr(d.ListenAndServe(port))
		},
	}
	serve.Flags().IntVarP(&port, "port", "p", 3000, "Port to listen on")
	serve.Flags().StringVar(&baseDomain, "base-domain", "localhost", "Base domain; sites are served at <subdomain>.<base-domain>")

	// friendo network provision <subdomain>
	var name string
	provision := &cobra.Command{
		Use:   "provision <subdomain>",
		Short: "Create a new site on the network",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
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
			exitOnErr(openReg().Destroy(args[0]))
			fmt.Printf("Destroyed %q.\n", args[0])
		},
	}
	destroy.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")

	// friendo network operator add <email>
	operator := &cobra.Command{
		Use:   "operator",
		Short: "Manage operator accounts (who runs the network)",
	}
	var opPassword string
	opAdd := &cobra.Command{
		Use:   "add <email>",
		Short: "Create an operator account",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ops, err := network.OpenOperators(root)
			exitOnErr(err)
			defer ops.Close()

			pw := opPassword
			if pw == "" {
				pw = os.Getenv("FRIENDO_OPERATOR_PASSWORD")
			}
			if pw == "" {
				fmt.Print("Password (8+ chars): ")
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				pw = strings.TrimSpace(line)
			}
			exitOnErr(ops.Create(args[0], pw))
			fmt.Printf("Created operator %q.\n", args[0])
		},
	}
	opAdd.Flags().StringVar(&opPassword, "password", "", "Operator password (or set FRIENDO_OPERATOR_PASSWORD; prompts if unset)")
	operator.AddCommand(opAdd)

	cmd.AddCommand(serve, provision, sites, destroy, operator)
	return cmd
}

// exitOnErr prints err and exits non-zero. Shared by the network subcommands.
func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
