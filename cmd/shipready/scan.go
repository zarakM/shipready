package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zarakM/shipready/internal/checks"
	"github.com/zarakM/shipready/internal/collect"
	"github.com/zarakM/shipready/internal/report"
)

type scanOptions struct {
	kubeContext string
	namespaces  []string
	output      string
	formats     []string
	skip        []string
	failOn      string
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
	f.StringVar(&opts.failOn, "fail-on", "", "exit with code 2 if a check of this severity or above fails: critical or high")
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

// parseFailOn returns the severity threshold, or "" when the flag is unset.
func parseFailOn(value string) (checks.Severity, error) {
	switch v := checks.Severity(strings.ToLower(strings.TrimSpace(value))); v {
	case "", checks.Critical, checks.High:
		return v, nil
	}
	return "", fmt.Errorf("unknown --fail-on value %q; use critical or high", value)
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
	failOn, err := parseFailOn(opts.failOn)
	if err != nil {
		return err
	}
	namespaces := dedupe(opts.namespaces)

	clients, clusterName, err := connect(opts.kubeContext)
	if err != nil {
		return err
	}
	snap, err := collect.Collect(cmd.Context(), clients, collect.Options{Namespaces: namespaces})
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
	if err := report.WriteSummary(cmd.OutOrStdout(), rep, opts.output); err != nil {
		return err
	}
	if failOn != "" && rep.Failed(failOn) {
		return &failOnError{severity: string(failOn)}
	}
	return nil
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
