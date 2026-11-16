package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
)

// connectFunc builds cluster clients for a kubeconfig context and returns the
// cluster's name. Tests replace it with a fake.
type connectFunc func(kubeContext string) (collect.Clients, string, error)

// buildInfo holds the values set at build time.
type buildInfo struct {
	Version string
	// CTAURL is the "Book a call" link in the HTML report. Empty hides the
	// button.
	CTAURL string
}

func newRootCmd(build buildInfo, connect connectFunc) *cobra.Command {
	root := &cobra.Command{
		Use:   "shipready",
		Short: "Find production-readiness problems in your Kubernetes cluster",
		Long: "ShipReady scans a Kubernetes cluster for reliability, security, cost and recovery problems.\n" +
			"It is read-only and runs entirely on your machine: nothing is sent anywhere.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd(build, connect), newChecksCmd(), newVersionCmd(build.Version))
	return root
}

func newChecksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "checks",
		Short: "List all checks with ID, category and severity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "ID\tCATEGORY\tSEVERITY\tCHECK"); err != nil {
				return err
			}
			for _, c := range checks.Default().All() {
				m := c.Meta()
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ID(), m.Category, m.Severity, m.Title); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ShipReady version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "shipready", version)
			return err
		},
	}
}
