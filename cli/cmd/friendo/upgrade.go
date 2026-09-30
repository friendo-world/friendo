package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/friendo-world/friendo/cli/internal/upgrade"
)

// newUpgradeCommand builds `friendo upgrade` — swap this binary for a newer
// release, the same one the installer would fetch.
func newUpgradeCommand() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "upgrade [version]",
		Short: "Replace this friendo with the latest release (or the version you name)",
		Long: `Downloads a friendo release from GitHub, checks it against the release's
checksums, and replaces this binary with it.

  friendo upgrade           # the latest release
  friendo upgrade v0.6.0    # a specific release
  friendo upgrade --check   # just say whether a newer one exists

Run inside a site folder, it first copies data/friendo.db to
data/friendo-before-<version>.db. Restart ` + "`friendo serve`" + ` afterwards; the
database updates itself when it starts.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}
			siteDir, _ := os.Getwd()
			exitOnErr(upgrade.Run(upgrade.Options{
				Current: resolveVersion(),
				// Release binaries get their version from ldflags; `go install`
				// builds only have the module version resolveVersion falls back to.
				GoInstall: version == "dev" && resolveVersion() != "dev",
				Target:    target,
				CheckOnly: check,
				SiteDir:   siteDir,
			}))
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only check whether a newer release exists")
	return cmd
}
