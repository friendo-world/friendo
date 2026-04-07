package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/henryholtgeerts/friendo/binary/internal/server"
)

func main() {
	var port int

	var rootCmd = &cobra.Command{
		Use:   "friendo",
		Short: "Friendo CLI",
	}

	var serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Start the Friendo server",
		Run: func(cmd *cobra.Command, args []string) {
			if err := server.Start(port); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	serveCmd.Flags().IntVarP(&port, "port", "p", 3000, "Port to run the server on")
	rootCmd.AddCommand(serveCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
