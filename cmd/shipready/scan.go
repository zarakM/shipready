package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/muhammadzarakbinkaleem/shipready/internal/checks"
	"github.com/muhammadzarakbinkaleem/shipready/internal/collect"
	"github.com/muhammadzarakbinkaleem/shipready/internal/report"
)

type scanOptions struct {
	kubeContext string
	namespaces  []string
	output      string
	formats     []string
	skip        []string
}

func newScanCmd(version string, connect connectFunc) *cobra.Command {
	var opts scanOptions
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan the cluster and write a report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd, version, connect, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.kubeContext, "context", "", "kubeconfig context (default: current)")
	f.StringSliceVar(&opts.namespaces, "namespace", nil, "limit to namespaces (default: all non-system)")
	f.StringVar(&opts.output, "output", "./shipready-report.json", "report path")
	f.StringSliceVar(&opts.formats, "format", []string{"json"}, "report format: json (html arrives in a later release)")
	f.StringSliceVar(&opts.skip, "skip", nil, "check IDs or categories to skip")
	return cmd
}

func validateFormats(formats []string) error {
	if len(formats) == 0 {
		return errors.New("--format needs at least one value")
	}
	for _, f := range formats {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "json":
		case "html":
			return errors.New("the HTML report is not available yet; use --format json")
		default:
			return fmt.Errorf("unknown format %q; supported: json", f)
		}
	}
	return nil
}

// dedupe trims values and drops blanks and repeats, keeping order.
func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func runScan(cmd *cobra.Command, version string, connect connectFunc, opts scanOptions) error {
	registry := checks.Default()
	if err := validateFormats(opts.formats); err != nil {
		return err
	}
	if err := registry.ValidateSkip(opts.skip); err != nil {
		return err
	}
	namespaces := dedupe(opts.namespaces)

	client, clusterName, err := connect(opts.kubeContext)
	if err != nil {
		return err
	}
	snap, err := collect.Collect(cmd.Context(), client, collect.Options{Namespaces: namespaces})
	if err != nil {
		return err
	}
	snap.ClusterName = clusterName

	rep := report.Build(report.Input{
		ToolVersion: version,
		ScanTime:    time.Now(),
		Namespaces:  namespaces,
		Snapshot:    snap,
		Outcomes:    registry.Run(snap, opts.skip),
	})
	if err := writeJSONFile(opts.output, rep); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return printSummary(cmd.OutOrStdout(), rep, opts.output)
}

func writeJSONFile(path string, rep report.Report) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return report.WriteJSON(f, rep)
}

// printSummary is a plain status listing. The scored summary with top
// findings replaces it in milestone 2.
func printSummary(out io.Writer, rep report.Report, path string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Cluster: %s", rep.Cluster)
	if rep.KubernetesVersion != "" {
		fmt.Fprintf(&b, " (Kubernetes %s)", rep.KubernetesVersion)
	}
	fmt.Fprintf(&b, "\nChecks:  %d failed, %d passed, %d skipped\n\n", rep.Summary.Failed, rep.Summary.Passed, rep.Summary.Skipped)

	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range rep.Checks {
		note := ""
		switch c.Status {
		case checks.Fail:
			note = fmt.Sprintf("(%d affected)", len(c.Resources))
		case checks.Skipped:
			note = "(" + c.SkipReason + ")"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s %s\n", strings.ToUpper(string(c.Status)), c.ID, c.Severity, c.Title, note); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(&b, "\nReport written to %s\n", path)

	_, err := io.WriteString(out, b.String())
	return err
}
