package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/henryholtgeerts/friendo/cli/internal/deploy"
	"github.com/henryholtgeerts/friendo/runtime/go/export"
	"github.com/henryholtgeerts/friendo/runtime/go/scaffold"
	"github.com/henryholtgeerts/friendo/runtime/go/server"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "friendo",
		Short: "Your site is a folder. Build it locally. Publish it anywhere.",
	}

	// --- init ---
	var initCmd = &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new Friendo site",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := "my-site"
			if len(args) > 0 {
				name = args[0]
			}

			if err := scaffold.Init(name); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("\nCreated %s/\n\n", name)
			fmt.Println("Get started:")
			fmt.Printf("  cd %s\n", name)
			fmt.Println("  friendo serve")
			fmt.Println()
			fmt.Println("Then open http://localhost:3000 in your browser.")
			fmt.Println("Visit http://localhost:3000/_/ to manage your content.")
			fmt.Println()
		},
	}

	// --- serve ---
	var port int
	var openAdmin bool
	var serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Start the local dev server",
		Run: func(cmd *cobra.Command, args []string) {
			if err := server.Start(port, openAdmin); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	serveCmd.Flags().IntVarP(&port, "port", "p", 3000, "Port to run the server on")
	serveCmd.Flags().BoolVar(&openAdmin, "open-admin", false, "Skip admin UI authentication")

	// --- export ---
	var exportMode string
	var exportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export your site as static HTML or a self-contained bundle",
		Run: func(cmd *cobra.Command, args []string) {
			if err := export.Run(exportMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	exportCmd.Flags().StringVar(&exportMode, "mode", "static", "Export mode: static or bundle")

	// --- deploy ---
	var deployAPIURL string
	var deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Provision and deploy your site to a hosting target",
		Long: `Interactive setup that provisions a site on a hosting target,
saves the target URL to friendo.toml, then pushes everything.

Targets:
  1. friendo.world (managed hosting)
  2. Cloudflare Workers (your own account)
  3. VPS / self-hosted`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := deploy.RunDeploy(deployAPIURL); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	deployCmd.Flags().StringVar(&deployAPIURL, "api-url", "", "Override the platform base URL (default https://friendo.world)")

	// --- redeploy ---
	var redeployAPIURL string
	var redeployCmd = &cobra.Command{
		Use:   "redeploy",
		Short: "Re-push the current runtime to your deployed site",
		Long:  "Redeploys the managed-hosting runtime to your site's Worker. Your data is untouched.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := deploy.RunRedeploy(deploy.RedeployOptions{APIURL: redeployAPIURL}); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	redeployCmd.Flags().StringVar(&redeployAPIURL, "api-url", "", "Override the platform base URL (default https://friendo.world)")

	// --- destroy ---
	var destroyAPIURL string
	var destroyYes bool
	var destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "Deprovision your site (deletes its Worker, database, and assets)",
		Long:  "Permanently tears down the deployed site and all its data on friendo.world. This cannot be undone.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := deploy.RunDestroy(deploy.DestroyOptions{APIURL: destroyAPIURL, Yes: destroyYes}); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	destroyCmd.Flags().StringVar(&destroyAPIURL, "api-url", "", "Override the platform base URL (default https://friendo.world)")
	destroyCmd.Flags().BoolVar(&destroyYes, "yes", false, "Skip the confirmation prompt")

	// --- push ---
	var pushData bool
	var pushUsers bool
	var pushDryRun bool
	var pushTarget string
	var pushCmd = &cobra.Command{
		Use:   "push",
		Short: "Push local state to the deployed site",
		Long:  "Push templates and static assets. Use --data to include records, --users to include user accounts.",
		Run: func(cmd *cobra.Command, args []string) {
			opts := deploy.PushOptions{
				DryRun: pushDryRun,
				Data:   pushData,
				Users:  pushUsers,
				Target: pushTarget,
			}
			if err := deploy.RunPush(opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Print what would be pushed without doing it")
	pushCmd.Flags().BoolVar(&pushData, "data", false, "Also push records (posts, etc.)")
	pushCmd.Flags().BoolVar(&pushUsers, "users", false, "Also push user accounts")
	pushCmd.Flags().StringVar(&pushTarget, "target", "", "Override deploy target URL")

	// --- pull ---
	var pullData bool
	var pullUsers bool
	var pullTarget string
	var pullCmd = &cobra.Command{
		Use:   "pull",
		Short: "Pull remote state into local",
		Long:  "Use --data to pull records, --users to pull user accounts.",
		Run: func(cmd *cobra.Command, args []string) {
			opts := deploy.PullOptions{
				Data:   pullData,
				Users:  pullUsers,
				Target: pullTarget,
			}
			if err := deploy.RunPull(opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	pullCmd.Flags().BoolVar(&pullData, "data", false, "Pull records from the deployed site")
	pullCmd.Flags().BoolVar(&pullUsers, "users", false, "Pull user accounts from the deployed site")
	pullCmd.Flags().StringVar(&pullTarget, "target", "", "Override deploy target URL")

	rootCmd.AddCommand(initCmd, serveCmd, exportCmd, deployCmd, redeployCmd, destroyCmd, pushCmd, pullCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
