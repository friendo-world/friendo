package main

import (
	"github.com/spf13/cobra"

	"github.com/friendo-world/friendo/cli/internal/deploy"
)

// newDomainCommand builds `friendo domain` — connecting your own domain to a
// site you own. This is a tenant command, not an operator one: it runs against
// the network with your own sign-in, the same as `friendo deploy`.
func newDomainCommand() *cobra.Command {
	var netURL string
	var site string

	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Use your own domain for a site (instead of the network address)",
		Long: `Point a domain you own at one of your sites.

  friendo domain add example.com      connect it, and print the DNS records to add
  friendo domain verify example.com   check the records are in place and go live
  friendo domain list                 see your domains and whether they're live
  friendo domain remove example.com   disconnect it

Adding a domain doesn't switch anything over on its own — your site keeps serving
at its network address the whole time, and the new domain only starts working
once you've added the DNS records and verified it.`,
	}
	cmd.PersistentFlags().StringVar(&netURL, "network", "", "Network URL (defaults to friendo.world)")

	add := &cobra.Command{
		Use:   "add <domain>",
		Short: "Connect a domain you own to your site",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			exitOnErr(deploy.RunDomainAdd(args[0], site, netURL))
		},
	}
	add.Flags().StringVar(&site, "site", "", "Which site (defaults to the site in this folder)")

	verify := &cobra.Command{
		Use:   "verify <domain>",
		Short: "Check the DNS records are in place and put the domain live",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			exitOnErr(deploy.RunDomainVerify(args[0], netURL))
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List your custom domains and whether each is live",
		Run: func(cmd *cobra.Command, args []string) {
			exitOnErr(deploy.RunDomainList(netURL))
		},
	}

	remove := &cobra.Command{
		Use:   "remove <domain>",
		Short: "Disconnect a custom domain (the site keeps its network address)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			exitOnErr(deploy.RunDomainRemove(args[0], netURL))
		},
	}

	cmd.AddCommand(add, verify, list, remove)
	return cmd
}
