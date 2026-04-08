package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/henryholtgeerts/friendo/binary/internal/deploy"
	"github.com/henryholtgeerts/friendo/binary/internal/export"
	"github.com/henryholtgeerts/friendo/binary/internal/scaffold"
	"github.com/henryholtgeerts/friendo/binary/internal/server"
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
	var dryRun bool
	var apiURL string
	var deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy your site to Friendo.world",
		Run: func(cmd *cobra.Command, args []string) {
			opts := deploy.Options{
				DryRun:  dryRun,
				BaseURL: apiURL,
			}
			if err := deploy.Run(opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	deployCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deployed without doing it")
	deployCmd.Flags().StringVar(&apiURL, "api-url", "", "Override API base URL (for local dev)")

	rootCmd.AddCommand(initCmd, serveCmd, exportCmd, deployCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
